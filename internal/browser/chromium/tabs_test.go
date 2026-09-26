package chromium

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/browser/chromium/chromiumtest"
)

func readTabs(t *testing.T, data []byte) []Tab {
	t.Helper()
	tabs, err := ReadTabs(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ReadTabs: %v", err)
	}
	return tabs
}

func assertTabs(t *testing.T, got, want []Tab) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tabs mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestReadTabs_MultiWindow(t *testing.T) {
	data := chromiumtest.NewSNSS(3).
		// Window 2 declared first to prove windows sort by id.
		Tab(2, 20, 0, "https://example.com/personal", "Personal").
		Tab(1, 11, 1, "https://example.com/b", "Work B").
		Tab(1, 10, 0, "https://example.com/a", "Work A").
		SelectedTab(1, 1).
		SelectedTab(2, 0).
		ActiveWindow(1).
		Marker().
		Bytes()

	assertTabs(t, readTabs(t, data), []Tab{
		{WindowID: 1, TabID: 10, Index: 0, URL: "https://example.com/a", Title: "Work A"},
		{WindowID: 1, TabID: 11, Index: 1, URL: "https://example.com/b", Title: "Work B", Active: true},
		{WindowID: 2, TabID: 20, Index: 0, URL: "https://example.com/personal", Title: "Personal", Active: true},
	})
}

func TestReadTabs_TabClosed(t *testing.T) {
	data := chromiumtest.NewSNSS(3).
		Tab(1, 10, 0, "https://example.com/a", "Work A").
		Tab(1, 11, 1, "https://example.com/b", "Work B").
		Tab(1, 12, 2, "https://example.com/c", "Work C").
		SelectedTab(1, 0).
		Marker().
		TabClosed(11).
		TabIndex(12, 1).
		SelectedTab(1, 1).
		Bytes()

	assertTabs(t, readTabs(t, data), []Tab{
		{WindowID: 1, TabID: 10, Index: 0, URL: "https://example.com/a", Title: "Work A"},
		{WindowID: 1, TabID: 12, Index: 1, URL: "https://example.com/c", Title: "Work C", Active: true},
	})
}

func TestReadTabs_WindowClosed(t *testing.T) {
	data := chromiumtest.NewSNSS(3).
		Tab(1, 10, 0, "https://example.com/a", "Work").
		Tab(2, 20, 0, "https://example.com/p", "Personal").
		SelectedTab(1, 0).
		SelectedTab(2, 0).
		Marker().
		WindowClosed(2).
		Bytes()

	assertTabs(t, readTabs(t, data), []Tab{
		{WindowID: 1, TabID: 10, Index: 0, URL: "https://example.com/a", Title: "Work", Active: true},
	})
}

func TestReadTabs_TabMovedBetweenWindows(t *testing.T) {
	data := chromiumtest.NewSNSS(3).
		Tab(1, 10, 0, "https://example.com/a", "Work").
		Tab(1, 11, 1, "https://example.com/b", "Personal").
		Marker().
		TabWindow(2, 11).
		TabIndex(11, 0).
		SelectedTab(2, 0).
		Bytes()

	assertTabs(t, readTabs(t, data), []Tab{
		{WindowID: 1, TabID: 10, Index: 0, URL: "https://example.com/a", Title: "Work"},
		{WindowID: 2, TabID: 11, Index: 0, URL: "https://example.com/b", Title: "Personal", Active: true},
	})
}

func TestReadTabs_NavigationBack(t *testing.T) {
	data := chromiumtest.NewSNSS(3).
		TabWindow(1, 10).
		TabIndex(10, 0).
		Nav(10, 0, "https://example.com/1", "One").
		Nav(10, 1, "https://example.com/2", "Two").
		Nav(10, 2, "https://example.com/3", "Three").
		SelectedNav(10, 2).
		Marker().
		// User goes back one entry: selected index is no longer the last.
		SelectedNav(10, 1).
		Bytes()

	got := readTabs(t, data)
	if len(got) != 1 || got[0].URL != "https://example.com/2" || got[0].Title != "Two" {
		t.Fatalf("want entry 1 (Two), got %+v", got)
	}
}

func TestReadTabs_NavigationOverwrite(t *testing.T) {
	data := chromiumtest.NewSNSS(3).
		Tab(1, 10, 0, "https://example.com/loading", "").
		Marker().
		// Title arrives after load: same index, rewritten entry.
		Nav(10, 0, "https://example.com/loaded", "Work").
		Bytes()

	got := readTabs(t, data)
	if len(got) != 1 || got[0].URL != "https://example.com/loaded" || got[0].Title != "Work" {
		t.Fatalf("want rewritten entry, got %+v", got)
	}
}

func TestReadTabs_NavigationPathPruned(t *testing.T) {
	data := chromiumtest.NewSNSS(3).
		TabWindow(1, 10).
		Nav(10, 0, "https://example.com/0", "Zero").
		Nav(10, 1, "https://example.com/1", "One").
		Nav(10, 2, "https://example.com/2", "Two").
		Nav(10, 3, "https://example.com/3", "Three").
		SelectedNav(10, 3).
		Marker().
		// Drop entries 0-1 from the front: entry 3 shifts down to index 1.
		Pruned(10, 0, 2).
		Bytes()

	got := readTabs(t, data)
	if len(got) != 1 || got[0].Title != "Three" {
		t.Fatalf("want Three after prune shift, got %+v", got)
	}
}

func TestReadTabs_NavigationPrunedFromBack(t *testing.T) {
	data := chromiumtest.NewSNSS(3).
		TabWindow(1, 10).
		Nav(10, 0, "https://example.com/0", "Zero").
		Nav(10, 1, "https://example.com/1", "One").
		Nav(10, 2, "https://example.com/2", "Two").
		SelectedNav(10, 2).
		Marker().
		PrunedFromBack(10, 2).
		Bytes()

	got := readTabs(t, data)
	if len(got) != 1 || got[0].Title != "One" {
		t.Fatalf("want One after back prune, got %+v", got)
	}
}

func TestReadTabs_Pinned(t *testing.T) {
	data := chromiumtest.NewSNSS(3).
		Tab(1, 10, 0, "https://example.com/mail", "Work").
		Tab(1, 11, 1, "https://example.com/news", "Personal").
		Pinned(10, true).
		Pinned(11, true).
		Marker().
		Pinned(11, false).
		Bytes()

	got := readTabs(t, data)
	if len(got) != 2 || !got[0].Pinned || got[1].Pinned {
		t.Fatalf("want tab 10 pinned, 11 unpinned, got %+v", got)
	}
}

func TestReadTabs_UTF16Title(t *testing.T) {
	title := "Café ☕ 𝄞 Work"
	data := chromiumtest.NewSNSS(3).Tab(1, 10, 0, "https://example.com/", title).Marker().Bytes()

	got := readTabs(t, data)
	if len(got) != 1 || got[0].Title != title {
		t.Fatalf("title = %q, want %q", got[0].Title, title)
	}
}

func TestReadTabs_TabWithoutNavigationOmitted(t *testing.T) {
	data := chromiumtest.NewSNSS(3).
		Tab(1, 10, 0, "https://example.com/", "Work").
		TabWindow(1, 11).
		TabIndex(11, 1).
		Marker().
		Bytes()

	got := readTabs(t, data)
	if len(got) != 1 || got[0].TabID != 10 {
		t.Fatalf("want only tab 10, got %+v", got)
	}
}

func TestReadTabs_UnknownAndMalformedCommandsSkipped(t *testing.T) {
	data := chromiumtest.NewSNSS(3).
		Command(200, []byte{1, 2, 3, 4, 5}).
		Tab(1, 10, 0, "https://example.com/", "Work").
		// Payload too short for SetTabWindow: ignored, tab stays in window 1.
		Command(cmdSetTabWindow, []byte{9}).
		// Navigation pickle whose declared URL length overruns the payload.
		Command(cmdUpdateTabNavigation, chromiumtest.Pack(uint32(12), int32(10), int32(0), uint32(999))).
		Marker().
		Bytes()

	assertTabs(t, readTabs(t, data), []Tab{
		{WindowID: 1, TabID: 10, Index: 0, URL: "https://example.com/", Title: "Work"},
	})
}

func TestReadTabs_TruncatedTail(t *testing.T) {
	full := chromiumtest.NewSNSS(3).
		Tab(1, 10, 0, "https://example.com/a", "Work").
		Marker().
		Tab(1, 11, 1, "https://example.com/b", "Personal").
		Bytes()
	base := chromiumtest.NewSNSS(3).
		Tab(1, 10, 0, "https://example.com/a", "Work").
		Marker().
		Bytes()

	// Cut anywhere inside the trailing commands: the complete prefix
	// still parses, the partial record is dropped, and nothing errors.
	for cut := len(base); cut < len(full); cut++ {
		tabs, err := ReadTabs(bytes.NewReader(full[:cut]))
		if err != nil {
			t.Fatalf("cut %d: %v", cut, err)
		}
		if len(tabs) == 0 || tabs[0].TabID != 10 {
			t.Fatalf("cut %d: lost complete prefix: %+v", cut, tabs)
		}
	}
}

func TestReadTabs_BadHeader(t *testing.T) {
	cases := map[string][]byte{
		"empty":         {},
		"short":         []byte("SNS"),
		"bad magic":     append([]byte("SSNS"), chromiumtest.Pack(int32(3))...),
		"encrypted v2":  chromiumtest.NewSNSS(2).Bytes(),
		"encrypted v4":  chromiumtest.NewSNSS(4).Bytes(),
		"unknown v99":   chromiumtest.NewSNSS(99).Bytes(),
		"magic no vers": []byte("SNSS"),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ReadTabs(bytes.NewReader(data))
			if !errors.Is(err, ErrBadHeader) {
				t.Fatalf("err = %v, want ErrBadHeader", err)
			}
			var he *HeaderError
			if !errors.As(err, &he) {
				t.Fatalf("err = %T, want *HeaderError", err)
			}
		})
	}
}

func TestReadTabs_MarkerRequiredForVersion3(t *testing.T) {
	noMarker := chromiumtest.NewSNSS(3).Tab(1, 10, 0, "https://example.com/", "Work").Bytes()
	if _, err := ReadTabs(bytes.NewReader(noMarker)); !errors.Is(err, ErrIncompleteSession) {
		t.Fatalf("v3 without marker: err = %v, want ErrIncompleteSession", err)
	}

	v1 := chromiumtest.NewSNSS(1).Tab(1, 10, 0, "https://example.com/", "Work").Bytes()
	if tabs, err := ReadTabs(bytes.NewReader(v1)); err != nil || len(tabs) != 1 {
		t.Fatalf("v1 without marker: tabs=%+v err=%v", tabs, err)
	}
}

func writeSession(t *testing.T, dir, name string, data []byte, mtime time.Time) {
	t.Helper()
	p := filepath.Join(dir, "Sessions", name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func TestOpenTabs_EmptyProfileDir(t *testing.T) {
	dir := t.TempDir()
	if _, err := OpenTabs(context.Background(), dir); !errors.Is(err, ErrNoSessionFile) {
		t.Fatalf("no Sessions dir: err = %v, want ErrNoSessionFile", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "Sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Tabs_* files hold closed-tab history, not the open session.
	writeSession(t, dir, "Tabs_13400000000000000", chromiumtest.NewSNSS(3).Marker().Bytes(), time.Now())
	if _, err := OpenTabs(context.Background(), dir); !errors.Is(err, ErrNoSessionFile) {
		t.Fatalf("empty Sessions dir: err = %v, want ErrNoSessionFile", err)
	}
}

func TestOpenTabs_UsesNewestSessionFile(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	older := chromiumtest.NewSNSS(3).Tab(1, 10, 0, "https://example.com/old", "Personal").Marker().Bytes()
	newer := chromiumtest.NewSNSS(3).Tab(1, 20, 0, "https://example.com/new", "Work").Marker().Bytes()
	// Name order disagrees with mtime order: mtime wins.
	writeSession(t, dir, "Session_13400000000000002", older, now.Add(-time.Hour))
	writeSession(t, dir, "Session_13400000000000001", newer, now)

	tabs, err := OpenTabs(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tabs) != 1 || tabs[0].Title != "Work" {
		t.Fatalf("want newest session, got %+v", tabs)
	}
}

func TestOpenTabs_FallsBackWhenNewestIncomplete(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	complete := chromiumtest.NewSNSS(3).Tab(1, 10, 0, "https://example.com/", "Work").Marker().Bytes()
	// Newest file caught mid-rewrite: initial state written, marker not yet.
	partial := chromiumtest.NewSNSS(3).Tab(1, 20, 0, "https://example.com/", "Personal").Bytes()
	writeSession(t, dir, "Session_13400000000000001", complete, now.Add(-time.Minute))
	writeSession(t, dir, "Session_13400000000000002", partial, now)

	tabs, err := OpenTabs(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tabs) != 1 || tabs[0].Title != "Work" {
		t.Fatalf("want fallback to complete file, got %+v", tabs)
	}
}

func TestOpenTabs_BadHeaderSurfaces(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "Session_13400000000000001", chromiumtest.NewSNSS(2).Bytes(), time.Now())
	if _, err := OpenTabs(context.Background(), dir); !errors.Is(err, ErrBadHeader) {
		t.Fatalf("err = %v, want ErrBadHeader", err)
	}
}

func TestOpenTabs_ContextCanceled(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "Session_13400000000000001",
		chromiumtest.NewSNSS(3).Tab(1, 10, 0, "https://example.com/", "Work").Marker().Bytes(), time.Now())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OpenTabs(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestOpenSession_ReportsFileUsed(t *testing.T) {
	dir := t.TempDir()
	// Whole seconds: some filesystems truncate sub-second mtimes.
	complete := time.Now().Add(-time.Minute).Truncate(time.Second)
	writeSession(t, dir, "Session_13400000000000001",
		chromiumtest.NewSNSS(3).Tab(1, 10, 0, "https://example.com/", "Work").Marker().Bytes(), complete)
	// Newest file lacks its marker, so the session comes from the older one.
	writeSession(t, dir, "Session_13400000000000002",
		chromiumtest.NewSNSS(3).Tab(1, 20, 0, "https://example.com/", "Personal").Bytes(), time.Now())

	s, err := OpenSession(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "Sessions", "Session_13400000000000001"); s.Path != want {
		t.Errorf("Path = %q, want %q", s.Path, want)
	}
	if !s.ModTime.Equal(complete) {
		t.Errorf("ModTime = %v, want %v", s.ModTime, complete)
	}
	if len(s.Tabs) != 1 || s.Tabs[0].Title != "Work" {
		t.Errorf("Tabs = %+v, want the Work tab", s.Tabs)
	}
}

func TestOpenSession_NoSessionFile(t *testing.T) {
	s, err := OpenSession(context.Background(), t.TempDir())
	if !errors.Is(err, ErrNoSessionFile) || s != nil {
		t.Fatalf("session=%+v err=%v, want nil and ErrNoSessionFile", s, err)
	}
}

// The test encoder keeps its own copy of the command ids; drift would
// make every fixture silently encode the wrong commands.
func TestChromiumtestCommandIDsMatch(t *testing.T) {
	pairs := map[string][2]uint8{
		"SetTabWindow":                    {cmdSetTabWindow, chromiumtest.CmdSetTabWindow},
		"SetTabIndexInWindow":             {cmdSetTabIndexInWindow, chromiumtest.CmdSetTabIndexInWindow},
		"TabNavigationPathPrunedFromBack": {cmdTabNavigationPathPrunedFromBack, chromiumtest.CmdTabNavigationPathPrunedFromBack},
		"UpdateTabNavigation":             {cmdUpdateTabNavigation, chromiumtest.CmdUpdateTabNavigation},
		"SetSelectedNavigationIndex":      {cmdSetSelectedNavigationIndex, chromiumtest.CmdSetSelectedNavigationIndex},
		"SetSelectedTabInIndex":           {cmdSetSelectedTabInIndex, chromiumtest.CmdSetSelectedTabInIndex},
		"SetPinnedState":                  {cmdSetPinnedState, chromiumtest.CmdSetPinnedState},
		"TabClosed":                       {cmdTabClosed, chromiumtest.CmdTabClosed},
		"WindowClosed":                    {cmdWindowClosed, chromiumtest.CmdWindowClosed},
		"SetActiveWindow":                 {cmdSetActiveWindow, chromiumtest.CmdSetActiveWindow},
		"TabNavigationPathPruned":         {cmdTabNavigationPathPruned, chromiumtest.CmdTabNavigationPathPruned},
		"InitialStateMarker":              {cmdInitialStateMarker, chromiumtest.CmdInitialStateMarker},
	}
	for name, p := range pairs {
		if p[0] != p[1] {
			t.Errorf("%s: chromium id %d, chromiumtest id %d", name, p[0], p[1])
		}
	}
	if snssVersionPlain != chromiumtest.VersionPlain || snssVersionWithMarker != chromiumtest.VersionWithMarker {
		t.Errorf("SNSS versions drifted")
	}
}

// WriteUserDataDir must produce a tree ResolveProfile and OpenSession
// read back: the contract the CLI tests build on.
func TestChromiumtestUserDataDirRoundTrip(t *testing.T) {
	mtime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	base := chromiumtest.WriteUserDataDir(
		t,
		chromiumtest.Profile{
			DirName: "Profile 1", Name: "Work", ModTime: mtime,
			Session: chromiumtest.Session(chromiumtest.Tab{URL: "https://example.com/a", Title: "A"}),
		},
		chromiumtest.Profile{DirName: "Profile 2", Name: "Gone", Missing: true},
	)
	p, err := ResolveProfile(Brave, "work", WithUserDataDir(base))
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if !p.LastUsed || p.DirName != "Profile 1" {
		t.Fatalf("profile = %+v", p)
	}
	s, err := OpenSession(context.Background(), p.Dir)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if !s.ModTime.Equal(mtime) {
		t.Errorf("ModTime = %v, want %v", s.ModTime, mtime)
	}
	assertTabs(t, s.Tabs, []Tab{{WindowID: 1, TabID: 100, Index: 0, URL: "https://example.com/a", Title: "A"}})
	if _, err := ResolveProfile(Brave, "Gone", WithUserDataDir(base)); !errors.Is(err, ErrProfileDirMissing) {
		t.Fatalf("missing dir: err = %v", err)
	}
}
