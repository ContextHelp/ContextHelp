package github

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	ghapi "github.com/ideacrafterslabs/ctxt/internal/github"
)

func sampleSnapshot() *ghapi.StarSnapshot {
	goTools := ghapi.StarList{Slug: "go-tools", Name: "Go tools", Description: "CLI things"}
	ai := ghapi.StarList{Slug: "ai", Name: "AI"}
	return &ghapi.StarSnapshot{
		Viewer: "octo",
		Lists:  []ghapi.StarList{goTools, ai},
		Repos: []ghapi.StarRepo{
			{FullName: "a/one", URL: "https://github.com/a/one", Starred: true, Lists: []ghapi.StarList{goTools, ai}},
			{FullName: "b/two", URL: "https://github.com/b/two", Starred: true},
			{FullName: "c/three", URL: "https://github.com/c/three", Lists: []ghapi.StarList{ai}},
		},
	}
}

func TestBuildStarItems_MapsHintsKeyAndVocabulary(t *testing.T) {
	items := BuildStarItems(sampleSnapshot())
	if len(items) != 3 {
		t.Fatalf("items = %d", len(items))
	}
	one := items[0]
	if one.SourceKey != "github:a/one" || one.URL != "https://github.com/a/one" {
		t.Errorf("key/url = %q %q", one.SourceKey, one.URL)
	}
	if want := []string{"starred", "star-list:ai", "star-list:go-tools"}; !reflect.DeepEqual(one.Hints, want) {
		t.Errorf("hints = %v, want %v", one.Hints, want)
	}
	lists, _ := one.Data["star_lists"].([]map[string]any)
	if len(lists) != 2 || lists[0]["slug"] != "ai" || lists[1]["name"] != "Go tools" || lists[1]["description"] != "CLI things" {
		t.Errorf("star_lists = %v", one.Data["star_lists"])
	}
	if one.Data["account"] != "octo" || one.Data["starred"] != true {
		t.Errorf("data = %v", one.Data)
	}
	// Starred, in no list: hint only, empty vocabulary (replaces a stale one).
	two := items[1]
	if !reflect.DeepEqual(two.Hints, []string{"starred"}) || len(two.Data["star_lists"].([]map[string]any)) != 0 {
		t.Errorf("two = %+v", two)
	}
	// Listed, not starred: no starred hint.
	three := items[2]
	if !reflect.DeepEqual(three.Hints, []string{"star-list:ai"}) || three.Data["starred"] != false {
		t.Errorf("three = %+v", three)
	}
}

func TestStarItemFingerprint_TracksMembershipOnly(t *testing.T) {
	a := BuildStarItems(sampleSnapshot())[0]
	snap := sampleSnapshot()
	snap.Repos[0].Lists = []ghapi.StarList{snap.Repos[0].Lists[1], snap.Repos[0].Lists[0]} // reordered
	if b := BuildStarItems(snap)[0]; a.Fingerprint != b.Fingerprint {
		t.Error("list order changed the fingerprint")
	}
	snap.Repos[0].Lists = snap.Repos[0].Lists[:1]
	if b := BuildStarItems(snap)[0]; a.Fingerprint == b.Fingerprint {
		t.Error("membership change kept the fingerprint")
	}
	snap = sampleSnapshot()
	snap.Repos[0].Lists[0].Description = "renamed"
	if b := BuildStarItems(snap)[0]; a.Fingerprint == b.Fingerprint {
		t.Error("list description change kept the fingerprint")
	}
}

func TestStarState_PlanSubmitsOnlyChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "state.json")
	st, err := LoadStarState(path)
	if err != nil {
		t.Fatal(err)
	}
	items := BuildStarItems(sampleSnapshot())
	if plan := st.Plan(items, false); len(plan) != 3 {
		t.Fatalf("first plan = %d, want 3", len(plan))
	}
	for _, it := range items {
		st.Record(it)
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	st2, err := LoadStarState(path)
	if err != nil {
		t.Fatal(err)
	}
	if plan := st2.Plan(items, false); len(plan) != 0 {
		t.Errorf("rerun plan = %d, want 0", len(plan))
	}
	if plan := st2.Plan(items, true); len(plan) != 3 {
		t.Errorf("full plan = %d, want 3", len(plan))
	}
	snap := sampleSnapshot()
	snap.Repos[1].Lists = []ghapi.StarList{snap.Lists[0]}
	plan := st2.Plan(BuildStarItems(snap), false)
	if len(plan) != 1 || plan[0].SourceKey != "github:b/two" {
		t.Errorf("changed plan = %+v", plan)
	}
}

func TestLoadStarState_CorruptIsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStarState(path); err == nil {
		t.Error("corrupt state loaded")
	}
}
