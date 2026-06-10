package requirement

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVerifyRequirementPassesAllGates(t *testing.T) {
	root := t.TempDir()
	writeStandardMergeGates(t, root, "T12345")

	err := Verify(root, "T12345", "merge", time.Now(), VerifyOptions{})

	if err != nil {
		t.Fatalf("expected requirement verification to pass, got %v", err)
	}
}

func TestVerifyRequirementRequiresGateReports(t *testing.T) {
	root := t.TempDir()

	err := Verify(root, "T12345", "merge", time.Now(), VerifyOptions{})

	verifyErr := assertVerifyError(t, err)
	if verifyErr.Code != 3 {
		t.Fatalf("expected code 3, got %d", verifyErr.Code)
	}
}

func TestVerifyRequirementRejectsMismatchedRequirementID(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "requirements/T12345/requirement.md", "123456")
	writeFile(t, root, "requirements/T12345/gates/design-review.gate.json", strings.Replace(validGateJSON(), `"requirement_id": "T12345"`, `"requirement_id": "T99999"`, 1))

	err := Verify(root, "T12345", "merge", time.Now(), VerifyOptions{})

	verifyErr := assertVerifyError(t, err)
	if verifyErr.Code != 2 {
		t.Fatalf("expected code 2, got %d", verifyErr.Code)
	}
}

func TestVerifyRequirementAllowsEarlyIDLImpactWithoutEvidence(t *testing.T) {
	root := t.TempDir()
	writeStandardMergeGates(t, root, "T12345")
	report := strings.Replace(gateJSON("T12345", GateDesignReview), `"decision": "允许进入下一阶段。"`, `"idl_impact": {"impact": "yes"}, "decision": "允许进入下一阶段。"`, 1)
	writeFile(t, root, "requirements/T12345/gates/design-review.gate.json", report)

	err := Verify(root, "T12345", "merge", time.Now(), VerifyOptions{})

	if err != nil {
		t.Fatalf("expected early IDL impact without evidence to pass, got %v", err)
	}
}

func TestVerifyRequirementRequiresIDLEvidenceOnMergeReadiness(t *testing.T) {
	root := t.TempDir()
	writeStandardMergeGates(t, root, "T12345")
	report := strings.Replace(gateJSON("T12345", GateMergeReadiness), `"decision": "允许进入下一阶段。"`, `"idl_impact": {"impact": "yes"}, "decision": "允许进入下一阶段。"`, 1)
	writeFile(t, root, "requirements/T12345/gates/merge-readiness.gate.json", report)

	err := Verify(root, "T12345", "merge", time.Now(), VerifyOptions{})

	verifyErr := assertVerifyError(t, err)
	if verifyErr.Code != 6 {
		t.Fatalf("expected code 6, got %d", verifyErr.Code)
	}
}

func TestVerifyRequirementRequiresContractEvidenceOnMergeReadiness(t *testing.T) {
	root := t.TempDir()
	writeStandardMergeGates(t, root, "T12345")
	writeFile(t, root, "requirements/T12345/evidence/service-tests.md", "ok")
	report := strings.Replace(gateJSON("T12345", GateMergeReadiness), `"decision": "允许进入下一阶段。"`, `"idl_impact": {"impact": "yes"}, "evidence": [{"path":"requirements/T12345/evidence/service-tests.md","sha256":"`+sha256Of("ok")+`"}], "decision": "允许进入下一阶段。"`, 1)
	writeFile(t, root, "requirements/T12345/gates/merge-readiness.gate.json", report)

	err := Verify(root, "T12345", "merge", time.Now(), VerifyOptions{})

	verifyErr := assertVerifyError(t, err)
	if verifyErr.Code != 6 {
		t.Fatalf("expected code 6, got %d", verifyErr.Code)
	}
	if !strings.Contains(verifyErr.Error(), "no Buf, IDL, or contract evidence") {
		t.Fatalf("expected contract evidence error, got %q", verifyErr.Error())
	}
}

func TestVerifyRequirementAllowsContractEvidenceOnMergeReadiness(t *testing.T) {
	root := t.TempDir()
	writeStandardMergeGates(t, root, "T12345")
	writeFile(t, root, "requirements/T12345/evidence/buf-checks.md", "ok")
	report := strings.Replace(gateJSON("T12345", GateMergeReadiness), `"decision": "允许进入下一阶段。"`, `"idl_impact": {"impact": "yes"}, "evidence": [{"path":"requirements/T12345/evidence/buf-checks.md","sha256":"`+sha256Of("ok")+`"}], "decision": "允许进入下一阶段。"`, 1)
	writeFile(t, root, "requirements/T12345/gates/merge-readiness.gate.json", report)

	err := Verify(root, "T12345", "merge", time.Now(), VerifyOptions{})

	if err != nil {
		t.Fatalf("expected requirement verification to pass, got %v", err)
	}
}

func TestVerifyRequirementRejectsBlockedGate(t *testing.T) {
	root := t.TempDir()
	writeStandardMergeGates(t, root, "T12345")
	report := strings.Replace(validGateJSON(), `"result": "PASS"`, `"result": "BLOCKED"`, 1)
	report = strings.Replace(report, `"blocks_next_stage": false`, `"blocks_next_stage": true`, 1)
	report = strings.Replace(report, `"blocking_issues": []`, `"blocking_issues": [{"issue":"缺少回滚方案","required_action":"补充回滚策略","owner":"backend"}]`, 1)
	writeFile(t, root, "requirements/T12345/gates/design-review.gate.json", report)

	err := Verify(root, "T12345", "merge", time.Now(), VerifyOptions{})

	verifyErr := assertVerifyError(t, err)
	if verifyErr.Code != 1 {
		t.Fatalf("expected code 1, got %d", verifyErr.Code)
	}
}

func TestVerifyRequirementUsesRequirementIDAsDefaultTicketID(t *testing.T) {
	root := t.TempDir()
	writeStandardMergeGates(t, root, "T12345")
	writeFile(t, root, "requirements/T12345/gates/service-repo-check.gate.json", gateJSONWithRepos("feature/user-api/T12345", "feature/user-api/T12345"))

	err := Verify(root, "T12345", "merge", time.Now(), VerifyOptions{})

	if err != nil {
		t.Fatalf("expected requirement verification to pass, got %v", err)
	}
}

func TestVerifyRequirementRejectsMismatchedRepoBranches(t *testing.T) {
	root := t.TempDir()
	writeStandardMergeGates(t, root, "T12345")
	writeFile(t, root, "requirements/T12345/gates/service-repo-check.gate.json", gateJSONWithRepos("feature/user-api/T12345", "feature/user-api/T99999"))

	err := Verify(root, "T12345", "merge", time.Now(), VerifyOptions{})

	verifyErr := assertVerifyError(t, err)
	if verifyErr.Code != 7 {
		t.Fatalf("expected code 7, got %d", verifyErr.Code)
	}
	if !strings.Contains(verifyErr.Error(), "does not match") {
		t.Fatalf("expected branch mismatch output, got %q", verifyErr.Error())
	}
}

func TestVerifyRequirementAllowsMasterRepoBranchesForMergeTarget(t *testing.T) {
	root := t.TempDir()
	writeStandardMergeGates(t, root, "T12345")
	writeFile(t, root, "requirements/T12345/gates/service-repo-check.gate.json", gateJSONWithRepos("master", "master"))

	err := Verify(root, "T12345", "merge", time.Now(), VerifyOptions{})

	if err != nil {
		t.Fatalf("expected master repo branches to pass merge verification, got %v", err)
	}
}

func TestVerifyRequirementRequiresStandardMergeGates(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "requirements/T12345/requirement.md", "123456")
	writeFile(t, root, "requirements/T12345/gates/design-review.gate.json", validGateJSON())

	err := Verify(root, "T12345", "merge", time.Now(), VerifyOptions{})

	verifyErr := assertVerifyError(t, err)
	if verifyErr.Code != 3 {
		t.Fatalf("expected code 3, got %d", verifyErr.Code)
	}
	if !strings.Contains(verifyErr.Error(), "missing required merge gates") {
		t.Fatalf("expected missing merge gates output, got %q", verifyErr.Error())
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
	return gateJSON("T12345", GateDesignReview)
}

func writeStandardMergeGates(t *testing.T, root string, requirementID string) {
	t.Helper()
	writeFile(t, root, "requirements/"+requirementID+"/requirement.md", "123456")
	for _, gateID := range []string{GateRequirementReview, GateDesignReview, GateDevEntry, GateServiceRepoCheck, GateMergeReadiness} {
		writeFile(t, root, "requirements/"+requirementID+"/gates/"+gateID+".gate.json", gateJSON(requirementID, gateID))
	}
}

func gateJSON(requirementID string, gateID string) string {
	return `{
  "schema_version": "1.0",
  "requirement_id": "` + requirementID + `",
  "gate_id": "` + gateID + `",
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
  "decision": "允许进入下一阶段。"
}`
}

func gateJSONWithRepos(harnessBranch string, businessBranch string) string {
	repos := `  "repos": [
    {
      "name": "harness-repo",
      "branch": "` + harnessBranch + `",
      "commit": "abc123"
    },
    {
      "name": "business-repo",
      "branch": "` + businessBranch + `",
      "commit": "def456"
    }
  ],
`
	return strings.Replace(gateJSON("T12345", GateServiceRepoCheck), `  "decision": "允许进入下一阶段。"`, repos+`  "decision": "允许进入下一阶段。"`, 1)
}

func sha256Of(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
