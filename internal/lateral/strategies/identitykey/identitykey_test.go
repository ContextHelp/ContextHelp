package identitykey_test

import (
	"reflect"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

func TestBuild_SingleIDSegment(t *testing.T) {
	got := identitykey.Build("substack", "publication", "anthropic-research")
	want := "substack/publication/anthropic-research"
	if got != want {
		t.Fatalf("Build = %q, want %q", got, want)
	}
}

func TestBuild_MultiIDSegmentsPreserveSlashes(t *testing.T) {
	// The original Build collapsed embedded slashes — Copilot flagged docs
	// vs code drift across 6 platforms. Variadic id parts join with "/"
	// without escaping, so multi-part ids land as documented.
	got := identitykey.Build("github", "repo", "samber", "lo")
	want := "github/repo/samber/lo"
	if got != want {
		t.Fatalf("Build = %q, want %q", got, want)
	}
}

func TestBuild_ChannelUploads(t *testing.T) {
	// YouTube uploads pseudo-id: documented as channel/<UC>/uploads.
	got := identitykey.Build("youtube", "channel", "UC12345", "uploads")
	want := "youtube/channel/uc12345/uploads"
	if got != want {
		t.Fatalf("Build = %q, want %q", got, want)
	}
}

func TestBuild_NormalizesAllSegments(t *testing.T) {
	got := identitykey.Build("  GitHub  ", "REPO", "  Samber  ", "  Lo  ")
	want := "github/repo/samber/lo"
	if got != want {
		t.Fatalf("Build trim+lowercase = %q, want %q", got, want)
	}
}

func TestBuild_EmbeddedSlashInSingleSegmentEscapes(t *testing.T) {
	// Slashes WITHIN a single id segment are still escaped — variadic
	// parts are the right way to express multi-segment ids; passing
	// "owner/with/slash" as one part means "this is one opaque id that
	// happens to contain slashes," and we keep it parseable.
	got := identitykey.Build("github", "repo", "owner/with/slash")
	want := "github/repo/owner_with_slash"
	if got != want {
		t.Fatalf("Build embedded-slash = %q, want %q", got, want)
	}
}

func TestBuild_EmptySegmentsDropped(t *testing.T) {
	cases := []struct {
		name string
		id   []string
		want string
	}{
		{"trailing_empty", []string{"samber", ""}, "github/repo/samber"},
		{"leading_empty", []string{"", "samber"}, "github/repo/samber"},
		{"middle_empty", []string{"samber", "", "lo"}, "github/repo/samber/lo"},
		{"whitespace_only", []string{"   ", "lo"}, "github/repo/lo"},
		// All-empty id parts must produce "" — a "github/repo" key with no
		// id is dangerously non-unique (every github repo capture would
		// dedup to one entity).
		{"all_empty", []string{"", " ", ""}, ""},
		{"no_id_args", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := identitykey.Build("github", "repo", tc.id...)
			if got != tc.want {
				t.Errorf("Build %v = %q, want %q", tc.id, got, tc.want)
			}
		})
	}
}

func TestBuild_RejectsEmptyPlatformOrEntityType(t *testing.T) {
	cases := []struct {
		name      string
		platform  string
		entityTyp string
	}{
		{"empty_platform", "", "repo"},
		{"empty_entity", "github", ""},
		{"both_empty", "", ""},
		{"whitespace_platform", "   ", "repo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := identitykey.Build(tc.platform, tc.entityTyp, "samber", "lo")
			if got != "" {
				t.Errorf("Build(%q, %q, ...) = %q, want \"\"", tc.platform, tc.entityTyp, got)
			}
		})
	}
}

func TestBuildLocalised_RejectsEmptyLocale(t *testing.T) {
	cases := []struct {
		name           string
		platform, e, l string
	}{
		{"empty_locale", "wikipedia", "article", ""},
		{"whitespace_locale", "wikipedia", "article", "   "},
		{"empty_platform", "", "article", "en"},
		{"empty_entity", "wikipedia", "", "en"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := identitykey.BuildLocalised(tc.platform, tc.e, tc.l, "turing_machine")
			if got != "" {
				t.Errorf("BuildLocalised(%q, %q, %q, ...) = %q, want \"\"", tc.platform, tc.e, tc.l, got)
			}
		})
	}
}

func TestBuildLocalised_RejectsEmptyID(t *testing.T) {
	got := identitykey.BuildLocalised("wikipedia", "article", "en")
	if got != "" {
		t.Errorf("BuildLocalised with no id parts = %q, want \"\"", got)
	}

	got = identitykey.BuildLocalised("wikipedia", "article", "en", "", "  ")
	if got != "" {
		t.Errorf("BuildLocalised with all-empty id parts = %q, want \"\"", got)
	}
}

func TestBuildLocalised(t *testing.T) {
	got := identitykey.BuildLocalised("wikipedia", "article", "en", "turing_machine")
	want := "wikipedia/article/en/turing_machine"
	if got != want {
		t.Fatalf("BuildLocalised = %q, want %q", got, want)
	}
}

func TestBuildLocalised_MultiIDParts(t *testing.T) {
	got := identitykey.BuildLocalised("wikipedia", "article", "fr", "Histoire", "Paris")
	want := "wikipedia/article/fr/histoire/paris"
	if got != want {
		t.Fatalf("BuildLocalised multi-id = %q, want %q", got, want)
	}
}

func TestParse_NonLocalised(t *testing.T) {
	got := identitykey.Parse("github/repo/samber/lo", false)
	want := identitykey.Parsed{
		Platform:   "github",
		EntityType: "repo",
		IDParts:    []string{"samber", "lo"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse = %+v, want %+v", got, want)
	}
}

func TestParse_Localised(t *testing.T) {
	got := identitykey.Parse("wikipedia/article/en/turing_machine", true)
	want := identitykey.Parsed{
		Platform:   "wikipedia",
		EntityType: "article",
		Locale:     "en",
		IDParts:    []string{"turing_machine"},
		Localised:  true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse localised = %+v, want %+v", got, want)
	}
}

func TestParse_TooFewSegments(t *testing.T) {
	cases := []string{"", "github", "github/repo"}
	for _, key := range cases {
		t.Run(key, func(t *testing.T) {
			got := identitykey.Parse(key, false)
			if !reflect.DeepEqual(got, identitykey.Parsed{}) {
				t.Errorf("Parse(%q) = %+v, want zero-value", key, got)
			}
		})
	}
}

func TestParse_LocalisedTooFewSegments(t *testing.T) {
	// localised=true requires at least 4 segments (platform/type/locale/id).
	got := identitykey.Parse("wikipedia/article/en", true)
	if !reflect.DeepEqual(got, identitykey.Parsed{}) {
		t.Errorf("Parse localised 3-segment = %+v, want zero-value", got)
	}
}

func TestRoundTrip_Build(t *testing.T) {
	cases := []struct {
		platform, entityType string
		id                   []string
	}{
		{"github", "repo", []string{"samber", "lo"}},
		{"substack", "publication", []string{"anthropic-research"}},
		{"youtube", "channel", []string{"uc12345", "uploads"}},
		{"arxiv", "paper", []string{"2401.12345"}},
	}
	for _, tc := range cases {
		t.Run(tc.platform+"/"+tc.entityType, func(t *testing.T) {
			key := identitykey.Build(tc.platform, tc.entityType, tc.id...)
			got := identitykey.Parse(key, false)
			if got.Platform != tc.platform {
				t.Errorf("Platform = %q, want %q", got.Platform, tc.platform)
			}
			if got.EntityType != tc.entityType {
				t.Errorf("EntityType = %q, want %q", got.EntityType, tc.entityType)
			}
			// Compare normalized parts (Build lowercases).
			wantParts := make([]string, 0, len(tc.id))
			for _, p := range tc.id {
				if p == "" {
					continue
				}
				// Normalisation here mirrors Build's normalize; tests use
				// already-lowercase fixtures so equality works directly.
				wantParts = append(wantParts, p)
			}
			if !reflect.DeepEqual(got.IDParts, wantParts) {
				t.Errorf("IDParts = %v, want %v", got.IDParts, wantParts)
			}
		})
	}
}

func TestRoundTrip_BuildLocalised(t *testing.T) {
	key := identitykey.BuildLocalised("wikipedia", "article", "en", "turing_machine")
	got := identitykey.Parse(key, true)
	if got.Platform != "wikipedia" || got.EntityType != "article" || got.Locale != "en" {
		t.Fatalf("Parse fields wrong: %+v", got)
	}
	if !reflect.DeepEqual(got.IDParts, []string{"turing_machine"}) {
		t.Errorf("IDParts = %v, want [turing_machine]", got.IDParts)
	}
	if !got.Localised {
		t.Error("Localised flag = false, want true")
	}
}

func TestGet_NilPreview(t *testing.T) {
	if got := identitykey.Get(nil); got != "" {
		t.Fatalf("Get(nil) = %q, want \"\"", got)
	}
}

func TestGet_MissingKey(t *testing.T) {
	if got := identitykey.Get(map[string]any{"other": "x"}); got != "" {
		t.Fatalf("Get(no key) = %q, want \"\"", got)
	}
}

func TestGet_NonStringValue(t *testing.T) {
	if got := identitykey.Get(map[string]any{identitykey.KeyField: 42}); got != "" {
		t.Fatalf("Get(int value) = %q, want \"\"", got)
	}
}

func TestGet_HappyPath(t *testing.T) {
	want := "github/repo/samber/lo"
	if got := identitykey.Get(map[string]any{identitykey.KeyField: want}); got != want {
		t.Fatalf("Get = %q, want %q", got, want)
	}
}

func TestSet_AllocatesNilMap(t *testing.T) {
	got := identitykey.Set(nil, "github/owner/jadb")
	if got == nil {
		t.Fatal("Set(nil, ...) returned nil")
	}
	if v := got[identitykey.KeyField]; v != "github/owner/jadb" {
		t.Fatalf("Set value = %v, want github/owner/jadb", v)
	}
}

func TestSet_OverwritesExisting(t *testing.T) {
	preview := map[string]any{identitykey.KeyField: "old"}
	identitykey.Set(preview, "new")
	if v := preview[identitykey.KeyField]; v != "new" {
		t.Fatalf("Set didn't overwrite: %v", v)
	}
}

func TestHostBackedIDParts(t *testing.T) {
	cases := []struct {
		name, url string
		want      []string
	}{
		{"plain_host", "https://example.com", []string{"example.com"}},
		{"www_stripped", "https://www.example.com", []string{"example.com"}},
		{"with_path", "https://example.com/post/1", []string{"example.com", "post", "1"}},
		{"path_lowered", "https://Example.com/POST/1", []string{"example.com", "post", "1"}},
		{"trailing_slash", "https://example.com/post/1/", []string{"example.com", "post", "1"}},
		{"unparseable_returns_nil", "::not::a::url", nil},
		{"no_host_returns_nil", "/just/a/path", nil},
		{"empty_url_returns_nil", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := identitykey.HostBackedIDParts(tc.url)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("HostBackedIDParts(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

// TestHostBackedIDParts_BuildIntegration verifies the documented usage
// pattern: spread the parts into Build's variadic id. Two URLs with the
// same host+path produce equal keys; two with different paths produce
// different keys; nil parts yield "" (Build's no-id contract).
func TestHostBackedIDParts_BuildIntegration(t *testing.T) {
	a := identitykey.HostBackedIDParts("https://example.com/post/1")
	b := identitykey.HostBackedIDParts("https://www.example.com/post/1")
	if !reflect.DeepEqual(a, b) {
		t.Errorf("same logical URL yielded different parts: %v vs %v", a, b)
	}

	keyA := identitykey.Build("google", identitykey.EntityArticle, a...)
	keyB := identitykey.Build("google", identitykey.EntityArticle, b...)
	if keyA != keyB {
		t.Errorf("dedup keys diverged: %q vs %q", keyA, keyB)
	}
	if keyA != "google/article/example.com/post/1" {
		t.Errorf("key = %q, want %q", keyA, "google/article/example.com/post/1")
	}

	c := identitykey.HostBackedIDParts("https://example.com/post/2")
	keyC := identitykey.Build("google", identitykey.EntityArticle, c...)
	if keyA == keyC {
		t.Errorf("different paths produced same key: %q", keyA)
	}

	// Nil parts → Build returns "" by its own contract.
	nilParts := identitykey.HostBackedIDParts("")
	if got := identitykey.Build("google", identitykey.EntityArticle, nilParts...); got != "" {
		t.Errorf("Build with nil HostBackedIDParts = %q, want \"\"", got)
	}
}
