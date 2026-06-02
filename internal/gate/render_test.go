package gate

import (
	"strings"
	"testing"
)

func TestRenderIncludesGeneratedHeaderAndDecision(t *testing.T) {
	report := validReport()

	rendered := Render(report, "requirements/T12345/gates/design-review.gate.json")

	if !strings.HasPrefix(rendered, "<!-- Generated from design-review.gate.json. Do not edit blocking fields here. -->") {
		t.Fatalf("expected generated header, got %q", rendered)
	}
	if !strings.Contains(rendered, "允许进入 4.1 任务拆分。") {
		t.Fatalf("expected decision, got %q", rendered)
	}
	if !strings.Contains(rendered, "| `requirements/T12345/requirement.md` | `8d969eef6ecad3c29a3a629280e686cf0c3f5d5a86aff3ca12020c923adc6c92` |") {
		t.Fatalf("expected input snapshot table, got %q", rendered)
	}
}
