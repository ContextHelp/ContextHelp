package podrun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"hop.top/kit/go/console/output"
)

// tokenRe is the P9 allowlist for every value interpolated into a pod-side
// command string. Any character outside this set is refused before execution.
var tokenRe = regexp.MustCompile(`^[A-Za-z0-9._/:@=+-]+$`)

// ValidateToken verifies value contains only safe P9 characters.
func ValidateToken(field, value string) error {
	if value == "" {
		return output.UsageError(fmt.Sprintf("%s must not be empty", field))
	}
	if !tokenRe.MatchString(value) {
		return output.UsageError(fmt.Sprintf("%s %q contains unsupported characters", field, value))
	}
	return nil
}

// ShellQuote wraps a validated token in single quotes for remote POSIX sh.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// PodEnvelope mirrors the structured error JSON object pod emits on stderr
// ahead of its human-formatted error block.
type PodEnvelope struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	SuggestedFix string `json:"suggested_fix"`
	ExitCode     int    `json:"exit_code"`
}

// DecodeEnvelope extracts the first JSON object on stderr that looks like a
// pod error envelope. Returns (nil, false) when stderr has no envelope.
func DecodeEnvelope(stderr []byte) (*PodEnvelope, bool) {
	trimmed := bytes.TrimSpace(stderr)
	idx := bytes.IndexByte(trimmed, '{')
	if idx < 0 {
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed[idx:]))
	var env PodEnvelope
	if err := dec.Decode(&env); err != nil {
		return nil, false
	}
	if env.Code == "" && env.Message == "" && env.ExitCode == 0 {
		return nil, false
	}
	return &env, true
}

// stderrTail returns the last non-empty line (and up to 4 trailing lines) of
// stderr for error diagnostics when pod emitted no structured envelope.
func stderrTail(stderr []byte) (lastLine, tail string) {
	lines := strings.Split(strings.TrimSpace(string(stderr)), "\n")
	var clean []string
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t != "" {
			clean = append(clean, t)
		}
	}
	if len(clean) == 0 {
		return "", ""
	}
	lastLine = clean[len(clean)-1]
	if len(clean) > 4 {
		clean = clean[len(clean)-4:]
	}
	return lastLine, strings.Join(clean, "\n")
}

func appendFix(base, extra string) string {
	base = strings.TrimSpace(base)
	extra = strings.TrimSpace(extra)
	switch {
	case base == "":
		return extra
	case extra == "":
		return base
	default:
		return base + "; " + extra
	}
}

// MapError maps a failed pod subprocess invocation to a kit *output.Error
// following the exit-code taxonomy. pod's own SuggestedFix is always preserved.
func MapError(verb, podName string, timeout time.Duration, res Result, runErr error) *output.Error {
	// Deadline exceeded or timeout -> transient (exit 6).
	if res.TimedOut || errors.Is(runErr, context.DeadlineExceeded) {
		msg := fmt.Sprintf("pod %s did not finish within %s", verb, timeout)
		if podName != "" {
			msg = fmt.Sprintf("pod %s did not finish within %s; the pod may be half-changed, check \"pod show %s\"", verb, timeout, podName)
		}
		return &output.Error{
			Code:         output.CodeTransient,
			Message:      msg,
			SuggestedFix: fmt.Sprintf("inspect pod state with `pod show %s` and retry", podName),
			ExitCode:     exitCodeFor(output.CodeTransient, 6),
		}
	}

	// Process cancelled by caller context.
	if errors.Is(runErr, context.Canceled) || res.ExitCode == 130 {
		return &output.Error{
			Code:     output.CodeGeneric,
			Message:  fmt.Sprintf("pod %s cancelled", verb),
			ExitCode: 130,
		}
	}

	// Failed to start subprocess at all (no exit code).
	if runErr != nil && res.ExitCode == 0 {
		return &output.Error{
			Code:         output.CodePrerequisite,
			Message:      fmt.Sprintf("cannot start pod %s: %v", verb, runErr),
			SuggestedFix: "verify pod is installed and executable on PATH",
			ExitCode:     exitCodePrerequisite(),
		}
	}

	env, hasEnv := DecodeEnvelope(res.Stderr)
	lastLine, tail := stderrTail(res.Stderr)

	msg := ""
	fix := ""
	if hasEnv {
		msg = env.Message
		fix = env.SuggestedFix
	}
	if msg == "" {
		if lastLine != "" {
			msg = fmt.Sprintf("pod %s failed: %s", verb, lastLine)
		} else {
			msg = fmt.Sprintf("pod %s failed (exit %d)", verb, res.ExitCode)
		}
	}

	switch res.ExitCode {
	case 2:
		return &output.Error{
			Code:         output.CodePrerequisite,
			Message:      fmt.Sprintf("pod surface does not match what ctxt expects (%s): %s", verb, msg),
			SuggestedFix: appendFix(fix, "upgrade pod to a release that supports this verb and flags"),
			ExitCode:     exitCodePrerequisite(),
		}
	case 3:
		return &output.Error{
			Code:         output.CodeNotFound,
			Message:      msg,
			SuggestedFix: fix,
			ExitCode:     exitCodeFor(output.CodeNotFound, 3),
		}
	case 4:
		return &output.Error{
			Code:         output.CodeConflict,
			Message:      msg,
			SuggestedFix: fix,
			ExitCode:     exitCodeFor(output.CodeConflict, 4),
		}
	case 5:
		return &output.Error{
			Code:         output.CodeUnauthorized,
			Message:      msg,
			SuggestedFix: fix,
			ExitCode:     exitCodeFor(output.CodeUnauthorized, 5),
		}
	case 6:
		return &output.Error{
			Code:         output.CodeTransient,
			Message:      msg,
			SuggestedFix: fix,
			ExitCode:     exitCodeFor(output.CodeTransient, 6),
		}
	case 70:
		return &output.Error{
			Code:         output.CodePrerequisite,
			Message:      msg,
			SuggestedFix: fix,
			ExitCode:     exitCodePrerequisite(),
		}
	default:
		out := &output.Error{
			Code:         output.CodeGeneric,
			Message:      msg,
			SuggestedFix: fix,
			ExitCode:     exitCodeFor(output.CodeGeneric, 1),
		}
		if !hasEnv && tail != "" && tail != lastLine {
			out.Cause = tail
		}
		return out
	}
}

func exitCodeFor(class string, fallback int) int {
	if code, ok := output.ExitCodeForClass(class); ok {
		return code
	}
	return fallback
}
