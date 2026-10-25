package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	uri "hop.top/cite/scheme"
)

func TestShow(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	obj := &storage.KnowledgeObject{
		ID:       "obj_12345678",
		Type:     "url",
		Subtype:  "article",
		Pipeline: "url.article",
		Source:   "https://example.com/ux-signup",
		Tags:     []storage.Tag{{Label: "ux"}, {Label: "onboarding"}},
		Mentions: []uri.URI{
			{Scheme: "ctxt", Namespace: "entity", ID: "ui/best-practice"},
			{Scheme: "ctxt", Namespace: "entity", ID: "ux/onboarding"},
		},
		Summaries: []string{"Best UX practices for signup flows"},
		Decisions: []storage.Decision{{Title: "Use progressive disclosure", Status: "accepted", Impact: "high"}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Driver.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	out, err := db.exec("show", "obj_12345678")
	if err != nil {
		t.Fatalf("open should succeed: %v", err)
	}
	if !strings.Contains(out, "Knowledge Object: obj_12345678") {
		t.Error("output should show object ID")
	}
	if !strings.Contains(out, "Type:") {
		t.Error("output should contain Type field")
	}
	if !strings.Contains(out, "Tags:") {
		t.Error("output should contain Tags field")
	}
	if !strings.Contains(out, "Mentions:") {
		t.Error("output should contain Mentions field")
	}
	if !strings.Contains(out, "Summary:") {
		t.Error("output should contain Summary section")
	}
	if !strings.Contains(out, "Decisions:") {
		t.Error("output should contain Decisions section")
	}
}

func TestShowNoIDError(t *testing.T) {
	_, err := executeCommand("show")
	if err == nil {
		t.Error("open without ID should fail")
	}
}

func TestShowHelp(t *testing.T) {
	out, err := executeCommand("show", "--help")
	if err != nil {
		t.Fatalf("open --help should succeed: %v", err)
	}
	if !strings.Contains(out, "--raw") {
		t.Error("open help should list --raw flag")
	}
}

// seedShowMarkdown stores an object whose text is markdown longer than
// any truncation show used to apply.
func seedShowMarkdown(t *testing.T, db *testDB) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	body := "# Rollout plan\n\nShip **behind a flag** first.\n\n" +
		strings.Repeat("Context paragraph. ", 30) + "\n\nLAST-LINE-MARKER"
	if err := db.Driver.Objects().Create(context.Background(), &storage.KnowledgeObject{
		ID: "obj_markdown", Type: "note", TextContent: body, RawContent: body, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed object: %v", err)
	}
}

func withShowTerminal(t *testing.T, tty bool) {
	t.Helper()
	prev := showTerminal
	showTerminal = func() bool { return tty }
	t.Cleanup(func() { showTerminal = prev })
}

// TestShowTextMarkdown: show prints object text in full. On a terminal
// it is rendered markdown (styled, or plain under --no-color / NO_COLOR);
// elsewhere it is the stored text.
func TestShowTextMarkdown(t *testing.T) {
	cases := []struct {
		name     string
		tty      bool
		args     []string
		env      string
		rendered bool // styled render: markdown markers gone
		ansi     bool
		raw      bool // stored text, indented, unwrapped
	}{
		{name: "pipe", tty: false, raw: true},
		{name: "pipe --no-color", tty: false, args: []string{"--no-color"}, raw: true},
		{name: "terminal", tty: true, rendered: true, ansi: true},
		{name: "terminal --no-color", tty: true, args: []string{"--no-color"}},
		{name: "terminal NO_COLOR", tty: true, env: "1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			seedShowMarkdown(t, db)
			withShowTerminal(t, tc.tty)
			t.Setenv("NO_COLOR", tc.env)

			out, err := db.exec(append([]string{"show", "obj_markdown"}, tc.args...)...)
			if err != nil {
				t.Fatalf("show: %v", err)
			}
			if !strings.Contains(out, "LAST-LINE-MARKER") {
				t.Errorf("text truncated:\n%s", out)
			}
			if tc.rendered && strings.Contains(out, "**behind a flag**") {
				t.Errorf("markdown not rendered:\n%s", out)
			}
			// The seeded paragraph is one long line; only a render
			// wraps it.
			long := strings.Repeat("Context paragraph. ", 30)
			if got := strings.Contains(out, strings.TrimSpace(long)); got != tc.raw {
				t.Errorf("unwrapped stored line present = %v, want %v:\n%s", got, tc.raw, out)
			}
			if !strings.Contains(out, "behind a flag") {
				t.Errorf("text lost:\n%s", out)
			}
			if got := strings.Contains(out, "\x1b["); got != tc.ansi {
				t.Errorf("ANSI escapes = %v, want %v:\n%q", got, tc.ansi, out)
			}
		})
	}
}

// TestShowJSONUnrendered: --format json and --raw print the object as
// stored, terminal or not.
func TestShowJSONUnrendered(t *testing.T) {
	db := setupTestDB(t)
	seedShowMarkdown(t, db)
	withShowTerminal(t, true)
	for _, args := range [][]string{{"--format", "json"}, {"--raw"}} {
		out, err := db.exec(append([]string{"show", "obj_markdown"}, args...)...)
		if err != nil {
			t.Fatalf("show %v: %v", args, err)
		}
		if !strings.Contains(out, `**behind a flag**`) || strings.Contains(out, "\x1b[") {
			t.Errorf("show %v altered the text:\n%s", args, out)
		}
	}
}
