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
)

// classifyArtifact recognizes requirements/<id>/{requirement.md, impact-analysis.md,
// design.md, tasks.json} by structure, independent of the absolute workspace path.
func classifyArtifact(path string) artifactKind {
	base := filepath.Base(path)
	idDir := filepath.Dir(path)
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
	if !in.HasCandidate {
		return GuardDecision{}
	}
	kind := classifyArtifact(in.TargetPath)
	if kind == artifactNone {
		return GuardDecision{}
	}
	// R3: an agent must not be the actor that flips status into "approved".
	if d := guardForgedApproval(in, kind); d.Deny {
		return d
	}
	// R1: requirement artifacts must be written in an isolated worktree, never
	// in the repo's main checkout.
	if d := guardWorktreeIsolation(in); d.Deny {
		return d
	}
	// R2 (stage order) follows.
	return GuardDecision{}
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
