package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/dblock"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/tui"
	"github.com/spf13/viper"
	"hop.top/kit/go/console/output"
)

// shellLine runs one shell line (already split into args) against db's
// config and the gated instance, as the REPL does.
func shellLine(t *testing.T, db *testDB, args ...string) error {
	t.Helper()
	resetAllFlags(rootCmd)
	for _, k := range []string{"format", "output", "output.format", "verbose", "quiet", "no-color", "no-hints", "profile", "profile.default", "offline", "offline.enabled"} {
		viper.Set(k, "")
	}
	viper.Set("instance", gateInstance)
	defer viper.Set("instance", "")
	defer rootCmd.SetArgs(nil)
	return shellExec(context.Background(), append([]string{"--config", db.ConfigPath, "--instance", gateInstance}, args...))
}

// The shell opens nothing for the session: it starts beside a running
// daemon, each line opens the store for its own command's class, a read
// works, and a write gets the daemon-held refusal with its fix.
func TestShell_OpensPerCommandBesideDaemon(t *testing.T) {
	db := setupTestDB(t)
	path := instanceDB(t)
	holdAsDaemon(t, path)
	restore := withInstance(t, db, gateInstance)
	defer restore()

	if _, err := prepareShell(shellCmd); err != nil {
		t.Fatalf("shell beside the daemon: %v", err)
	}
	if err := shellLine(t, db, "remind", "list"); err != nil {
		t.Fatalf("read inside the shell: %v", err)
	}
	err := shellLine(t, db, "remind", "clear", "obj-1")
	if !errors.Is(err, dblock.ErrHeld) {
		t.Fatalf("write inside the shell beside the daemon: %v, want lock contention", err)
	}
	if got := ExitCodeFor(err); got != output.ExitTransient {
		t.Errorf("exit %d, want %d", got, output.ExitTransient)
	}
	var env *output.Error
	if !errors.As(classifyReturn(err), &env) || !strings.Contains(env.SuggestedFix, "`dpkms shutdown --port 59177`") {
		t.Errorf("no stop-the-daemon fix: %v", env)
	}
}

// The TUI opens the store per operation: beside a running daemon a read
// works and a write gets the daemon-held refusal; nothing is held open.
func TestTUI_OpensPerOperationBesideDaemon(t *testing.T) {
	db := setupTestDB(t)
	path := instanceDB(t)
	holdAsDaemon(t, path)
	restore := withInstance(t, db, gateInstance)
	defer restore()

	a := tuiAdapter(tuiCmd)
	ctx := asOwner(context.Background())
	if _, _, err := a.ListObjects(ctx, storage.ObjectFilter{Limit: 5}); err != nil {
		t.Fatalf("read beside the daemon: %v", err)
	}
	if _, err := a.Analyze(ctx, tui.AnalyzeRequest{Content: "note"}); !errors.Is(err, dblock.ErrHeld) {
		t.Fatalf("write beside the daemon: %v, want lock contention", err)
	}
}

// fakeIngestAdapter yields one contact per fetch.
type fakeIngestAdapter struct{ n int }

func (f *fakeIngestAdapter) Name() string { return "fake" }
func (f *fakeIngestAdapter) Fetch(context.Context) ([]ingest.Object, error) {
	f.n++
	return []ingest.Object{{ID: fmt.Sprintf("c-%d", f.n), Type: "contact", Content: fmt.Sprintf("contact %d", f.n)}}, nil
}

// ingest --every opens the store per poll: a poll releases the lock when
// it ends (so the daemon can start), and the next poll beside the daemon
// is refused rather than writing through a handle held from before.
func TestIngestEvery_OpensPerPoll(t *testing.T) {
	db := setupTestDB(t)
	path := instanceDB(t)
	restore := withInstance(t, db, gateInstance)
	defer restore()

	a := &fakeIngestAdapter{}
	if _, err := ingestPoll(context.Background(), ingestCmd, a); err != nil {
		t.Fatalf("first poll: %v", err)
	}
	holdAsDaemon(t, path) // fails the test if the poll kept the lock
	if _, err := ingestPoll(context.Background(), ingestCmd, a); !errors.Is(err, dblock.ErrHeld) {
		t.Fatalf("poll beside the daemon: %v, want lock contention", err)
	}
}
