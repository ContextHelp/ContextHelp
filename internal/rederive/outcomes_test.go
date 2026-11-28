package rederive

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

func TestRun_ARefusalIsASkipAtOnceAndIsNotRetried(t *testing.T) {
	f := newFixture(t)
	cases := map[string]struct {
		err    error
		reason string
	}{
		"refused": {fmt.Errorf("reanalyze: %w: lost original", upgrade.ErrCannotReanalyze), storage.SkipRefused},
	}
	for id := range cases {
		f.add(id, "alpha@v1", alphaOld)
	}
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error { return cases[id].err }
	res, err := f.run()
	if err != nil {
		t.Fatal(err)
	}
	got := f.skips(alphaNew)
	for id, c := range cases {
		if got[id] != c.reason {
			t.Errorf("%s: skip reason %q, want %q", id, got[id], c.reason)
		}
		if sig := f.sigOf(id); sig != alphaOld {
			t.Errorf("%s: signature %q, want the old one kept", id, sig)
		}
	}
	if res.Failed != 0 || res.SkippedTotal() != 1 || res.Processed != 0 {
		t.Errorf("result = %+v: only a local refusal is a skip at once", res)
	}
	// The next run does not try them again.
	calls := len(f.svc.called())
	res, err = f.run()
	if err != nil || res.Total != 0 || len(f.svc.called()) != calls {
		t.Fatalf("second run: %+v, %v, calls %d -> %d", res, err, calls, len(f.svc.called()))
	}
}

// A skip holds for its target signature only.
func TestRun_ANewTargetMakesSkippedObjectsEligibleAgain(t *testing.T) {
	f := newFixture(t)
	f.add("o1", "alpha@v1", alphaOld)
	f.svc.result = func(string, service.ReanalyzeOptions) error { return fmt.Errorf("x: %w", upgrade.ErrCannotReanalyze) }
	if _, err := f.run(); err != nil {
		t.Fatal(err)
	}
	if f.stale() != 0 {
		t.Fatal("the skipped object is still listed")
	}
	// The pipeline moves on to a newer signature.
	newer := "alpha#0000000000000003"
	f.reg.Upsert("alpha", &pipeline.Pipeline{PipelineName: "alpha", Signature: newer,
		Entries: []string{"a@1", "b@3"}, BaselineSignature: alphaOld, BaselineEntries: alphaOldEntries})
	if err := f.store.UpsertDerivationSignature(context.Background(), newer, "alpha", []string{"a@1", "b@3"}); err != nil {
		t.Fatal(err)
	}
	f.svc.result = nil
	res, err := f.run()
	if err != nil || res.Processed != 1 {
		t.Fatalf("after the target moved: %+v, %v", res, err)
	}
	if got := f.sigOf("o1"); got != newer {
		t.Errorf("signature = %q, want %q", got, newer)
	}
}

func TestDetail_IsShortAndKeepsOnlySchemeAndHost(t *testing.T) {
	cases := map[string]struct {
		in   string
		want []string // kept
		gone []string // never kept
	}{
		"userinfo, path, query, fragment": {
			`fetch "https://user:s3cret@example.com/a/b?token=abc123&x=1#frag" failed`,
			[]string{"https://example.com"}, []string{"s3cret", "user", "/a/b", "abc123", "x=1", "frag"}},
		"ipv6 with a query": {
			`Get "http://[::1]:8080/p?token=SECRET": dial tcp`,
			[]string{"http://[::1]:8080"}, []string{"SECRET", "/p", "token"}},
		"a webhook path": {
			`post https://hooks.example.com/services/T000/B000/XXXXSECRETXXXX returned 500`,
			[]string{"https://hooks.example.com"}, []string{"XXXXSECRETXXXX", "services"}},
		"a bearer token in an error body": {
			`401 {"error":"bad credentials"} Authorization: Bearer ghp_abcdef123456 rejected`,
			[]string{"Authorization: <redacted>"}, []string{"ghp_abcdef123456", "Bearer ghp"}},
		"a token marker outside a url": {
			`request failed: api_key=sk-12345 and password: hunter2`,
			[]string{"api_key=<redacted>", "password: <redacted>"}, []string{"sk-12345", "hunter2"}},
		"two urls": {
			"from http://a.example/x?k=v to https://b.example/y",
			[]string{"http://a.example", "https://b.example"}, []string{"/x", "k=v", "/y"}},
	}
	for name, c := range cases {
		got := detail(c.in)
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q lacks %q", name, got, w)
			}
		}
		for _, g := range c.gone {
			if strings.Contains(got, g) {
				t.Errorf("%s: %q still holds %q", name, got, g)
			}
		}
	}
	long := detail("x " + strings.Repeat("y", 500) + "\nz")
	if len(long) > maxDetail || strings.Contains(long, "\n") {
		t.Errorf("detail is %d bytes, newline=%v", len(long), strings.Contains(long, "\n"))
	}
}

// --- provider outage ---

func llmFailure(id string) error {
	return fmt.Errorf("reanalyze: %q: %w, nothing written: %w", id, upgrade.ErrProviderFailed,
		&providers.CallError{Provider: "llm", Err: errors.New("connection refused")})
}

// --- the gate ---
