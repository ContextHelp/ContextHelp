package cmd

import (
	"strings"
	"testing"
)

// `ctxt "<text>"` routes to analyze, as the long help documents.
func TestRootPositionalRoutesToAnalyze(t *testing.T) {
	for _, args := range [][]string{
		{"some text to remember"},
		{"remember", "the", "milk"},
	} {
		rec := newAnalyzeRecorder(t)
		out, err := executeCommand(append(args, "--server", rec.srv.URL)...)
		if err != nil {
			t.Fatalf("ctxt %q: %v", args, err)
		}
		if !strings.Contains(out, "Job ID:") {
			t.Errorf("ctxt %q: output lacks job id: %s", args, out)
		}
		if len(rec.bodies) != 1 || rec.bodies[0]["content"] != strings.Join(args, " ") {
			t.Errorf("ctxt %q: bodies = %v", args, rec.bodies)
		}
	}
}

// A likely subcommand typo stays an unknown-command error with its
// suggestion and captures nothing.
func TestRootTypoStaysUnknownCommand(t *testing.T) {
	rec := newAnalyzeRecorder(t)
	_, err := executeCommand("fnd", "--server", rec.srv.URL)
	if err == nil {
		t.Fatal("ctxt fnd succeeded; want unknown command")
	}
	if !strings.Contains(err.Error(), `unknown command "fnd"`) || !strings.Contains(err.Error(), "find") {
		t.Errorf("err = %v, want unknown command with a find suggestion", err)
	}
	if len(rec.bodies) != 0 {
		t.Error("typo was captured as knowledge")
	}
}

// Real subcommands are never shadowed by the content route.
func TestRootRealSubcommandNotShadowed(t *testing.T) {
	rec := newAnalyzeRecorder(t)
	if _, err := executeCommand("analyze", "via analyze", "--server", rec.srv.URL); err != nil {
		t.Fatal(err)
	}
	if len(rec.bodies) != 1 || rec.bodies[0]["content"] != "via analyze" {
		t.Errorf("bodies = %v", rec.bodies)
	}
}
