package testguard

import "testing"

func withHome(t *testing.T, h string, ci bool) {
	t.Helper()
	oldH, oldCI := realHome, homeCaseInsensitive
	realHome, homeCaseInsensitive = cleanHome(h), ci
	t.Cleanup(func() { realHome, homeCaseInsensitive = oldH, oldCI })
}

// os.UserHomeDir does not clean $HOME, so a trailing slash must not turn the
// scrub off.
func TestScrubHomeTrailingSlashHome(t *testing.T) {
	withHome(t, "/Users/x/", false)
	for in, want := range map[string]string{
		"/Users/x/y":      "<home>/y",
		"/Users/x":        "<home>",
		"at /Users/x: ok": "at <home>: ok",
	} {
		if got := ScrubHome(in); got != want {
			t.Errorf("ScrubHome(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScrubHomeDegenerateHomesScrubNothing(t *testing.T) {
	for _, h := range []string{"", "/", "//"} {
		withHome(t, h, false)
		if got := ScrubHome("/a/b"); got != "/a/b" {
			t.Errorf("home %q rewrote %q", h, got)
		}
	}
}

func TestScrubHomeCaseInsensitive(t *testing.T) {
	withHome(t, "/Users/Jad", true)
	if got := ScrubHome("/users/jad/x and /USERS/JAD"); got != "<home>/x and <home>" {
		t.Errorf("case-insensitive = %q", got)
	}
	if got := ScrubHome("/users/jadb2/x"); got != "/users/jadb2/x" {
		t.Errorf("boundary must still hold: %q", got)
	}
	withHome(t, "/Users/Jad", false)
	if got := ScrubHome("/users/jad/x"); got != "/users/jad/x" {
		t.Errorf("case-sensitive rewrote %q", got)
	}
}
