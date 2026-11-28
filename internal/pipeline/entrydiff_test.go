package pipeline

import (
	"slices"
	"testing"
)

func TestDiffEntries(t *testing.T) {
	cases := []struct {
		name                                                   string
		old, new                                               []string
		added, removed, versioned, reordered, models, required []string
		noop                                                   bool
	}{
		{name: "no change", old: []string{"a@1", "b@1", "llm=x/y"}, new: []string{"a@1", "b@1", "llm=x/y"}, noop: true},
		{name: "version bump", old: []string{"a@1", "b@1"}, new: []string{"a@1", "b@2"}, versioned: []string{"b"}, required: []string{"b"}},
		{name: "step added", old: []string{"a@1", "b@1"}, new: []string{"a@1", "c@1", "b@1"}, added: []string{"c"}, required: []string{"c"}},
		{name: "step removed", old: []string{"a@1", "b@1"}, new: []string{"a@1"}, removed: []string{"b"}, noop: true},
		{name: "reordered", old: []string{"a@1", "b@1", "c@1"}, new: []string{"b@1", "a@1", "c@1"}, reordered: []string{"a", "b"}},
		{name: "model changed", old: []string{"a@1", "llm=x/y"}, new: []string{"a@1", "llm=x/z"}, models: []string{"llm"}},
		{name: "model added and removed", old: []string{"a@1", "llm=x/y"}, new: []string{"a@1", "vision=v/w"}, models: []string{"llm", "vision"}},
		{name: "an added step does not reorder the rest", old: []string{"a@1", "b@1"}, new: []string{"z@1", "a@1", "b@1"}, added: []string{"z"}, required: []string{"z"}},
		{name: "step repeated in the new list only", old: []string{"a@1", "e@1"}, new: []string{"a@1", "e@1", "e@1"}, added: []string{"e"}, required: []string{"e"}},
		{name: "step repeated in the old list only", old: []string{"a@1", "e@1", "e@1"}, new: []string{"a@1", "e@1"}, removed: []string{"e"}, noop: true},
		{name: "step repeated in both, one occurrence re-versioned", old: []string{"e@1", "a@1", "e@1"}, new: []string{"e@1", "a@1", "e@2"}, versioned: []string{"e"}, required: []string{"e"}},
		{name: "step repeated in both, unchanged", old: []string{"e@1", "a@1", "e@1"}, new: []string{"e@1", "a@1", "e@1"}, noop: true},
		{name: "repeated and moved", old: []string{"e@1", "a@1", "e@1"}, new: []string{"a@1", "e@1", "e@1", "e@1"}, added: []string{"e"}, reordered: []string{"a", "e"}, required: []string{"e"}},
		{name: "a removed step does not reorder the rest", old: []string{"z@1", "a@1", "b@1"}, new: []string{"a@1", "b@1"}, removed: []string{"z"}, noop: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := DiffEntries(c.old, c.new)
			for label, pair := range map[string][2][]string{
				"added": {d.Added, c.added}, "removed": {d.Removed, c.removed}, "versioned": {d.Versioned, c.versioned},
				"reordered": {d.Reordered, c.reordered}, "models": {d.Models, c.models}, "required": {d.Required(), c.required},
			} {
				got, want := pair[0], pair[1]
				slices.Sort(got)
				if !slices.Equal(got, want) && (len(got) != 0 || len(want) != 0) {
					t.Errorf("%s = %v, want %v", label, got, want)
				}
			}
			if !d.Applies() != c.noop {
				t.Errorf("!Applies() = %v, want %v", !d.Applies(), c.noop)
			}
		})
	}
}

func TestEntriesAreWhatTheSignatureHashes(t *testing.T) {
	steps := []StepRef{{Name: "a", Version: 0}, {Name: "b", Version: 3}}
	if got := EntriesOf(steps, "llm=x/y"); !slices.Equal(got, []string{"a@1", "b@3", "llm=x/y"}) {
		t.Fatalf("EntriesOf = %v", got)
	}
	if got := BaselineEntriesOf([]string{"a", "b"}, "llm=x/y"); !slices.Equal(got, []string{"a@1", "b@1", "llm=x/y"}) {
		t.Fatalf("BaselineEntriesOf = %v", got)
	}
	// The signature hashes exactly those entries: a version 0 reads as 1,
	// so the same entries give the same signature however they were built,
	// and the baseline is the signature of every step at version 1; another
	// entry gives another signature.
	sig := SignatureOf("p", steps, "llm=x/y")
	if sig != SignatureOf("p", []StepRef{{Name: "a", Version: 1}, {Name: "b", Version: 3}}, "llm=x/y") {
		t.Error("a step at version 0 must sign as version 1")
	}
	if BaselineSignatureOf("p", []string{"a", "b"}, "llm=x/y") != SignatureOf("p", []StepRef{{Name: "a", Version: 1}, {Name: "b", Version: 1}}, "llm=x/y") {
		t.Error("the baseline must sign as every step at version 1")
	}
	if sig == SignatureOf("p", steps, "llm=x/z") || sig == SignatureOf("p", []StepRef{{Name: "a", Version: 1}, {Name: "b", Version: 4}}, "llm=x/y") {
		t.Error("another entry must give another signature")
	}
}

func TestSignatureRecords_CurrentAndBaselineOfEveryPipeline(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("alpha", &Pipeline{PipelineName: "alpha", Signature: "alpha#2", Entries: []string{"a@2"},
		BaselineSignature: "alpha#1", BaselineEntries: []string{"a@1"}}); err != nil {
		t.Fatal(err)
	}
	// A pipeline left out for a missing provider: baseline only.
	MarkBaseline(r, "beta", "beta#1", []string{"b@1"})
	got := map[string][]string{}
	for _, rec := range SignatureRecords(r) {
		got[rec.Sig] = rec.Entries
	}
	want := map[string][]string{"alpha#2": {"a@2"}, "alpha#1": {"a@1"}, "beta#1": {"b@1"}}
	if len(got) != len(want) {
		t.Fatalf("records = %v, want %v", got, want)
	}
	for sig, entries := range want {
		if !slices.Equal(got[sig], entries) {
			t.Errorf("%s entries = %v, want %v", sig, got[sig], entries)
		}
	}
	if rec, ok := CurrentRecord(r, "alpha"); !ok || rec.Sig != "alpha#2" {
		t.Errorf("CurrentRecord(alpha) = %+v, %v", rec, ok)
	}
	if rec, ok := CurrentRecord(r, "beta"); !ok || rec.Sig != "beta#1" || !slices.Equal(rec.Entries, []string{"b@1"}) {
		t.Errorf("CurrentRecord(beta) = %+v, %v", rec, ok)
	}
	if _, ok := CurrentRecord(r, "gamma"); ok {
		t.Error("CurrentRecord(gamma) found a pipeline that is not known")
	}
}
