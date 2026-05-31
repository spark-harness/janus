package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"janus/internal/gate"
	"janus/internal/requirement"
)

const (
	ExitOK              = 0
	ExitBlocked         = 1
	ExitInvalid         = 2
	ExitMissing         = 3
	ExitInvalidWaiver   = 4
	ExitStaleInput      = 5
	ExitEvidenceFailure = 6

	Version = "0.1.0"
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
	case "hook":
		return runHook(args[1:], stdout, stderr)
	case "version", "--version":
		fmt.Fprintf(stdout, "janus %s\n", Version)
		return ExitOK
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
	case "render":
		return runGateRender(args[1:], stdout, stderr)
	case "verify":
		return runGateVerify(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown gate subcommand %q\n", args[0])
		printGateUsage(stderr)
		return ExitInvalid
	}
}

func runGateVerify(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("gate verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", "", "gate JSON path")
	if err := flags.Parse(args); err != nil {
		return ExitInvalid
	}
	if *input == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "gate verify requires --input")
		printGateUsage(stderr)
		return ExitInvalid
	}

	report, code := readValidGateFile(*input, stderr)
	if code != ExitOK {
		return code
	}
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "cannot determine working directory: %v\n", err)
		return ExitMissing
	}
	if err := gate.Verify(report, root, now()); err != nil {
		return printVerifyError(stderr, err)
	}

	fmt.Fprintln(stdout, "verified")
	return ExitOK
}

func runGateRender(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("gate render", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", "", "gate JSON path")
	output := flags.String("output", "", "gate Markdown path")
	check := flags.Bool("check", false, "check whether output is up to date")
	if err := flags.Parse(args); err != nil {
		return ExitInvalid
	}
	if *input == "" || *output == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "gate render requires --input and --output")
		printGateUsage(stderr)
		return ExitInvalid
	}

	report, code := readValidGateFile(*input, stderr)
	if code != ExitOK {
		return code
	}
	rendered := []byte(gate.Render(report, *input))

	if *check {
		current, err := os.ReadFile(*output)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				fmt.Fprintf(stderr, "missing file: %s\n", *output)
				return ExitMissing
			}
			fmt.Fprintf(stderr, "cannot read %s: %v\n", *output, err)
			return ExitMissing
		}
		if string(current) != string(rendered) {
			fmt.Fprintf(stderr, "rendered Markdown is out of date: %s\n", *output)
			return ExitInvalid
		}
		fmt.Fprintln(stdout, "up to date")
		return ExitOK
	}

	if err := os.WriteFile(*output, rendered, 0o644); err != nil {
		fmt.Fprintf(stderr, "cannot write %s: %v\n", *output, err)
		return ExitMissing
	}
	fmt.Fprintf(stdout, "rendered %s\n", *output)
	return ExitOK
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
		return runRequirementVerify(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown requirement subcommand %q\n", args[0])
		printRequirementUsage(stderr)
		return ExitInvalid
	}
}

func runRequirementVerify(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("requirement verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	requirementID := flags.String("requirement", "", "requirement id")
	target := flags.String("target", "", "verification target")
	if err := flags.Parse(args); err != nil {
		return ExitInvalid
	}
	if *requirementID == "" || *target == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "requirement verify requires --requirement and --target")
		printRequirementUsage(stderr)
		return ExitInvalid
	}

	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "cannot determine working directory: %v\n", err)
		return ExitMissing
	}
	if err := requirement.Verify(root, *requirementID, *target, now()); err != nil {
		return printRequirementVerifyError(stderr, err)
	}

	fmt.Fprintln(stdout, "verified")
	return ExitOK
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  janus gate validate <gate.json>")
	fmt.Fprintln(w, "  janus gate render --input <gate.json> --output <gate.md> [--check]")
	fmt.Fprintln(w, "  janus gate verify --input <gate.json>")
	fmt.Fprintln(w, "  janus requirement verify --requirement <id> --target merge")
	fmt.Fprintln(w, "  janus hook gate-drift-check [--root <repo-root>]")
	fmt.Fprintln(w, "  janus version")
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

func readValidGateFile(path string, stderr io.Writer) (*gate.Report, int) {
	report, code := readGateFile(path, stderr)
	if code != ExitOK {
		return nil, code
	}
	if err := gate.Validate(report); err != nil {
		printValidationError(stderr, err)
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

func printVerifyError(stderr io.Writer, err error) int {
	var verifyErr *gate.VerifyError
	if errors.As(err, &verifyErr) {
		for _, problem := range verifyErr.Problems {
			fmt.Fprintf(stderr, "- %s\n", problem)
		}
		return verifyErr.Code
	}
	fmt.Fprintln(stderr, err)
	return ExitInvalid
}

func printRequirementVerifyError(stderr io.Writer, err error) int {
	var verifyErr *requirement.VerifyError
	if errors.As(err, &verifyErr) {
		for _, problem := range verifyErr.Problems {
			fmt.Fprintf(stderr, "- %s\n", problem)
		}
		return verifyErr.Code
	}
	fmt.Fprintln(stderr, err)
	return ExitInvalid
}

var now = func() time.Time {
	return time.Now()
}
