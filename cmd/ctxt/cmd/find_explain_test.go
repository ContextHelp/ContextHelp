package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// seedExplainFilterPair seeds two objects that both match the query term
// "deployment": one whose metadata satisfies a facet filter, one that does
// not. A filter-honouring search returns only the first.
func seedExplainFilterPair(t *testing.T, db *testDB, tc explainFilterCase) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	for _, o := range []struct {
		id      string
		meta    map[string]any
		tags    []string
		created time.Time
	}{
		{"obj_explain_match", tc.match, tc.matchTags, tc.matchCreated},
		{"obj_explain_miss", tc.miss, tc.missTags, tc.missCreated},
	} {
		created := o.created
		if created.IsZero() {
			created = now
		}
		obj := &storage.KnowledgeObject{
			ID:         o.id,
			Type:       "text",
			Summaries:  []string{"deployment runbook notes"},
			RawContent: "deployment details",
			Metadata:   o.meta,
			CreatedAt:  created,
			UpdatedAt:  created,
		}
		for _, l := range o.tags {
			obj.Tags = append(obj.Tags, storage.Tag{Label: l})
		}
		if err := db.Driver.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("seed %s: %v", o.id, err)
		}
	}
	rebuildFTSForTest(t, db)
}

// explainFilterCases covers every facet filter family find exposes.
type explainFilterCase struct {
	name  string
	flags []string
	match map[string]any
	miss  map[string]any
	// Tags and creation times of the two objects, when the case filters
	// by them; now when unset.
	matchTags, missTags       []string
	matchCreated, missCreated time.Time
}

var explainFilterCases = []explainFilterCase{
	{
		name:  "meta-type",
		flags: []string{"--meta-type", "task"},
		match: map[string]any{"type": "task"},
		miss:  map[string]any{"type": "observation"},
	},
	{
		name:  "topic",
		flags: []string{"--topic", "kubernetes"},
		match: map[string]any{"topics": []any{"kubernetes"}},
		miss:  map[string]any{"topics": []any{"billing"}},
	},
	{
		name:  "person",
		flags: []string{"--person", "alice-chen"},
		match: map[string]any{"people": []any{"alice-chen"}},
		miss:  map[string]any{"people": []any{"bob-li"}},
	},
	{
		name:  "mentioned-since",
		flags: []string{"--mentioned-since", "2026-04-01"},
		match: map[string]any{"dates_mentioned": []any{"2026-05-10"}},
		miss:  map[string]any{"dates_mentioned": []any{"2026-01-10"}},
	},
	{
		name:  "mentioned-until",
		flags: []string{"--mentioned-until", "2026-04-01"},
		match: map[string]any{"dates_mentioned": []any{"2026-01-10"}},
		miss:  map[string]any{"dates_mentioned": []any{"2026-05-10"}},
	},
	{name: "tagged one", flags: []string{"--tagged", "ops"}, matchTags: []string{"ops"}, missTags: []string{"dev"}},
	{name: "tagged any", flags: []string{"--tagged", "ops,infra"}, matchTags: []string{"infra"}, missTags: []string{"dev"}},
	{name: "tagged any repeated", flags: []string{"--tagged", "ops", "--tagged", "infra"}, matchTags: []string{"infra"}, missTags: []string{"dev"}},
	{name: "tagged all", flags: []string{"--tagged", "+ops,+infra"}, matchTags: []string{"ops", "infra"}, missTags: []string{"ops"}},
	{name: "tagged all with optional", flags: []string{"--tagged", "+ops,infra"}, matchTags: []string{"ops"}, missTags: []string{"infra"}},
	{
		name: "since", flags: []string{"--since", "2026-03-01"},
		matchCreated: time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC), missCreated: time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC),
	},
	{
		name: "until", flags: []string{"--until", "2026-03-01"},
		matchCreated: time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC), missCreated: time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC),
	},
	{
		name: "since relative", flags: []string{"--since", "3d"},
		matchCreated: time.Now().Add(-time.Hour), missCreated: time.Now().Add(-10 * 24 * time.Hour),
	},
	{
		name: "tags and since", flags: []string{"--tagged", "ops", "--since", "1w"},
		matchTags: []string{"ops"}, missTags: []string{"ops"},
		matchCreated: time.Now().Add(-time.Hour), missCreated: time.Now().Add(-30 * 24 * time.Hour),
	},
	{
		name:  "source-type",
		flags: []string{"--source-type", "slack"},
		match: map[string]any{"source_type": "slack"},
		miss:  map[string]any{"source_type": "email"},
	},
}

// TestFindExplain_AppliesFacetFilters asserts --explain honours the same
// facet filters as plain hybrid search instead of silently dropping them.
func TestFindExplain_AppliesFacetFilters(t *testing.T) {
	for _, tc := range explainFilterCases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			seedExplainFilterPair(t, db, tc)

			args := append([]string{"find", "deployment", "--hybrid", "--explain"}, tc.flags...)
			out, err := db.exec(args...)
			if err != nil {
				t.Fatalf("find --explain %v: %v", tc.flags, err)
			}
			if !strings.Contains(out, "--explain") {
				t.Fatalf("expected explain output, got:\n%s", out)
			}
			if !strings.Contains(out, "obj_explain_match") {
				t.Errorf("filter-matching object missing from explain output:\n%s", out)
			}
			if strings.Contains(out, "obj_explain_miss") {
				t.Errorf("--explain ignored %v: non-matching object returned:\n%s", tc.flags, out)
			}
		})
	}
}

// TestFindExplain_JSONMatchesPlainHybrid asserts the structured --explain
// envelope carries the same filtered result set as plain hybrid search.
func TestFindExplain_JSONMatchesPlainHybrid(t *testing.T) {
	for _, tc := range explainFilterCases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			seedExplainFilterPair(t, db, tc)

			base := append([]string{"--format", "json", "find", "deployment", "--hybrid"}, tc.flags...)

			// stdout only: the no-default-model notice goes to stderr.
			plainOut, _, err := execFind(t, db, base...)
			if err != nil {
				t.Fatalf("plain find: %v", err)
			}
			var plain struct {
				Objects []struct {
					ID string `json:"id"`
				} `json:"objects"`
			}
			if err := json.Unmarshal([]byte(plainOut), &plain); err != nil {
				t.Fatalf("decode plain: %v\n%s", err, plainOut)
			}

			explainOut, _, err := execFind(t, db, append(base, "--explain")...)
			if err != nil {
				t.Fatalf("find --explain: %v", err)
			}
			var explain struct {
				Results []struct {
					Object struct {
						ID string `json:"id"`
					} `json:"object"`
				} `json:"results"`
			}
			if err := json.Unmarshal([]byte(explainOut), &explain); err != nil {
				t.Fatalf("decode explain: %v\n%s", err, explainOut)
			}

			var plainIDs, explainIDs []string
			for _, o := range plain.Objects {
				plainIDs = append(plainIDs, o.ID)
			}
			for _, r := range explain.Results {
				explainIDs = append(explainIDs, r.Object.ID)
			}
			if strings.Join(plainIDs, ",") != "obj_explain_match" {
				t.Fatalf("plain hybrid baseline = %v, want [obj_explain_match]", plainIDs)
			}
			if strings.Join(explainIDs, ",") != strings.Join(plainIDs, ",") {
				t.Errorf("--explain results %v differ from plain hybrid %v", explainIDs, plainIDs)
			}
		})
	}
}
