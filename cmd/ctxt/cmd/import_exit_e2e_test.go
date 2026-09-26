package cmd

import (
	"net/http"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// The feed and import commands on the built binary: with no --server they
// reach the configured server.url (here the guard's closed port, or a -c
// overlay) and never a built-in default; nothing answering is
// PREREQUISITE (70), a server error is GENERIC (1).
func TestE2EFeedImportServerExitCodes(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	bookmarks := writeTempBookmarks(t, chromeBookmarksFixture)
	ok := newRecordedServer(t, fakeDpkmsAPI())
	broken := newRecordedServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))

	cases := []struct {
		name string
		args []string
		want int
	}{
		{"feed list, configured server down", []string{"feed", "list"}, 70},
		{"feed list, --server down", []string{"feed", "list", "--server", testguard.ClosedServerURL}, 70},
		{"feed list, server error", []string{"feed", "list", "--server", broken.URL}, 1},
		{"feed list, -c server.url", []string{"-c", "server.url=" + ok.URL, "feed", "list"}, 0},
		{"import chrome, configured server down", []string{"import", "chrome", "--file", bookmarks}, 70},
		{"import chrome, -c server.url", []string{"-c", "server.url=" + ok.URL, "import", "chrome", "--file", bookmarks}, 0},
		{"import chrome, --server", []string{"import", "chrome", "--file", bookmarks, "--server", ok.URL}, 0},
		{"import batch status, configured server down", []string{"import", "batch", "status", "batch_1"}, 70},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, out := runBuiltCtxt(t, tc.args...); got != tc.want {
				t.Fatalf("ctxt %v: exit %d, want %d\n%s", tc.args, got, tc.want, out)
			}
		})
	}
	if len(ok.hits()) == 0 {
		t.Fatal("the -c / --server runs never reached the test server")
	}
}

// A -c server.token overlay reaches the configured server as the bearer
// token, on the built binary.
func TestE2EImportSendsConfiguredToken(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	srv := newRecordedServer(t, fakeDpkmsAPI())
	args := []string{
		"-c", "server.url=" + srv.URL, "-c", "server.token=tok-e2e",
		"import", "chrome", "--file", writeTempBookmarks(t, chromeBookmarksFixture),
	}
	if got, out := runBuiltCtxt(t, args...); got != 0 {
		t.Fatalf("ctxt %v: exit %d, want 0\n%s", args, got, out)
	}
	assertAllAuthorized(t, srv.hits(), "Bearer tok-e2e")
}
