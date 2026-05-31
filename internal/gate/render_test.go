package gate

import (
	"strings"
	"testing"
)

func TestRenderIncludesGeneratedHeaderAndDecision(t *testing.T) {
	report := validReport()

	rendered := Render(report, "requirements/T12345/gates/3.3-design-review.gate.json")

	if !strings.HasPrefix(rendered, "<!-- Generated from 3.3-design-review.gate.json. Do not edit blocking fields here. -->") {
		t.Fatalf("expected generated header, got %q", rendered)
	}
	if !strings.Contains(rendered, "允许进入 4.1 任务拆分。") {
		t.Fatalf("expected decision, got %q", rendered)
	}
	if !strings.Contains(rendered, "| `requirements/T12345/requirement.md` | `8d969eef6ecad3c29a3a629280e686cff8fab4d5a86c84a36c8f7cd6b5bfc2f7` |") {
		t.Fatalf("expected input snapshot table, got %q", rendered)
	}
}
