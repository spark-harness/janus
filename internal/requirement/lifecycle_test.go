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

func TestInspectDoesNotBlockOnFutureGates(t *testing.T) {
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
	if len(status.Problems) != 0 {
		t.Fatalf("expected no problems for future gates, got %#v", status.Problems)
	}
	if !strings.Contains(status.NextAction, "requirement next") {
		t.Fatalf("expected next-stage action, got %q", status.NextAction)
	}
}

func TestInspectReportsInvalidReadmeStatus(t *testing.T) {
	root := t.TempDir()
	writeHarnessTemplates(t, root)
	if err := Create(root, "SPARK-3", NewOptions{}, fixedTime()); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "requirements/SPARK-3/README.md", `---
requirement_id: "SPARK-3"
owner: "Harness Team"
current_stage: "1"
status: "reviewed"
created_at: "2026-06-07"
---

# SPARK-3
`)

	status, err := Inspect(root, "SPARK-3", fixedTime())

	if err != nil {
		t.Fatalf("expected inspect to pass, got %v", err)
	}
	if !containsProblem(status.Problems, "requirements/SPARK-3/README.md status 必须是") {
		t.Fatalf("expected invalid README status problem, got %#v", status.Problems)
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

func TestInspectReportsCurrentStageGateProblems(t *testing.T) {
	root := t.TempDir()
	writeHarnessTemplates(t, root)
	if err := Create(root, "SPARK-3", NewOptions{}, fixedTime()); err != nil {
		t.Fatal(err)
	}
	if err := writeCurrentStage(filepath.Join(root, "requirements", "SPARK-3", "README.md"), "2"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "requirements/SPARK-3/gates/requirement-review.gate.json", strings.Replace(validGateJSON(), `"result": "PASS"`, `"result": "BLOCKED"`, 1))

	status, err := Inspect(root, "SPARK-3", fixedTime())

	if err != nil {
		t.Fatalf("expected inspect to pass, got %v", err)
	}
	if !containsProblem(status.Problems, "invalid gate") {
		t.Fatalf("expected current gate problem, got %#v", status.Problems)
	}
	if !strings.Contains(status.NextAction, "BLOCKED") {
		t.Fatalf("expected blocked next action, got %q", status.NextAction)
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
	if _, err := os.Stat(filepath.Join(root, "requirements", "SPARK-3", "gates", "requirement-review.md")); !os.IsNotExist(err) {
		t.Fatalf("expected no rendered Markdown, stat err %v", err)
	}
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
status: "approved"
created_at: "2026-06-07"
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

func TestRunDevEntryPassesWithTasksApproval(t *testing.T) {
	root := t.TempDir()
	writeHarnessTemplates(t, root)
	if err := Create(root, "SPARK-3", NewOptions{}, fixedTime()); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "requirements/SPARK-3/tasks.json", `{
  "requirement_id": "SPARK-3",
  "status": "approved",
  "approved_by": "forest",
  "approved_at": "2026-06-07T20:30:00+08:00",
  "decision": "任务拆分通过，可以进入开发阶段。",
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
      "state": "todo"
    }
  ]
}`)

	result, err := RunGateCheck(root, "SPARK-3", GateCheckOptions{GateID: GateDevEntry, Now: fixedTime()})

	if err != nil {
		t.Fatalf("expected gate check to pass, got %v", err)
	}
	if result.Report.Result != "PASS" {
		t.Fatalf("expected PASS with tasks approval, got %s", result.Report.Result)
	}
	if result.Report.Decision != "任务拆分通过，可以进入开发阶段。" {
		t.Fatalf("expected tasks decision, got %q", result.Report.Decision)
	}
}

func TestRunDevEntryBlocksTaskWithoutState(t *testing.T) {
	root := t.TempDir()
	writeHarnessTemplates(t, root)
	if err := Create(root, "SPARK-3", NewOptions{}, fixedTime()); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "requirements/SPARK-3/tasks.json", `{
  "requirement_id": "SPARK-3",
  "status": "approved",
  "approved_by": "forest",
  "approved_at": "2026-06-07T20:30:00+08:00",
  "decision": "任务拆分通过，可以进入开发阶段。",
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
      "acceptance": ["AC1"]
    }
  ]
}`)

	result, err := RunGateCheck(root, "SPARK-3", GateCheckOptions{GateID: GateDevEntry, Now: fixedTime()})

	if err != nil {
		t.Fatalf("expected gate check to write blocked report, got %v", err)
	}
	if result.Report.Result != "BLOCKED" {
		t.Fatalf("expected BLOCKED with missing task state, got %s", result.Report.Result)
	}
	if !strings.Contains(result.Report.Checklist[len(result.Report.Checklist)-1].Evidence, "T1 缺少 state") {
		t.Fatalf("expected missing state evidence, got %#v", result.Report.Checklist)
	}
}

func TestRunServiceRepoCheckSkipsIDLRepoWhenImpactExplicitlyNo(t *testing.T) {
	root := t.TempDir()
	writeHarnessTemplates(t, root)
	if err := os.MkdirAll(filepath.Join(root, ".service-matrix"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, ".service-matrix/dependencies.yaml", `workspace: ".."
business_repo: "business-repo"
idl_repo: "idl-repo"
services:
  user-api:
    repo_path: "{business-repo}/services/backend/user-api"
    idl_required: false
`)
	if err := os.MkdirAll(filepath.Join(root, "..", "business-repo", "services", "backend", "user-api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Create(root, "SPARK-3", NewOptions{}, fixedTime()); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "requirements/SPARK-3/impact-analysis.md", `---
requirement_id: "SPARK-3"
status: "approved"
approved_by: "forest"
approved_at: "2026-06-07T20:30:00+08:00"
decision: "服务仓库检查通过。"
---

# Impact Analysis

## Affected Services

`+"`user-api`"+`

## API / Contract Impact

- Does this change involve protobuf IDL or external contracts: no
- Proto files: none

## Rollout And Rollback
`)
	writeFile(t, root, "requirements/SPARK-3/tasks.json", `{
  "requirement_id": "SPARK-3",
  "status": "approved",
  "approved_by": "forest",
  "approved_at": "2026-06-07T20:30:00+08:00",
  "decision": "任务拆分通过。",
  "tasks": [
    {
      "id": "T1",
      "title": "Task",
      "scope": "Scope",
      "trace": {
        "requirement_items": ["R1"],
        "design_decisions": ["D1"]
      },
      "affected_services": ["user-api"],
      "acceptance": ["AC1"],
      "state": "todo"
    }
  ]
}`)

	result, err := RunGateCheck(root, "SPARK-3", GateCheckOptions{GateID: GateServiceRepoCheck, Now: fixedTime()})

	if err != nil {
		t.Fatalf("expected gate check to pass, got %v", err)
	}
	if result.Report.IDLImpact == nil || result.Report.IDLImpact.Impact != "no" {
		t.Fatalf("expected no idl impact, got %#v", result.Report.IDLImpact)
	}
	for _, repo := range result.Report.Repos {
		if repo.Name == "idl-repo" {
			t.Fatalf("did not expect idl-repo in repos: %#v", result.Report.Repos)
		}
	}
}

func TestRunServiceRepoCheckAllowsGovernanceTaskWithoutAffectedServices(t *testing.T) {
	root := t.TempDir()
	writeHarnessTemplates(t, root)
	writeServiceMatrixWithBusinessRepo(t, root)
	if err := Create(root, "SPARK-3", NewOptions{}, fixedTime()); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "requirements/SPARK-3/impact-analysis.md", `---
requirement_id: "SPARK-3"
status: "approved"
approved_by: "forest"
approved_at: "2026-06-07T20:30:00+08:00"
decision: "服务仓库检查通过。"
idl_impact: "no"
idl_impact_reason: "治理流程变更，不涉及 protobuf IDL。"
---

# Impact Analysis

## Affected Services

本需求只影响 `+"`harness-repo`"+` 和 `+"`janus`"+`，不涉及业务服务。

## API / Contract Impact

- Does this change involve protobuf IDL or external contracts: no

## Rollout And Rollback
`)
	writeFile(t, root, "requirements/SPARK-3/tasks.json", `{
  "requirement_id": "SPARK-3",
  "status": "approved",
  "approved_by": "forest",
  "approved_at": "2026-06-07T20:30:00+08:00",
  "decision": "任务拆分通过。",
  "tasks": [
    {
      "id": "T1",
      "title": "Governance",
      "scope": "Update harness governance and Janus CLI.",
      "trace": {
        "requirement_items": ["R1"],
        "design_decisions": ["D1"]
      },
      "affected_services": [],
      "acceptance": ["AC1"],
      "state": "todo"
    }
  ]
}`)

	result, err := RunGateCheck(root, "SPARK-3", GateCheckOptions{GateID: GateServiceRepoCheck, Now: fixedTime()})

	if err != nil {
		t.Fatalf("expected gate check to pass, got %v", err)
	}
	if result.Report.Result != "PASS" {
		t.Fatalf("expected PASS without affected services, got %s: %#v", result.Report.Result, result.Report.BlockingIssues)
	}
}

func TestRunServiceRepoCheckPrefersStructuredIDLImpact(t *testing.T) {
	root := t.TempDir()
	writeHarnessTemplates(t, root)
	writeServiceMatrixWithBusinessRepo(t, root)
	if err := Create(root, "SPARK-3", NewOptions{}, fixedTime()); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "requirements/SPARK-3/impact-analysis.md", `---
requirement_id: "SPARK-3"
status: "approved"
approved_by: "forest"
approved_at: "2026-06-07T20:30:00+08:00"
decision: "服务仓库检查通过。"
idl_impact: "no"
idl_impact_reason: "只复用现有 protobuf IDL，不修改契约。"
---

# Impact Analysis

## Affected Services

`+"`user-api`"+`

## API / Contract Impact

- Does this change involve protobuf IDL or external contracts:
- Proto files: idl-repo/proto/user.proto

## Rollout And Rollback
`)
	writeApprovedTasks(t, root, "todo")

	result, err := RunGateCheck(root, "SPARK-3", GateCheckOptions{GateID: GateServiceRepoCheck, Now: fixedTime()})

	if err != nil {
		t.Fatalf("expected gate check to pass, got %v", err)
	}
	if result.Report.IDLImpact == nil || result.Report.IDLImpact.Impact != "no" {
		t.Fatalf("expected structured no idl impact, got %#v", result.Report.IDLImpact)
	}
	if result.Report.IDLImpact.NAReason != "只复用现有 protobuf IDL，不修改契约。" {
		t.Fatalf("expected structured reason, got %q", result.Report.IDLImpact.NAReason)
	}
	for _, repo := range result.Report.Repos {
		if repo.Name == "idl-repo" {
			t.Fatalf("did not expect idl-repo in repos: %#v", result.Report.Repos)
		}
	}
}

func TestRunServiceRepoCheckRequiresStructuredIDLReason(t *testing.T) {
	root := t.TempDir()
	writeHarnessTemplates(t, root)
	writeServiceMatrixWithBusinessRepo(t, root)
	if err := Create(root, "SPARK-3", NewOptions{}, fixedTime()); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "requirements/SPARK-3/impact-analysis.md", `---
requirement_id: "SPARK-3"
status: "approved"
approved_by: "forest"
approved_at: "2026-06-07T20:30:00+08:00"
decision: "服务仓库检查通过。"
idl_impact: "no"
---

# Impact Analysis

## Affected Services

`+"`user-api`"+`

## API / Contract Impact

- Does this change involve protobuf IDL or external contracts:

## Rollout And Rollback
`)
	writeApprovedTasks(t, root, "todo")

	_, err := RunGateCheck(root, "SPARK-3", GateCheckOptions{GateID: GateServiceRepoCheck, Now: fixedTime()})

	if err == nil || !strings.Contains(err.Error(), "idl_impact.na_reason is required") {
		t.Fatalf("expected missing idl reason validation error, got %v", err)
	}
}

func TestRunDevEntryBlocksInvalidLifecycleStatus(t *testing.T) {
	root := t.TempDir()
	writeHarnessTemplates(t, root)
	if err := Create(root, "SPARK-3", NewOptions{}, fixedTime()); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "requirements/SPARK-3/tasks.json", `{
  "requirement_id": "SPARK-3",
  "status": "reviewed",
  "approved_by": "forest",
  "approved_at": "2026-06-07T20:30:00+08:00",
  "decision": "任务拆分通过。",
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
      "state": "todo"
    }
  ]
}`)

	result, err := RunGateCheck(root, "SPARK-3", GateCheckOptions{GateID: GateDevEntry, Now: fixedTime()})

	if err != nil {
		t.Fatalf("expected gate check to write blocked report, got %v", err)
	}
	if result.Report.Result != "BLOCKED" {
		t.Fatalf("expected BLOCKED with invalid lifecycle status, got %s", result.Report.Result)
	}
	if !strings.Contains(result.Report.Checklist[len(result.Report.Checklist)-1].Evidence, "requirements/SPARK-3/tasks.json status 必须是") {
		t.Fatalf("expected invalid status evidence, got %#v", result.Report.Checklist)
	}
}

func TestRunRequirementReviewBlocksInvalidMarkdownStatus(t *testing.T) {
	root := t.TempDir()
	writeHarnessTemplates(t, root)
	if err := Create(root, "SPARK-3", NewOptions{}, fixedTime()); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "requirements/SPARK-3/requirement.md", `---
requirement_id: "SPARK-3"
owner: "Harness Team"
status: "Reviewed"
created_at: "2026-06-07"
approved_by: "forest"
approved_at: "2026-06-07T20:30:00+08:00"
decision: "需求定义通过，可以进入设计阶段。"
---

# SPARK-3

## Background
## Goals
## Non-Goals
## Acceptance Criteria
`)

	result, err := RunGateCheck(root, "SPARK-3", GateCheckOptions{GateID: GateRequirementReview, Now: fixedTime()})

	if err != nil {
		t.Fatalf("expected gate check to write blocked report, got %v", err)
	}
	if result.Report.Result != "BLOCKED" {
		t.Fatalf("expected BLOCKED with invalid markdown status, got %s", result.Report.Result)
	}
	if !strings.Contains(result.Report.Checklist[len(result.Report.Checklist)-1].Evidence, "requirements/SPARK-3/requirement.md status 必须是") {
		t.Fatalf("expected invalid status evidence, got %#v", result.Report.Checklist)
	}
}

func TestRunDevEntryBlocksInvalidTaskState(t *testing.T) {
	root := t.TempDir()
	writeHarnessTemplates(t, root)
	if err := Create(root, "SPARK-3", NewOptions{}, fixedTime()); err != nil {
		t.Fatal(err)
	}
	writeApprovedTasks(t, root, "reviewed")

	result, err := RunGateCheck(root, "SPARK-3", GateCheckOptions{GateID: GateDevEntry, Now: fixedTime()})

	if err != nil {
		t.Fatalf("expected gate check to write blocked report, got %v", err)
	}
	if result.Report.Result != "BLOCKED" {
		t.Fatalf("expected BLOCKED with invalid task state, got %s", result.Report.Result)
	}
	if !strings.Contains(result.Report.Checklist[len(result.Report.Checklist)-1].Evidence, "T1 state 必须是") {
		t.Fatalf("expected invalid state evidence, got %#v", result.Report.Checklist)
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
status: "draft"
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
status: "draft"
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
status: "draft"
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
      "state": "todo"
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

func writeServiceMatrixWithBusinessRepo(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".service-matrix"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, ".service-matrix/dependencies.yaml", `workspace: ".."
business_repo: "business-repo"
idl_repo: "idl-repo"
services:
  user-api:
    repo_path: "{business-repo}/services/backend/user-api"
    idl_required: false
`)
	if err := os.MkdirAll(filepath.Join(root, "..", "business-repo", "services", "backend", "user-api"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeApprovedTasks(t *testing.T, root string, state string) {
	t.Helper()
	writeFile(t, root, "requirements/SPARK-3/tasks.json", `{
  "requirement_id": "SPARK-3",
  "status": "approved",
  "approved_by": "forest",
  "approved_at": "2026-06-07T20:30:00+08:00",
  "decision": "任务拆分通过。",
  "tasks": [
    {
      "id": "T1",
      "title": "Task",
      "scope": "Scope",
      "trace": {
        "requirement_items": ["R1"],
        "design_decisions": ["D1"]
      },
      "affected_services": ["user-api"],
      "acceptance": ["AC1"],
      "state": "`+state+`"
    }
  ]
}`)
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

func containsProblem(problems []string, want string) bool {
	for _, problem := range problems {
		if strings.Contains(problem, want) {
			return true
		}
	}
	return false
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
