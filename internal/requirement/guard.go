package requirement

import "path/filepath"

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
	if classifyArtifact(in.TargetPath) == artifactNone {
		return GuardDecision{}
	}
	// Rules added incrementally:
	//   R3 (forged approval), R1 (worktree-before-requirement), R2 (stage order).
	return GuardDecision{}
}
