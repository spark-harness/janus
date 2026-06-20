package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"janus/internal/delivery"
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
	ExitBranchPolicy    = 7

	// ExitHookDeny is returned by PreToolUse-style hooks to block a tool call.
	// Claude Code, Codex, and Gemini all treat exit code 2 (with the reason on
	// stderr) as a deny; it shares the value of ExitInvalid by protocol.
	ExitHookDeny = 2

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
	case "delivery":
		return runDelivery(args[1:], stdout, stderr)
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

func runDelivery(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing delivery subcommand")
		printDeliveryUsage(stderr)
		return ExitInvalid
	}

	switch args[0] {
	case "verify":
		return runDeliveryVerify(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown delivery subcommand %q\n", args[0])
		printDeliveryUsage(stderr)
		return ExitInvalid
	}
}

func runDeliveryVerify(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("delivery verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	requirementID := flags.String("requirement", "", "requirement id")
	repoName := flags.String("repo", "", "current repository name")
	workspace := flags.String("workspace", "..", "multi-repo workspace root")
	baseBranch := flags.String("base", "", "current PR base branch")
	headBranch := flags.String("head", "", "current PR head branch")
	outputGate := flags.String("output-gate", "", "optional readiness gate JSON path")
	if err := flags.Parse(args); err != nil {
		return ExitInvalid
	}
	if *requirementID == "" || *repoName == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "delivery verify requires --requirement and --repo")
		printDeliveryUsage(stderr)
		return ExitInvalid
	}
	result, err := delivery.Verify(*workspace, delivery.Options{
		RequirementID: *requirementID,
		RepoName:      *repoName,
		BaseBranch:    *baseBranch,
		HeadBranch:    *headBranch,
		OutputGate:    *outputGate,
		Now:           now(),
	})
	if err != nil {
		var verifyErr *delivery.VerifyError
		if errors.As(err, &verifyErr) {
			for _, problem := range verifyErr.Problems {
				fmt.Fprintf(stderr, "- %s\n", problem)
			}
			if result != nil {
				printDeliveryResult(stdout, result)
			}
			return verifyErr.Code
		}
		fmt.Fprintln(stderr, err)
		return ExitInvalid
	}
	printDeliveryResult(stdout, result)
	return ExitOK
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
	ticketID := flags.String("ticket-id", "", "ticket id for repo branch policy checks")
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
	if err := gate.Verify(report, root, now(), gate.VerifyOptions{TicketID: *ticketID}); err != nil {
		return printVerifyError(stderr, err)
	}

	fmt.Fprintln(stdout, "verified")
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
	case "new":
		return runRequirementNew(args[1:], stdout, stderr)
	case "status":
		return runRequirementStatus(args[1:], stdout, stderr)
	case "gate-check":
		return runRequirementGateCheck(args[1:], stdout, stderr)
	case "approve":
		return runRequirementApprove(args[1:], stdout, stderr)
	case "next":
		return runRequirementNext(args[1:], stdout, stderr)
	case "verify":
		return runRequirementVerify(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown requirement subcommand %q\n", args[0])
		printRequirementUsage(stderr)
		return ExitInvalid
	}
}

func runRequirementNew(args []string, stdout io.Writer, stderr io.Writer) int {
	args = normalizeRequirementPositional(args, "title", "owner")
	flags := flag.NewFlagSet("requirement new", flag.ContinueOnError)
	flags.SetOutput(stderr)
	title := flags.String("title", "", "requirement title")
	owner := flags.String("owner", "", "requirement owner")
	force := flags.Bool("force", false, "overwrite existing template files")
	if err := flags.Parse(args); err != nil {
		return ExitInvalid
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "requirement new requires exactly one requirement id")
		printRequirementUsage(stderr)
		return ExitInvalid
	}
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "cannot determine working directory: %v\n", err)
		return ExitMissing
	}
	requirementID := flags.Arg(0)
	if err := requirement.Create(root, requirementID, requirement.NewOptions{Title: *title, Owner: *owner, Force: *force}, now()); err != nil {
		return printLifecycleError(stderr, err)
	}
	fmt.Fprintf(stdout, "created requirements/%s\n", requirementID)
	return ExitOK
}

func runRequirementStatus(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("requirement status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return ExitInvalid
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "requirement status requires exactly one requirement id")
		printRequirementUsage(stderr)
		return ExitInvalid
	}
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "cannot determine working directory: %v\n", err)
		return ExitMissing
	}
	status, err := requirement.Inspect(root, flags.Arg(0), now())
	if err != nil {
		return printLifecycleError(stderr, err)
	}
	printRequirementStatus(stdout, status)
	if len(status.Problems) > 0 {
		return ExitBlocked
	}
	return ExitOK
}

func runRequirementGateCheck(args []string, stdout io.Writer, stderr io.Writer) int {
	args = normalizeRequirementPositional(args, "requirement", "gate", "owner")
	flags := flag.NewFlagSet("requirement gate-check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	requirementID := flags.String("requirement", "", "requirement id")
	gateID := flags.String("gate", "", "gate id")
	owner := flags.String("owner", "", "human approval owner")
	if err := flags.Parse(args); err != nil {
		return ExitInvalid
	}
	if *requirementID == "" && flags.NArg() == 1 {
		*requirementID = flags.Arg(0)
	} else if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "requirement gate-check accepts at most one positional requirement id")
		printRequirementUsage(stderr)
		return ExitInvalid
	}
	if *requirementID == "" || *gateID == "" {
		fmt.Fprintln(stderr, "requirement gate-check requires --requirement and --gate")
		printRequirementUsage(stderr)
		return ExitInvalid
	}
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "cannot determine working directory: %v\n", err)
		return ExitMissing
	}
	result, err := requirement.RunGateCheck(root, *requirementID, requirement.GateCheckOptions{GateID: *gateID, Owner: *owner, Now: now()})
	if err != nil {
		return printLifecycleError(stderr, err)
	}
	fmt.Fprintf(stdout, "Gate: %s\n", result.Report.GateID)
	fmt.Fprintf(stdout, "Result: %s\n", result.Report.Result)
	fmt.Fprintf(stdout, "Source: %s\n", result.JSONPath)
	fmt.Fprintf(stdout, "Reason: %s\n", result.Report.Decision)
	if result.Report.Result == gate.ResultBlocked {
		return ExitBlocked
	}
	return ExitOK
}

func runRequirementApprove(args []string, stdout io.Writer, stderr io.Writer) int {
	args = normalizeRequirementPositional(args, "requirement", "gate", "approved-by", "approved-at", "decision")
	flags := flag.NewFlagSet("requirement approve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	requirementID := flags.String("requirement", "", "requirement id")
	gateID := flags.String("gate", "", "gate id")
	approvedBy := flags.String("approved-by", "", "human approver")
	decision := flags.String("decision", "", "approval decision text")
	approvedAt := flags.String("approved-at", "", "approval time (RFC3339); defaults to now")
	yes := flags.Bool("yes", false, "confirm a non-interactive approval")
	if err := flags.Parse(args); err != nil {
		return ExitInvalid
	}
	if *requirementID == "" && flags.NArg() == 1 {
		*requirementID = flags.Arg(0)
	} else if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "requirement approve accepts at most one positional requirement id")
		printRequirementUsage(stderr)
		return ExitInvalid
	}
	if *requirementID == "" || *gateID == "" {
		fmt.Fprintln(stderr, "requirement approve requires --requirement and --gate")
		printRequirementUsage(stderr)
		return ExitInvalid
	}

	// Approval is a deliberate human act. Refuse non-interactive runs (an agent
	// shelling out) unless a human explicitly passes --yes.
	if !*yes && !stdinIsInteractive() {
		fmt.Fprintln(stderr, "requirement approve refuses to run non-interactively without --yes")
		fmt.Fprintln(stderr, "human approval only: re-run with --yes (e.g. ! janus requirement approve --requirement <id> --gate <gate> --approved-by <you> --decision <text> --yes)")
		return ExitBlocked
	}

	at := strings.TrimSpace(*approvedAt)
	if at == "" {
		at = now().Format(time.RFC3339)
	}
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "cannot determine working directory: %v\n", err)
		return ExitMissing
	}
	if err := requirement.Approve(root, *requirementID, requirement.ApproveOptions{
		GateID:     *gateID,
		ApprovedBy: *approvedBy,
		ApprovedAt: at,
		Decision:   *decision,
	}); err != nil {
		return printLifecycleError(stderr, err)
	}
	fmt.Fprintf(stdout, "approved %s for %s\n", *gateID, *requirementID)
	return ExitOK
}

// stdinIsInteractive reports whether stdin is a terminal. It is a best-effort
// guard against an agent shelling out to approve non-interactively; it is a
// package var so tests can force the non-interactive branch deterministically.
var stdinIsInteractive = func() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func runRequirementNext(args []string, stdout io.Writer, stderr io.Writer) int {
	args = normalizeRequirementPositional(args, "requirement", "ticket-id")
	flags := flag.NewFlagSet("requirement next", flag.ContinueOnError)
	flags.SetOutput(stderr)
	requirementID := flags.String("requirement", "", "requirement id")
	ticketID := flags.String("ticket-id", "", "ticket id for repo branch policy checks")
	if err := flags.Parse(args); err != nil {
		return ExitInvalid
	}
	if *requirementID == "" && flags.NArg() == 1 {
		*requirementID = flags.Arg(0)
	} else if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "requirement next accepts at most one positional requirement id")
		printRequirementUsage(stderr)
		return ExitInvalid
	}
	if *requirementID == "" {
		fmt.Fprintln(stderr, "requirement next requires --requirement")
		printRequirementUsage(stderr)
		return ExitInvalid
	}
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "cannot determine working directory: %v\n", err)
		return ExitMissing
	}
	result, err := requirement.Advance(root, *requirementID, now(), *ticketID)
	if err != nil {
		return printLifecycleError(stderr, err)
	}
	if result.GateID == "" {
		fmt.Fprintf(stdout, "advanced %s from %s to %s\n", result.RequirementID, result.PreviousStage, result.CurrentStage)
	} else {
		fmt.Fprintf(stdout, "advanced %s from %s to %s via %s\n", result.RequirementID, result.PreviousStage, result.CurrentStage, result.GateID)
	}
	return ExitOK
}

func runRequirementVerify(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("requirement verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	requirementID := flags.String("requirement", "", "requirement id")
	target := flags.String("target", "", "verification target")
	ticketID := flags.String("ticket-id", "", "ticket id for repo branch policy checks")
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
	if err := requirement.Verify(root, *requirementID, *target, now(), requirement.VerifyOptions{TicketID: *ticketID}); err != nil {
		return printRequirementVerifyError(stderr, err)
	}

	fmt.Fprintln(stdout, "verified")
	return ExitOK
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  janus delivery verify --requirement <id> --repo <repo-name> [--workspace <path>] [--base <branch>] [--head <branch>] [--output-gate <path>]")
	fmt.Fprintln(w, "  janus gate validate <gate.json>")
	fmt.Fprintln(w, "  janus gate verify --input <gate.json> [--ticket-id <id>]")
	fmt.Fprintln(w, "  janus requirement new <id> [--title <title>] [--owner <owner>] [--force]")
	fmt.Fprintln(w, "  janus requirement status <id>")
	fmt.Fprintln(w, "  janus requirement gate-check --requirement <id> --gate <gate-id> [--owner <owner>]")
	fmt.Fprintln(w, "  janus requirement approve --requirement <id> --gate <gate-id> --approved-by <name> --decision <text> [--approved-at <rfc3339>] [--yes]")
	fmt.Fprintln(w, "  janus requirement next --requirement <id> [--ticket-id <id>]")
	fmt.Fprintln(w, "  janus requirement verify --requirement <id> --target merge [--ticket-id <id>]")
	fmt.Fprintln(w, "  janus hook gate-drift-check [--root <repo-root>]")
	fmt.Fprintln(w, "  janus version")
}

func printDeliveryUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  janus delivery verify --requirement <id> --repo <repo-name> [--workspace <path>] [--base <branch>] [--head <branch>] [--output-gate <path>]")
}

func printGateUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  janus gate validate <gate.json>")
	fmt.Fprintln(w, "  janus gate verify --input <gate.json> [--ticket-id <id>]")
}

func printRequirementUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  janus requirement new <id> [--title <title>] [--owner <owner>] [--force]")
	fmt.Fprintln(w, "  janus requirement status <id>")
	fmt.Fprintln(w, "  janus requirement gate-check --requirement <id> --gate <gate-id> [--owner <owner>]")
	fmt.Fprintln(w, "  janus requirement approve --requirement <id> --gate <gate-id> --approved-by <name> --decision <text> [--approved-at <rfc3339>] [--yes]")
	fmt.Fprintln(w, "  janus requirement next --requirement <id> [--ticket-id <id>]")
	fmt.Fprintln(w, "  janus requirement verify --requirement <id> --target merge [--ticket-id <id>]")
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

func printLifecycleError(stderr io.Writer, err error) int {
	var lifecycleErr *requirement.LifecycleError
	if errors.As(err, &lifecycleErr) {
		for _, problem := range lifecycleErr.Problems {
			fmt.Fprintf(stderr, "- %s\n", problem)
		}
		return lifecycleErr.Code
	}
	return printRequirementVerifyError(stderr, err)
}

func printRequirementStatus(stdout io.Writer, status *requirement.Status) {
	fmt.Fprintf(stdout, "Requirement: %s\n", status.RequirementID)
	fmt.Fprintf(stdout, "Current Stage: %s\n", status.CurrentStage)
	fmt.Fprintln(stdout, "Artifacts:")
	for _, artifact := range status.Artifacts {
		state := "OK"
		if !artifact.Exists {
			state = "MISSING"
		}
		fmt.Fprintf(stdout, "  - %s: %s\n", artifact.Path, state)
	}
	fmt.Fprintln(stdout, "Gates:")
	for _, gateStatus := range status.Gates {
		state := "MISSING"
		if gateStatus.Exists {
			state = gateStatus.Result
			if !gateStatus.Valid {
				state = "INVALID"
			} else if gateStatus.Stale {
				state += " (STALE)"
			}
		}
		fmt.Fprintf(stdout, "  - %s [%s]: %s\n", gateStatus.GateID, gateStatus.Stage, state)
	}
	if len(status.Problems) > 0 {
		fmt.Fprintln(stdout, "Problems:")
		for _, problem := range status.Problems {
			fmt.Fprintf(stdout, "  - %s\n", problem)
		}
	}
	fmt.Fprintf(stdout, "Next Action: %s\n", status.NextAction)
}

func printDeliveryResult(stdout io.Writer, result *delivery.Result) {
	fmt.Fprintf(stdout, "Gate: %s\n", result.GateID)
	fmt.Fprintf(stdout, "Requirement: %s\n", result.RequirementID)
	fmt.Fprintf(stdout, "Bound: %s\n", result.Bound)
	fmt.Fprintf(stdout, "Contract Mode: %s\n", result.ContractMode)
	if result.ContractScan != nil {
		fmt.Fprintf(stdout, "Contract Scan: %s %s %s\n", result.ContractScan.Repo, result.ContractScan.Status, result.ContractScan.Mode)
		for _, evidence := range result.ContractScan.FormalEvidence {
			fmt.Fprintf(stdout, "Formal Evidence: %s %s %s\n", evidence.Dependency, evidence.Status, evidence.Detail)
		}
	}
	for _, peer := range result.Peers {
		fmt.Fprintf(stdout, "Peer: %s %s %s\n", peer.Repo, peer.Status, peer.Branch)
	}
}

func normalizeRequirementPositional(args []string, flagNames ...string) []string {
	known := map[string]bool{}
	for _, name := range flagNames {
		known["-"+name] = true
		known["--"+name] = true
	}
	var flagsAndValues []string
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flagsAndValues = append(flagsAndValues, arg)
			if strings.Contains(arg, "=") {
				continue
			}
			if known[arg] && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flagsAndValues = append(flagsAndValues, args[i+1])
				i++
			}
			continue
		}
		positional = append(positional, arg)
	}
	return append(flagsAndValues, positional...)
}

var now = func() time.Time {
	return time.Now()
}
