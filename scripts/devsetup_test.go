//go:build unix

// Package scripts holds tests for the repo's shell scripts.
package scripts

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestDevSetupWritesAtRepoRoot runs scripts/dev-setup.sh inside a
// throwaway repo, from an unrelated working directory, and checks that
// .env and data/ land at the repo root and that hooks are wired through
// the canonical installer (make install-hooks -> core.hooksPath), both in
// a plain clone and in a linked worktree where .git is a file.
//
// Every tool the script could fetch or install is stubbed on PATH; HOME,
// XDG dirs and git's global/system config point into the temp dir.
func TestDevSetupWritesAtRepoRoot(t *testing.T) {
	for _, tool := range []string{"bash", "git", "make"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}

	t.Run("clone", func(t *testing.T) {
		h := newHarness(t)
		repo := h.newRepo("clone")
		h.runDevSetup(repo)
		h.assertRootLayout(repo)
	})

	t.Run("linked worktree", func(t *testing.T) {
		h := newHarness(t)
		repo := h.newRepo("hub")
		wt := filepath.Join(h.root, "wt")
		h.git(repo, "worktree", "add", "-q", "-b", "wt", wt)
		if fi, err := os.Lstat(filepath.Join(wt, ".git")); err != nil || fi.IsDir() {
			t.Fatalf("worktree .git should be a file: %v", err)
		}
		h.runDevSetup(wt)
		h.assertRootLayout(wt)
	})
}

type harness struct {
	t    *testing.T
	root string
	env  []string
	log  string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	stubs := filepath.Join(root, "stubs")
	log := filepath.Join(root, "stub.log")

	// Tools dev-setup probes or installs. Present stubs make every
	// "already installed" branch fire; network tools fail loudly.
	stub := map[string]string{
		"go": `case "$1" in
  version) echo "go version go1.26.1 stub/stub" ;;
  env) echo "$STUB_ROOT/gopath" ;;
  *) exit 0 ;;
esac`,
		"golangci-lint": `echo "golangci-lint stub"`,
		"task":          `echo "task stub"`,
		"govulncheck":   `exit 0`,
		"migrate":       `exit 0`,
		"pre-commit":    `exit 0`,
		"curl":          `exit 97`,
		"wget":          `exit 97`,
		"brew":          `exit 97`,
	}
	for name, body := range stub {
		script := "#!/bin/sh\necho \"" + name + " $*\" >> \"$STUB_LOG\"\n" + body + "\n"
		writeFile(t, filepath.Join(stubs, name), script, 0o755)
	}

	pathDirs := []string{stubs}
	for _, tool := range []string{"git", "make", "bash"} {
		p, _ := exec.LookPath(tool)
		pathDirs = append(pathDirs, filepath.Dir(p))
	}
	pathDirs = append(pathDirs, "/usr/bin", "/bin")

	env := []string{
		"PATH=" + strings.Join(pathDirs, ":"),
		"HOME=" + filepath.Join(root, "home"),
		"XDG_CONFIG_HOME=" + filepath.Join(root, "config"),
		"XDG_DATA_HOME=" + filepath.Join(root, "data"),
		"XDG_STATE_HOME=" + filepath.Join(root, "state"),
		"XDG_CACHE_HOME=" + filepath.Join(root, "cache"),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"LANG=C",
		"TERM=dumb",
		"STUB_LOG=" + log,
		"STUB_ROOT=" + root,
	}
	for _, d := range []string{"home", "config", "data", "state", "cache"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return &harness{t: t, root: root, env: env, log: log}
}

// newRepo builds a committed repo holding the real dev-setup.sh,
// Makefile and .githooks from this checkout, plus a minimal go.mod.
func (h *harness) newRepo(name string) string {
	h.t.Helper()
	src, err := filepath.Abs("..")
	if err != nil {
		h.t.Fatal(err)
	}
	repo := filepath.Join(h.root, name)
	copyFile(h.t, filepath.Join(src, "scripts", "dev-setup.sh"), filepath.Join(repo, "scripts", "dev-setup.sh"))
	copyFile(h.t, filepath.Join(src, "Makefile"), filepath.Join(repo, "Makefile"))
	hooks, err := filepath.Glob(filepath.Join(src, ".githooks", "*"))
	if err != nil || len(hooks) == 0 {
		h.t.Fatalf("no .githooks in checkout: %v", err)
	}
	for _, hk := range hooks {
		copyFile(h.t, hk, filepath.Join(repo, ".githooks", filepath.Base(hk)))
	}
	writeFile(h.t, filepath.Join(repo, "go.mod"), "module example.com/devsetup\n\ngo 1.21\n", 0o644)

	h.git(repo, "init", "-q", "-b", "main")
	h.git(repo, "add", "-A")
	h.git(repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "fixture")
	return repo
}

func (h *harness) runDevSetup(repo string) {
	h.t.Helper()
	cmd := exec.Command("bash", filepath.Join(repo, "scripts", "dev-setup.sh"))
	cmd.Dir = h.root // deliberately not the repo
	cmd.Env = h.env
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		h.t.Fatalf("dev-setup.sh failed: %v\n%s", err, out.String())
	}
	if b, _ := os.ReadFile(h.log); bytes.Contains(b, []byte("curl ")) ||
		bytes.Contains(b, []byte("wget ")) || bytes.Contains(b, []byte("brew ")) {
		h.t.Errorf("dev-setup.sh reached for the network:\n%s", b)
	}
}

func (h *harness) assertRootLayout(repo string) {
	h.t.Helper()
	for _, p := range []string{".env", "data/migrations", "data/sqlite"} {
		if _, err := os.Stat(filepath.Join(repo, p)); err != nil {
			h.t.Errorf("%s missing at repo root: %v", p, err)
		}
	}
	for _, p := range []string{"scripts/.env", "scripts/data", "scripts/.git", "scripts/cmd"} {
		if _, err := os.Lstat(filepath.Join(repo, p)); err == nil {
			h.t.Errorf("%s written under scripts/", p)
		}
	}

	// Hooks: dev-setup defers to make install-hooks, so git resolves its
	// hooks dir to the tracked .githooks, worktree or not.
	if got := h.git(repo, "rev-parse", "--git-path", "hooks"); got != ".githooks" {
		h.t.Errorf("git hooks path = %q, want .githooks", got)
	}
	fi, err := os.Stat(filepath.Join(repo, ".githooks", "pre-commit"))
	if err != nil || fi.Mode()&0o111 == 0 {
		h.t.Errorf(".githooks/pre-commit not executable: %v", err)
	}
	// No ad-hoc hook in the legacy .git/hooks dir.
	legacy := filepath.Join(h.git(repo, "rev-parse", "--path-format=absolute", "--git-common-dir"), "hooks", "pre-commit")
	if _, err := os.Stat(legacy); err == nil {
		h.t.Errorf("dev-setup wrote its own hook at %s", legacy)
	}
}

func (h *harness) git(dir string, args ...string) string {
	h.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = h.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		h.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(from)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, to, string(b), fi.Mode().Perm())
}

func writeFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}
