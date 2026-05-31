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

func TestGateRenderWritesMarkdown(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "3.3-design-review.gate.json")
	output := filepath.Join(dir, "3.3-design-review.md")
	if err := os.WriteFile(input, []byte(validGateJSON()), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"gate", "render", "--input", input, "--output", output}, &stdout, &stderr)

	if code != ExitOK {
		t.Fatalf("expected exit code %d, got %d with stderr %q", ExitOK, code, stderr.String())
	}
	rendered, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rendered), "Generated from 3.3-design-review.gate.json") {
		t.Fatalf("expected generated header, got %q", string(rendered))
	}
}

func TestGateRenderCheckDetectsDrift(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "3.3-design-review.gate.json")
	output := filepath.Join(dir, "3.3-design-review.md")
	if err := os.WriteFile(input, []byte(validGateJSON()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"gate", "render", "--check", "--input", input, "--output", output}, &stdout, &stderr)

	if code != ExitInvalid {
		t.Fatalf("expected exit code %d, got %d", ExitInvalid, code)
	}
	if !strings.Contains(stderr.String(), "out of date") {
		t.Fatalf("expected drift output, got %q", stderr.String())
	}
}

func TestGateRenderCheckPassesWhenCurrent(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "3.3-design-review.gate.json")
	output := filepath.Join(dir, "3.3-design-review.md")
	if err := os.WriteFile(input, []byte(validGateJSON()), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"gate", "render", "--input", input, "--output", output}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("expected render exit code %d, got %d with stderr %q", ExitOK, code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"gate", "render", "--check", "--input", input, "--output", output}, &stdout, &stderr)

	if code != ExitOK {
		t.Fatalf("expected check exit code %d, got %d with stderr %q", ExitOK, code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "up to date") {
		t.Fatalf("expected up to date output, got %q", stdout.String())
	}
}

func TestGateVerifyPasses(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeRequirementInput(t, dir, "123456")
	input := filepath.Join(dir, "3.3-design-review.gate.json")
	if err := os.WriteFile(input, []byte(validGateJSON()), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"gate", "verify", "--input", input}, &stdout, &stderr)

	if code != ExitOK {
		t.Fatalf("expected exit code %d, got %d with stderr %q", ExitOK, code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "verified" {
		t.Fatalf("expected verified output, got %q", stdout.String())
	}
}

func TestGateVerifyReturnsStaleInputCode(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeRequirementInput(t, dir, "changed")
	input := filepath.Join(dir, "3.3-design-review.gate.json")
	if err := os.WriteFile(input, []byte(validGateJSON()), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"gate", "verify", "--input", input}, &stdout, &stderr)

	if code != ExitStaleInput {
		t.Fatalf("expected exit code %d, got %d", ExitStaleInput, code)
	}
	if !strings.Contains(stderr.String(), "sha256 mismatch") {
		t.Fatalf("expected stale output, got %q", stderr.String())
	}
}

func writeRequirementInput(t *testing.T, root string, content string) {
	t.Helper()
	path := filepath.Join(root, "requirements", "T12345", "requirement.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
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
