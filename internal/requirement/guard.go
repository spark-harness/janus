package requirement

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	// R1 (worktree-before-requirement) and R2 (stage order) follow.
	return GuardDecision{}
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
