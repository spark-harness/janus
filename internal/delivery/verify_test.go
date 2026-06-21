package delivery

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVerifyReleaseBoundAcceptsMergedPeerAndFormalMode(t *testing.T) {
	workspace := t.TempDir()
	harness := initRepo(t, workspace, "harness-repo")
	business := initRepo(t, workspace, "business-repo")
	idl := initRepo(t, workspace, "idl-repo")

	writeRequirement(t, harness, "LEN-40", `---
requirement_id: "LEN-40"
related_branch: "feature/LEN-40-delivery-flow"
target_branch: "master"
release_branch: "master"
contract_gate_mode: "auto"
affected_repositories:
  - business-repo
  - idl-repo
---

# LEN-40
`)
	git(t, harness, "add", ".")
	git(t, harness, "commit", "-m", "docs: add requirement")
	git(t, harness, "checkout", "-b", "feature/LEN-40-delivery-flow")

	for _, repo := range []string{business, idl} {
		git(t, repo, "checkout", "-b", "feature/LEN-40-delivery-flow")
		writeFile(t, repo, "marker.txt", filepath.Base(repo))
		git(t, repo, "add", ".")
		git(t, repo, "commit", "-m", "feat: marker")
		git(t, repo, "checkout", "master")
		git(t, repo, "merge", "--no-ff", "feature/LEN-40-delivery-flow", "-m", "merge feature/LEN-40-delivery-flow")
		git(t, repo, "branch", "-D", "feature/LEN-40-delivery-flow")
	}

	result, err := Verify(workspace, Options{
		RequirementID: "LEN-40",
		RepoName:      "harness-repo",
		BaseBranch:    "master",
		HeadBranch:    "feature/LEN-40-delivery-flow",
		Now:           fixedTime(),
	})

	if err != nil {
		t.Fatalf("expected verify to pass, got %v", err)
	}
	if result.Bound != "release-bound" {
		t.Fatalf("expected release-bound, got %q", result.Bound)
	}
	if result.ContractMode != "formal-only" {
		t.Fatalf("expected formal-only, got %q", result.ContractMode)
	}
	for _, peer := range result.Peers {
		if peer.Status != "related_merged_to_release" {
			t.Fatalf("expected peer merged to release, got %#v", result.Peers)
		}
	}
}

func TestVerifyIntegrationBoundAcceptsExistingRelatedPeerAndRCMode(t *testing.T) {
	workspace := t.TempDir()
	harness := initRepo(t, workspace, "harness-repo")
	business := initRepo(t, workspace, "business-repo")

	writeRequirement(t, harness, "LEN-40", `---
requirement_id: "LEN-40"
related_branch: "feature/LEN-40-delivery-flow"
target_branch: "epic/lending"
release_branch: "master"
contract_gate_mode: "auto"
affected_repositories:
  - business-repo
---

# LEN-40
`)
	git(t, harness, "add", ".")
	git(t, harness, "commit", "-m", "docs: add requirement")

	git(t, business, "checkout", "-b", "feature/LEN-40-delivery-flow")

	result, err := Verify(workspace, Options{
		RequirementID: "LEN-40",
		RepoName:      "harness-repo",
		BaseBranch:    "epic/lending",
		HeadBranch:    "feature/LEN-40-delivery-flow",
		Now:           fixedTime(),
	})

	if err != nil {
		t.Fatalf("expected verify to pass, got %v", err)
	}
	if result.Bound != "integration-bound" {
		t.Fatalf("expected integration-bound, got %q", result.Bound)
	}
	if result.ContractMode != "rc-or-formal" {
		t.Fatalf("expected rc-or-formal, got %q", result.ContractMode)
	}
	if len(result.Peers) != 1 || result.Peers[0].Status != "related_branch_exists" {
		t.Fatalf("expected related branch peer status, got %#v", result.Peers)
	}
}

func TestVerifyPromotionToReleaseUsesFormalMode(t *testing.T) {
	workspace := t.TempDir()
	harness := initRepo(t, workspace, "harness-repo")
	business := initRepo(t, workspace, "business-repo")

	writeRequirement(t, harness, "LEN-40", `---
requirement_id: "LEN-40"
related_branch: "feature/LEN-40-delivery-flow"
target_branch: "epic/lending"
release_branch: "master"
contract_gate_mode: "auto"
affected_repositories:
  - business-repo
---

# LEN-40
`)
	git(t, business, "checkout", "-b", "feature/LEN-40-delivery-flow")
	writeFile(t, business, "marker.txt", "feature")
	git(t, business, "add", ".")
	git(t, business, "commit", "-m", "feat: marker")
	git(t, business, "checkout", "master")
	git(t, business, "checkout", "-b", "epic/lending")
	git(t, business, "merge", "--no-ff", "feature/LEN-40-delivery-flow", "-m", "merge feature/LEN-40-delivery-flow")
	git(t, business, "checkout", "master")
	git(t, business, "merge", "--no-ff", "epic/lending", "-m", "merge epic/lending")

	result, err := Verify(workspace, Options{
		RequirementID: "LEN-40",
		RepoName:      "harness-repo",
		BaseBranch:    "master",
		HeadBranch:    "epic/lending",
		Now:           fixedTime(),
	})

	if err != nil {
		t.Fatalf("expected promotion to release to pass, got %v", err)
	}
	if result.Bound != "release-bound" {
		t.Fatalf("expected release-bound promotion, got %q", result.Bound)
	}
	if result.ContractMode != "formal-only" {
		t.Fatalf("expected formal-only promotion, got %q", result.ContractMode)
	}
	if len(result.Peers) != 1 || result.Peers[0].Status != "related_merged_to_release" {
		t.Fatalf("expected peer merged to release, got %#v", result.Peers)
	}
}

func TestVerifyReleaseBoundRejectsPeerWithOnlyRelatedBranch(t *testing.T) {
	workspace := t.TempDir()
	harness := initRepo(t, workspace, "harness-repo")
	business := initRepo(t, workspace, "business-repo")
	disableGitHubPREvidence(t)

	writeRequirement(t, harness, "LEN-40", `---
related_branch: "feature/LEN-40-delivery-flow"
target_branch: "master"
release_branch: "master"
affected_repositories:
  - business-repo
---

# LEN-40
`)
	git(t, business, "checkout", "-b", "feature/LEN-40-delivery-flow")
	writeFile(t, business, "feature-only.txt", "not released")
	git(t, business, "add", ".")
	git(t, business, "commit", "-m", "feat: unreleased change")

	result, err := Verify(workspace, Options{
		RequirementID: "LEN-40",
		RepoName:      "harness-repo",
		BaseBranch:    "master",
		HeadBranch:    "feature/LEN-40-delivery-flow",
		Now:           fixedTime(),
	})

	if err == nil {
		t.Fatal("expected release-bound peer without release merge to fail")
	}
	if result == nil || result.Bound != "release-bound" {
		t.Fatalf("expected release-bound result, got %#v", result)
	}
	if !strings.Contains(err.Error(), "business-repo has no acceptable peer state") {
		t.Fatalf("expected peer state error, got %v", err)
	}
	if len(result.Peers) != 1 || result.Peers[0].Status != "missing_required_state" {
		t.Fatalf("expected missing release peer status, got %#v", result.Peers)
	}
}

func TestVerifyReleaseBoundAcceptsPeerWithOpenReleasePR(t *testing.T) {
	workspace := t.TempDir()
	harness := initRepo(t, workspace, "harness-repo")
	business := initRepo(t, workspace, "business-repo")

	writeRequirement(t, harness, "LEN-40", `---
related_branch: "feature/LEN-40-delivery-flow"
target_branch: "master"
release_branch: "master"
affected_repositories:
  - business-repo
---

# LEN-40
`)
	git(t, business, "checkout", "-b", "feature/LEN-40-delivery-flow")
	writeFile(t, business, "feature-only.txt", "not released")
	git(t, business, "add", ".")
	git(t, business, "commit", "-m", "feat: unreleased change")
	t.Setenv("JANUS_OPEN_RELEASE_PRS", "business-repo:feature/LEN-40-delivery-flow:master:17")

	result, err := Verify(workspace, Options{
		RequirementID: "LEN-40",
		RepoName:      "harness-repo",
		BaseBranch:    "master",
		HeadBranch:    "feature/LEN-40-delivery-flow",
		Now:           fixedTime(),
	})

	if err != nil {
		t.Fatalf("expected open release PR evidence to pass, got %v", err)
	}
	if len(result.Peers) != 1 || result.Peers[0].Status != "release_pr_open" {
		t.Fatalf("expected release_pr_open peer status, got %#v", result.Peers)
	}
	if result.Peers[0].Branch != "feature/LEN-40-delivery-flow -> master" {
		t.Fatalf("expected PR branch evidence, got %#v", result.Peers[0])
	}
}

func TestVerifyReleaseBoundAcceptsPeerWithMergedReleasePR(t *testing.T) {
	workspace := t.TempDir()
	harness := initRepo(t, workspace, "harness-repo")
	business := initRepo(t, workspace, "business-repo")
	disableGitHubPREvidence(t)

	writeRequirement(t, harness, "LEN-40", `---
related_branch: "feature/LEN-40-delivery-flow"
target_branch: "master"
release_branch: "master"
affected_repositories:
  - business-repo
---

# LEN-40
`)
	git(t, business, "checkout", "-b", "feature/LEN-40-delivery-flow")
	writeFile(t, business, "feature-only.txt", "squash merged elsewhere")
	git(t, business, "add", ".")
	git(t, business, "commit", "-m", "feat: unreleased local branch")
	t.Setenv("JANUS_MERGED_RELEASE_PRS", "business-repo:feature/LEN-40-delivery-flow:master:1234567890abcdef:PR #17")

	result, err := Verify(workspace, Options{
		RequirementID: "LEN-40",
		RepoName:      "harness-repo",
		BaseBranch:    "master",
		HeadBranch:    "feature/LEN-40-delivery-flow",
		Now:           fixedTime(),
	})

	if err != nil {
		t.Fatalf("expected merged release PR evidence to pass, got %v", err)
	}
	if len(result.Peers) != 1 || result.Peers[0].Status != "release_pr_merged" {
		t.Fatalf("expected release_pr_merged peer status, got %#v", result.Peers)
	}
	if result.Peers[0].Commit != "PR #17" {
		t.Fatalf("expected merged PR evidence, got %#v", result.Peers[0])
	}
}

func TestVerifyReleaseBoundRunsBusinessContractScanFormalOnly(t *testing.T) {
	workspace := t.TempDir()
	harness := initRepo(t, workspace, "harness-repo")
	business := initRepo(t, workspace, "business-repo")

	writeRequirement(t, harness, "LEN-40", `---
related_branch: "feature/LEN-40-delivery-flow"
target_branch: "master"
release_branch: "master"
affected_repositories:
  - business-repo
---

# LEN-40
	`)
	writeBusinessContractScanner(t, business)
	writeFile(t, business, "pom.xml", "formal")
	git(t, business, "add", ".")
	git(t, business, "commit", "-m", "test: add formal contract")
	git(t, business, "checkout", "-b", "epic/lending")
	git(t, business, "checkout", "master")
	git(t, business, "checkout", "-b", "feature/LEN-40-delivery-flow")
	writeFile(t, business, "pom.xml", "rc")
	git(t, business, "add", ".")
	git(t, business, "commit", "-m", "test: use rc contract")
	git(t, harness, "checkout", "-b", "feature/LEN-40-delivery-flow")
	git(t, harness, "checkout", "master")
	git(t, harness, "merge", "--no-ff", "feature/LEN-40-delivery-flow", "-m", "merge feature/LEN-40-delivery-flow")
	git(t, harness, "branch", "-D", "feature/LEN-40-delivery-flow")

	result, err := Verify(workspace, Options{
		RequirementID: "LEN-40",
		RepoName:      "business-repo",
		BaseBranch:    "master",
		HeadBranch:    "feature/LEN-40-delivery-flow",
		Now:           fixedTime(),
	})

	if err == nil {
		t.Fatal("expected release-bound RC dependency to fail")
	}
	if result == nil || result.ContractScan == nil {
		t.Fatalf("expected contract scan result, got %#v", result)
	}
	if result.ContractScan.Mode != "formal-only" {
		t.Fatalf("expected formal-only scan mode, got %q", result.ContractScan.Mode)
	}
	if !strings.Contains(err.Error(), "contract dependency scan failed") {
		t.Fatalf("expected contract scan failure, got %v", err)
	}
}

func TestVerifyIntegrationBoundAllowsBusinessContractRC(t *testing.T) {
	workspace := t.TempDir()
	harness := initRepo(t, workspace, "harness-repo")
	business := initRepo(t, workspace, "business-repo")

	writeRequirement(t, harness, "LEN-40", `---
related_branch: "feature/LEN-40-delivery-flow"
target_branch: "epic/lending"
release_branch: "master"
affected_repositories:
  - business-repo
---

# LEN-40
	`)
	writeBusinessContractScanner(t, business)
	writeFile(t, business, "pom.xml", "formal")
	git(t, business, "add", ".")
	git(t, business, "commit", "-m", "test: add formal contract")
	git(t, business, "checkout", "-b", "epic/lending")
	git(t, business, "checkout", "master")
	git(t, business, "checkout", "-b", "feature/LEN-40-delivery-flow")
	writeFile(t, business, "pom.xml", "rc")
	git(t, business, "add", ".")
	git(t, business, "commit", "-m", "test: use rc contract")
	git(t, harness, "checkout", "-b", "feature/LEN-40-delivery-flow")

	result, err := Verify(workspace, Options{
		RequirementID: "LEN-40",
		RepoName:      "business-repo",
		BaseBranch:    "epic/lending",
		HeadBranch:    "feature/LEN-40-delivery-flow",
		Now:           fixedTime(),
	})

	if err != nil {
		t.Fatalf("expected integration-bound RC dependency to pass, got %v", err)
	}
	if result.ContractScan == nil || result.ContractScan.Mode != "rc-or-formal" {
		t.Fatalf("expected rc-or-formal contract scan, got %#v", result.ContractScan)
	}
}

func TestVerifyReleaseBoundValidatesFormalTagAndJavaArtifact(t *testing.T) {
	workspace := t.TempDir()
	harness := initRepo(t, workspace, "harness-repo")
	business := initRepo(t, workspace, "business-repo")
	idl := initRepo(t, workspace, "idl-repo")

	writeRequirement(t, harness, "LEN-40", `---
related_branch: "feature/LEN-40-delivery-flow"
target_branch: "master"
release_branch: "master"
affected_repositories:
  - harness-repo
  - business-repo
  - idl-repo
---

# LEN-40
`)
	git(t, harness, "add", ".")
	git(t, harness, "commit", "-m", "docs: add requirement")
	git(t, harness, "checkout", "-b", "feature/LEN-40-delivery-flow")
	git(t, harness, "checkout", "master")
	git(t, harness, "merge", "--no-ff", "feature/LEN-40-delivery-flow", "-m", "merge feature/LEN-40-delivery-flow")
	git(t, idl, "checkout", "-b", "feature/LEN-40-delivery-flow")
	writeFile(t, idl, "contract.proto", "syntax = \"proto3\";")
	git(t, idl, "add", ".")
	git(t, idl, "commit", "-m", "feat: add contract")
	git(t, idl, "checkout", "master")
	git(t, idl, "merge", "--no-ff", "feature/LEN-40-delivery-flow", "-m", "merge feature/LEN-40-delivery-flow")
	git(t, idl, "tag", "v1.2.3")
	writeBusinessContractScanner(t, business)
	writeContractConfig(t, business)
	writePom(t, business, "1.0.0")
	git(t, business, "add", ".")
	git(t, business, "commit", "-m", "test: add formal contract")
	git(t, business, "checkout", "-b", "feature/LEN-40-delivery-flow")
	writePom(t, business, "1.2.3")
	git(t, business, "add", ".")
	git(t, business, "commit", "-m", "test: update formal contract")
	t.Setenv("JANUS_JAVA_ARTIFACT_VERSIONS", "1.2.3")

	result, err := Verify(workspace, Options{
		RequirementID: "LEN-40",
		RepoName:      "business-repo",
		BaseBranch:    "master",
		HeadBranch:    "feature/LEN-40-delivery-flow",
		Now:           fixedTime(),
	})

	if err != nil {
		t.Fatalf("expected formal evidence to pass, got %v", err)
	}
	if result.ContractScan == nil || len(result.ContractScan.FormalEvidence) != 2 {
		t.Fatalf("expected tag and artifact formal evidence, got %#v", result.ContractScan)
	}
	for _, evidence := range result.ContractScan.FormalEvidence {
		if evidence.Status != "passed" {
			t.Fatalf("expected formal evidence to pass, got %#v", result.ContractScan.FormalEvidence)
		}
	}
}

func TestVerifyReleaseBoundFormalTagUsesRemoteReleaseRef(t *testing.T) {
	workspace := t.TempDir()
	harness := initRepo(t, workspace, "harness-repo")
	business := initRepo(t, workspace, "business-repo")
	idl := initRepo(t, workspace, "idl-repo")

	writeRequirement(t, harness, "LEN-40", `---
related_branch: "feature/LEN-40-delivery-flow"
target_branch: "master"
release_branch: "master"
affected_repositories:
  - business-repo
---

# LEN-40
`)
	git(t, idl, "checkout", "-b", "release-squash")
	writeFile(t, idl, "contract.proto", "syntax = \"proto3\";")
	git(t, idl, "add", ".")
	git(t, idl, "commit", "-m", "feat: add contract")
	git(t, idl, "tag", "-a", "v1.2.3", "-m", "formal v1.2.3")
	remoteMaster := gitOutput(idl, "rev-parse", "HEAD")
	git(t, idl, "update-ref", "refs/remotes/origin/master", remoteMaster)
	git(t, idl, "checkout", "master")

	writeBusinessContractScanner(t, business)
	writeContractConfig(t, business)
	writePom(t, business, "1.0.0")
	git(t, business, "add", ".")
	git(t, business, "commit", "-m", "test: add formal contract")
	git(t, business, "checkout", "-b", "feature/LEN-40-delivery-flow")
	writePom(t, business, "1.2.3")
	git(t, business, "add", ".")
	git(t, business, "commit", "-m", "test: update formal contract")
	t.Setenv("JANUS_JAVA_ARTIFACT_VERSIONS", "1.2.3")

	result, err := Verify(workspace, Options{
		RequirementID: "LEN-40",
		RepoName:      "business-repo",
		BaseBranch:    "master",
		HeadBranch:    "feature/LEN-40-delivery-flow",
		Now:           fixedTime(),
	})

	if err != nil {
		t.Fatalf("expected remote release ref formal evidence to pass, got %v", err)
	}
	if result.ContractScan == nil || len(result.ContractScan.FormalEvidence) != 2 {
		t.Fatalf("expected tag and artifact formal evidence, got %#v", result.ContractScan)
	}
	if got := result.ContractScan.FormalEvidence[0].Detail; !strings.Contains(got, "formal tag v1.2.3 is reachable") {
		t.Fatalf("expected reachable formal tag detail, got %q", got)
	}
}

func TestVerifyReleaseBoundBlocksMissingFormalArtifact(t *testing.T) {
	workspace := t.TempDir()
	harness := initRepo(t, workspace, "harness-repo")
	business := initRepo(t, workspace, "business-repo")
	idl := initRepo(t, workspace, "idl-repo")

	writeRequirement(t, harness, "LEN-40", `---
related_branch: "feature/LEN-40-delivery-flow"
target_branch: "master"
release_branch: "master"
affected_repositories:
  - harness-repo
  - business-repo
  - idl-repo
---

# LEN-40
`)
	git(t, harness, "add", ".")
	git(t, harness, "commit", "-m", "docs: add requirement")
	git(t, harness, "checkout", "-b", "feature/LEN-40-delivery-flow")
	git(t, harness, "checkout", "master")
	git(t, harness, "merge", "--no-ff", "feature/LEN-40-delivery-flow", "-m", "merge feature/LEN-40-delivery-flow")
	git(t, idl, "checkout", "-b", "feature/LEN-40-delivery-flow")
	writeFile(t, idl, "contract.proto", "syntax = \"proto3\";")
	git(t, idl, "add", ".")
	git(t, idl, "commit", "-m", "feat: add contract")
	git(t, idl, "checkout", "master")
	git(t, idl, "merge", "--no-ff", "feature/LEN-40-delivery-flow", "-m", "merge feature/LEN-40-delivery-flow")
	git(t, idl, "tag", "v1.2.3")
	writeBusinessContractScanner(t, business)
	writeContractConfig(t, business)
	writePom(t, business, "1.0.0")
	git(t, business, "add", ".")
	git(t, business, "commit", "-m", "test: add formal contract")
	git(t, business, "checkout", "-b", "feature/LEN-40-delivery-flow")
	writePom(t, business, "1.2.3")
	git(t, business, "add", ".")
	git(t, business, "commit", "-m", "test: update formal contract")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("IDL_JAVA_REPO_TOKEN", "")

	result, err := Verify(workspace, Options{
		RequirementID: "LEN-40",
		RepoName:      "business-repo",
		BaseBranch:    "master",
		HeadBranch:    "feature/LEN-40-delivery-flow",
		Now:           fixedTime(),
	})

	if err == nil {
		t.Fatal("expected missing Java artifact evidence to block")
	}
	if result == nil || result.ContractScan == nil {
		t.Fatalf("expected contract scan result, got %#v", result)
	}
	if !strings.Contains(err.Error(), "missing GitHub token for Java artifact lookup") {
		t.Fatalf("expected artifact evidence error, got %v", err)
	}
}

func TestVerifyFailsWhenCurrentBaseIsNotTarget(t *testing.T) {
	workspace := t.TempDir()
	harness := initRepo(t, workspace, "harness-repo")
	initRepo(t, workspace, "business-repo")

	writeRequirement(t, harness, "LEN-40", `---
related_branch: "feature/LEN-40-delivery-flow"
target_branch: "epic/lending"
release_branch: "master"
affected_repositories:
  - business-repo
---

# LEN-40
`)

	_, err := Verify(workspace, Options{
		RequirementID: "LEN-40",
		RepoName:      "business-repo",
		BaseBranch:    "master",
		HeadBranch:    "feature/LEN-40-delivery-flow",
		Now:           fixedTime(),
	})

	if err == nil {
		t.Fatal("expected base mismatch to fail")
	}
	if !strings.Contains(err.Error(), `current PR base "master" does not match target_branch "epic/lending"`) {
		t.Fatalf("expected target branch error, got %v", err)
	}
}

func TestVerifyFailsWhenPeerHasNoValidStageState(t *testing.T) {
	workspace := t.TempDir()
	harness := initRepo(t, workspace, "harness-repo")
	initRepo(t, workspace, "business-repo")
	disableGitHubPREvidence(t)

	writeRequirement(t, harness, "LEN-40", `---
related_branch: "feature/LEN-40-delivery-flow"
target_branch: "master"
release_branch: "master"
affected_repositories:
  - business-repo
---

# LEN-40
`)

	_, err := Verify(workspace, Options{
		RequirementID: "LEN-40",
		RepoName:      "harness-repo",
		BaseBranch:    "master",
		HeadBranch:    "feature/LEN-40-delivery-flow",
		Now:           fixedTime(),
	})

	if err == nil {
		t.Fatal("expected missing peer state to fail")
	}
	if !strings.Contains(err.Error(), "business-repo has no acceptable peer state") {
		t.Fatalf("expected peer state error, got %v", err)
	}
}

func TestVerifyWritesGateReport(t *testing.T) {
	workspace := t.TempDir()
	harness := initRepo(t, workspace, "harness-repo")
	initRepo(t, workspace, "business-repo")
	disableGitHubPREvidence(t)

	writeRequirement(t, harness, "LEN-40", `---
related_branch: "feature/LEN-40-delivery-flow"
target_branch: "master"
release_branch: "master"
affected_repositories:
  - business-repo
---

# LEN-40
`)

	result, err := Verify(workspace, Options{
		RequirementID: "LEN-40",
		RepoName:      "harness-repo",
		BaseBranch:    "master",
		HeadBranch:    "feature/LEN-40-delivery-flow",
		OutputGate:    filepath.Join(harness, "requirements", "LEN-40", "gates", "release-readiness.gate.json"),
		Now:           fixedTime(),
	})

	if err == nil {
		t.Fatal("expected peer failure to keep result blocked")
	}
	if result == nil || result.GateID != "release-readiness" {
		t.Fatalf("expected blocked release readiness result, got %#v", result)
	}
	content := readFile(t, harness, "requirements/LEN-40/gates/release-readiness.gate.json")
	if !strings.Contains(content, `"gate_id": "release-readiness"`) {
		t.Fatalf("expected release-readiness gate, got %s", content)
	}
	if !strings.Contains(content, `"result": "BLOCKED"`) {
		t.Fatalf("expected blocked gate, got %s", content)
	}
}

func initRepo(t *testing.T, workspace string, name string) string {
	t.Helper()
	repo := filepath.Join(workspace, name)
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-b", "master")
	git(t, repo, "config", "user.email", "test@example.com")
	git(t, repo, "config", "user.name", "Test")
	writeFile(t, repo, ".gitkeep", name)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "chore: init")
	return repo
}

func writeRequirement(t *testing.T, harness string, requirementID string, content string) {
	t.Helper()
	path := filepath.Join("requirements", requirementID, "requirement.md")
	writeFile(t, harness, path, content)
}

func disableGitHubPREvidence(t *testing.T) {
	t.Helper()
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("JANUS_REPO_TOKEN", "")
	t.Setenv("BRANCH_COHERENCE_TOKEN", "")
	t.Setenv("JANUS_OPEN_RELEASE_PRS", "")
	t.Setenv("JANUS_MERGED_RELEASE_PRS", "")
}

func writeFile(t *testing.T, root string, relative string, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeBusinessContractScanner(t *testing.T, root string) {
	t.Helper()
	writeFile(t, root, "scripts/contract_dependency_scan.py", `#!/usr/bin/env python3
import argparse
import pathlib
import sys

parser = argparse.ArgumentParser()
parser.add_argument("--mode", required=True)
parser.add_argument("--root", default=".")
parser.add_argument("--path", action="append", default=[])
args = parser.parse_args()
paths = [pathlib.Path(args.root, path) for path in args.path] if args.path else [pathlib.Path(args.root, "pom.xml")]
text = "\n".join(path.read_text(encoding="utf-8") for path in paths if path.exists())
if "1.2.3-rc." in text:
    version = "rc"
elif "1.2.3" in text or "formal" in text:
    version = "formal"
else:
    version = "rc"
if args.mode == "formal-only" and version != "formal":
    print(f"blocked {version} for {args.mode}")
    sys.exit(1)
if args.mode == "rc-or-formal" and version not in {"rc", "formal"}:
    print(f"blocked {version} for {args.mode}")
    sys.exit(1)
print(f"allowed {version} for {args.mode}")
`)
}

func writeContractConfig(t *testing.T, root string) {
	t.Helper()
	writeFile(t, root, "config/contract-dependencies.json", `{
  "maven": [
    {
      "groupId": "com.spark.contract",
      "artifactId": "spark-idl-java"
    }
  ],
  "go": [
    {
      "modulePrefix": "github.com/spark-harness/idl-go-repo/"
    }
  ]
}
`)
}

func writePom(t *testing.T, root string, version string) {
	t.Helper()
	writeFile(t, root, "pom.xml", `<project>
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.spark.test</groupId>
  <artifactId>contract-consumer</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>com.spark.contract</groupId>
      <artifactId>spark-idl-java</artifactId>
      <version>`+version+`</version>
    </dependency>
  </dependencies>
</project>
`)
}

func readFile(t *testing.T, root string, relative string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

func fixedTime() time.Time {
	return time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
}
