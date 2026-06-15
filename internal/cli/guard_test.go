package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func guardEventJSON(t *testing.T, toolName string, toolInput map[string]any) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"tool_name": toolName, "tool_input": toolInput})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeArtifact(t *testing.T, dir string, rel string, content string) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

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

func TestGuardEditDeniesWriteApproved(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "requirements", "SPARK-3", "requirement.md")
	event := guardEventJSON(t, "Write", map[string]any{
		"file_path": target,
		"content":   "---\nstatus: \"approved\"\napproved_by: \"agent\"\n---\n# x\n",
	})

	code, _, stderr := runGuardEdit(t, event)
	if code != ExitHookDeny {
		t.Fatalf("expected deny (exit %d), got %d with stderr %q", ExitHookDeny, code, stderr)
	}
	if !strings.Contains(stderr, "approved") {
		t.Fatalf("expected approval reason on stderr, got %q", stderr)
	}
}

func TestGuardEditDeniesEditIntoApproved(t *testing.T) {
	dir := t.TempDir()
	target := writeArtifact(t, dir, "requirements/SPARK-3/requirement.md",
		"---\nstatus: \"draft\"\n---\n# x\n")
	event := guardEventJSON(t, "Edit", map[string]any{
		"file_path":  target,
		"old_string": `status: "draft"`,
		"new_string": `status: "approved"`,
	})

	code, _, stderr := runGuardEdit(t, event)
	if code != ExitHookDeny {
		t.Fatalf("expected deny (exit %d), got %d with stderr %q", ExitHookDeny, code, stderr)
	}
}

func TestGuardEditDeniesMultiEditAssemblingApproval(t *testing.T) {
	dir := t.TempDir()
	target := writeArtifact(t, dir, "requirements/SPARK-3/requirement.md",
		"---\nstatus: \"draft\"\napproved_by: \"\"\n---\n# x\n")
	event := guardEventJSON(t, "MultiEdit", map[string]any{
		"file_path": target,
		"edits": []map[string]any{
			{"old_string": `approved_by: ""`, "new_string": `approved_by: "forest"`},
			{"old_string": `status: "draft"`, "new_string": `status: "approved"`},
		},
	})

	code, _, stderr := runGuardEdit(t, event)
	if code != ExitHookDeny {
		t.Fatalf("expected deny (exit %d), got %d with stderr %q", ExitHookDeny, code, stderr)
	}
}

func TestGuardEditDeniesTasksApproved(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "requirements", "SPARK-3", "tasks.json")
	event := guardEventJSON(t, "Write", map[string]any{
		"file_path": target,
		"content":   "{\n  \"status\": \"approved\",\n  \"tasks\": []\n}\n",
	})

	code, _, stderr := runGuardEdit(t, event)
	if code != ExitHookDeny {
		t.Fatalf("expected deny (exit %d), got %d with stderr %q", ExitHookDeny, code, stderr)
	}
}

func TestGuardEditAllowsDraftWrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "requirements", "SPARK-3", "requirement.md")
	event := guardEventJSON(t, "Write", map[string]any{
		"file_path": target,
		"content":   "---\nstatus: \"draft\"\n---\n# x\n",
	})

	code, _, stderr := runGuardEdit(t, event)
	if code != ExitOK {
		t.Fatalf("expected allow (exit %d), got %d with stderr %q", ExitOK, code, stderr)
	}
}

func TestGuardEditAllowsBodyEditOfApproved(t *testing.T) {
	// Editing the body of an already-approved file keeps status approved; that
	// is not a forged transition (drift is handled elsewhere), so allow.
	dir := t.TempDir()
	target := writeArtifact(t, dir, "requirements/SPARK-3/requirement.md",
		"---\nstatus: \"approved\"\napproved_by: \"forest\"\n---\n# old body\n")
	event := guardEventJSON(t, "Edit", map[string]any{
		"file_path":  target,
		"old_string": "# old body",
		"new_string": "# new body",
	})

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
