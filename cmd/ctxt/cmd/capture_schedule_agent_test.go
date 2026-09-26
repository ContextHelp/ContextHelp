package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// oddProfile carries every character class that breaks naive plist
// rendering or shell quoting: spaces, XML metacharacters, both quote
// kinds, a slash, a dollar sign and non-ASCII text.
const oddProfile = `Work & "Play" <1>/it's $HOME é`

var labelSafe = regexp.MustCompile(`^[a-z0-9.-]+$`)

func TestScheduleLabel_FilesystemSafeAndDistinct(t *testing.T) {
	cases := []string{"Work", "work", "Work!", "Profile 1", oddProfile, "../../etc", "   ", "日本語"}
	seen := map[string]string{}
	for _, p := range cases {
		for _, k := range scheduleKinds {
			l := scheduleLabel(k, "chrome", p)
			if !labelSafe.MatchString(l) {
				t.Errorf("label %q for profile %q is not filesystem-safe", l, p)
			}
			if !strings.HasPrefix(l, scheduleLabelPrefix) {
				t.Errorf("label %q lacks prefix %q", l, scheduleLabelPrefix)
			}
			if len(l) > 100 {
				t.Errorf("label %q is %d chars; want <= 100", l, len(l))
			}
			if prev, dup := seen[l]; dup {
				t.Errorf("label %q shared by profiles %q and %q", l, prev, p)
			}
			seen[l] = p
		}
	}
}

func TestScheduleLabel_Stable(t *testing.T) {
	a := scheduleLabel(kindHistory, "chrome", "Work")
	b := scheduleLabel(kindHistory, "chrome", "Work")
	if a != b {
		t.Fatalf("label not stable: %q vs %q", a, b)
	}
	if !strings.Contains(a, ".capture-history.chrome.work-") {
		t.Errorf("label %q should read kind, browser and profile slug", a)
	}
	if scheduleLabel(kindHistory, "brave", "Work") == a {
		t.Error("browser must be part of the label identity")
	}
}

func testAgent(kind scheduleKind, profile string) scheduleAgent {
	return scheduleAgent{
		Kind:           kind,
		Browser:        "chrome",
		BrowserProfile: profile,
		Interval:       5 * time.Minute,
		Binary:         "/opt/ctxt/bin/ctxt",
		LogDir:         "/Users/u/Library/Logs/ctxt",
		Home:           "/Users/u",
		PathEnv:        "/usr/bin:/bin",
	}
}

func TestScheduleAgent_ProgramArguments(t *testing.T) {
	a := testAgent(kindHistory, oddProfile)
	want := []string{"/opt/ctxt/bin/ctxt", "capture", "history", "--browser", "chrome", "--browser-profile", oddProfile}
	if got := a.ProgramArguments(); !slices.Equal(got, want) {
		t.Errorf("args = %q\nwant   %q", got, want)
	}

	a.Instance = "work"
	a.FocusProfile = "research"
	want = append(want, "--instance", "work", "--profile", "research")
	if got := a.ProgramArguments(); !slices.Equal(got, want) {
		t.Errorf("args with passthrough = %q\nwant %q", got, want)
	}
}

func TestRenderSchedulePlist_Keys(t *testing.T) {
	a := testAgent(kindTabs, "Work")
	a.Interval = 30 * time.Minute
	out, err := renderSchedulePlist(a)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	label := scheduleLabel(kindTabs, "chrome", "Work")
	for _, want := range []string{
		`<plist version="1.0">`,
		"<string>" + label + "</string>",
		"<key>StartInterval</key>\n\t<integer>1800</integer>",
		"<key>RunAtLoad</key>",
		"<string>/opt/ctxt/bin/ctxt</string>\n\t\t<string>capture</string>\n\t\t<string>tabs</string>",
		"/Users/u/Library/Logs/ctxt/" + strings.TrimPrefix(label, "com.contexthelp.ctxt.") + ".out.log",
		"/Users/u/Library/Logs/ctxt/" + strings.TrimPrefix(label, "com.contexthelp.ctxt.") + ".err.log",
		"<string>/usr/bin:/bin</string>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plist missing %q\n--- rendered ---\n%s", want, out)
		}
	}
	// A periodic job must not be kept alive, and must never go
	// through a shell.
	for _, bad := range []string{"KeepAlive", "/bin/sh", "/bin/bash", "/bin/zsh"} {
		if strings.Contains(out, bad) {
			t.Errorf("plist must not contain %q\n%s", bad, out)
		}
	}
}

func TestRenderSchedulePlist_EscapesAndRoundTrips(t *testing.T) {
	a := testAgent(kindHistory, oddProfile)
	a.Instance = `a&b`
	out, err := renderSchedulePlist(a)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(out, `<1>`) || strings.Contains(out, "& ") {
		t.Fatalf("raw XML metacharacters leaked into plist:\n%s", out)
	}
	got, err := parseSchedulePlist([]byte(out))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.BrowserProfile != oddProfile {
		t.Errorf("profile round-trip = %q, want %q", got.BrowserProfile, oddProfile)
	}
	if got.Instance != `a&b` || got.Kind != kindHistory || got.Browser != "chrome" || got.Interval != 5*time.Minute {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if got.Label != scheduleLabel(kindHistory, "chrome", oddProfile) {
		t.Errorf("label round-trip = %q", got.Label)
	}
}

// TestRenderSchedulePlist_PlutilLint validates the rendered plist with
// Apple's own parser and confirms the profile survives as exactly one
// ProgramArguments element.
func TestRenderSchedulePlist_PlutilLint(t *testing.T) {
	plutil, err := exec.LookPath("plutil")
	if err != nil {
		t.Skip("plutil not available")
	}
	a := testAgent(kindHistory, oddProfile)
	a.Instance = "work"
	out, err := renderSchedulePlist(a)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	p := filepath.Join(t.TempDir(), "agent.plist")
	if err := os.WriteFile(p, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command(plutil, "-lint", p).CombinedOutput(); err != nil {
		t.Fatalf("plutil -lint: %v\n%s\n--- plist ---\n%s", err, b, out)
	}
	b, err := exec.Command(plutil, "-extract", "ProgramArguments.6", "raw", "-o", "-", p).Output()
	if err != nil {
		t.Fatalf("plutil -extract: %v", err)
	}
	if got := strings.TrimSuffix(string(b), "\n"); got != oddProfile {
		t.Errorf("ProgramArguments[6] = %q, want %q", got, oddProfile)
	}
}

func TestValidateBrowserProfile(t *testing.T) {
	for _, bad := range []string{"", "   ", "a\x00b", "line\nbreak", "tab\there"} {
		if err := validateBrowserProfile(bad); err == nil {
			t.Errorf("validateBrowserProfile(%q) = nil, want error", bad)
		}
	}
	for _, ok := range []string{"Work", "Profile 1", oddProfile} {
		if err := validateBrowserProfile(ok); err != nil {
			t.Errorf("validateBrowserProfile(%q) = %v", ok, err)
		}
	}
}
