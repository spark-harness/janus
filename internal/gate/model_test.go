package gate

import (
	"strings"
	"testing"
)

func TestValidatePassReport(t *testing.T) {
	report := validReport()

	if err := Validate(report); err != nil {
		t.Fatalf("expected valid report, got %v", err)
	}
}

func TestValidateBlockedRequiresBlockingIssue(t *testing.T) {
	report := validReport()
	report.Result = ResultBlocked
	report.BlocksNextStage = true
	report.BlockingIssues = nil

	err := Validate(report)

	if err == nil || !strings.Contains(err.Error(), "BLOCKED must include blocking_issues") {
		t.Fatalf("expected blocking issue error, got %v", err)
	}
}

func TestValidateRejectsInvalidStateCombination(t *testing.T) {
	report := validReport()
	report.Result = ResultPass
	report.BlocksNextStage = true

	err := Validate(report)

	if err == nil || !strings.Contains(err.Error(), "PASS must set blocks_next_stage to false") {
		t.Fatalf("expected PASS blocks_next_stage error, got %v", err)
	}
}

func TestValidateWarnRequiresFollowUp(t *testing.T) {
	report := validReport()
	report.Result = ResultWarn
	report.Warnings = []Warning{{Issue: "coverage gap"}}

	err := Validate(report)

	if err == nil || !strings.Contains(err.Error(), "warnings[0].follow_up_action is required") {
		t.Fatalf("expected warning follow-up error, got %v", err)
	}
}

func TestValidateWaivedRequiresWaiverFields(t *testing.T) {
	report := validReport()
	report.Result = ResultWaived
	report.Waiver = Waiver{Required: true, Reason: "release window"}

	err := Validate(report)

	if err == nil || !strings.Contains(err.Error(), "waiver.approver is required") {
		t.Fatalf("expected waiver field error, got %v", err)
	}
}

func TestReadRejectsUnknownFields(t *testing.T) {
	_, err := Read(strings.NewReader(`{"schema_version":"1.0","extra":true}`))

	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func validReport() *Report {
	return &Report{
		SchemaVersion:   "1.0",
		RequirementID:   "T12345",
		GateID:          "3.3-design-review",
		GateName:        "设计门禁",
		Stage:           "3.3",
		CheckedBy:       "detail-design-quality-reviewer",
		CheckedAt:       "2026-05-31T10:00:00+08:00",
		Result:          ResultPass,
		BlocksNextStage: false,
		Inputs: []Artifact{
			{
				Path:   "requirements/T12345/requirement.md",
				SHA256: "8d969eef6ecad3c29a3a629280e686cff8fab4d5a86c84a36c8f7cd6b5bfc2f7",
			},
		},
		Checklist: []ChecklistItem{
			{
				Item:     "明确回滚方案",
				Result:   ResultPass,
				Evidence: "design.md includes rollback plan",
			},
		},
		Waiver:   Waiver{Required: false},
		Decision: "允许进入 4.1 任务拆分。",
	}
}
