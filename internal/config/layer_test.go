package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const layerFixture = `# top comment
server:
  url: http://127.0.0.1:1 # trailing

# profiles below
profile:
  default: work
  profiles:
    work:
      description: 'Work stuff'
      tags: [a, b]

registries:
  - name: one
    url: https://one
  - name: two # keep
    url: https://two
notes: |
  first

  third
# foot comment
`

func writeLayerFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ctxt.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func replaceOnce(t *testing.T, s, old, repl string) string {
	t.Helper()
	if n := strings.Count(s, old); n != 1 {
		t.Fatalf("%q occurs %d times; want 1", old, n)
	}
	return strings.Replace(s, old, repl, 1)
}

func TestEditLayerSetScalarKeepsEveryOtherByte(t *testing.T) {
	path := writeLayerFixture(t, layerFixture)
	err := EditLayer(path, func(l *Layer) error {
		return l.Set("home", "profile", "default")
	})
	if err != nil {
		t.Fatal(err)
	}
	want := replaceOnce(t, layerFixture, "  default: work\n", "  default: home\n")
	if got := readString(t, path); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditLayerSetCreatesNestedKeyInPlace(t *testing.T) {
	path := writeLayerFixture(t, layerFixture)
	err := EditLayer(path, func(l *Layer) error {
		return l.Set("New one", "profile", "profiles", "home", "description")
	})
	if err != nil {
		t.Fatal(err)
	}
	want := replaceOnce(t, layerFixture, "      tags: [a, b]\n",
		"      tags: [a, b]\n    home:\n      description: New one\n")
	if got := readString(t, path); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditLayerSetKeepsQuotingAndFlowStyle(t *testing.T) {
	path := writeLayerFixture(t, layerFixture)
	err := EditLayer(path, func(l *Layer) error {
		if err := l.Set("Other", "profile", "profiles", "work", "description"); err != nil {
			return err
		}
		return l.Set([]string{"a", "b", "c"}, "profile", "profiles", "work", "tags")
	})
	if err != nil {
		t.Fatal(err)
	}
	want := replaceOnce(t, layerFixture, "'Work stuff'", "'Other'")
	want = replaceOnce(t, want, "[a, b]", "[a, b, c]")
	if got := readString(t, path); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditLayerDeleteAndRemoveItems(t *testing.T) {
	path := writeLayerFixture(t, layerFixture)
	err := EditLayer(path, func(l *Layer) error {
		if !l.Delete("profile", "profiles", "work") {
			return errors.New("work not deleted")
		}
		n, err := l.RemoveItems(func(item *yaml.Node) (bool, error) {
			var r struct{ Name string }
			err := item.Decode(&r)
			return r.Name == "two", err
		}, "registries")
		if n != 1 {
			return errors.New("registry two not removed")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	want := replaceOnce(t, layerFixture,
		"  profiles:\n    work:\n      description: 'Work stuff'\n      tags: [a, b]\n",
		"  profiles: {}\n")
	want = replaceOnce(t, want, "  - name: two # keep\n    url: https://two\n", "")
	if got := readString(t, path); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditLayerNoChangeLeavesFileUntouched(t *testing.T) {
	body := "server:\n    url:   http://x   # odd spacing\n"
	path := writeLayerFixture(t, body)
	err := EditLayer(path, func(l *Layer) error {
		return l.Set("http://x", "server", "url")
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := readString(t, path); got != body {
		t.Errorf("no-op edit rewrote the file:\n%s", got)
	}
}

func TestEditLayerErrorLeavesFileUntouched(t *testing.T) {
	path := writeLayerFixture(t, layerFixture)
	boom := errors.New("boom")
	err := EditLayer(path, func(l *Layer) error {
		_ = l.Set("home", "profile", "default")
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v; want boom", err)
	}
	if got := readString(t, path); got != layerFixture {
		t.Errorf("failed edit rewrote the file:\n%s", got)
	}
}

func TestEditLayerCreatesMissingFileWithOnlyTheKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "ctxt.yaml")
	err := EditLayer(path, func(l *Layer) error {
		return l.Set(true, "watch", "clipboard", "enabled")
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := readString(t, path), "watch:\n  clipboard:\n    enabled: true\n"; got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o; want 600", perm)
	}
}

func TestEditLayerKeepsFourSpaceIndent(t *testing.T) {
	body := "watch:\n    clipboard:\n        enabled: false\n"
	path := writeLayerFixture(t, body)
	err := EditLayer(path, func(l *Layer) error {
		return l.Set(true, "watch", "clipboard", "enabled")
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := readString(t, path), "watch:\n    clipboard:\n        enabled: true\n"; got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditLayerKeysWithDotsAreOneSegment(t *testing.T) {
	path := writeLayerFixture(t, "profile:\n  profiles: {}\n")
	err := EditLayer(path, func(l *Layer) error {
		return l.Set("dotted", "profile", "profiles", "a.b", "description")
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "profile:\n  profiles:\n    a.b:\n      description: dotted\n"
	if got := readString(t, path); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditLayerFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.yaml")
	if err := os.WriteFile(real, []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.yaml")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := EditLayer(link, func(l *Layer) error { return l.Set(2, "a") }); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink replaced by a regular file (err=%v)", err)
	}
	if got := readString(t, real); got != "a: 2\n" {
		t.Errorf("real file = %q; want %q", got, "a: 2\n")
	}
}

func TestEditLayerRefusesToClobberNonMapping(t *testing.T) {
	path := writeLayerFixture(t, "watch: [x]\n")
	err := EditLayer(path, func(l *Layer) error {
		return l.Set(true, "watch", "clipboard", "enabled")
	})
	if err == nil {
		t.Fatal("Set through a sequence succeeded; want an error")
	}
	if got := readString(t, path); got != "watch: [x]\n" {
		t.Errorf("file changed: %q", got)
	}
}

func TestLayerReadsOnlyItsOwnFile(t *testing.T) {
	t.Setenv("CH_SERVER_PORT", "9999")
	path := writeLayerFixture(t, "profile:\n  default: work\n")
	l, err := OpenLayer(path)
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if ok, err := l.Decode(&port, "server", "port"); ok || err != nil {
		t.Errorf("Decode(server.port) = %v, %v; the layer must not see env or defaults", ok, err)
	}
	var def string
	if ok, err := l.Decode(&def, "profile", "default"); !ok || err != nil || def != "work" {
		t.Errorf("Decode(profile.default) = %q, %v, %v", def, ok, err)
	}
	if got := l.Keys("profile"); len(got) != 1 || got[0] != "default" {
		t.Errorf("Keys(profile) = %v", got)
	}
}
