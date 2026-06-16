package requirement

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"janus/internal/gate"
)

// ApproveOptions carries the human approval record to write into a lifecycle
// artifact's front matter (or tasks.json top-level fields).
type ApproveOptions struct {
	GateID     string
	ApprovedBy string
	ApprovedAt string
	Decision   string
}

// Approve records a human approval (status: approved + approved_by/approved_at
// + decision) on the artifact that owns the given gate's approval source. It is
// the only sanctioned writer of the approval block: the guard-edit hook blocks
// agents from setting these fields through Write/Edit, so approval can only be
// performed by a person running this command.
func Approve(root string, requirementID string, options ApproveOptions) error {
	requirementID = strings.TrimSpace(requirementID)
	if err := validateRequirementID(requirementID); err != nil {
		return err
	}
	source := approvalSource(requirementID, options.GateID)
	if source == "" {
		return &LifecycleError{Code: gate.VerifyInvalid, Problems: []string{fmt.Sprintf("unknown gate: %s", options.GateID)}}
	}

	by := strings.TrimSpace(options.ApprovedBy)
	at := strings.TrimSpace(options.ApprovedAt)
	decision := strings.TrimSpace(options.Decision)
	var missing []string
	if by == "" {
		missing = append(missing, "--approved-by")
	}
	if at == "" {
		missing = append(missing, "--approved-at")
	}
	if decision == "" {
		missing = append(missing, "--decision")
	}
	if len(missing) > 0 {
		return &LifecycleError{Code: gate.VerifyInvalid, Problems: []string{fmt.Sprintf("approve requires %s", strings.Join(missing, ", "))}}
	}

	path := filepath.Join(root, filepath.FromSlash(source))
	contentBytes, err := os.ReadFile(path)
	if err != nil {
		return &LifecycleError{Code: gate.VerifyMissing, Problems: []string{fmt.Sprintf("cannot read %s: %v", source, err)}}
	}

	var updated string
	if strings.HasSuffix(source, ".json") {
		updated = setJSONApproval(string(contentBytes), by, at, decision)
	} else {
		updated = setFrontMatterApproval(string(contentBytes), by, at, decision)
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return &LifecycleError{Code: gate.VerifyMissing, Problems: []string{fmt.Sprintf("cannot write %s: %v", source, err)}}
	}
	return nil
}

func setFrontMatterApproval(content string, by string, at string, decision string) string {
	content = setFrontMatterField(content, "status", LifecycleStatusApproved)
	content = setFrontMatterField(content, "approved_by", by)
	content = setFrontMatterField(content, "approved_at", at)
	content = setFrontMatterField(content, "decision", yamlSafeScalar(decision))
	return content
}

// yamlSafeScalar keeps a value safe inside a double-quoted front-matter scalar.
// janus's front-matter reader only trims surrounding quotes (it does not
// unescape), so embedded double quotes are downgraded to single quotes to stay
// round-trippable.
func yamlSafeScalar(value string) string {
	return strings.ReplaceAll(value, `"`, `'`)
}

func setFrontMatterField(content string, key string, value string) string {
	line := fmt.Sprintf(`%s: "%s"`, key, value)
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:.*$`)
	if loc := re.FindStringIndex(content); loc != nil && loc[0] < frontMatterEnd(content) {
		return content[:loc[0]] + line + content[loc[1]:]
	}
	if strings.HasPrefix(content, "---\n") {
		if end := strings.Index(content[4:], "\n---"); end >= 0 {
			insertAt := 4 + end
			return content[:insertAt] + "\n" + line + content[insertAt:]
		}
	}
	return "---\n" + line + "\n---\n\n" + content
}

func frontMatterEnd(content string) int {
	if !strings.HasPrefix(content, "---\n") {
		return 0
	}
	if end := strings.Index(content[4:], "\n---"); end >= 0 {
		return 4 + end
	}
	return len(content)
}

func setJSONApproval(content string, by string, at string, decision string) string {
	content = setJSONStringField(content, "status", LifecycleStatusApproved)
	content = setJSONStringField(content, "approved_by", by)
	content = setJSONStringField(content, "approved_at", at)
	content = setJSONStringField(content, "decision", decision)
	return content
}

// setJSONStringField replaces the first occurrence of a top-level "key": "..."
// string field. Top-level approval fields appear before the tasks array, so
// replacing the first match keeps task-level fields untouched.
func setJSONStringField(content string, key string, value string) string {
	quoted, _ := json.Marshal(value)
	replacement := fmt.Sprintf(`"%s": %s`, key, string(quoted))
	re := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `"\s*:\s*"(?:[^"\\]|\\.)*"`)
	if loc := re.FindStringIndex(content); loc != nil {
		return content[:loc[0]] + replacement + content[loc[1]:]
	}
	if idx := strings.Index(content, "{"); idx >= 0 {
		return content[:idx+1] + "\n  " + replacement + "," + content[idx+1:]
	}
	return content
}
