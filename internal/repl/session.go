// Package repl is the interactive `ctxt shell`: line editing, history,
// completion, and dispatch of each line to a ctxt command.
package repl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/peterh/liner"
)

const replPrompt = "ctxt> "

// SessionContextKey is the context key used to inject *SessionState into cobra commands.
type SessionContextKey struct{}

// Session owns the REPL lifecycle: liner state, history and read loop.
// It holds no store: each line's command opens what it needs. Jobs run in
// dpkms, so the session prints no job notifications.
type Session struct {
	cfg   *config.Config
	state *SessionState
}

// NewSession creates a Session.
func NewSession(cfg *config.Config) *Session {
	return &Session{
		cfg:   cfg,
		state: &SessionState{},
	}
}

// Run starts the interactive read loop and blocks until the user exits.
// Returns nil on clean exit (EOF / "exit" / "quit"), or the error from
// restoring the terminal mode liner changed.
func (s *Session) Run(ctx context.Context) (err error) {
	l := liner.NewLiner()
	defer func() {
		if cerr := l.Close(); cerr != nil {
			err = errors.Join(err, fmt.Errorf("restore terminal: %w", cerr))
		}
	}()

	l.SetCtrlCAborts(true)
	l.SetMultiLineMode(false)

	completer := NewCompleter(s.state, &noopTagAdapter{})
	l.SetCompleter(func(line string) []string {
		return completer.Complete(line)
	})

	_ = LoadHistory(l, s.dataDir())

	ctx = context.WithValue(ctx, SessionContextKey{}, s.state)
	execCobra := buildCobraExecutor(ctx)

	for {
		line, err := l.Prompt(replPrompt)
		if err != nil {
			if errors.Is(err, liner.ErrPromptAborted) || errors.Is(err, io.EOF) {
				fmt.Fprintln(os.Stdout, "\nBye.")
				break
			}
			fmt.Fprintf(os.Stderr, "prompt error: %v\n", err)
			break
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		l.AppendHistory(line)

		if line == "exit" || line == "quit" {
			fmt.Fprintln(os.Stdout, "Bye.")
			break
		}

		if err := Dispatch(ctx, line, s.state, execCobra); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			// Continue reading — don't exit on command errors.
		}
	}

	_ = SaveHistory(l, s.dataDir())
	return nil
}

// dataDir is the session config's storage.data_dir, or "" without one.
func (s *Session) dataDir() string {
	if s.cfg == nil {
		return ""
	}
	return s.cfg.Storage.DataDir
}

// buildCobraExecutor returns the function that re-enters the cobra command tree.
// The actual executor is registered by shell.go via SetCobraExecHook before Run is called.
func buildCobraExecutor(ctx context.Context) func(args []string) error {
	return func(args []string) error {
		if cobraExecHook != nil {
			return cobraExecHook(ctx, args)
		}
		return fmt.Errorf("cobra executor not configured")
	}
}

// cobraExecHook is set by cmd/ctxt/cmd/shell.go to bridge back into cobra.
// Avoids import cycle: repl must not import cmd.
var cobraExecHook func(ctx context.Context, args []string) error

// SetCobraExecHook registers the cobra dispatcher. Called from shell.go before Run().
func SetCobraExecHook(fn func(ctx context.Context, args []string) error) {
	cobraExecHook = fn
}

// noopTagAdapter is a stub ServiceAdapter used until service.Service.ListTags is available.
type noopTagAdapter struct{}

func (n *noopTagAdapter) ListTags(_ context.Context) ([]string, error) { return nil, nil }
