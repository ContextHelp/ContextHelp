package customdomain

import "testing"

func TestDetect_CanonicalHostSuffixes(t *testing.T) {
	cases := []struct {
		url  string
		want Platform
	}{
		{"https://medium.com/@author/post-slug-abc123", PlatformMedium},
		{"https://blog.medium.com/announcement", PlatformMedium},
		{"https://author.substack.com/p/my-post", PlatformSubstack},
		{"https://substack.com/profile/123-name", PlatformSubstack},
		{"https://newsletter.beehiiv.com/p/post", PlatformBeehiiv},
		{"https://example.com/post", PlatformUnknown},
		{"not-a-url", PlatformUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			got := Detect(tc.url, Hints{})
			if got.Platform != tc.want {
				t.Fatalf("Detect(%q) platform = %q, want %q", tc.url, got.Platform, tc.want)
			}
			if got.Platform != PlatformUnknown && got.CustomHost {
				t.Fatalf("canonical match should not set CustomHost")
			}
		})
	}
}

func TestDetect_HintsResolveCustomDomain(t *testing.T) {
	cases := []struct {
		name  string
		url   string
		hints Hints
		want  Platform
	}{
		{"meta_platform_substack", "https://news.example.com/post", Hints{MetaPlatform: "Substack"}, PlatformSubstack},
		{"generator_beehiiv", "https://news.example.com/post", Hints{Generator: "beehiiv"}, PlatformBeehiiv},
		{"generator_medium_caps", "https://blog.example.com/p", Hints{Generator: "Medium 2.0"}, PlatformMedium},
		{"canonical_substack", "https://custom.example.com/p/post", Hints{CanonicalHost: "author.substack.com"}, PlatformSubstack},
		{"canonical_beehiiv", "https://custom.example.com/p/post", Hints{CanonicalHost: "name.beehiiv.com"}, PlatformBeehiiv},
		{"no_signal", "https://random.example.com/post", Hints{}, PlatformUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Detect(tc.url, tc.hints)
			if got.Platform != tc.want {
				t.Fatalf("got %q, want %q", got.Platform, tc.want)
			}
			if got.Platform != PlatformUnknown && !got.CustomHost {
				t.Fatalf("hint match should set CustomHost=true")
			}
		})
	}
}

func TestDetect_MetaPlatformPreferredOverGenerator(t *testing.T) {
	got := Detect("https://x.example.com/p", Hints{MetaPlatform: "substack", Generator: "beehiiv"})
	if got.Platform != PlatformSubstack {
		t.Fatalf("MetaPlatform should win, got %q", got.Platform)
	}
}

func TestIsHostedOn(t *testing.T) {
	if !IsHostedOn("foo.substack.com", PlatformSubstack) {
		t.Fatal("substack subdomain should match")
	}
	if !IsHostedOn("Medium.COM", PlatformMedium) {
		t.Fatal("apex match must be case-insensitive")
	}
	if IsHostedOn("example.com", PlatformBeehiiv) {
		t.Fatal("non-canonical host should not match")
	}
}
