package launch

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestOpenCommand(t *testing.T) {
	const url = "http://127.0.0.1:1/t/"
	cases := map[string][]string{
		"darwin":    {"open", url},
		"linux":     {"xdg-open", url},
		"freebsd":   {"xdg-open", url},
		"openbsd":   {"xdg-open", url},
		"netbsd":    {"xdg-open", url},
		"dragonfly": {"xdg-open", url},
		"solaris":   {"xdg-open", url},
		"illumos":   {"xdg-open", url},
		"windows":   {"rundll32", "url.dll,FileProtocolHandler", url},
	}
	for goos, want := range cases {
		name, args, ok := OpenCommand(goos, url)
		if got := append([]string{name}, args...); !ok || !slices.Equal(got, want) {
			t.Errorf("%s: %q (ok=%v), want %q", goos, got, ok, want)
		}
	}
	for _, goos := range []string{"plan9", "js", "wasip1", ""} {
		if _, _, ok := OpenCommand(goos, url); ok {
			t.Errorf("%q should have no handler", goos)
		}
	}
}

func TestOpen_RefusesSwitchLikeURL(t *testing.T) {
	for _, url := range []string{"", "-a", "--new-window"} {
		if err := Open(context.Background(), url); err == nil {
			t.Errorf("Open(%q) = nil, want an error", url)
		}
	}
}

// Open execs the platform handler from PATH with the URL as its only
// argument; a stub on PATH records what it received.
func TestSystemOpen_ExecsPlatformHandler(t *testing.T) {
	const url = "http://127.0.0.1:1/token/"
	name, wantArgs, ok := OpenCommand(runtime.GOOS, url)
	if !ok || runtime.GOOS == "windows" {
		t.Skipf("no stub-able handler on %s", runtime.GOOS)
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "argv")
	stub := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + log + ".tmp && /bin/mv " + log + ".tmp " + log + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(stub), 0o700); err != nil { //nolint:gosec // test-only executable stub
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	if err := (System{}).Open(context.Background(), url); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, err := os.ReadFile(log)
		if err == nil {
			if got := strings.Fields(string(b)); !slices.Equal(got, wantArgs) {
				t.Errorf("%s argv = %q, want %q", name, got, wantArgs)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s stub never ran", name)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
