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
)

type VerifyError struct {
	Code     int
	Problems []string
}

func (e *VerifyError) Error() string {
	return strings.Join(e.Problems, "\n")
}

func Verify(report *Report, root string, now time.Time) error {
	if err := Validate(report); err != nil {
		return &VerifyError{Code: VerifyInvalid, Problems: []string{err.Error()}}
	}

	if problems := checkArtifacts(root, report.Inputs, true); len(problems) > 0 {
		return &VerifyError{Code: VerifyMissing, Problems: problems}
	}
	if problems := checkArtifacts(root, report.Inputs, false); len(problems) > 0 {
		return &VerifyError{Code: VerifyStaleInput, Problems: problems}
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
