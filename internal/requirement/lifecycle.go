package requirement

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"janus/internal/gate"
)

const (
	GateRequirementReview = "requirement-review"
	GateDesignReview      = "design-review"
	GateDevEntry          = "dev-entry"
	GateServiceRepoCheck  = "service-repo-check"
	GateMergeReadiness    = "merge-readiness"
)

var allGateIDs = []string{
	GateRequirementReview,
	GateDesignReview,
	GateDevEntry,
	GateServiceRepoCheck,
	GateMergeReadiness,
}

type LifecycleError struct {
	Code     int
	Problems []string
}

func (e *LifecycleError) Error() string {
	return strings.Join(e.Problems, "\n")
}

type NewOptions struct {
	Title string
	Owner string
	Force bool
}

type Status struct {
	RequirementID string
	CurrentStage  string
	NextAction    string
	Artifacts     []ArtifactStatus
	Gates         []GateStatus
	Problems      []string
}

type ArtifactStatus struct {
	Path    string
	Exists  bool
	Problem string
}

type GateStatus struct {
	GateID          string
	Stage           string
	Path            string
	Exists          bool
	Valid           bool
	Result          string
	BlocksNextStage bool
	Stale           bool
	Problems        []string
}

type GateCheckOptions struct {
	GateID string
	Owner  string
	Now    time.Time
}

type GateCheckResult struct {
	JSONPath string
	Report   *gate.Report
}

type approval struct {
	Source     string
	Status     string
	ApprovedBy string
	ApprovedAt string
	Decision   string
}

type NextResult struct {
	RequirementID string
	PreviousStage string
	CurrentStage  string
	GateID        string
}

type tasksFile struct {
	RequirementID string     `json:"requirement_id"`
	Status        string     `json:"status"`
	ApprovedBy    string     `json:"approved_by"`
	ApprovedAt    string     `json:"approved_at"`
	Decision      string     `json:"decision"`
	Tasks         []taskItem `json:"tasks"`
}

type taskItem struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	Scope            string    `json:"scope"`
	Trace            taskTrace `json:"trace"`
	AffectedServices []string  `json:"affected_services"`
	Acceptance       []string  `json:"acceptance"`
	State            string    `json:"state"`
	LegacyStatus     string    `json:"status,omitempty"`
}

type taskTrace struct {
	RequirementItems []string `json:"requirement_items"`
	DesignDecisions  []string `json:"design_decisions"`
}

type serviceMatrix struct {
	Workspace    string                   `json:"workspace"`
	BusinessRepo string                   `json:"business_repo"`
	IDLRepo      string                   `json:"idl_repo"`
	Services     map[string]matrixService `json:"services"`
}

type matrixService struct {
	Module      string `json:"module"`
	RepoPath    string `json:"repo_path"`
	IDLRequired bool   `json:"idl_required"`
	IDLRepo     string `json:"idl_repo"`
	ProtoPath   string `json:"proto_path"`
}

func Create(root string, requirementID string, options NewOptions, now time.Time) error {
	requirementID = strings.TrimSpace(requirementID)
	if err := validateRequirementID(requirementID); err != nil {
		return err
	}

	requirementRoot := filepath.Join(root, "requirements", requirementID)
	if _, err := os.Stat(requirementRoot); err == nil && !options.Force {
		return &LifecycleError{Code: gate.VerifyInvalid, Problems: []string{fmt.Sprintf("requirement already exists: %s", requirementID)}}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return &LifecycleError{Code: gate.VerifyMissing, Problems: []string{fmt.Sprintf("cannot inspect requirement: %v", err)}}
	}

	for _, dir := range []string{
		requirementRoot,
		filepath.Join(requirementRoot, "gates"),
		filepath.Join(requirementRoot, "reviews"),
		filepath.Join(requirementRoot, "evidence"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return &LifecycleError{Code: gate.VerifyMissing, Problems: []string{fmt.Sprintf("cannot create %s: %v", dir, err)}}
		}
	}

	title := strings.TrimSpace(options.Title)
	if title == "" {
		title = requirementID
	}
	owner := strings.TrimSpace(options.Owner)
	if owner == "" {
		owner = "Harness Team"
	}
	date := now.Format("2006-01-02")

	templates := map[string]string{
		"requirement.md":     filepath.Join(root, "context", "harness-framework", "templates", "requirement.md"),
		"impact-analysis.md": filepath.Join(root, "context", "harness-framework", "templates", "impact-analysis.md"),
		"design.md":          filepath.Join(root, "context", "harness-framework", "templates", "design.md"),
		"tasks.json":         filepath.Join(root, "context", "harness-framework", "templates", "tasks.json"),
	}
	for name, source := range templates {
		target := filepath.Join(requirementRoot, name)
		if _, err := os.Stat(target); err == nil && !options.Force {
			continue
		}
		content, err := os.ReadFile(source)
		if err != nil {
			return &LifecycleError{Code: gate.VerifyMissing, Problems: []string{fmt.Sprintf("cannot read template %s: %v", source, err)}}
		}
		rendered := renderTemplate(string(content), requirementID, title, owner, date)
		if err := os.WriteFile(target, []byte(rendered), 0o644); err != nil {
			return &LifecycleError{Code: gate.VerifyMissing, Problems: []string{fmt.Sprintf("cannot write %s: %v", target, err)}}
		}
	}

	readme := requirementReadme(requirementID, title, owner, date, "1")
	readmePath := filepath.Join(requirementRoot, "README.md")
	if _, err := os.Stat(readmePath); errors.Is(err, os.ErrNotExist) || options.Force {
		if err := os.WriteFile(readmePath, []byte(readme), 0o644); err != nil {
			return &LifecycleError{Code: gate.VerifyMissing, Problems: []string{fmt.Sprintf("cannot write %s: %v", readmePath, err)}}
		}
	}

	return nil
}

func Inspect(root string, requirementID string, now time.Time) (*Status, error) {
	requirementID = strings.TrimSpace(requirementID)
	if err := validateRequirementID(requirementID); err != nil {
		return nil, err
	}

	requirementRoot := filepath.Join(root, "requirements", requirementID)
	info, err := os.Stat(requirementRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, &LifecycleError{Code: gate.VerifyMissing, Problems: []string{fmt.Sprintf("missing requirement: %s", requirementID)}}
		}
		return nil, &LifecycleError{Code: gate.VerifyMissing, Problems: []string{fmt.Sprintf("cannot inspect requirement: %v", err)}}
	}
	if !info.IsDir() {
		return nil, &LifecycleError{Code: gate.VerifyInvalid, Problems: []string{fmt.Sprintf("requirement path is not a directory: %s", requirementRoot)}}
	}

	status := &Status{
		RequirementID: requirementID,
		CurrentStage:  readCurrentStage(filepath.Join(requirementRoot, "README.md")),
	}

	for _, relative := range requiredArtifacts() {
		path := filepath.Join(requirementRoot, filepath.FromSlash(relative))
		artifact := ArtifactStatus{Path: filepath.ToSlash(filepath.Join("requirements", requirementID, relative))}
		if _, err := os.Stat(path); err != nil {
			artifact.Exists = false
			if errors.Is(err, os.ErrNotExist) {
				artifact.Problem = "missing"
				status.Problems = append(status.Problems, fmt.Sprintf("missing artifact: %s", artifact.Path))
			} else {
				artifact.Problem = err.Error()
				status.Problems = append(status.Problems, fmt.Sprintf("cannot inspect %s: %v", artifact.Path, err))
			}
		} else {
			artifact.Exists = true
			if problem := artifactStatusProblem(path, artifact.Path); problem != "" {
				artifact.Problem = problem
				status.Problems = append(status.Problems, problem)
			}
		}
		status.Artifacts = append(status.Artifacts, artifact)
	}

	for _, gateID := range allGateIDs {
		gateStatus := inspectGate(root, requirementID, gateID, now)
		status.Gates = append(status.Gates, gateStatus)
	}

	if status.CurrentStage == "" || status.CurrentStage == "1" && hasAnyGate(status.Gates) {
		status.CurrentStage = inferCurrentStage(status.Gates)
	}
	for _, gateStatus := range status.Gates {
		if gateStatus.Exists && gateAppliesAtStage(status.CurrentStage, gateStatus.GateID) && len(gateStatus.Problems) > 0 {
			status.Problems = append(status.Problems, gateStatus.Problems...)
		}
	}
	status.NextAction = nextAction(status)

	return status, nil
}

func RunGateCheck(root string, requirementID string, options GateCheckOptions) (*GateCheckResult, error) {
	requirementID = strings.TrimSpace(requirementID)
	if err := validateRequirementID(requirementID); err != nil {
		return nil, err
	}
	def, ok := gateDefinition(options.GateID)
	if !ok {
		return nil, &LifecycleError{Code: gate.VerifyInvalid, Problems: []string{fmt.Sprintf("unknown gate: %s", options.GateID)}}
	}

	checkedAt := options.Now
	if checkedAt.IsZero() {
		checkedAt = time.Now()
	}
	owner := strings.TrimSpace(options.Owner)
	if owner == "" {
		owner = "Human Reviewer"
	}

	var problems []string
	inputs := collectInputs(root, requirementID, def.Inputs, &problems)
	checklist := runChecklist(root, requirementID, def.GateID, &problems)
	var evidence []gate.Artifact
	if def.GateID == GateMergeReadiness {
		evidence = collectEvidence(root, requirementID)
	}
	idlImpact := inferIDLImpact(root, requirementID)
	repos := collectRepos(root, def.GateID, idlImpact)
	approval := readApproval(root, requirementID, def.GateID)
	if approval.Source != "" && approval.Status != "" && !isLifecycleStatus(approval.Status) {
		problems = append(problems, fmt.Sprintf("%s status 必须是 %s", approval.Source, lifecycleStatusValuesText()))
	}

	for _, problem := range problems {
		checklist = append(checklist, gate.ChecklistItem{
			Item:     problem,
			Result:   gate.ResultBlocked,
			Evidence: problem,
		})
	}

	blockingIssues := make([]gate.BlockingIssue, 0, len(problems)+1)
	for _, problem := range problems {
		blockingIssues = append(blockingIssues, gate.BlockingIssue{
			Issue:          problem,
			RequiredAction: "补齐或修正门禁输入后重新运行 gate-check。",
			Owner:          "Harness Team",
		})
	}

	result := gate.ResultBlocked
	blocksNextStage := true
	decision := fmt.Sprintf("机器检查完成，但 %s 缺少人工批准，不能放行下一阶段。", def.Name)
	if len(problems) == 0 && approval.Status == LifecycleStatusApproved && approval.ApprovedBy != "" && approval.ApprovedAt != "" && approval.Decision != "" {
		result = gate.ResultPass
		blocksNextStage = false
		decision = approval.Decision
		checklist = append(checklist, gate.ChecklistItem{
			Item:     "人工批准记录合法",
			Result:   gate.ResultPass,
			Evidence: fmt.Sprintf("%s approved by %s at %s.", approval.Source, approval.ApprovedBy, approval.ApprovedAt),
		})
	} else if len(problems) == 0 {
		issue := approvalBlockingIssue(def, owner, approval)
		blockingIssues = append(blockingIssues, issue)
		checklist = append(checklist, gate.ChecklistItem{
			Item:     "人工批准记录合法",
			Result:   gate.ResultBlocked,
			Evidence: issue.Issue,
		})
	}

	report := &gate.Report{
		SchemaVersion:   "1",
		RequirementID:   requirementID,
		GateID:          def.GateID,
		GateName:        def.Name,
		Stage:           def.Stage,
		CheckedBy:       def.CheckedBy,
		CheckedAt:       checkedAt.Format(time.RFC3339),
		Result:          result,
		BlocksNextStage: blocksNextStage,
		Inputs:          inputs,
		Checklist:       checklist,
		BlockingIssues:  blockingIssues,
		Warnings:        nil,
		Waiver:          gate.Waiver{Required: false},
		Repos:           repos,
		Evidence:        evidence,
		IDLImpact:       idlImpact,
		Decision:        decision,
	}

	if err := gate.Validate(report); err != nil {
		return nil, err
	}

	gateDir := filepath.Join(root, "requirements", requirementID, "gates")
	if err := os.MkdirAll(gateDir, 0o755); err != nil {
		return nil, &LifecycleError{Code: gate.VerifyMissing, Problems: []string{fmt.Sprintf("cannot create gates dir: %v", err)}}
	}

	jsonPath := filepath.Join(gateDir, def.GateID+".gate.json")
	if err := writeGateJSON(jsonPath, report); err != nil {
		return nil, err
	}

	return &GateCheckResult{
		JSONPath: filepath.ToSlash(filepath.Join("requirements", requirementID, "gates", def.GateID+".gate.json")),
		Report:   report,
	}, nil
}

func Advance(root string, requirementID string, now time.Time, ticketID string) (*NextResult, error) {
	status, err := Inspect(root, requirementID, now)
	if err != nil {
		return nil, err
	}
	current := status.CurrentStage
	next, gateID, ok := nextStage(current)
	if !ok {
		return nil, &LifecycleError{Code: gate.VerifyInvalid, Problems: []string{fmt.Sprintf("requirement %s is already at final stage or has unknown stage %q", requirementID, current)}}
	}
	if gateID != "" {
		gatePath := filepath.Join(root, "requirements", requirementID, "gates", gateID+".gate.json")
		report, err := readGate(gatePath)
		if err != nil {
			var verifyErr *VerifyError
			if errors.As(err, &verifyErr) {
				return nil, &LifecycleError{Code: verifyErr.Code, Problems: verifyErr.Problems}
			}
			return nil, &LifecycleError{Code: gate.VerifyInvalid, Problems: []string{err.Error()}}
		}
		effectiveTicketID := strings.TrimSpace(ticketID)
		if effectiveTicketID == "" {
			effectiveTicketID = requirementID
		}
		if err := gate.Verify(report, root, now, gate.VerifyOptions{TicketID: effectiveTicketID}); err != nil {
			var verifyErr *gate.VerifyError
			if errors.As(err, &verifyErr) {
				return nil, &LifecycleError{Code: verifyErr.Code, Problems: prefixProblems(gatePath, verifyErr.Problems)}
			}
			return nil, &LifecycleError{Code: gate.VerifyInvalid, Problems: []string{err.Error()}}
		}
	}
	if err := writeCurrentStage(filepath.Join(root, "requirements", requirementID, "README.md"), next); err != nil {
		return nil, err
	}
	return &NextResult{
		RequirementID: requirementID,
		PreviousStage: current,
		CurrentStage:  next,
		GateID:        gateID,
	}, nil
}

type gateDef struct {
	GateID    string
	Name      string
	Stage     string
	CheckedBy string
	Inputs    []string
}

func gateDefinition(gateID string) (gateDef, bool) {
	switch strings.TrimSpace(gateID) {
	case GateRequirementReview:
		return gateDef{GateID: GateRequirementReview, Name: "需求评审门禁", Stage: "2.2", CheckedBy: "requirement_reviewer", Inputs: []string{"requirement.md", "impact-analysis.md"}}, true
	case GateDesignReview:
		return gateDef{GateID: GateDesignReview, Name: "设计门禁", Stage: "3.3", CheckedBy: "design_reviewer", Inputs: []string{"requirement.md", "impact-analysis.md", "design.md"}}, true
	case GateDevEntry:
		return gateDef{GateID: GateDevEntry, Name: "Dev 进入门禁", Stage: "4.2", CheckedBy: "dev_entry_checker", Inputs: []string{"design.md", "tasks.json"}}, true
	case GateServiceRepoCheck:
		return gateDef{GateID: GateServiceRepoCheck, Name: "服务仓库检查门禁", Stage: "4.3", CheckedBy: "service_repo_checker", Inputs: []string{"impact-analysis.md", "design.md", "tasks.json", "../../.service-matrix/dependencies.yaml"}}, true
	case GateMergeReadiness:
		return gateDef{GateID: GateMergeReadiness, Name: "合并就绪门禁", Stage: "5.1", CheckedBy: "merge_readiness_checker", Inputs: []string{
			"requirement.md",
			"impact-analysis.md",
			"design.md",
			"tasks.json",
			"gates/requirement-review.gate.json",
			"gates/design-review.gate.json",
			"gates/dev-entry.gate.json",
			"gates/service-repo-check.gate.json",
			"../../.service-matrix/dependencies.yaml",
		}}, true
	default:
		return gateDef{}, false
	}
}

func readApproval(root string, requirementID string, gateID string) approval {
	source := approvalSource(requirementID, gateID)
	if source == "" {
		return approval{}
	}
	path := filepath.Join(root, filepath.FromSlash(source))
	content, err := os.ReadFile(path)
	if err != nil {
		return approval{Source: source}
	}
	if strings.HasSuffix(source, ".json") {
		var tasks tasksFile
		if json.Unmarshal(content, &tasks) == nil {
			return approval{
				Source:     source,
				Status:     normalizeLifecycleStatus(tasks.Status),
				ApprovedBy: tasks.ApprovedBy,
				ApprovedAt: tasks.ApprovedAt,
				Decision:   tasks.Decision,
			}
		}
	}
	frontMatter := parseFrontMatter(string(content))
	prefix := strings.ReplaceAll(gateID, "-", "_")
	return approval{
		Source:     source,
		Status:     normalizeLifecycleStatus(firstNonEmpty(frontMatter["status"], frontMatter[prefix+"_status"], frontMatter["gate_status"])),
		ApprovedBy: firstNonEmpty(frontMatter["approved_by"], frontMatter[prefix+"_approved_by"]),
		ApprovedAt: firstNonEmpty(frontMatter["approved_at"], frontMatter[prefix+"_approved_at"]),
		Decision:   firstNonEmpty(frontMatter["decision"], frontMatter[prefix+"_decision"]),
	}
}

func approvalSource(requirementID string, gateID string) string {
	switch gateID {
	case GateRequirementReview:
		return filepath.ToSlash(filepath.Join("requirements", requirementID, "requirement.md"))
	case GateDesignReview:
		return filepath.ToSlash(filepath.Join("requirements", requirementID, "design.md"))
	case GateDevEntry:
		return filepath.ToSlash(filepath.Join("requirements", requirementID, "tasks.json"))
	case GateServiceRepoCheck:
		return filepath.ToSlash(filepath.Join("requirements", requirementID, "impact-analysis.md"))
	case GateMergeReadiness:
		return filepath.ToSlash(filepath.Join("requirements", requirementID, "tasks.json"))
	default:
		return ""
	}
}

func approvalBlockingIssue(def gateDef, owner string, approval approval) gate.BlockingIssue {
	if approval.Source == "" {
		return gate.BlockingIssue{
			Issue:          fmt.Sprintf("%s 已完成机器检查，但该门禁暂不支持 Markdown approval 自动放行。", def.Name),
			RequiredAction: fmt.Sprintf("请负责人确认 %s 门禁输入、风险和证据后，按评审结果手动更新 gate JSON 为 PASS 或 WAIVED。", def.GateID),
			Owner:          owner,
		}
	}
	missing := []string{}
	if approval.Status != LifecycleStatusApproved {
		missing = append(missing, "status: \"approved\"")
	}
	if approval.ApprovedBy == "" {
		missing = append(missing, "approved_by")
	}
	if approval.ApprovedAt == "" {
		missing = append(missing, "approved_at")
	}
	if approval.Decision == "" {
		missing = append(missing, "decision")
	}
	return gate.BlockingIssue{
		Issue:          fmt.Sprintf("%s 已完成机器检查，但 %s 缺少合法人工批准字段：%s。", def.Name, approval.Source, strings.Join(missing, ", ")),
		RequiredAction: fmt.Sprintf("在 %s 中补齐批准字段后重新运行 gate-check。", approval.Source),
		Owner:          owner,
	}
}

func parseFrontMatter(content string) map[string]string {
	values := map[string]string{}
	if !strings.HasPrefix(content, "---\n") {
		return values
	}
	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return values
	}
	block := content[4 : 4+end]
	for _, line := range strings.Split(block, "\n") {
		key, value, ok := splitYAMLScalar(strings.TrimSpace(line))
		if ok && key != "" {
			values[key] = value
		}
	}
	return values
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func requiredArtifacts() []string {
	return []string{"README.md", "requirement.md", "impact-analysis.md", "design.md", "tasks.json"}
}

func artifactStatusProblem(path string, displayPath string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var statusValue string
	if strings.HasSuffix(path, ".json") {
		var tasks tasksFile
		if json.Unmarshal(content, &tasks) != nil {
			return ""
		}
		statusValue = normalizeLifecycleStatus(tasks.Status)
	} else {
		statusValue = normalizeLifecycleStatus(parseFrontMatter(string(content))["status"])
	}
	if statusValue == "" || isLifecycleStatus(statusValue) {
		return ""
	}
	return fmt.Sprintf("%s status 必须是 %s", displayPath, lifecycleStatusValuesText())
}

func validateRequirementID(requirementID string) error {
	if requirementID == "" {
		return &LifecycleError{Code: gate.VerifyInvalid, Problems: []string{"requirement id is required"}}
	}
	matched, _ := regexp.MatchString(`^[A-Za-z0-9][A-Za-z0-9._-]*$`, requirementID)
	if !matched {
		return &LifecycleError{Code: gate.VerifyInvalid, Problems: []string{"requirement id may only contain letters, numbers, dot, underscore, and dash"}}
	}
	return nil
}

func renderTemplate(content string, requirementID string, title string, owner string, date string) string {
	replacements := map[string]string{
		`requirement_id: ""`:    fmt.Sprintf(`requirement_id: "%s"`, requirementID),
		`owner: ""`:             fmt.Sprintf(`owner: "%s"`, owner),
		`analyst: ""`:           fmt.Sprintf(`analyst: "%s"`, owner),
		`created_at: ""`:        fmt.Sprintf(`created_at: "%s"`, date),
		`updated_at: ""`:        fmt.Sprintf(`updated_at: "%s"`, date),
		`# {Requirement Title}`: "# " + title,
		`"requirement_id": ""`:  fmt.Sprintf(`"requirement_id": "%s"`, requirementID),
		`"status": "draft"`:     `"status": "draft"`,
		`"status": "Draft"`:     `"status": "draft"`,
		`related_branch: ""`:    `related_branch: ""`,
	}
	for from, to := range replacements {
		content = strings.ReplaceAll(content, from, to)
	}
	return content
}

func requirementReadme(requirementID string, title string, owner string, date string, stage string) string {
	return fmt.Sprintf(`---
requirement_id: "%s"
owner: "%s"
current_stage: "%s"
status: "draft"
created_at: "%s"
---

# %s

## Lifecycle Artifacts

- requirement.md
- impact-analysis.md
- design.md
- tasks.json
- gates/
- reviews/
- evidence/
`, requirementID, owner, stage, date, title)
}

func readCurrentStage(readmePath string) string {
	content, err := os.ReadFile(readmePath)
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`(?m)^current_stage:\s*"?([^"\n]+)"?\s*$`)
	match := re.FindStringSubmatch(string(content))
	if len(match) == 2 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func writeCurrentStage(readmePath string, stage string) error {
	contentBytes, err := os.ReadFile(readmePath)
	if err != nil {
		return &LifecycleError{Code: gate.VerifyMissing, Problems: []string{fmt.Sprintf("cannot read README.md: %v", err)}}
	}
	content := string(contentBytes)
	re := regexp.MustCompile(`(?m)^current_stage:\s*"?[^"\n]+"?\s*$`)
	line := fmt.Sprintf(`current_stage: "%s"`, stage)
	if re.MatchString(content) {
		content = re.ReplaceAllString(content, line)
	} else if strings.HasPrefix(content, "---\n") {
		end := strings.Index(content[4:], "\n---")
		if end >= 0 {
			insertAt := 4 + end
			content = content[:insertAt] + "\n" + line + content[insertAt:]
		} else {
			content = line + "\n" + content
		}
	} else {
		content = "---\n" + line + "\n---\n\n" + content
	}
	if err := os.WriteFile(readmePath, []byte(content), 0o644); err != nil {
		return &LifecycleError{Code: gate.VerifyMissing, Problems: []string{fmt.Sprintf("cannot write README.md: %v", err)}}
	}
	return nil
}

func inspectGate(root string, requirementID string, gateID string, now time.Time) GateStatus {
	def, _ := gateDefinition(gateID)
	jsonRelative := filepath.ToSlash(filepath.Join("requirements", requirementID, "gates", gateID+".gate.json"))
	jsonPath := filepath.Join(root, filepath.FromSlash(jsonRelative))
	status := GateStatus{
		GateID: gateID,
		Stage:  def.Stage,
		Path:   jsonRelative,
	}
	report, err := readGate(jsonPath)
	if err != nil {
		status.Exists = false
		status.Problems = append(status.Problems, fmt.Sprintf("missing or invalid gate: %s", jsonRelative))
		return status
	}
	status.Exists = true
	status.Result = report.Result
	status.BlocksNextStage = report.BlocksNextStage
	if err := gate.Validate(report); err != nil {
		status.Valid = false
		status.Problems = append(status.Problems, fmt.Sprintf("invalid gate %s: %v", jsonRelative, err))
		return status
	}
	status.Valid = true
	if problems := staleProblems(root, report.Inputs); len(problems) > 0 {
		status.Stale = true
		status.Problems = append(status.Problems, prefixProblems(jsonRelative, problems)...)
	}
	if problems := staleProblems(root, report.Evidence); len(problems) > 0 {
		status.Stale = true
		status.Problems = append(status.Problems, prefixProblems(jsonRelative, problems)...)
	}
	if report.Result == gate.ResultWaived {
		if err := gate.Verify(report, root, now, gate.VerifyOptions{TicketID: requirementID}); err != nil {
			status.Problems = append(status.Problems, fmt.Sprintf("waiver is not currently valid: %v", err))
		}
	}
	return status
}

func inferCurrentStage(gates []GateStatus) string {
	stage := "1"
	for _, gateStatus := range gates {
		if !gateStatus.Valid {
			continue
		}
		if gateStatus.Result == gate.ResultPass || gateStatus.Result == gate.ResultWarn || gateStatus.Result == gate.ResultWaived {
			switch gateStatus.GateID {
			case GateRequirementReview:
				stage = "3"
			case GateDesignReview:
				stage = "4.1"
			case GateDevEntry:
				stage = "4.3"
			case GateServiceRepoCheck:
				stage = "4.4"
			}
		}
	}
	return stage
}

func hasAnyGate(gates []GateStatus) bool {
	for _, gateStatus := range gates {
		if gateStatus.Exists {
			return true
		}
	}
	return false
}

func nextAction(status *Status) string {
	for _, artifact := range status.Artifacts {
		if !artifact.Exists {
			return "补齐缺失生命周期产物。"
		}
	}
	for _, gateStatus := range status.Gates {
		if !gateAppliesAtStage(status.CurrentStage, gateStatus.GateID) {
			continue
		}
		if !gateStatus.Exists {
			return "当前阶段完成并批准后，运行 gate-check 生成对应门禁。"
		}
		if gateStatus.Stale {
			return "重新运行 gate-check 刷新过期门禁 JSON。"
		}
		if gateStatus.Result == gate.ResultBlocked {
			return "处理 BLOCKED 门禁或补充人工批准。"
		}
	}
	if stageRank(status.CurrentStage) < stageRank("5") {
		return "继续当前阶段；满足条件后运行 requirement next。"
	}
	return "运行 requirement verify --target merge。"
}

func gateAppliesAtStage(currentStage string, gateID string) bool {
	return stageRank(currentStage) >= gateDueRank(gateID)
}

func gateDueRank(gateID string) int {
	switch gateID {
	case GateRequirementReview:
		return stageRank("2")
	case GateDesignReview:
		return stageRank("3")
	case GateDevEntry:
		return stageRank("4.2")
	case GateServiceRepoCheck:
		return stageRank("4.3")
	case GateMergeReadiness:
		return stageRank("5")
	default:
		return 999
	}
}

func stageRank(stage string) int {
	switch strings.TrimSpace(stage) {
	case "", "1":
		return 10
	case "2":
		return 20
	case "3":
		return 30
	case "4.1":
		return 41
	case "4.2":
		return 42
	case "4.3":
		return 43
	case "4.4":
		return 44
	case "5":
		return 50
	case "5.1":
		return 51
	default:
		return 999
	}
}

func collectInputs(root string, requirementID string, relatives []string, problems *[]string) []gate.Artifact {
	var artifacts []gate.Artifact
	for _, relative := range relatives {
		path := resolveRequirementRelative(root, requirementID, relative)
		displayPath := displayRequirementRelative(requirementID, relative)
		hash, err := fileSHA256(path)
		if err != nil {
			*problems = append(*problems, fmt.Sprintf("missing or unreadable input: %s", displayPath))
			continue
		}
		artifacts = append(artifacts, gate.Artifact{Path: displayPath, SHA256: hash})
	}
	return artifacts
}

func resolveRequirementRelative(root string, requirementID string, relative string) string {
	clean := filepath.Clean(filepath.FromSlash(relative))
	if strings.HasPrefix(clean, "..") {
		return filepath.Clean(filepath.Join(root, "requirements", requirementID, clean))
	}
	return filepath.Join(root, "requirements", requirementID, clean)
}

func displayRequirementRelative(requirementID string, relative string) string {
	if strings.HasPrefix(relative, "../") {
		path := filepath.Clean(filepath.Join("requirements", requirementID, filepath.FromSlash(relative)))
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(filepath.Join("requirements", requirementID, filepath.FromSlash(relative)))
}

func runChecklist(root string, requirementID string, gateID string, problems *[]string) []gate.ChecklistItem {
	switch gateID {
	case GateRequirementReview:
		return []gate.ChecklistItem{
			checkFileContains(root, requirementID, "requirement.md", "需求文档存在", []string{"## Background", "## Goals", "## Non-Goals", "## Acceptance Criteria"}),
			checkFileContains(root, requirementID, "impact-analysis.md", "影响面分析存在", []string{"## Affected Services", "## API / Contract Impact", "## Rollout And Rollback"}),
		}
	case GateDesignReview:
		return []gate.ChecklistItem{
			checkFileContains(root, requirementID, "design.md", "设计覆盖关键章节", []string{"## Requirement Traceability", "## Affected Services", "## API / Contract Design", "## Rollout And Rollback"}),
			checkFileContains(root, requirementID, "impact-analysis.md", "影响面分析可追溯", []string{"## Affected Services", "## API / Contract Impact"}),
		}
	case GateDevEntry:
		return checkTasks(root, requirementID, problems)
	case GateServiceRepoCheck:
		return checkServiceRepo(root, requirementID, problems)
	case GateMergeReadiness:
		return checkMergeReadiness(root, requirementID, problems)
	default:
		return nil
	}
}

func checkFileContains(root string, requirementID string, relative string, item string, markers []string) gate.ChecklistItem {
	path := filepath.Join(root, "requirements", requirementID, filepath.FromSlash(relative))
	content, err := os.ReadFile(path)
	if err != nil {
		return gate.ChecklistItem{Item: item, Result: gate.ResultBlocked, Evidence: "missing " + relative}
	}
	var missing []string
	text := string(content)
	for _, marker := range markers {
		if !strings.Contains(text, marker) {
			missing = append(missing, marker)
		}
	}
	if len(missing) > 0 {
		return gate.ChecklistItem{Item: item, Result: gate.ResultBlocked, Evidence: "missing markers: " + strings.Join(missing, ", ")}
	}
	return gate.ChecklistItem{Item: item, Result: gate.ResultPass, Evidence: relative + " contains required sections."}
}

func checkTasks(root string, requirementID string, problems *[]string) []gate.ChecklistItem {
	path := filepath.Join(root, "requirements", requirementID, "tasks.json")
	data, err := os.ReadFile(path)
	if err != nil {
		*problems = append(*problems, "tasks.json 缺失")
		return []gate.ChecklistItem{{Item: "tasks.json 存在且格式合法", Result: gate.ResultBlocked, Evidence: "missing tasks.json"}}
	}
	var tasks tasksFile
	if err := json.Unmarshal(data, &tasks); err != nil {
		*problems = append(*problems, "tasks.json 格式不合法")
		return []gate.ChecklistItem{{Item: "tasks.json 存在且格式合法", Result: gate.ResultBlocked, Evidence: err.Error()}}
	}
	checklist := []gate.ChecklistItem{{Item: "tasks.json 存在且格式合法", Result: gate.ResultPass, Evidence: "tasks.json parsed successfully."}}
	if tasks.RequirementID != requirementID {
		*problems = append(*problems, "tasks.json requirement_id 与需求目录不一致")
		checklist = append(checklist, gate.ChecklistItem{Item: "tasks.json requirement_id 与需求目录一致", Result: gate.ResultBlocked, Evidence: tasks.RequirementID})
	} else {
		checklist = append(checklist, gate.ChecklistItem{Item: "tasks.json requirement_id 与需求目录一致", Result: gate.ResultPass, Evidence: tasks.RequirementID})
	}
	if len(tasks.Tasks) == 0 {
		*problems = append(*problems, "tasks.json 没有任务")
		checklist = append(checklist, gate.ChecklistItem{Item: "任务拆分完整", Result: gate.ResultBlocked, Evidence: "tasks is empty"})
		return checklist
	}
	var taskProblems []string
	for _, task := range tasks.Tasks {
		if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.Scope) == "" || strings.TrimSpace(task.Title) == "" {
			taskProblems = append(taskProblems, "任务缺少 id/title/scope")
		}
		state := normalizeTaskState(firstNonEmpty(task.State, task.LegacyStatus))
		if state == "" {
			taskProblems = append(taskProblems, fmt.Sprintf("%s 缺少 state", task.ID))
		} else if !isTaskState(state) {
			taskProblems = append(taskProblems, fmt.Sprintf("%s state 必须是 %s", task.ID, taskStateValuesText()))
		}
		if len(task.Acceptance) == 0 {
			taskProblems = append(taskProblems, fmt.Sprintf("%s 缺少 acceptance", task.ID))
		}
		if len(task.Trace.RequirementItems) == 0 || len(task.Trace.DesignDecisions) == 0 {
			taskProblems = append(taskProblems, fmt.Sprintf("%s 缺少 trace", task.ID))
		}
	}
	if len(taskProblems) > 0 {
		*problems = append(*problems, taskProblems...)
		checklist = append(checklist, gate.ChecklistItem{Item: "每个任务有状态、范围、验收和追溯来源", Result: gate.ResultBlocked, Evidence: strings.Join(taskProblems, "; ")})
	} else {
		checklist = append(checklist, gate.ChecklistItem{Item: "每个任务有状态、范围、验收和追溯来源", Result: gate.ResultPass, Evidence: fmt.Sprintf("%d tasks include state, scope, acceptance, and trace.", len(tasks.Tasks))})
	}
	return checklist
}

func checkServiceRepo(root string, requirementID string, problems *[]string) []gate.ChecklistItem {
	checklist := checkTasks(root, requirementID, problems)
	matrix, matrixProblems := readServiceMatrix(root)
	if len(matrixProblems) > 0 {
		*problems = append(*problems, matrixProblems...)
		checklist = append(checklist, gate.ChecklistItem{Item: "服务矩阵可读取", Result: gate.ResultBlocked, Evidence: strings.Join(matrixProblems, "; ")})
		return checklist
	}
	checklist = append(checklist, gate.ChecklistItem{Item: "服务矩阵可读取", Result: gate.ResultPass, Evidence: ".service-matrix/dependencies.yaml parsed successfully."})

	affected := affectedServices(root, requirementID)
	var serviceProblems []string
	for _, service := range affected {
		entry, ok := matrix.Services[service]
		if !ok {
			serviceProblems = append(serviceProblems, fmt.Sprintf("服务不在服务矩阵中: %s", service))
			continue
		}
		if strings.TrimSpace(entry.RepoPath) == "" {
			serviceProblems = append(serviceProblems, fmt.Sprintf("%s repo_path 缺失", service))
		} else if _, err := os.Stat(resolveMatrixPath(root, matrix, entry.RepoPath)); err != nil {
			serviceProblems = append(serviceProblems, fmt.Sprintf("%s repo_path 不存在", service))
		}
		if entry.IDLRequired {
			if strings.TrimSpace(entry.ProtoPath) == "" {
				serviceProblems = append(serviceProblems, fmt.Sprintf("%s proto_path 缺失", service))
			} else if _, err := os.Stat(resolveMatrixPath(root, matrix, entry.ProtoPath)); err != nil {
				serviceProblems = append(serviceProblems, fmt.Sprintf("%s proto_path 不存在", service))
			}
		}
	}
	if len(serviceProblems) > 0 {
		*problems = append(*problems, serviceProblems...)
		checklist = append(checklist, gate.ChecklistItem{Item: "涉及服务存在且路径可解析", Result: gate.ResultBlocked, Evidence: strings.Join(serviceProblems, "; ")})
	} else {
		evidence := strings.Join(affected, ", ")
		if evidence == "" {
			evidence = "tasks.json 未声明业务服务；跳过服务矩阵路径校验。"
		}
		checklist = append(checklist, gate.ChecklistItem{Item: "涉及服务存在且路径可解析", Result: gate.ResultPass, Evidence: evidence})
	}
	return checklist
}

func checkMergeReadiness(root string, requirementID string, problems *[]string) []gate.ChecklistItem {
	checklist := []gate.ChecklistItem{}
	gateProblems := []string{}
	for _, gateID := range []string{GateRequirementReview, GateDesignReview, GateDevEntry, GateServiceRepoCheck} {
		reportPath := filepath.Join(root, "requirements", requirementID, "gates", gateID+".gate.json")
		report, err := readGate(reportPath)
		if err != nil {
			gateProblems = append(gateProblems, fmt.Sprintf("%s missing or invalid", gateID))
			continue
		}
		if report.Result == gate.ResultBlocked {
			gateProblems = append(gateProblems, fmt.Sprintf("%s is BLOCKED", gateID))
		}
	}
	if len(gateProblems) > 0 {
		*problems = append(*problems, gateProblems...)
		checklist = append(checklist, gate.ChecklistItem{Item: "阶段门禁已通过", Result: gate.ResultBlocked, Evidence: strings.Join(gateProblems, "; ")})
	} else {
		checklist = append(checklist, gate.ChecklistItem{Item: "阶段门禁已通过", Result: gate.ResultPass, Evidence: "requirement-review, design-review, dev-entry, and service-repo-check are present and not BLOCKED."})
	}

	evidence := collectEvidence(root, requirementID)
	if len(evidence) == 0 {
		*problems = append(*problems, "merge-readiness 缺少 evidence")
		checklist = append(checklist, gate.ChecklistItem{Item: "实现证据已记录", Result: gate.ResultBlocked, Evidence: "requirements/" + requirementID + "/evidence is empty or missing"})
	} else {
		checklist = append(checklist, gate.ChecklistItem{Item: "实现证据已记录", Result: gate.ResultPass, Evidence: fmt.Sprintf("%d evidence files recorded.", len(evidence))})
	}

	idlImpact := inferIDLImpact(root, requirementID)
	if idlImpact != nil && idlImpact.Impact == "yes" {
		if hasIDLEvidence(evidence) {
			checklist = append(checklist, gate.ChecklistItem{Item: "IDL 证据已记录", Result: gate.ResultPass, Evidence: "Buf or contract evidence exists."})
		} else {
			*problems = append(*problems, "IDL impact 为 yes 但缺少 Buf 或契约检查证据")
			checklist = append(checklist, gate.ChecklistItem{Item: "IDL 证据已记录", Result: gate.ResultBlocked, Evidence: "Expected evidence filename to include buf, idl, or contract."})
		}
	}

	return checklist
}

func hasIDLEvidence(evidence []gate.Artifact) bool {
	for _, artifact := range evidence {
		name := strings.ToLower(filepath.Base(artifact.Path))
		if strings.Contains(name, "buf") || strings.Contains(name, "idl") || strings.Contains(name, "contract") {
			return true
		}
	}
	return false
}

func readServiceMatrix(root string) (serviceMatrix, []string) {
	path := filepath.Join(root, ".service-matrix", "dependencies.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return serviceMatrix{}, []string{"无法读取服务矩阵"}
	}
	matrix := serviceMatrix{Services: map[string]matrixService{}}
	if err := parseSimpleMatrix(data, &matrix); err != nil {
		return serviceMatrix{}, []string{fmt.Sprintf("服务矩阵格式不支持: %v", err)}
	}
	return matrix, nil
}

func parseSimpleMatrix(data []byte, matrix *serviceMatrix) error {
	lines := strings.Split(string(data), "\n")
	section := ""
	currentService := ""
	for _, line := range lines {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		trimmed := strings.TrimSpace(line)
		if indent == 0 && strings.HasSuffix(trimmed, ":") {
			section = strings.TrimSuffix(trimmed, ":")
			currentService = ""
			continue
		}
		if indent == 0 {
			key, value, ok := splitYAMLScalar(trimmed)
			if !ok {
				continue
			}
			switch key {
			case "workspace":
				matrix.Workspace = value
			case "business_repo":
				matrix.BusinessRepo = value
			case "idl_repo":
				matrix.IDLRepo = value
			}
			continue
		}
		if section != "services" {
			continue
		}
		if indent == 2 && strings.HasSuffix(trimmed, ":") {
			currentService = strings.TrimSuffix(trimmed, ":")
			matrix.Services[currentService] = matrixService{}
			continue
		}
		if indent >= 4 && currentService != "" {
			key, value, ok := splitYAMLScalar(trimmed)
			if !ok {
				continue
			}
			service := matrix.Services[currentService]
			switch key {
			case "module":
				service.Module = value
			case "repo_path":
				service.RepoPath = value
			case "idl_required":
				service.IDLRequired = value == "true"
			case "idl_repo":
				service.IDLRepo = value
			case "proto_path":
				service.ProtoPath = value
			}
			matrix.Services[currentService] = service
		}
	}
	if matrix.Workspace == "" {
		matrix.Workspace = ".."
	}
	if matrix.BusinessRepo == "" {
		matrix.BusinessRepo = "business-repo"
	}
	if matrix.IDLRepo == "" {
		matrix.IDLRepo = "idl-repo"
	}
	return nil
}

func splitYAMLScalar(line string) (string, string, bool) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	value = strings.Trim(value, `"`)
	return key, value, true
}

func resolveMatrixPath(root string, matrix serviceMatrix, value string) string {
	workspace := filepath.Clean(filepath.Join(root, filepath.FromSlash(matrix.Workspace)))
	businessRepo := filepath.Join(workspace, matrix.BusinessRepo)
	idlRepo := filepath.Join(workspace, matrix.IDLRepo)
	replacements := map[string]string{
		"{business-repo}":      businessRepo,
		"{business-repo-name}": filepath.Base(businessRepo),
		"{idl-repo}":           idlRepo,
		"{idl-repo-name}":      filepath.Base(idlRepo),
	}
	resolved := value
	for from, to := range replacements {
		resolved = strings.ReplaceAll(resolved, from, to)
	}
	if filepath.IsAbs(resolved) {
		return filepath.Clean(resolved)
	}
	return filepath.Join(root, filepath.FromSlash(resolved))
}

func affectedServices(root string, requirementID string) []string {
	seen := map[string]bool{}
	path := filepath.Join(root, "requirements", requirementID, "tasks.json")
	data, err := os.ReadFile(path)
	if err == nil {
		var tasks tasksFile
		if json.Unmarshal(data, &tasks) == nil {
			hasTasks := len(tasks.Tasks) > 0
			for _, task := range tasks.Tasks {
				for _, service := range task.AffectedServices {
					service = strings.TrimSpace(service)
					if service != "" {
						seen[service] = true
					}
				}
			}
			if hasTasks {
				return sortedKeys(seen)
			}
		}
	}
	if len(seen) == 0 {
		for _, relative := range []string{"impact-analysis.md", "design.md"} {
			content, err := os.ReadFile(filepath.Join(root, "requirements", requirementID, relative))
			if err != nil {
				continue
			}
			for _, service := range extractBacktickedWords(string(content)) {
				seen[service] = true
			}
		}
	}
	var services []string
	for service := range seen {
		services = append(services, service)
	}
	sort.Strings(services)
	return services
}

func sortedKeys(values map[string]bool) []string {
	var keys []string
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func extractBacktickedWords(text string) []string {
	re := regexp.MustCompile("`([A-Za-z0-9][A-Za-z0-9._-]*)`")
	matches := re.FindAllStringSubmatch(text, -1)
	var values []string
	for _, match := range matches {
		values = append(values, match[1])
	}
	return values
}

func collectEvidence(root string, requirementID string) []gate.Artifact {
	evidenceDir := filepath.Join(root, "requirements", requirementID, "evidence")
	entries, err := os.ReadDir(evidenceDir)
	if err != nil {
		return nil
	}
	var artifacts []gate.Artifact
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		relative := filepath.ToSlash(filepath.Join("requirements", requirementID, "evidence", entry.Name()))
		hash, err := fileSHA256(filepath.Join(evidenceDir, entry.Name()))
		if err == nil {
			artifacts = append(artifacts, gate.Artifact{Path: relative, SHA256: hash})
		}
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts
}

func inferIDLImpact(root string, requirementID string) *gate.IDLImpact {
	mentionsIDL := false
	explicitNo := false
	for _, relative := range []string{"impact-analysis.md", "design.md"} {
		content, err := os.ReadFile(filepath.Join(root, "requirements", requirementID, relative))
		if err != nil {
			continue
		}
		if relative == "impact-analysis.md" {
			if impact, ok := structuredIDLImpact(string(content)); ok {
				return impact
			}
		}
		lower := strings.ToLower(string(content))
		if hasExplicitIDLYes(lower) {
			return &gate.IDLImpact{Impact: "yes"}
		}
		if hasExplicitIDLNo(lower) {
			explicitNo = true
			continue
		}
		if strings.Contains(lower, "protobuf") || strings.Contains(lower, "idl") || strings.Contains(lower, "proto files:") {
			mentionsIDL = true
		}
	}
	if mentionsIDL && !explicitNo {
		return &gate.IDLImpact{Impact: "yes"}
	}
	if explicitNo {
		return &gate.IDLImpact{Impact: "no", NAReason: "需求文档明确声明不涉及 protobuf IDL 或外部契约影响。"}
	}
	return &gate.IDLImpact{Impact: "no", NAReason: "需求文档未声明 protobuf IDL 或外部契约影响。"}
}

func structuredIDLImpact(content string) (*gate.IDLImpact, bool) {
	frontMatter := parseFrontMatter(content)
	impact := strings.ToLower(strings.TrimSpace(firstNonEmpty(frontMatter["idl_impact"], frontMatter["idl_impact.impact"])))
	if impact == "" {
		return nil, false
	}
	reason := firstNonEmpty(frontMatter["idl_impact_reason"], frontMatter["idl_impact.na_reason"], frontMatter["idl_impact_na_reason"])
	return &gate.IDLImpact{Impact: impact, NAReason: reason}, true
}

func hasExplicitIDLYes(lower string) bool {
	return strings.Contains(lower, "protobuf idl required: yes") ||
		strings.Contains(lower, "does this change involve protobuf idl or external contracts: yes")
}

func hasExplicitIDLNo(lower string) bool {
	return strings.Contains(lower, "protobuf idl required: no") ||
		strings.Contains(lower, "does this change involve protobuf idl or external contracts: no")
}

func collectRepos(root string, gateID string, idlImpact *gate.IDLImpact) []gate.Repo {
	if gateID != GateServiceRepoCheck {
		return nil
	}
	candidates := []struct {
		name string
		path string
	}{
		{name: "harness-repo", path: root},
		{name: "business-repo", path: filepath.Clean(filepath.Join(root, "..", "business-repo"))},
	}
	if idlImpact != nil && idlImpact.Impact == "yes" {
		candidates = append(candidates, struct {
			name string
			path string
		}{name: "idl-repo", path: filepath.Clean(filepath.Join(root, "..", "idl-repo"))})
	}
	var repos []gate.Repo
	for _, candidate := range candidates {
		branch := gitOutput(candidate.path, "branch", "--show-current")
		commit := gitOutput(candidate.path, "rev-parse", "--short", "HEAD")
		if branch == "" && commit == "" {
			continue
		}
		repos = append(repos, gate.Repo{Name: candidate.name, Branch: branch, Commit: commit})
	}
	return repos
}

func gitOutput(dir string, args ...string) string {
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func writeGateJSON(path string, report *gate.Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return &LifecycleError{Code: gate.VerifyInvalid, Problems: []string{fmt.Sprintf("cannot marshal gate JSON: %v", err)}}
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return &LifecycleError{Code: gate.VerifyMissing, Problems: []string{fmt.Sprintf("cannot write %s: %v", path, err)}}
	}
	return nil
}

func staleProblems(root string, artifacts []gate.Artifact) []string {
	var problems []string
	for _, artifact := range artifacts {
		path := filepath.Join(root, filepath.FromSlash(artifact.Path))
		hash, err := fileSHA256(path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("missing file: %s", artifact.Path))
			continue
		}
		if hash != strings.ToLower(artifact.SHA256) {
			problems = append(problems, fmt.Sprintf("sha256 mismatch: %s", artifact.Path))
		}
	}
	return problems
}

func nextStage(current string) (next string, gateID string, ok bool) {
	switch strings.TrimSpace(current) {
	case "", "1":
		return "2", "", true
	case "2":
		return "3", GateRequirementReview, true
	case "3":
		return "4.1", GateDesignReview, true
	case "4.1":
		return "4.2", "", true
	case "4.2":
		return "4.3", GateDevEntry, true
	case "4.3":
		return "4.4", GateServiceRepoCheck, true
	case "4.4":
		return "5", "", true
	default:
		return "", "", false
	}
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
