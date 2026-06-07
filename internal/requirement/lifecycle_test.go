package requirement

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreateRequirementCopiesTemplates(t *testing.T) {
	root := t.TempDir()
	writeHarnessTemplates(t, root)

	err := Create(root, "SPARK-3", NewOptions{Title: "Test Requirement", Owner: "Alice"}, fixedTime())

	if err != nil {
		t.Fatalf("expected create to pass, got %v", err)
	}
	assertFileContains(t, root, "requirements/SPARK-3/README.md", `current_stage: "1"`)
	assertFileContains(t, root, "requirements/SPARK-3/requirement.md", `requirement_id: "SPARK-3"`)
	assertFileContains(t, root, "requirements/SPARK-3/tasks.json", `"requirement_id": "SPARK-3"`)
}

func TestInspectReportsMissingGateProblems(t *testing.T) {
	root := t.TempDir()
	writeHarnessTemplates(t, root)
	if err := Create(root, "SPARK-3", NewOptions{}, fixedTime()); err != nil {
		t.Fatal(err)
	}

	status, err := Inspect(root, "SPARK-3", fixedTime())

	if err != nil {
		t.Fatalf("expected inspect to pass, got %v", err)
	}
	if status.CurrentStage != "1" {
		t.Fatalf("expected current stage 1, got %q", status.CurrentStage)
	}
	if len(status.Problems) == 0 {
		t.Fatal("expected missing gate problems")
	}
	if !strings.Contains(status.NextAction, "gate-check") {
		t.Fatalf("expected gate-check next action, got %q", status.NextAction)
	}
}

func TestInspectInfersStageWhenReadmeHasNoStage(t *testing.T) {
	root := t.TempDir()
	writeHarnessTemplates(t, root)
	if err := Create(root, "SPARK-3", NewOptions{}, fixedTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "requirements", "SPARK-3", "README.md"), []byte("# SPARK-3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "requirements/SPARK-3/gates/design-review.gate.json", strings.Replace(validGateJSON(), "T12345", "SPARK-3", -1))
	writeFile(t, root, "requirements/SPARK-3/requirement.md", "123456")

	status, err := Inspect(root, "SPARK-3", fixedTime())

	if err != nil {
		t.Fatalf("expected inspect to pass, got %v", err)
	}
	if status.CurrentStage != "4.1" {
		t.Fatalf("expected inferred stage 4.1, got %q", status.CurrentStage)
	}
}

func TestRunGateCheckWritesBlockedMachineReport(t *testing.T) {
	root := t.TempDir()
	writeHarnessTemplates(t, root)
	if err := Create(root, "SPARK-3", NewOptions{}, fixedTime()); err != nil {
		t.Fatal(err)
	}

	result, err := RunGateCheck(root, "SPARK-3", GateCheckOptions{GateID: GateRequirementReview, Now: fixedTime()})

	if err != nil {
		t.Fatalf("expected gate check to pass, got %v", err)
	}
	if result.Report.Result != "BLOCKED" {
		t.Fatalf("expected BLOCKED pending human approval, got %s", result.Report.Result)
	}
	assertFileContains(t, root, "requirements/SPARK-3/gates/requirement-review.gate.json", `"gate_id": "requirement-review"`)
	assertFileContains(t, root, "requirements/SPARK-3/gates/requirement-review.md", "Generated from requirement-review.gate.json")
}

func TestRunGateCheckPassesWithMarkdownApproval(t *testing.T) {
	root := t.TempDir()
	writeHarnessTemplates(t, root)
	if err := Create(root, "SPARK-3", NewOptions{}, fixedTime()); err != nil {
		t.Fatal(err)
	}
	approvedRequirement := `---
requirement_id: "SPARK-3"
owner: "Harness Team"
status: "Draft"
created_at: "2026-06-07"
requirement_review_status: "approved"
approved_by: "forest"
approved_at: "2026-06-07T20:30:00+08:00"
decision: "需求定义通过，可以进入设计阶段。"
---

# SPARK-3

## Background
## Goals
## Non-Goals
## Acceptance Criteria
`
	writeFile(t, root, "requirements/SPARK-3/requirement.md", approvedRequirement)

	result, err := RunGateCheck(root, "SPARK-3", GateCheckOptions{GateID: GateRequirementReview, Now: fixedTime()})

	if err != nil {
		t.Fatalf("expected gate check to pass, got %v", err)
	}
	if result.Report.Result != "PASS" {
		t.Fatalf("expected PASS with markdown approval, got %s", result.Report.Result)
	}
	if result.Report.BlocksNextStage {
		t.Fatal("expected approved gate not to block next stage")
	}
}

func TestAdvanceBlocksOnBlockedGate(t *testing.T) {
	root := t.TempDir()
	writeHarnessTemplates(t, root)
	if err := Create(root, "SPARK-3", NewOptions{}, fixedTime()); err != nil {
		t.Fatal(err)
	}
	if err := writeCurrentStage(filepath.Join(root, "requirements", "SPARK-3", "README.md"), "2"); err != nil {
		t.Fatal(err)
	}
	if _, err := RunGateCheck(root, "SPARK-3", GateCheckOptions{GateID: GateRequirementReview, Now: fixedTime()}); err != nil {
		t.Fatal(err)
	}

	_, err := Advance(root, "SPARK-3", fixedTime(), "")

	lifecycleErr := assertLifecycleError(t, err)
	if lifecycleErr.Code != 1 {
		t.Fatalf("expected blocked code 1, got %d", lifecycleErr.Code)
	}
}

func writeHarnessTemplates(t *testing.T, root string) {
	t.Helper()
	templateRoot := filepath.Join(root, "context", "harness-framework", "templates")
	if err := os.MkdirAll(templateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"requirement.md": `---
requirement_id: ""
owner: ""
status: "Draft"
created_at: ""
related_branch: ""
---

# {Requirement Title}

## Background
## Goals
## Non-Goals
## Acceptance Criteria
`,
		"impact-analysis.md": `---
requirement_id: ""
analyst: ""
status: "Draft"
updated_at: ""
---

# Impact Analysis

## Affected Services
## API / Contract Impact
## Rollout And Rollback
`,
		"design.md": `---
requirement_id: ""
owner: ""
status: "Draft"
updated_at: ""
---

# Design

## Requirement Traceability
## Affected Services
## API / Contract Design
## Rollout And Rollback
`,
		"tasks.json": `{
  "requirement_id": "",
  "status": "draft",
  "tasks": [
    {
      "id": "T1",
      "title": "Task",
      "scope": "Scope",
      "trace": {
        "requirement_items": ["R1"],
        "design_decisions": ["D1"]
      },
      "affected_services": [],
      "acceptance": ["AC1"],
      "status": "todo"
    }
  ]
}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(templateRoot, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func assertFileContains(t *testing.T, root string, relativePath string, want string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), want) {
		t.Fatalf("expected %s to contain %q, got %q", relativePath, want, string(content))
	}
}

func assertLifecycleError(t *testing.T, err error) *LifecycleError {
	t.Helper()
	if err == nil {
		t.Fatal("expected lifecycle error")
	}
	lifecycleErr, ok := err.(*LifecycleError)
	if !ok {
		t.Fatalf("expected LifecycleError, got %T", err)
	}
	return lifecycleErr
}

func fixedTime() time.Time {
	return time.Date(2026, 6, 7, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
}
