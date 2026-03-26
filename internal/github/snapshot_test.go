package github

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// mustTime parses an RFC3339 string or panics — test helper only.
func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestRepoSnapshot_JSONRoundtrip(t *testing.T) {
	pushed := mustTime("2026-01-15T12:00:00Z")
	snap := RepoSnapshot{
		Owner:       "acme",
		Name:        "widget",
		FullName:    "acme/widget",
		URL:         "https://github.com/acme/widget",
		Description: "The widget library",
		Language:    "Go",
		Topics:      []string{"go", "library"},
		License:     "MIT",
		Stars:       42,
		Forks:       7,
		OpenIssues:  3,
		IsArchived:  false,
		IsFork:      false,
		CreatedAt:   mustTime("2024-06-01T00:00:00Z"),
		UpdatedAt:   mustTime("2026-01-15T12:00:00Z"),
		PushedAt:    pushed,
		README:      "# widget\nA Go library.",
		Sections: []pluginapi.Section{
			{Title: "Installation", Content: "go get github.com/acme/widget", Order: 0},
		},
		Dependencies: []Dependency{
			{Name: "github.com/some/dep", Version: "v1.2.3", Ecosystem: "go"},
		},
		Releases: []Release{
			{Tag: "v1.0.0", Name: "Initial release", Body: "First stable release.", PublishedAt: pushed},
		},
	}

	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got RepoSnapshot
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.FullName != snap.FullName {
		t.Errorf("FullName: got %q, want %q", got.FullName, snap.FullName)
	}
	if got.Stars != snap.Stars {
		t.Errorf("Stars: got %d, want %d", got.Stars, snap.Stars)
	}
	if len(got.Topics) != len(snap.Topics) {
		t.Errorf("Topics len: got %d, want %d", len(got.Topics), len(snap.Topics))
	}
	if len(got.Dependencies) != 1 {
		t.Fatalf("Dependencies len: got %d, want 1", len(got.Dependencies))
	}
	if got.Dependencies[0].Ecosystem != "go" {
		t.Errorf("Dependency.Ecosystem: got %q, want %q", got.Dependencies[0].Ecosystem, "go")
	}
	if len(got.Releases) != 1 {
		t.Fatalf("Releases len: got %d, want 1", len(got.Releases))
	}
	if got.Releases[0].Tag != "v1.0.0" {
		t.Errorf("Release.Tag: got %q, want %q", got.Releases[0].Tag, "v1.0.0")
	}
}

func TestRepoSnapshot_ZeroValue(t *testing.T) {
	var snap RepoSnapshot
	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal zero: %v", err)
	}
	var got RepoSnapshot
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal zero: %v", err)
	}
	if got.Stars != 0 || got.IsArchived != false {
		t.Error("zero RepoSnapshot round-trip produced non-zero values")
	}
}

func TestIssueSnapshot_JSONRoundtrip(t *testing.T) {
	closedAt := mustTime("2026-02-20T08:00:00Z")
	snap := IssueSnapshot{
		Owner:     "acme",
		Repo:      "widget",
		Number:    99,
		URL:       "https://github.com/acme/widget/issues/99",
		Title:     "Fix panic on nil input",
		Body:      "Steps to reproduce...",
		State:     "closed",
		Labels:    []string{"bug", "priority:high"},
		Author:    "alice",
		Assignees: []string{"bob"},
		LinkedPR:  123,
		Sections: []pluginapi.Section{
			{Title: "Discussion", Content: "See comment thread.", Order: 0},
		},
		CreatedAt: mustTime("2026-02-10T09:00:00Z"),
		UpdatedAt: mustTime("2026-02-20T08:00:00Z"),
		ClosedAt:  &closedAt,
	}

	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got IssueSnapshot
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Number != snap.Number {
		t.Errorf("Number: got %d, want %d", got.Number, snap.Number)
	}
	if got.State != snap.State {
		t.Errorf("State: got %q, want %q", got.State, snap.State)
	}
	if got.LinkedPR != snap.LinkedPR {
		t.Errorf("LinkedPR: got %d, want %d", got.LinkedPR, snap.LinkedPR)
	}
	if got.ClosedAt == nil {
		t.Fatal("ClosedAt: got nil, want non-nil")
	}
	if !got.ClosedAt.Equal(*snap.ClosedAt) {
		t.Errorf("ClosedAt: got %v, want %v", got.ClosedAt, snap.ClosedAt)
	}
	if len(got.Labels) != 2 {
		t.Errorf("Labels len: got %d, want 2", len(got.Labels))
	}
}

func TestIssueSnapshot_OpenIssue(t *testing.T) {
	// Open issues have no ClosedAt; linked PR is 0.
	snap := IssueSnapshot{
		Owner:     "acme",
		Repo:      "widget",
		Number:    1,
		URL:       "https://github.com/acme/widget/issues/1",
		Title:     "Add feature X",
		State:     "open",
		CreatedAt: mustTime("2026-03-01T00:00:00Z"),
		UpdatedAt: mustTime("2026-03-01T00:00:00Z"),
	}

	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got IssueSnapshot
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.ClosedAt != nil {
		t.Errorf("ClosedAt: expected nil for open issue, got %v", got.ClosedAt)
	}
	if got.LinkedPR != 0 {
		t.Errorf("LinkedPR: expected 0 for open issue, got %d", got.LinkedPR)
	}
}

func TestDependency_Fields(t *testing.T) {
	d := Dependency{Name: "express", Version: "4.18.2", Ecosystem: "npm"}
	if d.Name != "express" || d.Version != "4.18.2" || d.Ecosystem != "npm" {
		t.Errorf("unexpected Dependency fields: %+v", d)
	}
}

func TestRelease_Fields(t *testing.T) {
	pub := mustTime("2026-01-01T00:00:00Z")
	r := Release{Tag: "v2.0.0", Name: "v2 launch", Body: "Breaking changes.", PublishedAt: pub}
	if r.Tag != "v2.0.0" || !r.PublishedAt.Equal(pub) {
		t.Errorf("unexpected Release fields: %+v", r)
	}
}
