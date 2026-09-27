package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/browser/launch"
)

func TestTargetURL(t *testing.T) {
	dir := t.TempDir()
	page := filepath.Join(dir, "page one.html")
	if err := os.WriteFile(page, []byte("<html></html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"http://127.0.0.1:1/x/", "https://example.com/", "file:///tmp/x.html", "about:blank"} {
		if got, err := targetURL(u); err != nil || got != u {
			t.Errorf("targetURL(%q) = %q, %v; want it unchanged", u, got, err)
		}
	}
	got, err := targetURL(page)
	if err != nil || !strings.HasPrefix(got, "file:///") || !strings.HasSuffix(got, "/page%20one.html") {
		t.Errorf("targetURL(file) = %q, %v", got, err)
	}
	if _, err := targetURL(filepath.Join(dir, "missing.html")); err == nil {
		t.Error("missing file accepted")
	}
}

func TestRun_ExitCodes(t *testing.T) {
	t.Setenv(launch.EnvChrome, "off")
	for _, tc := range []struct {
		args []string
		code int
		err  string
	}{
		{nil, 2, "usage"},
		{[]string{"bogus"}, 2, "unknown command"},
		{[]string{"dump"}, 2, "usage"},
		{[]string{"dump", "http://x/", "--lang=fr"}, 2, "after --"},
		{[]string{"dump", "--until", "(", "http://x/"}, 2, "--until"},
		{[]string{"dump", "http://x/"}, 1, "disabled"},
		{[]string{"path"}, 1, "disabled"},
		{[]string{"help"}, 0, ""},
	} {
		var out, errb bytes.Buffer
		if code := run(context.Background(), tc.args, &out, &errb); code != tc.code || !strings.Contains(errb.String(), tc.err) {
			t.Errorf("run %q = %d, stderr %q; want %d containing %q", tc.args, code, errb.String(), tc.code, tc.err)
		}
	}
}
