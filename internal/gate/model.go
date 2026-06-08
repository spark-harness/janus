package gate

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	ResultPass    = "PASS"
	ResultBlocked = "BLOCKED"
	ResultWarn    = "WARN"
	ResultWaived  = "WAIVED"
)

type Report struct {
	SchemaVersion   string          `json:"schema_version"`
	RequirementID   string          `json:"requirement_id"`
	GateID          string          `json:"gate_id"`
	GateName        string          `json:"gate_name"`
	Stage           string          `json:"stage"`
	CheckedBy       string          `json:"checked_by"`
	CheckedAt       string          `json:"checked_at"`
	Result          string          `json:"result"`
	BlocksNextStage bool            `json:"blocks_next_stage"`
	Inputs          []Artifact      `json:"inputs"`
	Checklist       []ChecklistItem `json:"checklist"`
	BlockingIssues  []BlockingIssue `json:"blocking_issues"`
	Warnings        []Warning       `json:"warnings"`
	Waiver          Waiver          `json:"waiver"`
	Repos           []Repo          `json:"repos,omitempty"`
	Evidence        []Artifact      `json:"evidence,omitempty"`
	IDLImpact       *IDLImpact      `json:"idl_impact,omitempty"`
	Decision        string          `json:"decision"`
}

type Artifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type ChecklistItem struct {
	Item     string `json:"item"`
	Result   string `json:"result"`
	Evidence string `json:"evidence"`
}

type BlockingIssue struct {
	Issue          string `json:"issue"`
	RequiredAction string `json:"required_action"`
	Owner          string `json:"owner"`
}

type Warning struct {
	Issue          string `json:"issue"`
	FollowUpAction string `json:"follow_up_action"`
	Owner          string `json:"owner,omitempty"`
}

type Waiver struct {
	Required      bool   `json:"required"`
	Reason        string `json:"reason,omitempty"`
	Approver      string `json:"approver,omitempty"`
	ApprovedAt    string `json:"approved_at,omitempty"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	FollowUpIssue string `json:"follow_up_issue,omitempty"`
}

type Repo struct {
	Name   string `json:"name"`
	Branch string `json:"branch"`
	Commit string `json:"commit"`
}

type IDLImpact struct {
	Impact   string `json:"impact"`
	NAReason string `json:"na_reason,omitempty"`
}

type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return strings.Join(e.Problems, "\n")
}

func Read(r io.Reader) (*Report, error) {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()

	var report Report
	if err := decoder.Decode(&report); err != nil {
		return nil, err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("multiple JSON values are not allowed")
	}
	return &report, nil
}

func Validate(report *Report) error {
	var problems []string

	requireString(&problems, "schema_version", report.SchemaVersion)
	requireString(&problems, "requirement_id", report.RequirementID)
	requireString(&problems, "gate_id", report.GateID)
	requireString(&problems, "gate_name", report.GateName)
	requireString(&problems, "stage", report.Stage)
	requireString(&problems, "checked_by", report.CheckedBy)
	requireString(&problems, "checked_at", report.CheckedAt)
	requireString(&problems, "result", report.Result)
	requireString(&problems, "decision", report.Decision)

	if report.CheckedAt != "" {
		if _, err := time.Parse(time.RFC3339, report.CheckedAt); err != nil {
			problems = append(problems, "checked_at must be RFC3339")
		}
	}

	validateArtifacts(&problems, "inputs", report.Inputs, true)
	validateChecklist(&problems, report.Checklist)
	validateResult(&problems, report)
	validateWaiver(&problems, report)
	validateArtifacts(&problems, "evidence", report.Evidence, false)
	validateIDLImpact(&problems, report.IDLImpact)

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func validateResult(problems *[]string, report *Report) {
	switch report.Result {
	case ResultPass:
		if report.BlocksNextStage {
			*problems = append(*problems, "PASS must set blocks_next_stage to false")
		}
	case ResultWarn:
		if report.BlocksNextStage {
			*problems = append(*problems, "WARN must set blocks_next_stage to false")
		}
		if len(report.Warnings) == 0 {
			*problems = append(*problems, "WARN must include warnings")
		}
		for i, warning := range report.Warnings {
			requireString(problems, fmt.Sprintf("warnings[%d].issue", i), warning.Issue)
			requireString(problems, fmt.Sprintf("warnings[%d].follow_up_action", i), warning.FollowUpAction)
		}
	case ResultBlocked:
		if !report.BlocksNextStage {
			*problems = append(*problems, "BLOCKED must set blocks_next_stage to true")
		}
		if len(report.BlockingIssues) == 0 {
			*problems = append(*problems, "BLOCKED must include blocking_issues")
		}
	case ResultWaived:
		if report.BlocksNextStage {
			*problems = append(*problems, "WAIVED must set blocks_next_stage to false")
		}
	default:
		if report.Result != "" {
			*problems = append(*problems, "result must be PASS, BLOCKED, WARN, or WAIVED")
		}
	}

	for i, issue := range report.BlockingIssues {
		requireString(problems, fmt.Sprintf("blocking_issues[%d].issue", i), issue.Issue)
		requireString(problems, fmt.Sprintf("blocking_issues[%d].required_action", i), issue.RequiredAction)
		requireString(problems, fmt.Sprintf("blocking_issues[%d].owner", i), issue.Owner)
	}
}

func validateWaiver(problems *[]string, report *Report) {
	if report.Result != ResultWaived {
		return
	}

	if !report.Waiver.Required {
		*problems = append(*problems, "WAIVED must set waiver.required to true")
	}
	requireString(problems, "waiver.reason", report.Waiver.Reason)
	requireString(problems, "waiver.approver", report.Waiver.Approver)
	requireString(problems, "waiver.approved_at", report.Waiver.ApprovedAt)
	requireString(problems, "waiver.expires_at", report.Waiver.ExpiresAt)
	requireString(problems, "waiver.follow_up_issue", report.Waiver.FollowUpIssue)

	if report.Waiver.ApprovedAt != "" {
		if _, err := time.Parse(time.RFC3339, report.Waiver.ApprovedAt); err != nil {
			*problems = append(*problems, "waiver.approved_at must be RFC3339")
		}
	}
	if report.Waiver.ExpiresAt != "" {
		if _, err := time.Parse(time.RFC3339, report.Waiver.ExpiresAt); err != nil {
			*problems = append(*problems, "waiver.expires_at must be RFC3339")
		}
	}
}

func validateArtifacts(problems *[]string, field string, artifacts []Artifact, required bool) {
	if required && len(artifacts) == 0 {
		*problems = append(*problems, field+" must include at least one item")
		return
	}

	for i, artifact := range artifacts {
		requireString(problems, fmt.Sprintf("%s[%d].path", field, i), artifact.Path)
		requireString(problems, fmt.Sprintf("%s[%d].sha256", field, i), artifact.SHA256)
		if artifact.SHA256 != "" && len(artifact.SHA256) != 64 {
			*problems = append(*problems, fmt.Sprintf("%s[%d].sha256 must be 64 hex characters", field, i))
		}
		if artifact.SHA256 != "" {
			if _, err := hex.DecodeString(artifact.SHA256); err != nil {
				*problems = append(*problems, fmt.Sprintf("%s[%d].sha256 must be 64 hex characters", field, i))
			}
		}
	}
}

func validateChecklist(problems *[]string, checklist []ChecklistItem) {
	for i, item := range checklist {
		requireString(problems, fmt.Sprintf("checklist[%d].item", i), item.Item)
		requireString(problems, fmt.Sprintf("checklist[%d].result", i), item.Result)
		if item.Result != "" {
			switch item.Result {
			case ResultPass, ResultBlocked, ResultWarn, ResultWaived:
			default:
				*problems = append(*problems, fmt.Sprintf("checklist[%d].result must be PASS, BLOCKED, WARN, or WAIVED", i))
			}
		}
		requireString(problems, fmt.Sprintf("checklist[%d].evidence", i), item.Evidence)
	}
}

func validateIDLImpact(problems *[]string, impact *IDLImpact) {
	if impact == nil {
		return
	}

	switch impact.Impact {
	case "yes":
	case "no":
		requireString(problems, "idl_impact.na_reason", impact.NAReason)
	case "":
		*problems = append(*problems, "idl_impact.impact is required")
	default:
		*problems = append(*problems, "idl_impact.impact must be yes or no")
	}
}

func requireString(problems *[]string, field string, value string) {
	if strings.TrimSpace(value) == "" {
		*problems = append(*problems, field+" is required")
	}
}
