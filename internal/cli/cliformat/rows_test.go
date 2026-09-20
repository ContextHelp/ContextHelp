package cliformat

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"hop.top/kit/go/console/output"
)

type testRow struct {
	Name   string `table:"NAME"`
	Hidden string // no table tag: must never reach any format
	Count  int    `table:"COUNT"`
}

// dispatchCmd builds a root carrying kit's output flags bound to the
// global viper and returns a LEAF beneath it, matching how both
// binaries wire their roots: the flags live on the root's persistent
// set and the command that renders is a subcommand.
//
// The leaf matters. Dispatch reads --format and --cols by walking
// Parent(), so a test that registered the flags on the same command it
// dispatches would pass even if the parent link were broken — which is
// exactly the bug this shape is here to catch.
func dispatchCmd(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	// The registry binds --format into the global viper, which outlives
	// any one case. Scrub it first so a value left by an earlier test
	// cannot masquerade as this run's explicit format — without this
	// the suite passes or fails depending on the order it runs in.
	prev := viper.GetString("format")
	viper.Set("format", "")
	t.Cleanup(func() { viper.Set("format", prev) })

	root := &cobra.Command{Use: "root"}
	output.RegisterFlags(root, viper.GetViper())

	leaf := &cobra.Command{Use: "leaf", RunE: func(*cobra.Command, []string) error { return nil }}
	root.AddCommand(leaf)

	root.SetArgs(append([]string{"leaf"}, args...))
	root.SetOut(new(bytes.Buffer))
	if err := root.Execute(); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	Bind(leaf)
	t.Cleanup(func() { activeCmd = nil })
	return leaf
}

// TestDispatchRowsLeavesCommandTreeIntact pins the reason DispatchRows
// renders through a detached proxy rather than by re-pointing the
// command it was handed.
//
// Both binaries pass the long-lived root here, and the in-process test
// harnesses point that root at an os.Pipe they close after each case.
// An implementation that swapped the writer and restored it in a defer
// handed the closed pipe back, and every later command in the process
// wrote into a dead fd. It also must not leave the proxy registered,
// or it would surface in help and completions.
func TestDispatchRowsLeavesCommandTreeIntact(t *testing.T) {
	cmd := dispatchCmd(t, "--format", "csv")
	parent := cmd.Parent()
	before := len(parent.Commands())

	sentinel := new(bytes.Buffer)
	cmd.SetOut(sentinel)

	var buf bytes.Buffer
	if err := DispatchRows(cmd, &buf, []testRow{{Name: "alpha", Count: 1}}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if got := cmd.OutOrStdout(); got != sentinel {
		t.Error("DispatchRows must not repoint the command's own writer")
	}
	if after := len(parent.Commands()); after != before {
		t.Errorf("command tree changed: %d children before, %d after", before, after)
	}
	if sentinel.Len() != 0 {
		t.Errorf("output went to the command's writer instead of the caller's: %q", sentinel.String())
	}
	if buf.Len() == 0 {
		t.Error("nothing was written to the caller's writer")
	}
}

func TestRowsCoversTabularFormatsOnly(t *testing.T) {
	// The whole point of the fix: csv and text must be row formats, so
	// they stop falling through to a human renderer. human must NOT be,
	// so the bespoke prose survives.
	for _, f := range []string{output.Table, output.CSV, output.Text} {
		if !IsRows(f) {
			t.Errorf("%s must be a row format so it renders as itself", f)
		}
	}
	for _, f := range []string{output.Human, output.JSON, output.YAML} {
		if IsRows(f) {
			t.Errorf("%s must not be a row format", f)
		}
	}
}

// TestRowsIsFalseWhenFormatUnset pins the default invocation.
//
// kit reports an unset --format as "table", so a Rows() that trusted
// Active() alone would route every bare `ctxt list` through the row
// projection and silently drop the prose operators see today — the
// "(N total)" counts, the "*" default marker, the config-path footer.
// Fixing csv must not cost that.
func TestRowsIsFalseWhenFormatUnset(t *testing.T) {
	withFormat(t, "")
	if Active() != Table {
		t.Fatalf("precondition: an unset format should resolve to %q, got %q", Table, Active())
	}
	if Rows() {
		t.Error("an unset --format must keep the human renderer, not dispatch rows")
	}
}

// TestRowsIsTrueForExplicitTable is driven through a real parsed
// --format rather than viper.Set.
//
// It has to be. Both roots BindPFlag --format, so viper reports "table"
// for a run that never passed the flag, and a viper-only test could not
// tell an explicit `--format table` from the default. The pflag's
// Changed state is what separates them, and only parsing sets it.
func TestRowsIsTrueForExplicitTable(t *testing.T) {
	dispatchCmd(t, "--format", "table")
	if !Rows() {
		t.Error("an explicitly requested table must dispatch through the projection")
	}
}

// TestRowsIsFalseForDefaultTableFlag is the same command without the
// flag: kit's default still makes Active() report "table", and the
// human renderer must still win.
func TestRowsIsFalseForDefaultTableFlag(t *testing.T) {
	dispatchCmd(t)
	if Active() != Table {
		t.Fatalf("precondition: default should resolve to %q, got %q", Table, Active())
	}
	if Rows() {
		t.Error("an unpassed --format must keep the human renderer")
	}
}

func TestDispatchRowsRendersRealCSV(t *testing.T) {
	cmd := dispatchCmd(t, "--format", "csv")
	var buf bytes.Buffer
	rows := []testRow{
		{Name: "alpha", Count: 1, Hidden: "secret"},
		// A comma in a value is the case that proves this is really CSV
		// and not a table with commas in it: a real encoder quotes it.
		{Name: "beta,gamma", Count: 2},
	}
	if err := DispatchRows(cmd, &buf, rows); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	records, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
	if err != nil {
		t.Fatalf("output is not parseable as CSV: %v\ngot:\n%s", err, buf.String())
	}
	if len(records) != 3 {
		t.Fatalf("want header + 2 rows, got %d records: %#v", len(records), records)
	}
	if got := records[0]; got[0] != "NAME" || got[1] != "COUNT" {
		t.Errorf("header should come from the table tags, got %#v", got)
	}
	if got := records[2][0]; got != "beta,gamma" {
		t.Errorf("a comma-bearing value must survive a CSV round trip, got %q", got)
	}
	if strings.Contains(buf.String(), "secret") {
		t.Error("an untagged field must not leak into the output")
	}
}

func TestDispatchRowsEmptyCSVKeepsHeader(t *testing.T) {
	// An empty result still has to be parseable. Emitting nothing at
	// all leaves a caller unable to distinguish "no matches" from "the
	// command broke", and no CSV reader can infer the columns.
	cmd := dispatchCmd(t, "--format", "csv")
	var buf bytes.Buffer
	if err := DispatchRows(cmd, &buf, []testRow{}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	records, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
	if err != nil {
		t.Fatalf("empty output is not parseable as CSV: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("want exactly the header row, got %d records: %#v", len(records), records)
	}
	if records[0][0] != "NAME" || records[0][1] != "COUNT" {
		t.Errorf("header should come from the table tags, got %#v", records[0])
	}
	// The old behavior: a human sentence, or a bare null.
	if strings.Contains(buf.String(), "No results") || strings.TrimSpace(buf.String()) == "null" {
		t.Errorf("empty csv must not be prose or null, got %q", buf.String())
	}
}

func TestDispatchRowsEmptyTableAndTextAreSilent(t *testing.T) {
	// Unlike csv, a bare header here would read as a phantom record.
	for _, format := range []string{output.Table, output.Text} {
		cmd := dispatchCmd(t, "--format", format)
		var buf bytes.Buffer
		if err := DispatchRows(cmd, &buf, []testRow{}); err != nil {
			t.Fatalf("%s dispatch: %v", format, err)
		}
		if buf.Len() != 0 {
			t.Errorf("%s with no rows should write nothing, got %q", format, buf.String())
		}
	}
}

func TestDispatchRowsRendersText(t *testing.T) {
	cmd := dispatchCmd(t, "--format", "text")
	var buf bytes.Buffer
	if err := DispatchRows(cmd, &buf, []testRow{{Name: "alpha", Count: 1}}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	got := buf.String()
	// text's default style is key=value, keyed by the table header.
	for _, want := range []string{"NAME=alpha", "COUNT=1"} {
		if !strings.Contains(got, want) {
			t.Errorf("text output should contain %q, got %q", want, got)
		}
	}
}

func TestDispatchRowsHonoursCols(t *testing.T) {
	cmd := dispatchCmd(t, "--format", "csv", "--cols", "COUNT,NAME")
	var buf bytes.Buffer
	if err := DispatchRows(cmd, &buf, []testRow{{Name: "alpha", Count: 7}}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	// --cols reorders as well as selects: the caller's order wins.
	if got := strings.SplitN(buf.String(), "\n", 2)[0]; got != "COUNT,NAME" {
		t.Errorf("header should follow --cols order, got %q", got)
	}
}

func TestDispatchRowsEmptyHonoursCols(t *testing.T) {
	// The empty rendering must agree with the populated one, or a
	// caller's column layout silently changes when a query happens to
	// match nothing.
	cmd := dispatchCmd(t, "--format", "csv", "--cols", "COUNT,NAME")
	var buf bytes.Buffer
	if err := DispatchRows(cmd, &buf, []testRow{}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "COUNT,NAME" {
		t.Errorf("empty header should follow --cols order, got %q", got)
	}
}

func TestDispatchRowsEmptyRejectsUnknownCol(t *testing.T) {
	// With rows present kit rejects an unknown column; with none it
	// must not silently succeed, or a typo goes unnoticed exactly when
	// there is no output to notice it in.
	cmd := dispatchCmd(t, "--format", "csv", "--cols", "NOPE")
	var buf bytes.Buffer
	err := DispatchRows(cmd, &buf, []testRow{})
	if err == nil {
		t.Fatal("an unknown column must be rejected even with no rows")
	}
	if !strings.Contains(err.Error(), "NOPE") {
		t.Errorf("error should echo the rejected column, got: %v", err)
	}
}

func TestDispatchRowsRejectsNonSlice(t *testing.T) {
	cmd := dispatchCmd(t, "--format", "csv")
	var buf bytes.Buffer
	if err := DispatchRows(cmd, &buf, testRow{Name: "alpha"}); err == nil {
		t.Fatal("a bare struct should be rejected: list commands render slices")
	}
}

// The -o tests below pin the destination contract: when the caller
// names a file, the document belongs in that file and stdout stays
// empty. Every one of these failed before the fix by writing to the
// caller's writer and never creating the file, while still exiting 0
// — a caller that redirected output got a success code and no data.

// TestDispatchRowsWritesOutputFile covers the populated-rows path,
// which renders through kit's Dispatch.
func TestDispatchRowsWritesOutputFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rows.csv")
	cmd := dispatchCmd(t, "--format", "csv", "--output", path)

	var stdout bytes.Buffer
	if err := DispatchRows(cmd, &stdout, []testRow{{Name: "a", Count: 1}}); err != nil {
		t.Fatalf("DispatchRows: %v", err)
	}

	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty: -o names a file, so nothing should reach the caller's writer", stdout.String())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read -o file: %v (the requested file was never created)", err)
	}
	if !strings.Contains(string(got), "a") {
		t.Errorf("-o file = %q, want the rendered row", got)
	}
}

// TestDispatchRowsEmptyWritesOutputFile covers the zero-row path,
// which returns before reaching Dispatch and so needed its own
// resolution. For csv the header IS the document when nothing
// matched, and a parser reading the file needs it there.
func TestDispatchRowsEmptyWritesOutputFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.csv")
	cmd := dispatchCmd(t, "--format", "csv", "--output", path)

	var stdout bytes.Buffer
	if err := DispatchRows(cmd, &stdout, []testRow{}); err != nil {
		t.Fatalf("DispatchRows: %v", err)
	}

	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read -o file: %v (empty result skipped the -o path)", err)
	}
	if !strings.Contains(string(got), "NAME") {
		t.Errorf("-o file = %q, want the csv header", got)
	}
}

// TestEncodeToWritesOutputFile covers the json/yaml document path that
// every outputJSON callsite funnels through.
func TestEncodeToWritesOutputFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.json")
	cmd := dispatchCmd(t, "--format", "json", "--output", path)

	var stdout bytes.Buffer
	if err := EncodeTo(cmd, &stdout, map[string]any{"total": 7}); err != nil {
		t.Fatalf("EncodeTo: %v", err)
	}

	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read -o file: %v", err)
	}
	if !strings.Contains(string(got), `"total": 7`) {
		t.Errorf("-o file = %q, want the encoded document", got)
	}
}

// TestEncodeToResolvesNilCommand pins the nil-cmd path. Every
// outputJSON callsite in both binaries passes no command, so this is
// the route the whole json/yaml surface actually takes; if it stopped
// resolving -o, all of them would silently revert to stdout.
//
// It resolves through viper, which kit's RegisterFlags binds --output
// into. An earlier version of this fix also threaded Bind's command
// in as a fallback; mutation testing showed removing it changed
// nothing, so it was dead code and is gone.
func TestEncodeToResolvesNilCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bound.json")
	dispatchCmd(t, "--format", "json", "--output", path) // Binds the leaf.

	var stdout bytes.Buffer
	if err := EncodeTo(nil, &stdout, map[string]any{"total": 7}); err != nil {
		t.Fatalf("EncodeTo: %v", err)
	}

	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty: nil cmd must fall back to the bound command", stdout.String())
	}
	if _, err := os.ReadFile(path); err != nil {
		t.Fatalf("read -o file: %v (nil cmd did not resolve -o)", err)
	}
}

// TestOutputStdoutSentinelStaysOnStdout guards the other direction.
// "-" means stdout by kit's convention, and treating it as a path
// would create a file literally named "-" while the caller saw
// nothing.
func TestOutputStdoutSentinelStaysOnStdout(t *testing.T) {
	// Exercised on the two paths this package resolves itself: the
	// empty-rows header and the json/yaml document. The populated path
	// delegates to kit's Dispatch, which applies its own sentinel
	// rule, so driving only that one would test kit rather than this.
	dir := t.TempDir()
	t.Chdir(dir)

	t.Run("empty rows", func(t *testing.T) {
		cmd := dispatchCmd(t, "--format", "csv", "--output", "-")
		var stdout bytes.Buffer
		if err := DispatchRows(cmd, &stdout, []testRow{}); err != nil {
			t.Fatalf("DispatchRows: %v", err)
		}
		if stdout.Len() == 0 {
			t.Error(`stdout is empty: -o - must render to the caller's writer, not a file named "-"`)
		}
	})

	t.Run("document", func(t *testing.T) {
		cmd := dispatchCmd(t, "--format", "json", "--output", "-")
		var stdout bytes.Buffer
		if err := EncodeTo(cmd, &stdout, map[string]any{"total": 7}); err != nil {
			t.Fatalf("EncodeTo: %v", err)
		}
		if stdout.Len() == 0 {
			t.Error(`stdout is empty: -o - must render to the caller's writer`)
		}
	})

	if _, err := os.Stat(filepath.Join(dir, "-")); err == nil {
		t.Error(`a file named "-" was created; the sentinel was treated as a path`)
	}
}

// TestNoOutputFlagStillWritesToCaller is the regression guard for the
// fix itself: the common case has no -o at all, and it must keep
// reaching the writer the command passed.
func TestNoOutputFlagStillWritesToCaller(t *testing.T) {
	cmd := dispatchCmd(t, "--format", "csv")

	var stdout bytes.Buffer
	if err := DispatchRows(cmd, &stdout, []testRow{{Name: "a", Count: 1}}); err != nil {
		t.Fatalf("DispatchRows: %v", err)
	}
	if stdout.Len() == 0 {
		t.Error("stdout is empty: without -o the caller's writer must still receive the rendering")
	}
}

// TestOutputFileIsClosed pins the closer.
//
// os.File writes are unbuffered, so a missing Close never truncates:
// the file content looks correct and the only consequence is a
// descriptor held open. Truncation is therefore the wrong probe — it
// cannot fail.
//
// Counting entries in /dev/fd is also wrong here: macOS does not let
// it be read as a directory, so that check SKIPPED rather than ran,
// which is how this was caught. Instead the test reads the number the
// kernel assigns to a freshly opened file. Descriptors are handed out
// lowest-available-first, so if each render leaks one, the number a
// probe receives climbs; if they are closed, the slot is reused and
// it stays flat.
func TestOutputFileIsClosed(t *testing.T) {
	dir := t.TempDir()

	probeFD := func() uintptr {
		f, err := os.Open(os.DevNull)
		if err != nil {
			t.Fatalf("probe open: %v", err)
		}
		defer func() {
			if cerr := f.Close(); cerr != nil {
				t.Errorf("probe close: %v", cerr)
			}
		}()
		return f.Fd()
	}

	render := func(i int) {
		path := filepath.Join(dir, fmt.Sprintf("f%d.json", i))
		c := dispatchCmd(t, "--format", "json", "--output", path)
		if err := EncodeTo(c, new(bytes.Buffer), map[string]any{"n": i}); err != nil {
			t.Fatalf("EncodeTo #%d: %v", i, err)
		}
	}

	render(0) // warm up: settle any one-off descriptor
	before := probeFD()

	const iterations = 64
	for i := 1; i <= iterations; i++ {
		render(i)
	}
	after := probeFD()

	// Closed files free their slot, so the probe lands at roughly the
	// same number. A leak per render pushes it up by ~iterations.
	if after > before+uintptr(iterations/4) {
		t.Errorf("probe descriptor moved %d -> %d across %d renders; "+
			"the -o file is not being closed", before, after, iterations)
	}
}

// TestOutputPathDirectoryIsRejected pins the outcome, not the guard:
// a directory as -o must fail rather than report success with no data
// written.
//
// The explicit IsDir check ahead of the open is deliberately NOT what
// this asserts. os.OpenFile already refuses a directory, and on the
// platforms here its own error text says "is a directory" too, so
// removing the check leaves this test green — verified by mutation.
// The check earns its place by making the message name the path and
// by matching what kit's own resolveWriter does, neither of which a
// behavioral test can distinguish. What matters, and what this pins,
// is that the failure is reported at all.
func TestOutputPathDirectoryIsRejected(t *testing.T) {
	dir := t.TempDir()
	cmd := dispatchCmd(t, "--format", "json", "--output", dir)

	err := EncodeTo(cmd, new(bytes.Buffer), map[string]any{"total": 7})
	if err == nil {
		t.Fatal("writing to a directory path succeeded; the caller would see exit 0 and no data")
	}
	if !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("error = %q, want it to say the path is a directory", err)
	}
}

// TestOutputSentinelViaViper covers the sentinel on the viper route,
// which is the one every outputJSON callsite takes (they pass no
// command). Handling "-" only on the flag would create a file named
// "-" for exactly those callers.
func TestOutputSentinelViaViper(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	prev := viper.GetString("output")
	viper.Set("output", "-")
	t.Cleanup(func() { viper.Set("output", prev) })

	var stdout bytes.Buffer
	if err := EncodeTo(nil, &stdout, map[string]any{"total": 7}); err != nil {
		t.Fatalf("EncodeTo: %v", err)
	}
	if stdout.Len() == 0 {
		t.Error(`stdout is empty: "-" from viper must mean stdout, not a file named "-"`)
	}
	if _, err := os.Stat(filepath.Join(dir, "-")); err == nil {
		t.Error(`a file named "-" was created from the viper value`)
	}
}
