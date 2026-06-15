package requirement

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// artifactKind classifies a target file path against the lifecycle artifacts the
// guard cares about. Anything else is out of scope and always allowed.
type artifactKind int

const (
	artifactNone artifactKind = iota
	artifactRequirement
	artifactImpact
	artifactDesign
	artifactTasks
	artifactGateJSON
)

// IsGuardedArtifact reports whether a path is a lifecycle artifact the guard
// cares about. Adapters use it to pick the relevant file from a multi-file edit.
func IsGuardedArtifact(path string) bool {
	return classifyArtifact(path) != artifactNone
}

// classifyArtifact recognizes requirements/<id>/{requirement.md, impact-analysis.md,
// design.md, tasks.json} and requirements/<id>/gates/<gate>.gate.json by
// structure, independent of the absolute workspace path.
func classifyArtifact(path string) artifactKind {
	base := filepath.Base(path)
	idDir := filepath.Dir(path)
	if filepath.Base(idDir) == "gates" && strings.HasSuffix(base, ".gate.json") {
		if filepath.Base(filepath.Dir(filepath.Dir(idDir))) == "requirements" {
			return artifactGateJSON
		}
		return artifactNone
	}
	if filepath.Base(filepath.Dir(idDir)) != "requirements" {
		return artifactNone
	}
	switch base {
	case "requirement.md":
		return artifactRequirement
	case "impact-analysis.md":
		return artifactImpact
	case "design.md":
		return artifactDesign
	case "tasks.json":
		return artifactTasks
	}
	return artifactNone
}

// GuardInput is the host-neutral view of a pending file edit, produced by the
// CLI's per-host adapters.
type GuardInput struct {
	ToolName         string
	TargetPath       string
	CandidateContent string
	HasCandidate     bool
}

// GuardDecision is the verdict for a pending edit. Deny=false means "allow"
// (the hook lets the tool proceed); the reason is shown when denying.
type GuardDecision struct {
	Deny   bool
	Reason string
}

// GuardEdit decides whether a pending edit to a lifecycle artifact may proceed.
// It fails open for inputs it cannot reason about (no resolvable candidate
// content, or a target outside the lifecycle artifacts). Rules are layered in
// by later commits.
func GuardEdit(in GuardInput) GuardDecision {
	if in.TargetPath == "" {
		return GuardDecision{}
	}
	kind := classifyArtifact(in.TargetPath)
	if kind == artifactNone {
		return GuardDecision{}
	}
	// R3 needs the resulting content; location (R1) and stage (R2) rules only
	// need the target path, so they still apply when an adapter knows the path
	// but cannot reconstruct the candidate (e.g. an unparsable Codex hunk).
	if in.HasCandidate {
		if d := guardForgedApproval(in, kind); d.Deny {
			return d
		}
	}
	// R1: requirement artifacts must be written in an isolated worktree, never
	// in the repo's main checkout.
	if d := guardWorktreeIsolation(in); d.Deny {
		return d
	}
	// R2: lifecycle stage order — an artifact may not be written before its
	// prerequisite artifacts exist and (where required) are approved.
	if d := guardStageOrder(in, kind); d.Deny {
		return d
	}
	return GuardDecision{}
}

// guardStageOrder enforces the within-requirement prerequisite order. The
// .proto stage rule from the design is intentionally not implemented here: a
// .proto edit in idl-repo has no deterministic link back to a requirement, so
// it is left to the merge-readiness gate / CI rather than guessed.
func guardStageOrder(in GuardInput, kind artifactKind) GuardDecision {
	reqDir := filepath.Dir(in.TargetPath)
	if kind == artifactGateJSON {
		reqDir = filepath.Dir(reqDir) // up from gates/ to requirements/<id>
	}
	switch kind {
	case artifactDesign:
		var missing []string
		if !siblingExists(reqDir, "requirement.md") {
			missing = append(missing, "requirement.md")
		}
		if !siblingExists(reqDir, "impact-analysis.md") {
			missing = append(missing, "impact-analysis.md")
		}
		if len(missing) > 0 {
			return denyDecision(fmt.Sprintf("design.md 的前置产物缺失：%s。先完成需求与影响分析再写设计。", strings.Join(missing, ", ")))
		}
	case artifactTasks:
		if !siblingExists(reqDir, "design.md") {
			return denyDecision("tasks.json 的前置产物 design.md 缺失。先完成设计再拆任务。")
		}
		if !siblingApproved(reqDir, "design.md", artifactDesign) {
			return denyDecision("design-review 未批准（design.md status 非 approved）。先经 janus requirement approve 批准设计门禁，再拆任务。")
		}
	case artifactGateJSON:
		if filepath.Base(in.TargetPath) == "requirement-review.gate.json" && !siblingExists(reqDir, "impact-analysis.md") {
			return denyDecision("requirement-review 门禁需要 impact-analysis.md 与 requirement.md 同时存在。先补影响分析，两者齐备后再生成门禁。")
		}
	}
	return GuardDecision{}
}

func siblingExists(reqDir string, name string) bool {
	info, err := os.Stat(filepath.Join(reqDir, name))
	return err == nil && !info.IsDir()
}

func siblingApproved(reqDir string, name string, kind artifactKind) bool {
	content, err := os.ReadFile(filepath.Join(reqDir, name))
	if err != nil {
		return false
	}
	return statusApproved(string(content), kind)
}

func denyDecision(reason string) GuardDecision {
	return GuardDecision{Deny: true, Reason: reason}
}

// guardWorktreeIsolation denies writing a lifecycle artifact into a repo's main
// checkout. A linked worktree has an absolute git-dir that differs from its
// git-common-dir; a normal checkout has them equal. Unknown/non-git locations
// fail open (the guard cannot assert it is a main checkout).
func guardWorktreeIsolation(in GuardInput) GuardDecision {
	dir := nearestExistingDir(filepath.Dir(in.TargetPath))
	if dir == "" {
		return GuardDecision{}
	}
	gitDir, ok := gitRevParse(dir, "--absolute-git-dir")
	if !ok {
		return GuardDecision{}
	}
	commonDir, ok := gitRevParse(dir, "--path-format=absolute", "--git-common-dir")
	if !ok {
		return GuardDecision{}
	}
	if filepath.Clean(gitDir) != filepath.Clean(commonDir) {
		return GuardDecision{}
	}
	return GuardDecision{
		Deny: true,
		Reason: fmt.Sprintf(
			"%s 位于仓库主 checkout，不是隔离 worktree。先运行 spark-worktree-isolation 在 .worktrees/ 下建立隔离 worktree，再写需求文件。",
			artifactHint(in.TargetPath),
		),
	}
}

// nearestExistingDir walks up from dir until it finds an existing directory, so
// the guard can query git even when the target's parent dirs do not exist yet.
func nearestExistingDir(dir string) string {
	for {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func gitRevParse(dir string, args ...string) (string, bool) {
	full := append([]string{"-C", dir, "rev-parse"}, args...)
	out, err := exec.Command("git", full...).Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

func artifactHint(path string) string {
	return filepath.ToSlash(filepath.Join(filepath.Base(filepath.Dir(path)), filepath.Base(path)))
}

// guardForgedApproval denies an edit that transitions the artifact's status
// into "approved" when it was not already approved. It compares the resulting
// candidate state against the on-disk state, so it cannot be bypassed by
// splitting the approval across multiple edits, and it allows body edits to an
// already-approved file (status drift is handled by gate input hashing, not
// here).
func guardForgedApproval(in GuardInput, kind artifactKind) GuardDecision {
	if !statusApproved(in.CandidateContent, kind) {
		return GuardDecision{}
	}
	before := false
	if current, err := os.ReadFile(in.TargetPath); err == nil {
		before = statusApproved(string(current), kind)
	}
	if before {
		return GuardDecision{}
	}
	return GuardDecision{
		Deny: true,
		Reason: fmt.Sprintf(
			"%s 的 status 被改为 approved。批准只能由你本人执行：! janus requirement approve --requirement <id> --gate <gate> --approved-by <you> --decision <text> --yes（agent 不能写批准字段）。",
			filepath.Base(in.TargetPath),
		),
	}
}

func statusApproved(content string, kind artifactKind) bool {
	if kind == artifactTasks {
		var tasks tasksFile
		if json.Unmarshal([]byte(content), &tasks) != nil {
			return false
		}
		return normalizeLifecycleStatus(tasks.Status) == LifecycleStatusApproved
	}
	return normalizeLifecycleStatus(parseFrontMatter(content)["status"]) == LifecycleStatusApproved
}
