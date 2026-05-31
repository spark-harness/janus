package cli

import (
	"fmt"
	"io"
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
	case "validate", "render", "verify":
		fmt.Fprintf(stderr, "gate %s is not implemented yet\n", args[0])
		return ExitInvalid
	default:
		fmt.Fprintf(stderr, "unknown gate subcommand %q\n", args[0])
		printGateUsage(stderr)
		return ExitInvalid
	}
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
