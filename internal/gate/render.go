package gate

import (
	"fmt"
	"path/filepath"
	"strings"
)

func Render(report *Report, inputPath string) string {
	var b strings.Builder
	source := filepath.Base(inputPath)

	writeMetadata(&b, report)
	fmt.Fprintf(&b, "<!-- Generated from %s. Do not edit blocking fields here. -->\n\n", source)
	fmt.Fprintf(&b, "# %s\n\n", report.GateName)
	writeDecision(&b, report)
	writeInputs(&b, report.Inputs)
	writeChecklist(&b, report.Checklist)
	writeBlockingIssues(&b, report.BlockingIssues)
	writeWarnings(&b, report.Warnings)
	writeWaiver(&b, report.Waiver)
	writeEvidence(&b, report.Evidence)
	return b.String()
}

func writeMetadata(b *strings.Builder, report *Report) {
	fmt.Fprintln(b, "---")
	fmt.Fprintf(b, "requirement_id: %s\n", yamlQuote(report.RequirementID))
	fmt.Fprintf(b, "gate_id: %s\n", yamlQuote(report.GateID))
	fmt.Fprintf(b, "gate_name: %s\n", yamlQuote(report.GateName))
	fmt.Fprintf(b, "stage: %s\n", yamlQuote(report.Stage))
	fmt.Fprintf(b, "checked_by: %s\n", yamlQuote(report.CheckedBy))
	fmt.Fprintf(b, "checked_at: %s\n", yamlQuote(report.CheckedAt))
	fmt.Fprintf(b, "result: %s\n", yamlQuote(report.Result))
	fmt.Fprintf(b, "blocks_next_stage: %t\n", report.BlocksNextStage)
	fmt.Fprintln(b, "---")
	fmt.Fprintln(b)
}

func writeDecision(b *strings.Builder, report *Report) {
	fmt.Fprintln(b, "## 结论")
	fmt.Fprintln(b)
	fmt.Fprintln(b, report.Decision)
	fmt.Fprintln(b)
}

func writeInputs(b *strings.Builder, inputs []Artifact) {
	fmt.Fprintln(b, "## 输入快照")
	fmt.Fprintln(b)
	writeArtifactsTable(b, inputs)
}

func writeChecklist(b *strings.Builder, checklist []ChecklistItem) {
	fmt.Fprintln(b, "## 检查项")
	fmt.Fprintln(b)
	if len(checklist) == 0 {
		fmt.Fprintln(b, "无。")
		fmt.Fprintln(b)
		return
	}

	fmt.Fprintln(b, "| Item | Result | Evidence |")
	fmt.Fprintln(b, "| --- | --- | --- |")
	for _, item := range checklist {
		fmt.Fprintf(b, "| %s | `%s` | %s |\n", escapeTable(item.Item), item.Result, escapeTable(item.Evidence))
	}
	fmt.Fprintln(b)
}

func writeBlockingIssues(b *strings.Builder, issues []BlockingIssue) {
	fmt.Fprintln(b, "## 阻塞问题")
	fmt.Fprintln(b)
	if len(issues) == 0 {
		fmt.Fprintln(b, "无。")
		fmt.Fprintln(b)
		return
	}

	fmt.Fprintln(b, "| Issue | Required action | Owner |")
	fmt.Fprintln(b, "| --- | --- | --- |")
	for _, issue := range issues {
		fmt.Fprintf(b, "| %s | %s | `%s` |\n", escapeTable(issue.Issue), escapeTable(issue.RequiredAction), issue.Owner)
	}
	fmt.Fprintln(b)
}

func writeWarnings(b *strings.Builder, warnings []Warning) {
	fmt.Fprintln(b, "## 警告")
	fmt.Fprintln(b)
	if len(warnings) == 0 {
		fmt.Fprintln(b, "无。")
		fmt.Fprintln(b)
		return
	}

	fmt.Fprintln(b, "| Issue | Follow-up action | Owner |")
	fmt.Fprintln(b, "| --- | --- | --- |")
	for _, warning := range warnings {
		fmt.Fprintf(b, "| %s | %s | `%s` |\n", escapeTable(warning.Issue), escapeTable(warning.FollowUpAction), warning.Owner)
	}
	fmt.Fprintln(b)
}

func writeWaiver(b *strings.Builder, waiver Waiver) {
	fmt.Fprintln(b, "## 豁免")
	fmt.Fprintln(b)
	fmt.Fprintf(b, "- Required: `%t`\n", waiver.Required)
	if waiver.Reason != "" {
		fmt.Fprintf(b, "- Reason: %s\n", waiver.Reason)
	}
	if waiver.Approver != "" {
		fmt.Fprintf(b, "- Approver: `%s`\n", waiver.Approver)
	}
	if waiver.ApprovedAt != "" {
		fmt.Fprintf(b, "- Approved at: `%s`\n", waiver.ApprovedAt)
	}
	if waiver.ExpiresAt != "" {
		fmt.Fprintf(b, "- Expires at: `%s`\n", waiver.ExpiresAt)
	}
	if waiver.FollowUpIssue != "" {
		fmt.Fprintf(b, "- Follow-up issue: `%s`\n", waiver.FollowUpIssue)
	}
	fmt.Fprintln(b)
}

func writeEvidence(b *strings.Builder, evidence []Artifact) {
	fmt.Fprintln(b, "## 外部证据")
	fmt.Fprintln(b)
	if len(evidence) == 0 {
		fmt.Fprintln(b, "无。")
		fmt.Fprintln(b)
		return
	}
	writeArtifactsTable(b, evidence)
}

func writeArtifactsTable(b *strings.Builder, artifacts []Artifact) {
	fmt.Fprintln(b, "| Path | SHA-256 |")
	fmt.Fprintln(b, "| --- | --- |")
	for _, artifact := range artifacts {
		fmt.Fprintf(b, "| `%s` | `%s` |\n", artifact.Path, artifact.SHA256)
	}
	fmt.Fprintln(b)
}

func escapeTable(value string) string {
	return strings.ReplaceAll(value, "|", "\\|")
}

func yamlQuote(value string) string {
	escaped := strings.ReplaceAll(value, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
	return "\"" + escaped + "\""
}
