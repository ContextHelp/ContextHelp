package chromium

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Tab is one open tab reconstructed from a session file.
type Tab struct {
	WindowID int32
	TabID    int32
	// Index is the tab's position within its window, left to right.
	Index int
	// URL and Title belong to the tab's current navigation entry.
	URL   string
	Title string
	// Active marks the selected tab of its window.
	Active bool
	Pinned bool
}

// Session is the open-tab state read from one session file.
type Session struct {
	// Path is the Sessions/Session_* file the tabs were read from.
	Path string
	// ModTime is that file's modification time: the state is as of then.
	// A running browser keeps it within seconds of now; after the browser
	// quits it records the tabs open at exit.
	ModTime time.Time
	Tabs    []Tab
}

// OpenTabs returns the tabs open in the profile at profileDir (absolute
// path of a profile directory, e.g. .../Profile 14). It is OpenSession
// without the file metadata.
func OpenTabs(ctx context.Context, profileDir string) ([]Tab, error) {
	s, err := OpenSession(ctx, profileDir)
	if err != nil {
		return nil, err
	}
	return s.Tabs, nil
}

// OpenSession reads the newest Sessions/Session_* file (by modification
// time) under profileDir. A newest file without its initial-state marker
// (a snapshot Chromium is still writing) is skipped in favor of the next
// newest, as Chromium itself does; Path and ModTime name the file used.
//
// The browser appends to the file while running, so the result trails
// the live window state by the browser's save delay (a few seconds).
// Tabs are ordered by window id, then left to right.
func OpenSession(ctx context.Context, profileDir string) (*Session, error) {
	files, err := sessionFiles(profileDir)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tabs, err := readTabsFile(f.path)
		if errors.Is(err, ErrIncompleteSession) {
			lastErr = err
			continue
		}
		if err != nil {
			return nil, err
		}
		return &Session{Path: f.path, ModTime: f.mtime, Tabs: tabs}, nil
	}
	return nil, lastErr
}

type sessionFile struct {
	path  string
	mtime time.Time
}

// sessionFiles lists Sessions/Session_* under profileDir, newest first.
func sessionFiles(profileDir string) ([]sessionFile, error) {
	dir := filepath.Join(profileDir, "Sessions")
	paths, err := filepath.Glob(filepath.Join(dir, "Session_*"))
	if err != nil {
		return nil, err
	}
	files := make([]sessionFile, 0, len(paths))
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		files = append(files, sessionFile{p, fi.ModTime()})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%w in %s", ErrNoSessionFile, dir)
	}
	sort.Slice(files, func(i, j int) bool {
		if !files[i].mtime.Equal(files[j].mtime) {
			return files[i].mtime.After(files[j].mtime)
		}
		return files[i].path > files[j].path
	})
	return files, nil
}

func readTabsFile(path string) ([]Tab, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tabs, err := ReadTabs(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return tabs, nil
}

// ReadTabs replays an SNSS session command log from r and returns the
// tabs open at the end of it. A truncated trailing command is ignored;
// unknown or malformed commands are skipped.
func ReadTabs(r io.Reader) ([]Tab, error) {
	sr, err := newSNSSReader(r)
	if err != nil {
		return nil, err
	}
	s := newSessionState()
	sawMarker := false
	for {
		cmd, err := sr.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if cmd.id == cmdInitialStateMarker {
			sawMarker = true
			continue
		}
		s.apply(cmd)
	}
	if sr.version == snssVersionWithMarker && !sawMarker {
		return nil, ErrIncompleteSession
	}
	return s.openTabs(), nil
}

// navEntry is one entry of a tab's back/forward list.
type navEntry struct {
	index int32
	url   string
	title string
}

type tabState struct {
	id          int32
	windowID    int32
	hasWindow   bool
	visualIndex int32
	// currentNav is a navEntry.index value, not a slice position;
	// Chromium's SessionTab starts it at -1.
	currentNav int32
	navs       []navEntry // sorted by index
	pinned     bool
}

// sessionState mirrors the replay in Chromium's
// RestoreSessionFromCommands: per-id tab and window records mutated
// command by command.
type sessionState struct {
	tabs          map[int32]*tabState
	selectedTab   map[int32]int32 // window id -> selected position
	closedWindows map[int32]bool
}

func newSessionState() *sessionState {
	return &sessionState{
		tabs:          map[int32]*tabState{},
		selectedTab:   map[int32]int32{},
		closedWindows: map[int32]bool{},
	}
}

func (s *sessionState) tab(id int32) *tabState {
	t, ok := s.tabs[id]
	if !ok {
		t = &tabState{id: id, currentNav: -1}
		s.tabs[id] = t
	}
	return t
}

// commandHandlers maps each replayed command id to its handler. Ids
// absent here are skipped.
var commandHandlers = map[uint8]func(*sessionState, *payloadReader){
	cmdSetTabWindow:                    (*sessionState).setTabWindow,
	cmdSetTabIndexInWindow:             (*sessionState).setTabIndex,
	cmdUpdateTabNavigation:             (*sessionState).updateNavigation,
	cmdSetSelectedNavigationIndex:      (*sessionState).setSelectedNavigation,
	cmdSetSelectedTabInIndex:           (*sessionState).setSelectedTab,
	cmdSetPinnedState:                  (*sessionState).setPinned,
	cmdTabClosed:                       (*sessionState).closeTab,
	cmdWindowClosed:                    (*sessionState).closeWindow,
	cmdTabNavigationPathPruned:         (*sessionState).prunePath,
	cmdTabNavigationPathPrunedFromBack: (*sessionState).prunePathFromBack,
}

// apply mutates state for one command. Every handler ignores a payload
// too short for its command, matching Chromium, which drops unreadable
// commands.
func (s *sessionState) apply(cmd snssCommand) {
	if h, ok := commandHandlers[cmd.id]; ok {
		h(s, &payloadReader{b: cmd.payload})
	}
}

func (s *sessionState) setTabWindow(p *payloadReader) {
	window, tab := p.int32(), p.int32()
	if !p.bad {
		t := s.tab(tab)
		t.windowID, t.hasWindow = window, true
	}
}

func (s *sessionState) setTabIndex(p *payloadReader) {
	tab, index := p.int32(), p.int32()
	if !p.bad {
		s.tab(tab).visualIndex = index
	}
}

func (s *sessionState) setSelectedNavigation(p *payloadReader) {
	tab, index := p.int32(), p.int32()
	if !p.bad {
		s.tab(tab).currentNav = index
	}
}

func (s *sessionState) setSelectedTab(p *payloadReader) {
	window, index := p.int32(), p.int32()
	if !p.bad {
		s.selectedTab[window] = index
	}
}

// setPinned reads the bool only: the three padding bytes after it are
// not guaranteed to be zero.
func (s *sessionState) setPinned(p *payloadReader) {
	tab, pinned := p.int32(), p.uint8()
	if !p.bad {
		s.tab(tab).pinned = pinned != 0
	}
}

func (s *sessionState) closeTab(p *payloadReader) {
	tab := p.int32()
	if !p.bad {
		delete(s.tabs, tab)
	}
}

func (s *sessionState) closeWindow(p *payloadReader) {
	window := p.int32()
	if !p.bad {
		s.closedWindows[window] = true
		delete(s.selectedTab, window)
	}
}

func (s *sessionState) prunePath(p *payloadReader) {
	tab, index, count := p.int32(), p.int32(), p.int32()
	if !p.bad && index >= 0 && count > 0 {
		s.tab(tab).prune(index, count)
	}
}

func (s *sessionState) prunePathFromBack(p *payloadReader) {
	tab, index := p.int32(), p.int32()
	if !p.bad {
		s.tab(tab).pruneFromBack(index)
	}
}

// updateNavigation decodes an UpdateTabNavigation pickle and inserts or
// replaces the entry at its index. Fields after the title (page state,
// referrer, timestamps, ...) are ignored.
func (s *sessionState) updateNavigation(p *payloadReader) {
	p.take(4) // pickle payload size
	tab := p.int32()
	index := p.int32()
	url := p.pickleString()
	title := p.pickleString16()
	if p.bad {
		return
	}
	t := s.tab(tab)
	i := sort.Search(len(t.navs), func(i int) bool { return t.navs[i].index >= index })
	e := navEntry{index: index, url: url, title: title}
	if i < len(t.navs) && t.navs[i].index == index {
		t.navs[i] = e
		return
	}
	t.navs = append(t.navs, navEntry{})
	copy(t.navs[i+1:], t.navs[i:])
	t.navs[i] = e
}

// prune removes entries [index, index+count) and shifts later entries
// down, as Chromium's kCommandTabNavigationPathPruned handler does.
func (t *tabState) prune(index, count int32) {
	end := index + count
	switch {
	case t.currentNav >= index && t.currentNav < end:
		t.currentNav = index - 1
	case t.currentNav >= end:
		t.currentNav -= count
	}
	kept := t.navs[:0]
	for _, n := range t.navs {
		if n.index >= index && n.index < end {
			continue
		}
		if n.index >= end {
			n.index -= count
		}
		kept = append(kept, n)
	}
	t.navs = kept
}

// pruneFromBack removes entries at or after index (legacy command).
func (t *tabState) pruneFromBack(index int32) {
	t.currentNav = max(-1, min(t.currentNav, index-1))
	kept := t.navs[:0]
	for _, n := range t.navs {
		if n.index < index {
			kept = append(kept, n)
		}
	}
	t.navs = kept
}

// current returns the entry Chromium would restore: the first entry
// whose index is at or past currentNav, else the last entry.
func (t *tabState) current() navEntry {
	for _, n := range t.navs {
		if n.index >= t.currentNav {
			return n
		}
	}
	return t.navs[len(t.navs)-1]
}

// openTabs assembles open tabs: those assigned to a window that is still
// open and holding at least one navigation entry, grouped by window id
// and ordered by visual index (ties by tab id, as Chromium's stable
// sort over its id-ordered map yields).
func (s *sessionState) openTabs() []Tab {
	byWindow := map[int32][]*tabState{}
	for _, t := range s.tabs {
		if !t.hasWindow || len(t.navs) == 0 || s.closedWindows[t.windowID] {
			continue
		}
		byWindow[t.windowID] = append(byWindow[t.windowID], t)
	}
	windows := make([]int32, 0, len(byWindow))
	for w := range byWindow {
		windows = append(windows, w)
	}
	sort.Slice(windows, func(i, j int) bool { return windows[i] < windows[j] })

	out := make([]Tab, 0, len(s.tabs))
	for _, w := range windows {
		ts := byWindow[w]
		sort.Slice(ts, func(i, j int) bool {
			if ts[i].visualIndex != ts[j].visualIndex {
				return ts[i].visualIndex < ts[j].visualIndex
			}
			return ts[i].id < ts[j].id
		})
		selected, hasSelected := s.selectedTab[w]
		for i, t := range ts {
			nav := t.current()
			out = append(out, Tab{
				WindowID: w,
				TabID:    t.id,
				Index:    i,
				URL:      nav.url,
				Title:    nav.title,
				Active:   hasSelected && int(selected) == i,
				Pinned:   t.pinned,
			})
		}
	}
	return out
}
