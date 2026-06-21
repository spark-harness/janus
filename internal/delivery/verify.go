package delivery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
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
	BoundIntegration = "integration-bound"
	BoundRelease     = "release-bound"

	ContractModeRCOrFormal = "rc-or-formal"
	ContractModeFormalOnly = "formal-only"
)

type Options struct {
	RequirementID string
	RepoName      string
	Workspace     string
	BaseBranch    string
	HeadBranch    string
	OutputGate    string
	Now           time.Time
}

type Result struct {
	RequirementID string
	GateID        string
	Bound         string
	ContractMode  string
	CurrentRepo   string
	Peers         []PeerStatus
	ContractScan  *ContractScanStatus
	Report        *gate.Report
}

type PeerStatus struct {
	Repo   string
	Status string
	Branch string
	Commit string
}

type ContractScanStatus struct {
	Repo               string
	Mode               string
	Status             string
	Output             string
	ChangedPaths       []string
	FormalDependencies []FormalDependency
	FormalEvidence     []FormalEvidenceStatus
}

type FormalDependency struct {
	Language   string
	File       string
	Dependency string
	Version    string
	Tag        string
}

type FormalEvidenceStatus struct {
	Dependency string
	Version    string
	Tag        string
	Status     string
	Detail     string
}

type VerifyError struct {
	Code     int
	Problems []string
}

func (e *VerifyError) Error() string {
	return strings.Join(e.Problems, "\n")
}

type requirementConfig struct {
	RequirementID        string
	RelatedBranch        string
	TargetBranch         string
	ReleaseBranch        string
	ContractGateMode     string
	AffectedRepositories []string
}

func Verify(workspace string, options Options) (*Result, error) {
	workspace = firstNonEmpty(workspace, options.Workspace)
	if workspace == "" {
		return nil, verifyError(gate.VerifyInvalid, "workspace is required")
	}
	requirementID := strings.TrimSpace(options.RequirementID)
	if requirementID == "" {
		return nil, verifyError(gate.VerifyInvalid, "requirement is required")
	}
	repoName := strings.TrimSpace(options.RepoName)
	if repoName == "" {
		return nil, verifyError(gate.VerifyInvalid, "repo is required")
	}

	now := options.Now
	if now.IsZero() {
		now = time.Now()
	}

	config, requirementPath, err := readRequirementConfig(workspace, requirementID)
	if err != nil {
		return nil, err
	}
	if config.RelatedBranch == "" {
		config.RelatedBranch = firstNonEmpty(options.HeadBranch, gitOutput(filepath.Join(workspace, repoName), "branch", "--show-current"))
	}
	if config.TargetBranch == "" {
		config.TargetBranch = firstNonEmpty(options.BaseBranch, "master")
	}
	if config.ReleaseBranch == "" {
		config.ReleaseBranch = "master"
	}

	baseBranch := strings.TrimSpace(options.BaseBranch)
	if baseBranch == "" {
		baseBranch = config.TargetBranch
	}
	headBranch := strings.TrimSpace(options.HeadBranch)
	if headBranch == "" {
		headBranch = config.RelatedBranch
	}

	result := &Result{
		RequirementID: requirementID,
		CurrentRepo:   repoName,
	}
	result.Bound = deliveryBound(config, baseBranch, headBranch)
	result.ContractMode = contractMode(config, result.Bound)
	result.GateID = gateIDForBound(result.Bound)

	var problems []string
	promotion := isPromotionToRelease(config, baseBranch, headBranch)
	if baseBranch != config.TargetBranch && !promotion {
		problems = append(problems, fmt.Sprintf("current PR base %q does not match target_branch %q or release promotion %q", baseBranch, config.TargetBranch, config.ReleaseBranch))
	}
	if headBranch != "" && config.RelatedBranch != "" && headBranch != config.RelatedBranch && !promotion {
		problems = append(problems, fmt.Sprintf("current PR head %q does not match related_branch %q or target_branch %q promotion", headBranch, config.RelatedBranch, config.TargetBranch))
	}

	for _, peerRepo := range peerRepositories(config.AffectedRepositories, repoName) {
		status := evaluatePeer(filepath.Join(workspace, peerRepo), peerRepo, config, result.Bound)
		result.Peers = append(result.Peers, status)
		if !isAcceptablePeerStatus(result.Bound, status.Status) {
			problems = append(problems, fmt.Sprintf("%s has no acceptable peer state for related=%q target=%q release=%q", peerRepo, config.RelatedBranch, config.TargetBranch, config.ReleaseBranch))
		}
	}
	sort.Slice(result.Peers, func(i, j int) bool { return result.Peers[i].Repo < result.Peers[j].Repo })

	if repoName == "business-repo" && containsRepository(config.AffectedRepositories, "business-repo") {
		scan := runBusinessContractScan(filepath.Join(workspace, "business-repo"), result.ContractMode, baseBranch, headBranch)
		if result.Bound == BoundRelease && scan.Status == "passed" {
			scan.FormalEvidence = validateFormalEvidence(workspace, config, scan.FormalDependencies)
			for _, evidence := range scan.FormalEvidence {
				if evidence.Status != "passed" {
					scan.Status = "failed"
					if scan.Output == "" || scan.Output == "no output" {
						scan.Output = evidence.Detail
					} else {
						scan.Output += "\n" + evidence.Detail
					}
				}
			}
		}
		result.ContractScan = scan
		if scan.Status != "passed" {
			problems = append(problems, fmt.Sprintf("business-repo contract dependency scan failed in %s mode: %s", scan.Mode, scan.Output))
		}
	}

	report, reportErr := buildReport(requirementID, result, requirementPath, now, problems)
	if reportErr != nil {
		return result, reportErr
	}
	result.Report = report
	if options.OutputGate != "" {
		if err := writeReport(options.OutputGate, report); err != nil {
			return result, err
		}
	}
	if len(problems) > 0 {
		return result, &VerifyError{Code: gate.VerifyBranchPolicy, Problems: problems}
	}
	return result, nil
}

func readRequirementConfig(workspace string, requirementID string) (requirementConfig, string, error) {
	path := filepath.Join(workspace, "harness-repo", "requirements", requirementID, "requirement.md")
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return requirementConfig{}, "", verifyError(gate.VerifyMissing, "missing requirement: "+path)
		}
		return requirementConfig{}, "", verifyError(gate.VerifyMissing, fmt.Sprintf("cannot read requirement: %v", err))
	}
	frontMatter := parseFrontMatter(string(content))
	config := requirementConfig{
		RequirementID:        firstNonEmpty(frontMatter["requirement_id"], requirementID),
		RelatedBranch:        firstNonEmpty(frontMatter["related_branch"], frontMatter["source_branch"]),
		TargetBranch:         frontMatter["target_branch"],
		ReleaseBranch:        frontMatter["release_branch"],
		ContractGateMode:     firstNonEmpty(frontMatter["contract_gate_mode"], "auto"),
		AffectedRepositories: parseStringList(frontMatter["affected_repositories"]),
	}
	if len(config.AffectedRepositories) == 0 {
		config.AffectedRepositories = inferAffectedRepositories(string(content))
	}
	return config, path, nil
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
	lines := strings.Split(content[4:4+end], "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := splitYAMLScalar(line)
		if !ok {
			continue
		}
		if value == "" && i+1 < len(lines) {
			var items []string
			for j := i + 1; j < len(lines); j++ {
				next := strings.TrimSpace(lines[j])
				if !strings.HasPrefix(next, "- ") {
					break
				}
				items = append(items, strings.Trim(strings.TrimSpace(strings.TrimPrefix(next, "- ")), `"`))
				i = j
			}
			if len(items) > 0 {
				values[key] = strings.Join(items, ",")
				continue
			}
		}
		values[key] = value
	}
	return values
}

func splitYAMLScalar(line string) (string, string, bool) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	value = strings.Trim(value, `"`)
	return key, value, key != ""
}

func parseStringList(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(strings.Trim(item, `[]"`))
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func inferAffectedRepositories(content string) []string {
	known := []string{"harness-repo", "business-repo", "idl-repo", "idl-java-repo"}
	var repos []string
	for _, repo := range known {
		if strings.Contains(content, repo) {
			repos = append(repos, repo)
		}
	}
	return repos
}

func deliveryBound(config requirementConfig, baseBranch string, headBranch string) string {
	if isPromotionToRelease(config, baseBranch, headBranch) {
		return BoundRelease
	}
	if config.TargetBranch == config.ReleaseBranch {
		return BoundRelease
	}
	return BoundIntegration
}

func isPromotionToRelease(config requirementConfig, baseBranch string, headBranch string) bool {
	return config.TargetBranch != "" &&
		config.ReleaseBranch != "" &&
		config.TargetBranch != config.ReleaseBranch &&
		baseBranch == config.ReleaseBranch &&
		headBranch == config.TargetBranch
}

func contractMode(config requirementConfig, bound string) string {
	mode := strings.TrimSpace(config.ContractGateMode)
	if mode == ContractModeFormalOnly || mode == ContractModeRCOrFormal {
		return mode
	}
	if bound == BoundRelease {
		return ContractModeFormalOnly
	}
	return ContractModeRCOrFormal
}

func gateIDForBound(bound string) string {
	if bound == BoundRelease {
		return "release-readiness"
	}
	return "integration-readiness"
}

func peerRepositories(repos []string, current string) []string {
	seen := map[string]bool{}
	var peers []string
	for _, repo := range repos {
		repo = strings.TrimSpace(repo)
		if repo == "" || repo == current || seen[repo] {
			continue
		}
		seen[repo] = true
		peers = append(peers, repo)
	}
	sort.Strings(peers)
	return peers
}

func containsRepository(repos []string, target string) bool {
	for _, repo := range repos {
		if strings.TrimSpace(repo) == target {
			return true
		}
	}
	return false
}

func evaluatePeer(path string, name string, config requirementConfig, bound string) PeerStatus {
	status := PeerStatus{Repo: name}
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		status.Status = "repo_missing"
		return status
	}
	if bound == BoundRelease {
		if branchContains(path, config.ReleaseBranch, config.RelatedBranch) || relatedMergeMentioned(path, config.ReleaseBranch, config.RelatedBranch) {
			status.Status = "related_merged_to_release"
			status.Branch = config.ReleaseBranch
			status.Commit = gitOutput(path, "rev-parse", "--short", config.ReleaseBranch)
			return status
		}
		if config.TargetBranch != config.ReleaseBranch && branchContains(path, config.ReleaseBranch, config.TargetBranch) {
			status.Status = "target_merged_to_release"
			status.Branch = config.ReleaseBranch
			status.Commit = gitOutput(path, "rev-parse", "--short", config.ReleaseBranch)
			return status
		}
		if pr := openReleasePR(name, config.RelatedBranch, config.ReleaseBranch); pr != "" {
			status.Status = "release_pr_open"
			status.Branch = config.RelatedBranch + " -> " + config.ReleaseBranch
			status.Commit = pr
			return status
		}
		if pr := mergedReleasePR(path, name, config.RelatedBranch, config.ReleaseBranch); pr != "" {
			status.Status = "release_pr_merged"
			status.Branch = config.RelatedBranch + " -> " + config.ReleaseBranch
			status.Commit = pr
			return status
		}
		status.Status = "missing_required_state"
		return status
	}
	if branchExists(path, config.RelatedBranch) {
		status.Status = "related_branch_exists"
		status.Branch = config.RelatedBranch
		status.Commit = gitOutput(path, "rev-parse", "--short", config.RelatedBranch)
		return status
	}
	if branchContains(path, config.TargetBranch, config.RelatedBranch) {
		status.Status = "related_merged_to_target"
		status.Branch = config.TargetBranch
		status.Commit = gitOutput(path, "rev-parse", "--short", config.TargetBranch)
		return status
	}
	if branchContains(path, config.ReleaseBranch, config.RelatedBranch) || relatedMergeMentioned(path, config.ReleaseBranch, config.RelatedBranch) {
		status.Status = "related_merged_to_release"
		status.Branch = config.ReleaseBranch
		status.Commit = gitOutput(path, "rev-parse", "--short", config.ReleaseBranch)
		return status
	}
	if config.TargetBranch != config.ReleaseBranch && branchContains(path, config.ReleaseBranch, config.TargetBranch) {
		status.Status = "target_merged_to_release"
		status.Branch = config.ReleaseBranch
		status.Commit = gitOutput(path, "rev-parse", "--short", config.ReleaseBranch)
		return status
	}
	status.Status = "missing_required_state"
	return status
}

func branchExists(path string, branch string) bool {
	return resolveRef(path, branch) != ""
}

func branchContains(path string, ancestor string, descendant string) bool {
	if strings.TrimSpace(ancestor) == "" || strings.TrimSpace(descendant) == "" {
		return false
	}
	ancestorRef := resolveRef(path, ancestor)
	descendantRef := resolveRef(path, descendant)
	if ancestorRef == "" || descendantRef == "" {
		return false
	}
	return gitRun(path, "merge-base", "--is-ancestor", descendantRef, ancestorRef) == nil
}

func isAcceptablePeerStatus(bound string, status string) bool {
	if bound == BoundRelease {
		return status == "related_merged_to_release" || status == "target_merged_to_release" || status == "release_pr_merged" || status == "release_pr_open"
	}
	switch status {
	case "related_branch_exists", "related_merged_to_target", "related_merged_to_release", "target_merged_to_release":
		return true
	default:
		return false
	}
}

func mergedReleasePR(path string, repo string, headBranch string, baseBranch string) string {
	repo = strings.TrimSpace(repo)
	headBranch = strings.TrimSpace(headBranch)
	baseBranch = strings.TrimSpace(baseBranch)
	if repo == "" || headBranch == "" || baseBranch == "" {
		return ""
	}
	if pr := mergedReleasePRFromEnv(path, repo, headBranch, baseBranch); pr != "" {
		return pr
	}
	token := firstNonEmpty(os.Getenv("GH_TOKEN"), os.Getenv("GITHUB_TOKEN"), os.Getenv("JANUS_REPO_TOKEN"), os.Getenv("BRANCH_COHERENCE_TOKEN"))
	if token == "" {
		return ""
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return ""
	}
	owner := firstNonEmpty(os.Getenv("JANUS_GITHUB_OWNER"), os.Getenv("GITHUB_REPOSITORY_OWNER"), "spark-harness")
	apiPath := fmt.Sprintf(
		"/repos/%s/%s/pulls?state=closed&head=%s:%s&base=%s",
		url.PathEscape(owner),
		url.PathEscape(repo),
		url.QueryEscape(owner),
		url.QueryEscape(headBranch),
		url.QueryEscape(baseBranch),
	)
	cmd := exec.Command("gh", "api", apiPath)
	cmd.Env = append(os.Environ(), "GH_TOKEN="+token)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	var pulls []struct {
		Number   int    `json:"number"`
		URL      string `json:"html_url"`
		MergedAt string `json:"merged_at"`
		MergeSHA string `json:"merge_commit_sha"`
		Head     struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := json.Unmarshal(output, &pulls); err != nil {
		return ""
	}
	for _, pull := range pulls {
		if pull.MergedAt == "" {
			continue
		}
		if !mergeCommitReachable(path, baseBranch, pull.MergeSHA) {
			continue
		}
		if pull.URL != "" {
			return pull.URL
		}
		if pull.Number > 0 {
			return fmt.Sprintf("PR #%d", pull.Number)
		}
		if pull.MergeSHA != "" {
			return shortSHA(pull.MergeSHA)
		}
		if pull.Head.SHA != "" {
			return shortSHA(pull.Head.SHA)
		}
		return "merged"
	}
	return ""
}

func mergedReleasePRFromEnv(path string, repo string, headBranch string, baseBranch string) string {
	for _, entry := range strings.Split(os.Getenv("JANUS_MERGED_RELEASE_PRS"), ",") {
		parts := strings.Split(strings.TrimSpace(entry), ":")
		if len(parts) < 3 {
			continue
		}
		if parts[0] != repo || parts[1] != headBranch || parts[2] != baseBranch {
			continue
		}
		if len(parts) >= 4 && parts[3] != "" {
			if !mergeCommitReachable(path, baseBranch, parts[3]) {
				continue
			}
			if len(parts) >= 5 && parts[4] != "" {
				return parts[4]
			}
			return shortSHA(parts[3])
		}
		return "merged"
	}
	return ""
}

func mergeCommitReachable(path string, releaseBranch string, mergeSHA string) bool {
	mergeSHA = strings.TrimSpace(mergeSHA)
	if mergeSHA == "" {
		return true
	}
	releaseRef := resolveRef(path, releaseBranch)
	if releaseRef == "" {
		return true
	}
	if gitRun(path, "cat-file", "-e", mergeSHA+"^{commit}") != nil {
		return true
	}
	return gitRun(path, "merge-base", "--is-ancestor", mergeSHA, releaseRef) == nil
}

func openReleasePR(repo string, headBranch string, baseBranch string) string {
	repo = strings.TrimSpace(repo)
	headBranch = strings.TrimSpace(headBranch)
	baseBranch = strings.TrimSpace(baseBranch)
	if repo == "" || headBranch == "" || baseBranch == "" {
		return ""
	}
	if pr := openReleasePRFromEnv(repo, headBranch, baseBranch); pr != "" {
		return pr
	}
	token := firstNonEmpty(os.Getenv("GH_TOKEN"), os.Getenv("GITHUB_TOKEN"), os.Getenv("JANUS_REPO_TOKEN"), os.Getenv("BRANCH_COHERENCE_TOKEN"))
	if token == "" {
		return ""
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return ""
	}
	owner := firstNonEmpty(os.Getenv("JANUS_GITHUB_OWNER"), os.Getenv("GITHUB_REPOSITORY_OWNER"), "spark-harness")
	apiPath := fmt.Sprintf(
		"/repos/%s/%s/pulls?state=open&head=%s:%s&base=%s",
		url.PathEscape(owner),
		url.PathEscape(repo),
		url.QueryEscape(owner),
		url.QueryEscape(headBranch),
		url.QueryEscape(baseBranch),
	)
	cmd := exec.Command("gh", "api", apiPath)
	cmd.Env = append(os.Environ(), "GH_TOKEN="+token)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	var pulls []struct {
		Number int    `json:"number"`
		URL    string `json:"html_url"`
		Head   struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := json.Unmarshal(output, &pulls); err != nil || len(pulls) == 0 {
		return ""
	}
	if pulls[0].URL != "" {
		return pulls[0].URL
	}
	if pulls[0].Number > 0 {
		return fmt.Sprintf("PR #%d", pulls[0].Number)
	}
	if pulls[0].Head.SHA != "" {
		return shortSHA(pulls[0].Head.SHA)
	}
	return "open"
}

func openReleasePRFromEnv(repo string, headBranch string, baseBranch string) string {
	for _, entry := range strings.Split(os.Getenv("JANUS_OPEN_RELEASE_PRS"), ",") {
		parts := strings.Split(strings.TrimSpace(entry), ":")
		if len(parts) < 3 {
			continue
		}
		if parts[0] == repo && parts[1] == headBranch && parts[2] == baseBranch {
			if len(parts) >= 4 && parts[3] != "" {
				return "PR #" + parts[3]
			}
			return "open"
		}
	}
	return ""
}

func shortSHA(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func runBusinessContractScan(path string, mode string, baseBranch string, headBranch string) *ContractScanStatus {
	status := &ContractScanStatus{Repo: "business-repo", Mode: mode}
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		status.Status = "repo_missing"
		status.Output = "business-repo checkout is missing"
		return status
	}
	script := filepath.Join(path, "scripts", "contract_dependency_scan.py")
	if _, err := os.Stat(script); err != nil {
		status.Status = "scanner_missing"
		status.Output = "scripts/contract_dependency_scan.py is missing"
		return status
	}
	changedPaths, diffErr := changedContractDependencyPaths(path, baseBranch, headBranch)
	if diffErr != nil {
		status.Status = "diff_unavailable"
		status.Output = diffErr.Error()
		return status
	}
	if len(changedPaths) == 0 {
		status.Status = "passed"
		status.Output = "no contract dependency files changed"
		return status
	}
	status.ChangedPaths = changedPaths
	args := []string{"scripts/contract_dependency_scan.py", "--mode", mode, "--root", "."}
	for _, changedPath := range changedPaths {
		args = append(args, "--path", changedPath)
	}
	cmd := exec.Command("python3", args...)
	cmd.Dir = path
	output, err := cmd.CombinedOutput()
	status.Output = strings.TrimSpace(string(output))
	if status.Output == "" {
		status.Output = "no output"
	}
	if err != nil {
		status.Status = "failed"
		return status
	}
	dependencies, dependencyErr := collectFormalDependencies(path, changedPaths)
	if dependencyErr != nil {
		status.Status = "failed"
		status.Output = dependencyErr.Error()
		return status
	}
	status.FormalDependencies = dependencies
	status.Status = "passed"
	return status
}

type contractConfig struct {
	Maven []struct {
		GroupID    string `json:"groupId"`
		ArtifactID string `json:"artifactId"`
	} `json:"maven"`
	Go []struct {
		ModulePrefix string `json:"modulePrefix"`
	} `json:"go"`
}

type xmlNode struct {
	XMLName  xml.Name
	Content  string    `xml:",chardata"`
	Children []xmlNode `xml:",any"`
}

func collectFormalDependencies(businessPath string, changedPaths []string) ([]FormalDependency, error) {
	configPath := filepath.Join(businessPath, "config", "contract-dependencies.json")
	data, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read contract dependency config: %v", err)
	}
	var config contractConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("cannot parse contract dependency config: %v", err)
	}

	var dependencies []FormalDependency
	for _, changedPath := range changedPaths {
		fullPath := filepath.Join(businessPath, filepath.FromSlash(changedPath))
		switch filepath.Base(changedPath) {
		case "pom.xml":
			found, err := collectMavenFormalDependencies(fullPath, changedPath, config)
			if err != nil {
				return nil, err
			}
			dependencies = append(dependencies, found...)
		case "go.mod":
			found, err := collectGoFormalDependencies(fullPath, changedPath, config)
			if err != nil {
				return nil, err
			}
			dependencies = append(dependencies, found...)
		}
	}
	return dependencies, nil
}

func collectMavenFormalDependencies(path string, displayPath string, config contractConfig) ([]FormalDependency, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %v", displayPath, err)
	}
	var root xmlNode
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("cannot parse %s: %v", displayPath, err)
	}
	properties := mavenProperties(root)
	managed := managedMavenVersions(root, properties)
	contractDeps := map[string]bool{}
	for _, item := range config.Maven {
		contractDeps[item.GroupID+":"+item.ArtifactID] = true
	}

	var dependencies []FormalDependency
	for _, depsNode := range childrenNamed(root, "dependencies") {
		for _, depNode := range childrenNamed(depsNode, "dependency") {
			groupID := firstText(depNode, "groupId")
			artifactID := firstText(depNode, "artifactId")
			coordinate := groupID + ":" + artifactID
			if !contractDeps[coordinate] {
				continue
			}
			version := firstText(depNode, "version")
			if version == "" {
				version = managed[coordinate]
			}
			version = resolveMavenVersion(version, properties)
			if classifyJavaVersion(version) != "formal" {
				continue
			}
			dependencies = append(dependencies, FormalDependency{
				Language:   "java",
				File:       displayPath,
				Dependency: coordinate,
				Version:    version,
				Tag:        "v" + version,
			})
		}
	}
	return dependencies, nil
}

func mavenProperties(root xmlNode) map[string]string {
	properties := map[string]string{}
	for _, propertiesNode := range childrenNamed(root, "properties") {
		for _, child := range propertiesNode.Children {
			properties[child.XMLName.Local] = strings.TrimSpace(child.Content)
		}
	}
	return properties
}

func managedMavenVersions(root xmlNode, properties map[string]string) map[string]string {
	versions := map[string]string{}
	for _, management := range childrenNamed(root, "dependencyManagement") {
		for _, depsNode := range childrenNamed(management, "dependencies") {
			for _, depNode := range childrenNamed(depsNode, "dependency") {
				groupID := firstText(depNode, "groupId")
				artifactID := firstText(depNode, "artifactId")
				version := resolveMavenVersion(firstText(depNode, "version"), properties)
				if groupID != "" && artifactID != "" && version != "" {
					versions[groupID+":"+artifactID] = version
				}
			}
		}
	}
	return versions
}

func childrenNamed(node xmlNode, name string) []xmlNode {
	var children []xmlNode
	for _, child := range node.Children {
		if child.XMLName.Local == name {
			children = append(children, child)
		}
	}
	return children
}

func firstText(node xmlNode, name string) string {
	for _, child := range childrenNamed(node, name) {
		return strings.TrimSpace(child.Content)
	}
	return ""
}

func resolveMavenVersion(version string, properties map[string]string) string {
	if strings.HasPrefix(version, "${") && strings.HasSuffix(version, "}") {
		key := strings.TrimSuffix(strings.TrimPrefix(version, "${"), "}")
		if resolved := properties[key]; resolved != "" {
			return resolved
		}
	}
	return version
}

func classifyJavaVersion(version string) string {
	if regexp.MustCompile(`(?i)SNAPSHOT`).MatchString(version) {
		return "snapshot"
	}
	if regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(version) {
		return "formal"
	}
	if regexp.MustCompile(`^\d+\.\d+\.\d+-rc\.[A-Za-z]+-\d+\.\d{8}\.[0-9a-fA-F]{7,40}$`).MatchString(version) {
		return "rc"
	}
	return "branch_or_unclassified"
}

func collectGoFormalDependencies(path string, displayPath string, config contractConfig) ([]FormalDependency, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %v", displayPath, err)
	}
	var dependencies []FormalDependency
	for _, line := range strings.Split(string(data), "\n") {
		fields := splitGoFields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		if fields[0] == "require" && len(fields) >= 3 {
			fields = fields[1:3]
		}
		module := fields[0]
		version := fields[1]
		if !isContractGoModule(module, config) || classifyGoVersion(version) != "formal" {
			continue
		}
		dependencies = append(dependencies, FormalDependency{
			Language:   "go",
			File:       displayPath,
			Dependency: module,
			Version:    version,
			Tag:        version,
		})
	}
	return dependencies, nil
}

func splitGoFields(line string) []string {
	return strings.Fields(strings.SplitN(line, "//", 2)[0])
}

func isContractGoModule(module string, config contractConfig) bool {
	for _, item := range config.Go {
		if strings.HasPrefix(module, item.ModulePrefix) {
			return true
		}
	}
	return false
}

func classifyGoVersion(version string) string {
	if regexp.MustCompile(`(?i)^v\d+\.\d+\.\d+-\d{14}-[0-9a-f]{12}$`).MatchString(version) {
		return "pseudo"
	}
	if regexp.MustCompile(`^v\d+\.\d+\.\d+$`).MatchString(version) {
		return "formal"
	}
	if regexp.MustCompile(`(?i)^v\d+\.\d+\.\d+-rc\.[a-z]+[0-9]+(?:-[a-z0-9]+)*\.\d{8}\.[0-9a-f]{7,40}$`).MatchString(version) {
		return "rc"
	}
	return "branch_or_unclassified"
}

func validateFormalEvidence(workspace string, config requirementConfig, dependencies []FormalDependency) []FormalEvidenceStatus {
	var statuses []FormalEvidenceStatus
	for _, dependency := range dependencies {
		tagStatus := verifyIDLTag(filepath.Join(workspace, "idl-repo"), config.ReleaseBranch, dependency)
		statuses = append(statuses, tagStatus)
		if tagStatus.Status != "passed" {
			continue
		}
		statuses = append(statuses, verifyArtifact(workspace, dependency))
	}
	return statuses
}

func verifyIDLTag(idlPath string, releaseBranch string, dependency FormalDependency) FormalEvidenceStatus {
	status := FormalEvidenceStatus{
		Dependency: dependency.Dependency,
		Version:    dependency.Version,
		Tag:        dependency.Tag,
	}
	tagCommit := resolveTagCommit(idlPath, dependency.Tag)
	if tagCommit == "" {
		status.Status = "blocked"
		status.Detail = fmt.Sprintf("formal tag %s is missing in idl-repo", dependency.Tag)
		return status
	}
	releaseRef := resolveRef(idlPath, releaseBranch)
	if releaseRef == "" {
		status.Status = "blocked"
		status.Detail = fmt.Sprintf("release branch %s is missing in idl-repo", releaseBranch)
		return status
	}
	if gitRun(idlPath, "merge-base", "--is-ancestor", tagCommit, releaseRef) != nil {
		status.Status = "blocked"
		status.Detail = fmt.Sprintf("formal tag %s commit is not reachable from idl-repo %s", dependency.Tag, releaseBranch)
		return status
	}
	status.Status = "passed"
	status.Detail = fmt.Sprintf("formal tag %s is reachable from idl-repo %s", dependency.Tag, releaseBranch)
	return status
}

func verifyArtifact(workspace string, dependency FormalDependency) FormalEvidenceStatus {
	status := FormalEvidenceStatus{
		Dependency: dependency.Dependency,
		Version:    dependency.Version,
		Tag:        dependency.Tag,
	}
	switch dependency.Language {
	case "java":
		return verifyJavaArtifact(dependency, status)
	case "go":
		return verifyGoArtifact(workspace, dependency, status)
	default:
		status.Status = "blocked"
		status.Detail = "unsupported formal dependency language: " + dependency.Language
		return status
	}
}

func verifyJavaArtifact(dependency FormalDependency, status FormalEvidenceStatus) FormalEvidenceStatus {
	if containsCSV(os.Getenv("JANUS_JAVA_ARTIFACT_VERSIONS"), dependency.Version) {
		status.Status = "passed"
		status.Detail = fmt.Sprintf("Java artifact %s version %s found in JANUS_JAVA_ARTIFACT_VERSIONS", dependency.Dependency, dependency.Version)
		return status
	}
	tokens := githubTokens("IDL_JAVA_REPO_TOKEN", "GH_TOKEN", "GITHUB_TOKEN")
	if len(tokens) == 0 {
		status.Status = "blocked"
		status.Detail = "missing GitHub token for Java artifact lookup"
		return status
	}
	if _, err := exec.LookPath("gh"); err != nil {
		status.Status = "blocked"
		status.Detail = "gh CLI is required for Java artifact lookup"
		return status
	}
	owner := firstNonEmpty(os.Getenv("JANUS_GITHUB_OWNER"), os.Getenv("GITHUB_REPOSITORY_OWNER"), "spark-harness")
	packageName := strings.ReplaceAll(dependency.Dependency, ":", ".")
	apiPath := fmt.Sprintf("/orgs/%s/packages/maven/%s/versions", owner, url.PathEscape(packageName))
	var queryErr error
	for _, token := range tokens {
		cmd := exec.Command("gh", "api", apiPath)
		cmd.Env = append(os.Environ(), "GH_TOKEN="+token)
		output, err := cmd.Output()
		if err != nil {
			queryErr = err
			continue
		}
		var versions []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(output, &versions); err != nil {
			status.Status = "blocked"
			status.Detail = fmt.Sprintf("cannot parse Java artifact versions for %s: %v", dependency.Dependency, err)
			return status
		}
		for _, version := range versions {
			if version.Name == dependency.Version {
				status.Status = "passed"
				status.Detail = fmt.Sprintf("Java artifact %s version %s exists", dependency.Dependency, dependency.Version)
				return status
			}
		}
	}
	if queryErr != nil {
		status.Status = "blocked"
		status.Detail = fmt.Sprintf("cannot query Java artifact %s: %v", dependency.Dependency, queryErr)
		return status
	}
	status.Status = "blocked"
	status.Detail = fmt.Sprintf("Java artifact %s version %s is missing", dependency.Dependency, dependency.Version)
	return status
}

func verifyGoArtifact(workspace string, dependency FormalDependency, status FormalEvidenceStatus) FormalEvidenceStatus {
	localGoRepo := filepath.Join(workspace, "idl-go-repo")
	if _, err := os.Stat(filepath.Join(localGoRepo, ".git")); err == nil {
		if resolveTagCommit(localGoRepo, dependency.Tag) != "" {
			status.Status = "passed"
			status.Detail = fmt.Sprintf("Go artifact tag %s exists in local idl-go-repo", dependency.Tag)
			return status
		}
		status.Status = "blocked"
		status.Detail = fmt.Sprintf("Go artifact tag %s is missing in local idl-go-repo", dependency.Tag)
		return status
	}
	repoURL := firstNonEmpty(os.Getenv("JANUS_GO_IDL_REPOSITORY"), "https://github.com/spark-harness/idl-go-repo.git")
	token := firstNonEmpty(os.Getenv("IDL_GO_REPO_TOKEN"), os.Getenv("GH_TOKEN"), os.Getenv("GITHUB_TOKEN"))
	if token != "" && strings.HasPrefix(repoURL, "https://github.com/") {
		repoURL = strings.Replace(repoURL, "https://github.com/", "https://x-access-token:"+url.QueryEscape(token)+"@github.com/", 1)
	}
	if gitRun("", "ls-remote", "--exit-code", "--tags", repoURL, "refs/tags/"+dependency.Tag) == nil {
		status.Status = "passed"
		status.Detail = fmt.Sprintf("Go artifact tag %s exists", dependency.Tag)
		return status
	}
	status.Status = "blocked"
	status.Detail = fmt.Sprintf("Go artifact tag %s is missing", dependency.Tag)
	return status
}

func containsCSV(values string, target string) bool {
	for _, value := range strings.Split(values, ",") {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func changedContractDependencyPaths(path string, baseBranch string, headBranch string) ([]string, error) {
	baseRef := resolveRef(path, baseBranch)
	if baseRef == "" {
		return nil, fmt.Errorf("cannot resolve base branch %q for contract dependency diff", baseBranch)
	}
	headRef := resolveRef(path, headBranch)
	if headRef == "" {
		headRef = "HEAD"
	}
	output, err := gitOutputErr(path, "diff", "--name-only", baseRef, headRef)
	if err != nil {
		return nil, fmt.Errorf("cannot diff contract dependency files from %q to %q", baseBranch, headBranch)
	}
	var paths []string
	for _, line := range strings.Split(output, "\n") {
		changedPath := strings.TrimSpace(line)
		if isContractDependencyPath(changedPath) {
			paths = append(paths, changedPath)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func isContractDependencyPath(path string) bool {
	return path == "pom.xml" || path == "go.mod" || path == "go.sum" ||
		strings.HasSuffix(path, "/pom.xml") ||
		strings.HasSuffix(path, "/go.mod") ||
		strings.HasSuffix(path, "/go.sum")
}

func relatedMergeMentioned(path string, branch string, relatedBranch string) bool {
	if strings.TrimSpace(branch) == "" || strings.TrimSpace(relatedBranch) == "" {
		return false
	}
	ref := resolveRef(path, branch)
	if ref == "" {
		return false
	}
	output := gitOutput(path, "log", "--format=%s", ref, "-n", "200")
	return strings.Contains(output, relatedBranch)
}

func resolveRef(path string, branch string) string {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return ""
	}
	candidates := []string{
		"refs/remotes/origin/" + branch,
		"origin/" + branch,
		"refs/heads/" + branch,
		branch,
	}
	for _, candidate := range candidates {
		if gitRun(path, "rev-parse", "--verify", "--quiet", candidate) == nil {
			return candidate
		}
	}
	return ""
}

func resolveTagCommit(path string, tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ""
	}
	candidates := []string{
		"refs/tags/" + tag + "^{}",
		"refs/tags/" + tag,
		tag + "^{}",
		tag,
	}
	for _, candidate := range candidates {
		if gitRun(path, "rev-parse", "--verify", "--quiet", candidate) == nil {
			return candidate
		}
	}
	return ""
}

func buildReport(requirementID string, result *Result, requirementPath string, now time.Time, problems []string) (*gate.Report, error) {
	inputPath := filepath.ToSlash(filepath.Join("requirements", requirementID, "requirement.md"))
	hash, err := fileSHA256(requirementPath)
	if err != nil {
		return nil, verifyError(gate.VerifyMissing, fmt.Sprintf("cannot hash %s: %v", inputPath, err))
	}

	checklist := []gate.ChecklistItem{
		{Item: "交付阶段可判定", Result: gate.ResultPass, Evidence: result.Bound},
		{Item: "contract gate mode 可判定", Result: gate.ResultPass, Evidence: result.ContractMode},
	}
	if result.ContractScan != nil {
		itemResult := gate.ResultPass
		if result.ContractScan.Status != "passed" {
			itemResult = gate.ResultBlocked
		}
		checklist = append(checklist, gate.ChecklistItem{
			Item:     "business contract dependency 符合当前阶段",
			Result:   itemResult,
			Evidence: result.ContractScan.Status + " (" + result.ContractScan.Mode + ")",
		})
		for _, evidence := range result.ContractScan.FormalEvidence {
			evidenceResult := gate.ResultPass
			if evidence.Status != "passed" {
				evidenceResult = gate.ResultBlocked
			}
			checklist = append(checklist, gate.ChecklistItem{
				Item:     "formal 发布证据可验证: " + evidence.Dependency,
				Result:   evidenceResult,
				Evidence: evidence.Detail,
			})
		}
	}
	for _, peer := range result.Peers {
		itemResult := gate.ResultPass
		if !isAcceptablePeerStatus(result.Bound, peer.Status) {
			itemResult = gate.ResultBlocked
		}
		checklist = append(checklist, gate.ChecklistItem{
			Item:     "peer repo 阶段状态: " + peer.Repo,
			Result:   itemResult,
			Evidence: peer.Status,
		})
	}

	blocking := make([]gate.BlockingIssue, 0, len(problems))
	for _, problem := range problems {
		blocking = append(blocking, gate.BlockingIssue{
			Issue:          problem,
			RequiredAction: "修正 requirement 分支声明或 peer repo 阶段状态后重新运行 janus delivery verify。",
			Owner:          "Harness Team",
		})
	}
	reportResult := gate.ResultPass
	blocks := false
	decision := "delivery readiness verified."
	if len(blocking) > 0 {
		reportResult = gate.ResultBlocked
		blocks = true
		decision = "delivery readiness blocked."
	}

	report := &gate.Report{
		SchemaVersion:   "1",
		RequirementID:   requirementID,
		GateID:          result.GateID,
		GateName:        gateName(result.GateID),
		Stage:           "delivery",
		CheckedBy:       "janus delivery verify",
		CheckedAt:       now.Format(time.RFC3339),
		Result:          reportResult,
		BlocksNextStage: blocks,
		Inputs:          []gate.Artifact{{Path: inputPath, SHA256: hash}},
		Checklist:       checklist,
		BlockingIssues:  blocking,
		Warnings:        nil,
		Waiver:          gate.Waiver{Required: false},
		Decision:        decision,
	}
	if err := gate.Validate(report); err != nil {
		return nil, err
	}
	return report, nil
}

func gateName(gateID string) string {
	switch gateID {
	case "release-readiness":
		return "发布就绪门禁"
	case "integration-readiness":
		return "集成就绪门禁"
	default:
		return gateID
	}
}

func writeReport(path string, report *gate.Report) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return verifyError(gate.VerifyMissing, fmt.Sprintf("cannot create gate directory: %v", err))
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return verifyError(gate.VerifyInvalid, fmt.Sprintf("cannot marshal gate report: %v", err))
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return verifyError(gate.VerifyMissing, fmt.Sprintf("cannot write gate report: %v", err))
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func gitOutput(dir string, args ...string) string {
	output, err := gitOutputErr(dir, args...)
	if err != nil {
		return ""
	}
	return output
}

func gitOutputErr(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func gitRun(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd.Run()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func githubTokens(envNames ...string) []string {
	seen := map[string]bool{}
	var tokens []string
	for _, name := range envNames {
		token := strings.TrimSpace(os.Getenv(name))
		if token == "" || seen[token] {
			continue
		}
		seen[token] = true
		tokens = append(tokens, token)
	}
	return tokens
}

func verifyError(code int, problems ...string) *VerifyError {
	return &VerifyError{Code: code, Problems: problems}
}

var requirementIDPattern = regexp.MustCompile(`[A-Z][A-Z0-9]*-[0-9]+`)

func RequirementIDFromBranch(branch string) string {
	matches := requirementIDPattern.FindAllString(branch, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}
