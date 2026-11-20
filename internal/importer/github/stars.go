package github

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	ghapi "github.com/ideacrafterslabs/ctxt/internal/github"
)

// Hints a star import attaches. Union semantics: a stored object keeps
// every hint ever submitted, so a list a repo left stays as a tag; the
// authoritative membership is the star_lists vocabulary in the object's
// data, which each re-submission replaces.
const (
	HintStarred    = "starred"
	HintListPrefix = "star-list:"
)

// StarItem is one repository submission of a star import.
type StarItem struct {
	// SourceKey is github:<owner>/<repo>, the key the object is stored
	// and updated under.
	SourceKey string
	URL       string
	Hints     []string
	// Data is stored as the object's metadata["data"]: account, starred,
	// star_lists (slug, name, description per List the repo is in).
	Data map[string]any
	// Fingerprint identifies Hints and Data; an unchanged repo has the
	// same one on every run.
	Fingerprint string
}

// BuildStarItems maps a snapshot to submissions, one per repository, in
// snapshot order.
func BuildStarItems(snap *ghapi.StarSnapshot) []StarItem {
	items := make([]StarItem, 0, len(snap.Repos))
	for _, r := range snap.Repos {
		lists := append([]ghapi.StarList(nil), r.Lists...)
		sort.Slice(lists, func(i, j int) bool { return lists[i].Slug < lists[j].Slug })
		vocab := make([]map[string]any, 0, len(lists))
		var hints []string
		if r.Starred {
			hints = append(hints, HintStarred)
		}
		for _, l := range lists {
			hints = append(hints, HintListPrefix+l.Slug)
			e := map[string]any{"slug": l.Slug, "name": l.Name}
			if l.Description != "" {
				e["description"] = l.Description
			}
			vocab = append(vocab, e)
		}
		data := map[string]any{"account": snap.Viewer, "starred": r.Starred, "star_lists": vocab}
		it := StarItem{SourceKey: "github:" + r.FullName, URL: r.URL, Hints: hints, Data: data}
		b, _ := json.Marshal(struct {
			H []string
			D map[string]any
		}{hints, data})
		sum := sha256.Sum256(b)
		it.Fingerprint = hex.EncodeToString(sum[:])
		items = append(items, it)
	}
	return items
}

// StarState records the fingerprint last submitted per source key, so a
// rerun submits only repositories whose membership changed. It is a
// local cache: losing it costs one full resubmission, which the server
// treats as an idempotent update.
type StarState struct {
	path string
	Keys map[string]string `json:"keys"`
}

// LoadStarState reads the state at path; a missing file is an empty
// state, an unreadable one an error (never silently reset).
func LoadStarState(path string) (*StarState, error) {
	st := &StarState{path: path, Keys: map[string]string{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, fmt.Errorf("star import state: %w", err)
	}
	if err := json.Unmarshal(b, st); err != nil {
		return nil, fmt.Errorf("star import state %s is corrupt (delete it or pass --full): %w", path, err)
	}
	if st.Keys == nil {
		st.Keys = map[string]string{}
	}
	return st, nil
}

// Plan returns the items that differ from the recorded state, or all of
// them when full.
func (s *StarState) Plan(items []StarItem, full bool) []StarItem {
	var out []StarItem
	for _, it := range items {
		if full || s.Keys[it.SourceKey] != it.Fingerprint {
			out = append(out, it)
		}
	}
	return out
}

// Record notes it as submitted.
func (s *StarState) Record(it StarItem) { s.Keys[it.SourceKey] = it.Fingerprint }

// Save writes the state atomically.
func (s *StarState) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("star import state: %w", err)
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".state-*")
	if err != nil {
		return fmt.Errorf("star import state: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}
