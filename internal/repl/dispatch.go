package repl

import (
	"context"
	"strconv"
	"strings"
)

// Dispatch routes a REPL input line to the appropriate handler.
//
// Priority chain (evaluated top to bottom, first match wins):
//  1. Empty line or line beginning with '#' -> no-op, return nil
//  1.5. `show N` shorthand — resolve 1-based index to full object ID
//  2. First token is a known command -> delegate to execCobra
//  3. Line contains ' | ' (space-pipe-space) -> pipe: run LHS as find, then RHS as make
//  4. Line contains '==' or '=in=' -> treat as RSQL; delegate as `list --q <line>`
//  5. Else -> treat as NLQ; delegate as `find <words...>`
func Dispatch(ctx context.Context, line string, state *SessionState, execCobra func(args []string) error) error {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return nil
	}

	// Branch 1.5: `show N` shorthand — resolve 1-based index to full object ID.
	if strings.HasPrefix(line, "show ") {
		suffix := strings.TrimSpace(strings.TrimPrefix(line, "show "))
		if n, parseErr := strconv.Atoi(suffix); parseErr == nil {
			obj, resolveErr := state.ResolveIndex(n)
			if resolveErr != nil {
				return resolveErr
			}
			return execCobra([]string{"show", obj.ID})
		}
		// Not a pure integer — fall through to known-command dispatch.
	}

	// Branch 2: known cobra command.
	firstToken := strings.SplitN(line, " ", 2)[0]
	for _, cmd := range knownCommands {
		if firstToken == cmd {
			return execCobra(splitArgs(line))
		}
	}

	// Branch 3: pipe syntax (" | " with surrounding spaces).
	if lhs, rhs, isPipe := ParsePipe(line); isPipe {
		return dispatchPipe(ctx, lhs, rhs, state, execCobra)
	}

	// Branch 4: RSQL expression.
	if strings.Contains(line, "==") || strings.Contains(line, "=in=") {
		return execCobra([]string{"list", "--q", line})
	}

	// Branch 5: NLQ fallback.
	return execCobra(append([]string{"find"}, splitArgs(line)...))
}

// dispatchPipe executes a piped expression: LHS as find, then builds and
// runs a make command targeting the results.
func dispatchPipe(ctx context.Context, lhs, rhs string, state *SessionState, execCobra func(args []string) error) error {
	lhsArgs := splitArgs(lhs)
	if len(lhsArgs) > 0 && lhsArgs[0] != "find" {
		lhsArgs = append([]string{"find"}, lhsArgs...)
	}
	if err := execCobra(lhsArgs); err != nil {
		return err
	}

	if len(state.LastResults) == 0 {
		return nil
	}

	rhsArgs := splitArgs(rhs)
	artifactType := ""
	if len(rhsArgs) >= 2 && rhsArgs[0] == "compose" {
		artifactType = rhsArgs[1]
	} else if len(rhsArgs) == 1 {
		artifactType = rhsArgs[0]
	}
	if artifactType == "" {
		return nil
	}

	makeCmd := BuildPipeCommand(state.LastResults, artifactType)
	if makeCmd == "" {
		return nil
	}
	return execCobra(splitArgs(makeCmd))
}

// splitArgs splits a command line into arguments, respecting double-quoted strings.
func splitArgs(line string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	for _, ch := range line {
		switch {
		case ch == '"':
			inQuote = !inQuote
		case ch == ' ' && !inQuote:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}
