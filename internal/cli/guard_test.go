package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runGuardEdit(t *testing.T, eventJSON string) (int, string, string) {
	t.Helper()
	previous := hookStdin
	hookStdin = strings.NewReader(eventJSON)
	defer func() { hookStdin = previous }()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"hook", "guard-edit"}, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestGuardEditAllowsNonArtifact(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "notes.txt")
	event := `{"tool_name":"Write","tool_input":{"file_path":"` + target + `","content":"status: \"approved\""}}`

	code, _, stderr := runGuardEdit(t, event)
	if code != ExitOK {
		t.Fatalf("expected allow (exit %d), got %d with stderr %q", ExitOK, code, stderr)
	}
}

func TestGuardEditAllowsArtifactWithoutRules(t *testing.T) {
	// B1 has no rules yet: even an approved requirement.md write is allowed.
	dir := t.TempDir()
	target := filepath.Join(dir, "requirements", "SPARK-3", "requirement.md")
	event := `{"tool_name":"Write","tool_input":{"file_path":"` + target + `","content":"---\nstatus: \"approved\"\n---\n"}}`

	code, _, stderr := runGuardEdit(t, event)
	if code != ExitOK {
		t.Fatalf("expected allow (exit %d), got %d with stderr %q", ExitOK, code, stderr)
	}
}

func TestGuardEditAllowsMalformedEvent(t *testing.T) {
	code, _, _ := runGuardEdit(t, "not json")
	if code != ExitOK {
		t.Fatalf("expected fail-open allow (exit %d), got %d", ExitOK, code)
	}
}

func TestClaudeCandidateWrite(t *testing.T) {
	path, content, ok := claudeCandidate("Write", []byte(`{"file_path":"/x/a.md","content":"hello"}`))
	if !ok || path != "/x/a.md" || content != "hello" {
		t.Fatalf("unexpected (%q, %q, %v)", path, content, ok)
	}
}

func TestClaudeCandidateEdit(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.md")
	if err := os.WriteFile(target, []byte("alpha beta gamma"), 0o644); err != nil {
		t.Fatal(err)
	}
	input := `{"file_path":"` + target + `","old_string":"beta","new_string":"BETA"}`
	path, content, ok := claudeCandidate("Edit", []byte(input))
	if !ok || path != target || content != "alpha BETA gamma" {
		t.Fatalf("unexpected (%q, %q, %v)", path, content, ok)
	}
}

func TestClaudeCandidateEditMissingOldStringNotApplicable(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.md")
	if err := os.WriteFile(target, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	input := `{"file_path":"` + target + `","old_string":"zzz","new_string":"y"}`
	_, _, ok := claudeCandidate("Edit", []byte(input))
	if ok {
		t.Fatalf("expected ok=false when old_string is absent")
	}
}

func TestClaudeCandidateMultiEdit(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.md")
	if err := os.WriteFile(target, []byte("one two three"), 0o644); err != nil {
		t.Fatal(err)
	}
	input := `{"file_path":"` + target + `","edits":[{"old_string":"one","new_string":"1"},{"old_string":"three","new_string":"3"}]}`
	path, content, ok := claudeCandidate("MultiEdit", []byte(input))
	if !ok || path != target || content != "1 two 3" {
		t.Fatalf("unexpected (%q, %q, %v)", path, content, ok)
	}
}
