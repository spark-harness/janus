package requirement

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVerifyRequirementPassesAllGates(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "requirements/T12345/requirement.md", "123456")
	writeFile(t, root, "requirements/T12345/gates/3.3-design-review.gate.json", validGateJSON())

	err := Verify(root, "T12345", "merge", time.Now())

	if err != nil {
		t.Fatalf("expected requirement verification to pass, got %v", err)
	}
}

func TestVerifyRequirementRequiresGateReports(t *testing.T) {
	root := t.TempDir()

	err := Verify(root, "T12345", "merge", time.Now())

	verifyErr := assertVerifyError(t, err)
	if verifyErr.Code != 3 {
		t.Fatalf("expected code 3, got %d", verifyErr.Code)
	}
}

func TestVerifyRequirementRejectsMismatchedRequirementID(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "requirements/T12345/requirement.md", "123456")
	writeFile(t, root, "requirements/T12345/gates/3.3-design-review.gate.json", strings.Replace(validGateJSON(), `"requirement_id": "T12345"`, `"requirement_id": "T99999"`, 1))

	err := Verify(root, "T12345", "merge", time.Now())

	verifyErr := assertVerifyError(t, err)
	if verifyErr.Code != 2 {
		t.Fatalf("expected code 2, got %d", verifyErr.Code)
	}
}

func TestVerifyRequirementRequiresIDLEvidenceWhenImpacted(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "requirements/T12345/requirement.md", "123456")
	report := strings.Replace(validGateJSON(), `"decision": "允许进入 4.1 任务拆分。"`, `"idl_impact": {"impact": "yes"}, "decision": "允许进入 4.1 任务拆分。"`, 1)
	writeFile(t, root, "requirements/T12345/gates/3.3-design-review.gate.json", report)

	err := Verify(root, "T12345", "merge", time.Now())

	verifyErr := assertVerifyError(t, err)
	if verifyErr.Code != 6 {
		t.Fatalf("expected code 6, got %d", verifyErr.Code)
	}
}

func TestVerifyRequirementRejectsBlockedGate(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "requirements/T12345/requirement.md", "123456")
	report := strings.Replace(validGateJSON(), `"result": "PASS"`, `"result": "BLOCKED"`, 1)
	report = strings.Replace(report, `"blocks_next_stage": false`, `"blocks_next_stage": true`, 1)
	report = strings.Replace(report, `"blocking_issues": []`, `"blocking_issues": [{"issue":"缺少回滚方案","required_action":"补充回滚策略","owner":"backend"}]`, 1)
	writeFile(t, root, "requirements/T12345/gates/3.3-design-review.gate.json", report)

	err := Verify(root, "T12345", "merge", time.Now())

	verifyErr := assertVerifyError(t, err)
	if verifyErr.Code != 1 {
		t.Fatalf("expected code 1, got %d", verifyErr.Code)
	}
}

func writeFile(t *testing.T, root string, relativePath string, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
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

func validGateJSON() string {
	return `{
  "schema_version": "1.0",
  "requirement_id": "T12345",
  "gate_id": "3.3-design-review",
  "gate_name": "设计门禁",
  "stage": "3.3",
  "checked_by": "detail-design-quality-reviewer",
  "checked_at": "2026-05-31T10:00:00+08:00",
  "result": "PASS",
  "blocks_next_stage": false,
  "inputs": [
    {
      "path": "requirements/T12345/requirement.md",
      "sha256": "8d969eef6ecad3c29a3a629280e686cf0c3f5d5a86aff3ca12020c923adc6c92"
    }
  ],
  "checklist": [
    {
      "item": "明确回滚方案",
      "result": "PASS",
      "evidence": "design.md includes rollback plan"
    }
  ],
  "blocking_issues": [],
  "warnings": [],
  "waiver": {
    "required": false
  },
  "decision": "允许进入 4.1 任务拆分。"
}`
}
