package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"janus/internal/gate"
)

const (
	ExitOK              = 0
	ExitBlocked         = 1
	ExitInvalid         = 2
	ExitMissing         = 3
	ExitInvalidWaiver   = 4
	ExitStaleInput      = 5
	ExitEvidenceFailure = 6
)

func Run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return ExitInvalid
	}

	switch args[0] {
	case "gate":
		return runGate(args[1:], stdout, stderr)
	case "requirement":
		return runRequirement(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return ExitOK
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		printUsage(stderr)
		return ExitInvalid
	}
}

func runGate(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing gate subcommand")
		printGateUsage(stderr)
		return ExitInvalid
	}

	switch args[0] {
	case "validate":
		return runGateValidate(args[1:], stdout, stderr)
	case "render", "verify":
		fmt.Fprintf(stderr, "gate %s is not implemented yet\n", args[0])
		return ExitInvalid
	default:
		fmt.Fprintf(stderr, "unknown gate subcommand %q\n", args[0])
		printGateUsage(stderr)
		return ExitInvalid
	}
}

func runGateValidate(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("gate validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return ExitInvalid
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "gate validate requires exactly one JSON file")
		printGateUsage(stderr)
		return ExitInvalid
	}

	report, code := readGateFile(flags.Arg(0), stderr)
	if code != ExitOK {
		return code
	}
	if err := gate.Validate(report); err != nil {
		printValidationError(stderr, err)
		return ExitInvalid
	}

	fmt.Fprintln(stdout, "valid")
	return ExitOK
}

func runRequirement(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing requirement subcommand")
		printRequirementUsage(stderr)
		return ExitInvalid
	}

	switch args[0] {
	case "verify":
		fmt.Fprintln(stderr, "requirement verify is not implemented yet")
		return ExitInvalid
	default:
		fmt.Fprintf(stderr, "unknown requirement subcommand %q\n", args[0])
		printRequirementUsage(stderr)
		return ExitInvalid
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  janus gate validate <gate.json>")
	fmt.Fprintln(w, "  janus gate render --input <gate.json> --output <gate.md> [--check]")
	fmt.Fprintln(w, "  janus gate verify --input <gate.json>")
	fmt.Fprintln(w, "  janus requirement verify --requirement <id> --target merge")
}

func printGateUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  janus gate validate <gate.json>")
	fmt.Fprintln(w, "  janus gate render --input <gate.json> --output <gate.md> [--check]")
	fmt.Fprintln(w, "  janus gate verify --input <gate.json>")
}

func printRequirementUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  janus requirement verify --requirement <id> --target merge")
}

func readGateFile(path string, stderr io.Writer) (*gate.Report, int) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(stderr, "missing file: %s\n", path)
			return nil, ExitMissing
		}
		fmt.Fprintf(stderr, "cannot read %s: %v\n", path, err)
		return nil, ExitMissing
	}
	defer file.Close()

	report, err := gate.Read(file)
	if err != nil {
		fmt.Fprintf(stderr, "invalid JSON: %v\n", err)
		return nil, ExitInvalid
	}
	return report, ExitOK
}

func printValidationError(stderr io.Writer, err error) {
	var validationErr *gate.ValidationError
	if errors.As(err, &validationErr) {
		for _, problem := range validationErr.Problems {
			fmt.Fprintf(stderr, "- %s\n", problem)
		}
		return
	}
	fmt.Fprintln(stderr, err)
}
