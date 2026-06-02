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

func TestRunVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"version"}, &stdout, &stderr)

	if code != ExitOK {
		t.Fatalf("expected exit code %d, got %d", ExitOK, code)
	}
	if strings.TrimSpace(stdout.String()) != "janus "+Version {
		t.Fatalf("expected version output, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestGateValidateValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "design-review.gate.json")
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
	path := filepath.Join(dir, "design-review.gate.json")
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
	input := filepath.Join(dir, "design-review.gate.json")
	output := filepath.Join(dir, "design-review.md")
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
	if !strings.Contains(string(rendered), "Generated from design-review.gate.json") {
		t.Fatalf("expected generated header, got %q", string(rendered))
	}
}

func TestGateRenderCheckDetectsDrift(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "design-review.gate.json")
	output := filepath.Join(dir, "design-review.md")
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
	input := filepath.Join(dir, "design-review.gate.json")
	output := filepath.Join(dir, "design-review.md")
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
	input := filepath.Join(dir, "design-review.gate.json")
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

func TestGateVerifyRequiresTicketIDWhenReposArePresent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeRequirementInput(t, dir, "123456")
	input := filepath.Join(dir, "service-repo-check.gate.json")
	if err := os.WriteFile(input, []byte(gateJSONWithRepos("feature/user-api/T12345", "feature/user-api/T12345")), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"gate", "verify", "--input", input}, &stdout, &stderr)

	if code != ExitBranchPolicy {
		t.Fatalf("expected exit code %d, got %d with stderr %q", ExitBranchPolicy, code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "ticket-id is required") {
		t.Fatalf("expected ticket-id output, got %q", stderr.String())
	}
}

func TestGateVerifyRejectsRepoBranchMismatch(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeRequirementInput(t, dir, "123456")
	input := filepath.Join(dir, "service-repo-check.gate.json")
	if err := os.WriteFile(input, []byte(gateJSONWithRepos("feature/user-api/T12345", "feature/user-api/T99999")), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"gate", "verify", "--input", input, "--ticket-id", "T12345"}, &stdout, &stderr)

	if code != ExitBranchPolicy {
		t.Fatalf("expected exit code %d, got %d with stderr %q", ExitBranchPolicy, code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "does not match") {
		t.Fatalf("expected branch mismatch output, got %q", stderr.String())
	}
}

func TestGateVerifyPassesRepoBranchPolicy(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeRequirementInput(t, dir, "123456")
	input := filepath.Join(dir, "service-repo-check.gate.json")
	if err := os.WriteFile(input, []byte(gateJSONWithRepos("feature/user-api/T12345", "feature/user-api/T12345")), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"gate", "verify", "--input", input, "--ticket-id", "T12345"}, &stdout, &stderr)

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
	input := filepath.Join(dir, "design-review.gate.json")
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

func TestRequirementVerifyPasses(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeRequirementInput(t, dir, "123456")
	gatePath := filepath.Join(dir, "requirements", "T12345", "gates", "design-review.gate.json")
	if err := os.MkdirAll(filepath.Dir(gatePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gatePath, []byte(validGateJSON()), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"requirement", "verify", "--requirement", "T12345", "--target", "merge"}, &stdout, &stderr)

	if code != ExitOK {
		t.Fatalf("expected exit code %d, got %d with stderr %q", ExitOK, code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "verified" {
		t.Fatalf("expected verified output, got %q", stdout.String())
	}
}

func TestRequirementVerifyUsesRequirementIDForRepoBranchPolicy(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeRequirementInput(t, dir, "123456")
	gatePath := filepath.Join(dir, "requirements", "T12345", "gates", "service-repo-check.gate.json")
	if err := os.MkdirAll(filepath.Dir(gatePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gatePath, []byte(gateJSONWithRepos("feature/user-api/T12345", "feature/user-api/T12345")), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"requirement", "verify", "--requirement", "T12345", "--target", "merge"}, &stdout, &stderr)

	if code != ExitOK {
		t.Fatalf("expected exit code %d, got %d with stderr %q", ExitOK, code, stderr.String())
	}
}

func TestRequirementVerifyRequiresReports(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"requirement", "verify", "--requirement", "T12345", "--target", "merge"}, &stdout, &stderr)

	if code != ExitMissing {
		t.Fatalf("expected exit code %d, got %d", ExitMissing, code)
	}
	if !strings.Contains(stderr.String(), "missing gate reports") {
		t.Fatalf("expected missing gate output, got %q", stderr.String())
	}
}

func TestHookGateDriftCheckPassesWithoutRequirements(t *testing.T) {
	dir := t.TempDir()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"hook", "gate-drift-check", "--root", dir}, &stdout, &stderr)

	if code != ExitOK {
		t.Fatalf("expected exit code %d, got %d with stderr %q", ExitOK, code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"continue":true`) {
		t.Fatalf("expected continue response, got %q", stdout.String())
	}
}

func TestHookGateDriftCheckBlocksMissingMarkdown(t *testing.T) {
	dir := t.TempDir()
	gatePath := filepath.Join(dir, "requirements", "T12345", "gates", "design-review.gate.json")
	if err := os.MkdirAll(filepath.Dir(gatePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gatePath, []byte(validGateJSON()), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"hook", "gate-drift-check", "--root", dir}, &stdout, &stderr)

	if code != ExitOK {
		t.Fatalf("expected exit code %d, got %d with stderr %q", ExitOK, code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"continue":false`) {
		t.Fatalf("expected stop response, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "missing rendered gate Markdown") {
		t.Fatalf("expected missing markdown reason, got %q", stdout.String())
	}
}

func TestHookGateDriftCheckPassesRenderedMarkdown(t *testing.T) {
	dir := t.TempDir()
	gatePath := filepath.Join(dir, "requirements", "T12345", "gates", "design-review.gate.json")
	mdPath := filepath.Join(dir, "requirements", "T12345", "gates", "design-review.md")
	if err := os.MkdirAll(filepath.Dir(gatePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gatePath, []byte(validGateJSON()), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"gate", "render", "--input", gatePath, "--output", mdPath}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("expected render exit code %d, got %d with stderr %q", ExitOK, code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"hook", "gate-drift-check", "--root", dir}, &stdout, &stderr)

	if code != ExitOK {
		t.Fatalf("expected exit code %d, got %d with stderr %q", ExitOK, code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"continue":true`) {
		t.Fatalf("expected continue response, got %q", stdout.String())
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
  "gate_id": "design-review",
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
	return strings.Replace(validGateJSON(), `  "decision": "允许进入 4.1 任务拆分。"`, repos+`  "decision": "允许进入 4.1 任务拆分。"`, 1)
}
