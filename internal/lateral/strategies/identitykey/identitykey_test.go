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
		{"all_empty", []string{"", " ", ""}, "github/repo"},
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

func TestHostBackedID(t *testing.T) {
	cases := []struct {
		name, url, want string
	}{
		{"plain_host", "https://example.com", "example.com"},
		{"www_stripped", "https://www.example.com", "example.com"},
		{"with_path", "https://example.com/post/1", "example.com/post/1"},
		{"path_lowered", "https://Example.com/POST/1", "example.com/post/1"},
		{"unparseable_returns_empty", "::not::a::url", ""},
		{"no_host_returns_empty", "/just/a/path", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := identitykey.HostBackedID(tc.url); got != tc.want {
				t.Errorf("HostBackedID(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

// TestHostBackedID_EmptyResultPropagatesNonUniqueKey documents why
// callers MUST guard. Copilot flagged Search/News/Trends emitting
// "q|" or similar non-unique keys when result URLs failed HostBackedID.
// This test pins the empty-string return so callers know what to guard
// against.
func TestHostBackedID_EmptyResultPropagatesNonUniqueKey(t *testing.T) {
	if got := identitykey.HostBackedID(""); got != "" {
		t.Fatalf("HostBackedID(\"\") = %q, want \"\"", got)
	}
	// Demonstrate the trap: Build with empty id segments would drop them,
	// yielding only "<platform>/<entity>" — non-unique across captures.
	emptyID := identitykey.HostBackedID("")
	key := identitykey.Build("google", "search", "query-text", emptyID)
	// Empty segments dropped, so key looks fine here. But if BOTH segments
	// were empty (caller passed unguarded HostBackedID twice), the key
	// would just be "google/search" — non-unique. Callers must check for
	// "" return and skip the candidate.
	if key != "google/search/query-text" {
		t.Fatalf("Build dropped empty id segment; got %q", key)
	}
}
