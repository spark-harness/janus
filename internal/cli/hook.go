package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"janus/internal/gate"
)

type hookResponse struct {
	Continue       bool   `json:"continue"`
	SuppressOutput bool   `json:"suppressOutput,omitempty"`
	StopReason     string `json:"stopReason,omitempty"`
	SystemMessage  string `json:"systemMessage,omitempty"`
}

func runHook(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing hook subcommand")
		printHookUsage(stderr)
		return ExitInvalid
	}

	switch args[0] {
	case "gate-drift-check":
		return runHookGateJSONCheck(args[1:], stdout, stderr)
	case "guard-edit":
		return runHookGuardEdit(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown hook subcommand %q\n", args[0])
		printHookUsage(stderr)
		return ExitInvalid
	}
}

func runHookGateJSONCheck(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("hook gate-drift-check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "repository root")
	if err := flags.Parse(args); err != nil {
		return ExitInvalid
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "hook gate-drift-check accepts only --root")
		printHookUsage(stderr)
		return ExitInvalid
	}

	repoRoot := *root
	if repoRoot == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "cannot determine working directory: %v\n", err)
			return ExitMissing
		}
		repoRoot = wd
	}

	issues, err := checkGateJSONReports(repoRoot)
	if err != nil {
		issues = append(issues, err.Error())
	}

	response := hookResponse{Continue: true, SuppressOutput: true}
	if len(issues) > 0 {
		response = hookResponse{
			Continue:      false,
			StopReason:    strings.Join(issues, "\n"),
			SystemMessage: "Janus gate JSON reports are invalid. Fix gate JSON before finishing.",
		}
	}

	if err := json.NewEncoder(stdout).Encode(response); err != nil {
		fmt.Fprintf(stderr, "cannot encode hook response: %v\n", err)
		return ExitInvalid
	}
	return ExitOK
}

func checkGateJSONReports(root string) ([]string, error) {
	var issues []string
	requirementsDir := filepath.Join(root, "requirements")
	if _, err := os.Stat(requirementsDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot stat requirements directory: %v", err)
	}

	err := filepath.WalkDir(requirementsDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			issues = append(issues, fmt.Sprintf("cannot read %s: %v", cleanRel(root, path), err))
			return nil
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".gate.json") {
			return nil
		}

		issues = append(issues, checkSingleGateJSON(root, path)...)
		return nil
	})
	if err != nil {
		return issues, err
	}
	return issues, nil
}

func checkSingleGateJSON(root string, gateJSONPath string) []string {
	relJSON := cleanRel(root, gateJSONPath)

	file, err := os.Open(gateJSONPath)
	if err != nil {
		return []string{fmt.Sprintf("cannot read gate JSON: %s: %v", relJSON, err)}
	}
	defer file.Close()

	report, err := gate.Read(file)
	if err != nil {
		return []string{fmt.Sprintf("invalid gate JSON: %s: %v", relJSON, err)}
	}
	if err := gate.Validate(report); err != nil {
		return []string{fmt.Sprintf("invalid gate JSON: %s: %v", relJSON, err)}
	}
	return nil
}

func cleanRel(root string, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.ToSlash(rel)
}

func printHookUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  janus hook gate-drift-check [--root <repo-root>]")
	fmt.Fprintln(w, "  janus hook guard-edit   (reads a PreToolUse event JSON on stdin)")
}
