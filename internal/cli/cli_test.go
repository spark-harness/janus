package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunShowsUsage(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run(nil, &stdout, &stderr)

	if code != ExitInvalid {
		t.Fatalf("expected exit code %d, got %d", ExitInvalid, code)
	}
	if !strings.Contains(stderr.String(), "janus gate validate") {
		t.Fatalf("expected usage in stderr, got %q", stderr.String())
	}
}

func TestRunHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"help"}, &stdout, &stderr)

	if code != ExitOK {
		t.Fatalf("expected exit code %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout.String(), "janus requirement verify") {
		t.Fatalf("expected usage in stdout, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestGateValidateValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "3.3-design-review.gate.json")
	if err := os.WriteFile(path, []byte(validGateJSON()), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"gate", "validate", path}, &stdout, &stderr)

	if code != ExitOK {
		t.Fatalf("expected exit code %d, got %d with stderr %q", ExitOK, code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "valid" {
		t.Fatalf("expected valid output, got %q", stdout.String())
	}
}

func TestGateValidateMissingFile(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"gate", "validate", "missing.gate.json"}, &stdout, &stderr)

	if code != ExitMissing {
		t.Fatalf("expected exit code %d, got %d", ExitMissing, code)
	}
	if !strings.Contains(stderr.String(), "missing file") {
		t.Fatalf("expected missing file output, got %q", stderr.String())
	}
}

func TestGateValidateInvalidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "3.3-design-review.gate.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":"1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"gate", "validate", path}, &stdout, &stderr)

	if code != ExitInvalid {
		t.Fatalf("expected exit code %d, got %d", ExitInvalid, code)
	}
	if !strings.Contains(stderr.String(), "requirement_id is required") {
		t.Fatalf("expected validation output, got %q", stderr.String())
	}
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
      "sha256": "8d969eef6ecad3c29a3a629280e686cff8fab4d5a86c84a36c8f7cd6b5bfc2f7"
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
