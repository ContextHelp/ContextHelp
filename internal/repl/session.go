package repl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/peterh/liner"
)

const replPrompt = "ctxt> "

// SessionContextKey is the context key used to inject *SessionState into cobra commands.
type SessionContextKey struct{}

// Session owns the REPL lifecycle: liner state, history, job watcher, and read loop.
type Session struct {
	svc   *service.Service
	cfg   *config.Config
	state *SessionState
	watch *JobWatcher
}

// NewSession creates a Session. svc may be nil in tests that only exercise dispatch.
func NewSession(svc *service.Service, cfg *config.Config) *Session {
	return &Session{
		svc:   svc,
		cfg:   cfg,
		state: &SessionState{},
	}
}

// Run starts the interactive read loop and blocks until the user exits.
// Returns nil on clean exit (EOF / "exit" / "quit").
func (s *Session) Run(ctx context.Context) error {
	l := liner.NewLiner()
	defer l.Close()

	l.SetCtrlCAborts(true)
	l.SetMultiLineMode(false)

	completer := NewCompleter(s.state, &noopTagAdapter{})
	l.SetCompleter(func(line string) []string {
		return completer.Complete(line)
	})

	_ = LoadHistory(l)

	if s.svc != nil {
		s.watch = NewJobWatcher(s.svc.Bus, replPrompt, os.Stdout)
		s.watch.Start(ctx)
	}

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

	if s.watch != nil {
		s.watch.Stop()
	}
	_ = SaveHistory(l)
	return nil
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
