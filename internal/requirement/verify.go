package requirement

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"janus/internal/gate"
)

type VerifyError struct {
	Code     int
	Problems []string
}

func (e *VerifyError) Error() string {
	return strings.Join(e.Problems, "\n")
}

func Verify(root string, requirementID string, target string, now time.Time) error {
	if strings.TrimSpace(requirementID) == "" {
		return &VerifyError{Code: gate.VerifyInvalid, Problems: []string{"requirement is required"}}
	}
	if target != "merge" {
		return &VerifyError{Code: gate.VerifyInvalid, Problems: []string{"target must be merge"}}
	}

	gatePaths, err := filepath.Glob(filepath.Join(root, "requirements", requirementID, "gates", "*.gate.json"))
	if err != nil {
		return &VerifyError{Code: gate.VerifyInvalid, Problems: []string{err.Error()}}
	}
	sort.Strings(gatePaths)
	if len(gatePaths) == 0 {
		return &VerifyError{Code: gate.VerifyMissing, Problems: []string{fmt.Sprintf("missing gate reports for requirement %s", requirementID)}}
	}

	for _, path := range gatePaths {
		report, err := readGate(path)
		if err != nil {
			var verifyErr *VerifyError
			if errors.As(err, &verifyErr) {
				return verifyErr
			}
			return &VerifyError{Code: gate.VerifyInvalid, Problems: []string{err.Error()}}
		}
		if report.RequirementID != requirementID {
			return &VerifyError{Code: gate.VerifyInvalid, Problems: []string{fmt.Sprintf("%s has requirement_id %s", path, report.RequirementID)}}
		}
		if err := checkIDLPolicy(report); err != nil {
			return err
		}
		if err := gate.Verify(report, root, now); err != nil {
			var verifyErr *gate.VerifyError
			if errors.As(err, &verifyErr) {
				return &VerifyError{Code: verifyErr.Code, Problems: prefixProblems(path, verifyErr.Problems)}
			}
			return &VerifyError{Code: gate.VerifyInvalid, Problems: []string{err.Error()}}
		}
	}

	return nil
}

func readGate(path string) (*gate.Report, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, &VerifyError{Code: gate.VerifyMissing, Problems: []string{"missing file: " + path}}
		}
		return nil, &VerifyError{Code: gate.VerifyMissing, Problems: []string{fmt.Sprintf("cannot read %s: %v", path, err)}}
	}
	defer file.Close()

	report, err := gate.Read(file)
	if err != nil {
		return nil, &VerifyError{Code: gate.VerifyInvalid, Problems: []string{fmt.Sprintf("invalid JSON in %s: %v", path, err)}}
	}
	return report, nil
}

func checkIDLPolicy(report *gate.Report) error {
	if report.IDLImpact == nil {
		return nil
	}
	if report.IDLImpact.Impact == "yes" && len(report.Evidence) == 0 {
		return &VerifyError{Code: gate.VerifyEvidenceFailure, Problems: []string{fmt.Sprintf("%s declares IDL impact but has no evidence", report.GateID)}}
	}
	return nil
}

func prefixProblems(path string, problems []string) []string {
	prefixed := make([]string, 0, len(problems))
	for _, problem := range problems {
		prefixed = append(prefixed, fmt.Sprintf("%s: %s", path, problem))
	}
	return prefixed
}
