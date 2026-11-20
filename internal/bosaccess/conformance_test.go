package bosaccess

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// The scenarios are the fixtures of the authority's access contract, copied
// into testdata/fixtures; each starts from a peer that holds nothing and
// steps through sets, feed events, catch-ups, restarts and checks.

type scenarioStep struct {
	At      string          `json:"at"`
	Note    string          `json:"note"`
	Apply   json.RawMessage `json:"apply"`
	Expect  string          `json:"expect"`
	Unrep   []string        `json:"unrepresentable"`
	Restart bool            `json:"restart"`
	Feed    *Event          `json:"feed"`
	CatchUp bool            `json:"catch_up"`
	Resolve *struct {
		Issuer    string  `json:"issuer"`
		Subject   string  `json:"subject"`
		Principal *string `json:"principal"`
	} `json:"resolve"`
	Check *struct {
		Query
		Expect   string   `json:"expect"`
		Excluded []string `json:"excluded"`
	} `json:"check"`
}

type fixture struct {
	Schema      string          `json:"schema"`
	Valid       bool            `json:"valid"`
	Description string          `json:"description"`
	Document    json.RawMessage `json:"document"`
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func readFixtures(t *testing.T) map[string]fixture {
	t.Helper()
	files, err := filepath.Glob("testdata/fixtures/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	out := map[string]fixture{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var fx fixture
		if err := json.Unmarshal(raw, &fx); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		out[strings.TrimSuffix(filepath.Base(f), ".json")] = fx
	}
	return out
}

// allShapes is a peer that enforces every grant shape of the contract: the
// engine runs every scenario against it.
func allShapes() Profile {
	return Profile{
		Tool: "ctxt-test", ToolVersion: "0",
		Contracts:  []Listed{{ID: Contract, Level: "complete", Features: []string{FeatureRecordAccess, FeaturePrincipalIdentities}}},
		Represents: []string{ShapeTypeGrant, ShapeFieldExclusion, ShapeDirectionLimit, ShapeExpiry, ShapeRowRelation, ShapeRecordGrant},
		Limits:     Limits{MaxStalenessSeconds: 86400},
	}
}

func verdictWord(v Verdict) string {
	if v.Allow {
		return "allow"
	}
	return "deny"
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func TestConformanceScenarios(t *testing.T) {
	prof := allShapes()
	ran := 0
	for name, fx := range readFixtures(t) {
		if fx.Schema != SchemaScenario {
			continue
		}
		var doc struct {
			Requires []string       `json:"requires"`
			Steps    []scenarioStep `json:"steps"`
		}
		if err := json.Unmarshal(fx.Document, &doc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		skip := false
		for _, r := range doc.Requires {
			skip = skip || !prof.Supports(r)
		}
		if skip {
			continue
		}
		ran++
		t.Run(name, func(t *testing.T) { runScenario(t, prof, doc.Steps) })
	}
	if ran < 30 {
		t.Fatalf("only %d scenarios ran", ran)
	}
}

func runScenario(t *testing.T, prof Profile, steps []scenarioStep) {
	clk := &clock{t: time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)}
	store := &MemStore{}
	p, err := Open(prof, store, clk.now)
	if err != nil {
		t.Fatal(err)
	}
	var exp *Held // what the peer must hold, as the sets sent so far make it
	for i, st := range steps {
		at := fmt.Sprintf("step %d (%s)", i+1, st.Note)
		if st.At != "" {
			ts, err := time.Parse(time.RFC3339Nano, st.At)
			if err != nil {
				t.Fatalf("%s: %v", at, err)
			}
			clk.t = ts
		}
		switch {
		case len(st.Apply) > 0:
			applyStep(t, p, st, at, &exp)
		case st.Restart:
			if err := p.Reload(); err != nil {
				t.Fatalf("%s: restart: %v", at, err)
			}
		case st.Feed != nil:
			if err := p.Consume(*st.Feed); err != nil {
				t.Fatalf("%s: feed: %v", at, err)
			}
		case st.Resolve != nil:
			checkResolve(t, p, st, at)
		case st.CatchUp:
			if err := p.CatchUp(); err != nil {
				t.Fatalf("%s: catch up: %v", at, err)
			}
		case st.Check != nil:
			checkDecision(t, p, st, at)
		}
	}
}

func applyStep(t *testing.T, p *Peer, st scenarioStep, at string, exp **Held) {
	t.Helper()
	var set Set
	if err := json.Unmarshal(st.Apply, &set); err != nil {
		t.Fatalf("%s: %v", at, err)
	}
	before := p.State()
	ack, err := p.Apply(set)
	var ae *Error
	switch {
	case err == nil:
		checkApplied(t, p, st, set, ack, at, exp)
	case errors.As(err, &ae):
		if ae.Code != st.Expect {
			t.Errorf("%s: refused with %s, want %s", at, ae.Code, st.Expect)
		}
		if after := p.State(); string(mustJSON(before)) != string(mustJSON(after)) {
			t.Errorf("%s: a refused set changed what the peer holds", at)
		}
	default:
		t.Fatalf("%s: apply: %v", at, err)
	}
}

// The parser accepts exactly the fixtures marked valid.
func TestFixtureDocuments(t *testing.T) {
	for name, fx := range readFixtures(t) {
		t.Run(name, func(t *testing.T) {
			var err error
			switch fx.Schema {
			case SchemaSet:
				_, err = ParseSet(fx.Document)
			default:
				err = Validate(fx.Schema, fx.Document)
			}
			if (err == nil) != fx.Valid {
				t.Fatalf("accepted=%v, fixture says valid=%v (%s): %v", err == nil, fx.Valid, fx.Description, err)
			}
		})
	}
}

// checkApplied checks an acknowledgement and the state after it against what
// the scenario step expects.
func checkApplied(t *testing.T, p *Peer, st scenarioStep, set Set, ack Ack, at string, exp **Held) {
	t.Helper()
	if st.Expect != "applied" && st.Expect != "replayed" {
		t.Errorf("%s: applied a set it must refuse with %s", at, st.Expect)
		return
	}
	if ack.State != st.Expect {
		t.Errorf("%s: answered %s, want %s", at, ack.State, st.Expect)
	}
	if verr := Validate(SchemaAck, mustJSON(ack)); verr != nil {
		t.Errorf("%s: acknowledgement is not a valid document: %v", at, verr)
	}
	if ack.Epoch != set.Epoch || ack.Digest != set.Digest {
		t.Errorf("%s: acknowledged epoch %d digest %s, sent epoch %d digest %s", at, ack.Epoch, ack.Digest, set.Epoch, set.Digest)
	}
	// what the peer must hold of the entries now: the set, or the delta
	// merged into what it held
	if res, aerr := Apply(*exp, set, time.Time{}, Profile{}); aerr == nil {
		*exp = res.Held
	}
	checkRecordAck(t, ack, (*exp).Set.RecordAccess, at)
	got := make([]string, 0, len(ack.Unrepresentable))
	for _, u := range ack.Unrepresentable {
		got = append(got, u.Grant)
	}
	if ra := ack.RecordAccess; ra != nil {
		for _, u := range ra.Unrepresentable {
			got = append(got, u.Entry)
		}
	}
	want := slices.Clone(st.Unrep)
	sort.Strings(got)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Errorf("%s: reports %v unrepresentable, want %v", at, got, want)
	}
	rep := p.State()
	if verr := Validate(SchemaState, mustJSON(rep)); verr != nil {
		t.Errorf("%s: state is not a valid document: %v", at, verr)
	}
	if rep.AppliedEpoch == nil || *rep.AppliedEpoch != set.Epoch || rep.Digest == nil || *rep.Digest != set.Digest {
		t.Errorf("%s: state does not say what was applied: %+v", at, rep)
	}
}

// checkRecordAck: the acknowledgement counts the entries the peer holds and
// carries their digest; it says nothing of them when there are none.
func checkRecordAck(t *testing.T, ack Ack, held []RecordEntry, at string) {
	t.Helper()
	n := len(held)
	if n == 0 {
		if ack.RecordAccess != nil && ack.RecordAccess.Count != 0 {
			t.Errorf("%s: reports %d entries, set carries none", at, ack.RecordAccess.Count)
		}
		return
	}
	want := RecordAccessDigest(held)
	switch ra := ack.RecordAccess; {
	case ra == nil:
		t.Errorf("%s: set carries %d entries, acknowledgement says nothing of them", at, n)
	case ra.Count != n || ra.Digest != want:
		t.Errorf("%s: holds %d entries digest %s, sent %d digest %s", at, ra.Count, ra.Digest, n, want)
	}
}

func checkResolve(t *testing.T, p *Peer, st scenarioStep, at string) {
	t.Helper()
	want := ""
	if st.Resolve.Principal != nil {
		want = *st.Resolve.Principal
	}
	if got, _ := p.Resolve(st.Resolve.Issuer, st.Resolve.Subject); got != want {
		t.Errorf("%s: identity %s of %s maps to %q, want %q", at, st.Resolve.Subject, st.Resolve.Issuer, got, want)
	}
}

func checkDecision(t *testing.T, p *Peer, st scenarioStep, at string) {
	t.Helper()
	v := p.Decide(st.Check.Query)
	if got := verdictWord(v); got != st.Check.Expect {
		t.Errorf("%s: %s may read %s in %s (on behalf of %q): got %s (%s), want %s",
			at, st.Check.Principal, st.Check.TypeKey, st.Check.Scope, st.Check.OnBehalfOf, got, v.Reason, st.Check.Expect)
	}
	if st.Check.Expect == "allow" && v.Allow && len(st.Check.Excluded) > 0 {
		ex, want := slices.Clone(v.Excluded), slices.Clone(st.Check.Excluded)
		sort.Strings(ex)
		sort.Strings(want)
		if !slices.Equal(ex, want) {
			t.Errorf("%s: leaves out %v, want %v", at, ex, want)
		}
	}
}
