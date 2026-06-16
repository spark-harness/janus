package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"janus/internal/requirement"
)

// hookStdin is the source of the PreToolUse event JSON. It is a package var so
// tests can feed a constructed event without touching os.Stdin.
var hookStdin io.Reader = os.Stdin

// guardEvent is the host-neutral envelope of a PreToolUse event. Claude, Codex
// and Gemini all deliver tool_name + tool_input on stdin.
type guardEvent struct {
	ToolName  string          `json:"tool_name"`
	CWD       string          `json:"cwd"`
	ToolInput json.RawMessage `json:"tool_input"`
}

func runHookGuardEdit(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "hook guard-edit accepts no arguments")
		printHookUsage(stderr)
		return ExitInvalid
	}

	raw, err := io.ReadAll(hookStdin)
	if err != nil {
		// Failing to read the event itself must not block the user's edit.
		return ExitOK
	}
	var event guardEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return ExitOK
	}

	targetPath, candidate, ok := guardCandidate(event)
	decision := requirement.GuardEdit(requirement.GuardInput{
		ToolName:         event.ToolName,
		TargetPath:       targetPath,
		CandidateContent: candidate,
		HasCandidate:     ok,
	})
	if decision.Deny {
		// Exit code 2 + stderr is the deny signal honored by Claude/Codex/Gemini.
		fmt.Fprintln(stderr, decision.Reason)
		return ExitHookDeny
	}
	return ExitOK
}

// guardCandidate dispatches to the host adapter selected by tool_name and
// returns (targetPath, resultingContent, ok). ok=false means the edit is not
// something the guard can reason about, so it is allowed.
func guardCandidate(event guardEvent) (string, string, bool) {
	switch event.ToolName {
	case "Write", "Edit", "MultiEdit":
		return claudeCandidate(event.ToolName, event.ToolInput)
	case "write_file", "replace":
		return geminiCandidate(event.ToolName, event.ToolInput)
	case "apply_patch":
		return codexCandidate(event.CWD, event.ToolInput)
	default:
		return "", "", false
	}
}

// geminiCandidate adapts Gemini CLI's write_file / replace tools. Gemini passes
// absolute file paths.
func geminiCandidate(toolName string, input json.RawMessage) (string, string, bool) {
	switch toolName {
	case "write_file":
		var w struct {
			FilePath string `json:"file_path"`
			Content  string `json:"content"`
		}
		if json.Unmarshal(input, &w) != nil || w.FilePath == "" {
			return "", "", false
		}
		return w.FilePath, w.Content, true
	case "replace":
		var e struct {
			FilePath  string `json:"file_path"`
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		}
		if json.Unmarshal(input, &e) != nil || e.FilePath == "" {
			return "", "", false
		}
		current, err := os.ReadFile(e.FilePath)
		if err != nil {
			return e.FilePath, "", false
		}
		next, ok := applyEdit(string(current), e.OldString, e.NewString, false)
		if !ok {
			return e.FilePath, "", false
		}
		return e.FilePath, next, true
	}
	return "", "", false
}

// codexCandidate adapts Codex CLI's apply_patch tool. tool_input.command holds
// the patch; paths are relative to the session cwd. It returns the first file
// op that targets a guarded artifact, resolving the path to absolute. For an
// Update whose hunk cannot be applied, the path is returned with ok=false so
// location/stage rules still apply even though approval state is unknown.
func codexCandidate(cwd string, input json.RawMessage) (string, string, bool) {
	var p struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(input, &p) != nil || p.Command == "" {
		return "", "", false
	}
	for _, op := range parseApplyPatch(p.Command) {
		abs := op.path
		if !filepath.IsAbs(abs) && cwd != "" {
			abs = filepath.Join(cwd, op.path)
		}
		if !requirement.IsGuardedArtifact(abs) {
			continue
		}
		if op.isAdd {
			return abs, op.added, true
		}
		current, err := os.ReadFile(abs)
		if err != nil {
			return abs, "", false
		}
		if op.oldBlock == "" || !strings.Contains(string(current), op.oldBlock) {
			return abs, "", false
		}
		return abs, strings.Replace(string(current), op.oldBlock, op.newBlock, 1), true
	}
	return "", "", false
}

type patchFileOp struct {
	path     string
	isAdd    bool
	added    string // full new content (Add File)
	oldBlock string // context+removed lines (Update File)
	newBlock string // context+added lines (Update File)
}

// parseApplyPatch parses the apply_patch envelope into per-file operations. For
// an Update, oldBlock/newBlock are reconstructed from context (' '), removed
// ('-') and added ('+') lines so a single string replacement reproduces the
// edit for the guard's purposes.
func parseApplyPatch(patch string) []patchFileOp {
	var ops []patchFileOp
	lines := strings.Split(patch, "\n")
	i := 0
	for i < len(lines) {
		line := lines[i]
		var op patchFileOp
		switch {
		case strings.HasPrefix(line, "*** Add File: "):
			op.isAdd = true
			op.path = strings.TrimSpace(strings.TrimPrefix(line, "*** Add File: "))
		case strings.HasPrefix(line, "*** Update File: "):
			op.path = strings.TrimSpace(strings.TrimPrefix(line, "*** Update File: "))
		default:
			i++
			continue
		}
		i++

		var added, oldLines, newLines []string
		for i < len(lines) && !strings.HasPrefix(lines[i], "*** ") {
			hl := lines[i]
			switch {
			case strings.HasPrefix(hl, "@@"):
				// hunk header; no content
			case strings.HasPrefix(hl, "+"):
				added = append(added, hl[1:])
				newLines = append(newLines, hl[1:])
			case strings.HasPrefix(hl, "-"):
				oldLines = append(oldLines, hl[1:])
			case strings.HasPrefix(hl, " "):
				oldLines = append(oldLines, hl[1:])
				newLines = append(newLines, hl[1:])
			default:
				oldLines = append(oldLines, hl)
				newLines = append(newLines, hl)
			}
			i++
		}
		if op.isAdd {
			op.added = strings.Join(added, "\n")
			if len(added) > 0 {
				op.added += "\n"
			}
		} else {
			op.oldBlock = strings.Join(oldLines, "\n")
			op.newBlock = strings.Join(newLines, "\n")
		}
		ops = append(ops, op)
	}
	return ops
}

func claudeCandidate(toolName string, input json.RawMessage) (string, string, bool) {
	switch toolName {
	case "Write":
		var w struct {
			FilePath string `json:"file_path"`
			Content  string `json:"content"`
		}
		if json.Unmarshal(input, &w) != nil || w.FilePath == "" {
			return "", "", false
		}
		return w.FilePath, w.Content, true
	case "Edit":
		var e struct {
			FilePath   string `json:"file_path"`
			OldString  string `json:"old_string"`
			NewString  string `json:"new_string"`
			ReplaceAll bool   `json:"replace_all"`
		}
		if json.Unmarshal(input, &e) != nil || e.FilePath == "" {
			return "", "", false
		}
		current, err := os.ReadFile(e.FilePath)
		if err != nil {
			return "", "", false
		}
		next, ok := applyEdit(string(current), e.OldString, e.NewString, e.ReplaceAll)
		if !ok {
			return "", "", false
		}
		return e.FilePath, next, true
	case "MultiEdit":
		var m struct {
			FilePath string `json:"file_path"`
			Edits    []struct {
				OldString  string `json:"old_string"`
				NewString  string `json:"new_string"`
				ReplaceAll bool   `json:"replace_all"`
			} `json:"edits"`
		}
		if json.Unmarshal(input, &m) != nil || m.FilePath == "" {
			return "", "", false
		}
		current, err := os.ReadFile(m.FilePath)
		if err != nil {
			return "", "", false
		}
		content := string(current)
		for _, ed := range m.Edits {
			next, ok := applyEdit(content, ed.OldString, ed.NewString, ed.ReplaceAll)
			if !ok {
				return "", "", false
			}
			content = next
		}
		return m.FilePath, content, true
	}
	return "", "", false
}

// applyEdit reproduces the Edit/MultiEdit string replacement so the guard can
// reason about the resulting file. ok=false when the edit cannot be applied
// (empty or absent old_string), in which case the real tool will error and the
// guard need not block.
func applyEdit(content string, oldStr string, newStr string, replaceAll bool) (string, bool) {
	if oldStr == "" {
		return "", false
	}
	if !strings.Contains(content, oldStr) {
		return "", false
	}
	if replaceAll {
		return strings.ReplaceAll(content, oldStr, newStr), true
	}
	return strings.Replace(content, oldStr, newStr, 1), true
}
