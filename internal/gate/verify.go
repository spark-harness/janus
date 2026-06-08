package gate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	VerifyOK              = 0
	VerifyBlocked         = 1
	VerifyInvalid         = 2
	VerifyMissing         = 3
	VerifyInvalidWaiver   = 4
	VerifyStaleInput      = 5
	VerifyEvidenceFailure = 6
	VerifyBranchPolicy    = 7
)

type VerifyError struct {
	Code     int
	Problems []string
}

func (e *VerifyError) Error() string {
	return strings.Join(e.Problems, "\n")
}

type VerifyOptions struct {
	TicketID string
}

func Verify(report *Report, root string, now time.Time, options VerifyOptions) error {
	if err := Validate(report); err != nil {
		return &VerifyError{Code: VerifyInvalid, Problems: []string{err.Error()}}
	}

	if problems := checkArtifacts(root, report.Inputs, true); len(problems) > 0 {
		return &VerifyError{Code: VerifyMissing, Problems: problems}
	}
	if problems := checkArtifacts(root, report.Inputs, false); len(problems) > 0 {
		return &VerifyError{Code: VerifyStaleInput, Problems: problems}
	}
	if problems := checkRepoBranches(report.Repos, options.TicketID); len(problems) > 0 {
		return &VerifyError{Code: VerifyBranchPolicy, Problems: problems}
	}
	if report.Result == ResultBlocked {
		return &VerifyError{Code: VerifyBlocked, Problems: []string{"gate result is BLOCKED"}}
	}
	if report.Result == ResultWaived {
		if problems := checkWaiverFresh(report.Waiver, now); len(problems) > 0 {
			return &VerifyError{Code: VerifyInvalidWaiver, Problems: problems}
		}
	}
	if problems := checkArtifacts(root, report.Evidence, true); len(problems) > 0 {
		return &VerifyError{Code: VerifyEvidenceFailure, Problems: problems}
	}
	if problems := checkArtifacts(root, report.Evidence, false); len(problems) > 0 {
		return &VerifyError{Code: VerifyEvidenceFailure, Problems: problems}
	}

	return nil
}

func checkRepoBranches(repos []Repo, ticketID string) []string {
	if len(repos) == 0 {
		return nil
	}

	ticketID = strings.TrimSpace(ticketID)
	if ticketID == "" {
		return []string{"ticket-id is required when gate report includes repos"}
	}

	var problems []string
	var expectedBranch string
	for i, repo := range repos {
		name := strings.TrimSpace(repo.Name)
		branch := strings.TrimSpace(repo.Branch)
		if name == "" {
			name = fmt.Sprintf("repos[%d]", i)
		}
		if branch == "" {
			problems = append(problems, fmt.Sprintf("%s branch is required", name))
			continue
		}
		if expectedBranch == "" {
			expectedBranch = branch
		} else if branch != expectedBranch {
			problems = append(problems, fmt.Sprintf("%s branch %q does not match %q", name, branch, expectedBranch))
		}
		if !strings.Contains(strings.ToLower(branch), strings.ToLower(ticketID)) {
			problems = append(problems, fmt.Sprintf("%s branch %q does not contain ticket-id %q", name, branch, ticketID))
		}
	}
	return problems
}

func checkWaiverFresh(waiver Waiver, now time.Time) []string {
	expiresAt, err := time.Parse(time.RFC3339, waiver.ExpiresAt)
	if err != nil {
		return []string{"waiver.expires_at must be RFC3339"}
	}
	if !expiresAt.After(now) {
		return []string{"waiver has expired"}
	}
	return nil
}

func checkArtifacts(root string, artifacts []Artifact, missingOnly bool) []string {
	var problems []string
	for _, artifact := range artifacts {
		path := resolveArtifactPath(root, artifact.Path)
		actual, err := fileSHA256(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				problems = append(problems, fmt.Sprintf("missing file: %s", artifact.Path))
				continue
			}
			problems = append(problems, fmt.Sprintf("cannot read %s: %v", artifact.Path, err))
			continue
		}
		if missingOnly {
			continue
		}
		if actual != strings.ToLower(artifact.SHA256) {
			problems = append(problems, fmt.Sprintf("sha256 mismatch: %s", artifact.Path))
		}
	}
	return problems
}

func resolveArtifactPath(root string, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(root, filepath.Clean(path))
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
