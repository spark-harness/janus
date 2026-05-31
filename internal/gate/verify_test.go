package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVerifyPassReport(t *testing.T) {
	root := writeInputSnapshot(t, "123456")
	report := validReport()

	err := Verify(report, root, time.Date(2026, 5, 31, 9, 0, 0, 0, time.FixedZone("CST", 8*60*60)))

	if err != nil {
		t.Fatalf("expected verified report, got %v", err)
	}
}

func TestVerifyDetectsStaleInput(t *testing.T) {
	root := writeInputSnapshot(t, "changed")
	report := validReport()

	err := Verify(report, root, time.Now())

	verifyErr := assertVerifyError(t, err)
	if verifyErr.Code != VerifyStaleInput {
		t.Fatalf("expected code %d, got %d", VerifyStaleInput, verifyErr.Code)
	}
	if !strings.Contains(verifyErr.Error(), "sha256 mismatch") {
		t.Fatalf("expected stale input error, got %v", verifyErr)
	}
}

func TestVerifyBlocksBlockedReport(t *testing.T) {
	root := writeInputSnapshot(t, "123456")
	report := validReport()
	report.Result = ResultBlocked
	report.BlocksNextStage = true
	report.BlockingIssues = []BlockingIssue{
		{Issue: "缺少回滚方案", RequiredAction: "补充回滚策略", Owner: "backend"},
	}

	err := Verify(report, root, time.Now())

	verifyErr := assertVerifyError(t, err)
	if verifyErr.Code != VerifyBlocked {
		t.Fatalf("expected code %d, got %d", VerifyBlocked, verifyErr.Code)
	}
}

func TestVerifyRejectsExpiredWaiver(t *testing.T) {
	root := writeInputSnapshot(t, "123456")
	report := validReport()
	report.Result = ResultWaived
	report.Waiver = Waiver{
		Required:      true,
		Reason:        "release window",
		Approver:      "tech-lead",
		ApprovedAt:    "2026-05-30T10:00:00+08:00",
		ExpiresAt:     "2026-05-31T10:00:00+08:00",
		FollowUpIssue: "T12345-FOLLOWUP",
	}

	err := Verify(report, root, time.Date(2026, 5, 31, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60)))

	verifyErr := assertVerifyError(t, err)
	if verifyErr.Code != VerifyInvalidWaiver {
		t.Fatalf("expected code %d, got %d", VerifyInvalidWaiver, verifyErr.Code)
	}
}

func TestVerifyDetectsEvidenceFailure(t *testing.T) {
	root := writeInputSnapshot(t, "123456")
	report := validReport()
	report.Evidence = []Artifact{
		{Path: "reports/T12345/buf-breaking.txt", SHA256: "8d969eef6ecad3c29a3a629280e686cf0c3f5d5a86aff3ca12020c923adc6c92"},
	}

	err := Verify(report, root, time.Now())

	verifyErr := assertVerifyError(t, err)
	if verifyErr.Code != VerifyEvidenceFailure {
		t.Fatalf("expected code %d, got %d", VerifyEvidenceFailure, verifyErr.Code)
	}
}

func writeInputSnapshot(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "requirements", "T12345", "requirement.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func assertVerifyError(t *testing.T, err error) *VerifyError {
	t.Helper()
	if err == nil {
		t.Fatal("expected verify error")
	}
	verifyErr, ok := err.(*VerifyError)
	if !ok {
		t.Fatalf("expected VerifyError, got %T", err)
	}
	return verifyErr
}
