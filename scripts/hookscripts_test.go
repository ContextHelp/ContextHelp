//go:build unix

package scripts

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// hookScriptFiles are the extra checkout files the hook scripts need.
var hookScriptFiles = []string{
	"scripts/install-hooks.sh",
	"scripts/test-hooks.sh",
	".gitleaks.toml",
}

// gitleaksStub stands in for gitleaks: it fails when the staged diff
// adds a key-shaped string, as the real scanner would.
const gitleaksStub = `[ "$1" = version ] && { echo "8.0.0-stub"; exit 0; }
git diff --cached -U0 | grep -E '^\+' |
  grep -qE 'sk-proj-[A-Za-z0-9]{32}|sk-ant-[A-Za-z0-9-]{90}|AKIA[0-9A-Z]{16}|gh[pousr]_[A-Za-z0-9]{30}|glpat-[A-Za-z0-9_-]{20}' && exit 1
exit 0`

// TestInstallHooksScript runs scripts/install-hooks.sh from an unrelated
// cwd, in a clone and in a linked worktree (.git is a file there), and
// checks it wires the tracked .githooks dir via core.hooksPath rather
// than writing into .git/hooks.
func TestInstallHooksScript(t *testing.T) {
	requireTools(t)
	for name, setup := range repoLayouts() {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			repo := setup(h)
			out, err := h.runScript(repo, "scripts/install-hooks.sh")
			if err != nil {
				t.Fatalf("install-hooks.sh failed: %v\n%s", err, out)
			}
			h.assertHooksWired(repo)
		})
	}
}

// TestTestHooksScript runs scripts/test-hooks.sh after installing hooks
// and checks the scenarios run against the installed .githooks
// pre-commit without touching the checkout: index, worktree and an
// existing untracked .env all survive.
func TestTestHooksScript(t *testing.T) {
	requireTools(t)
	for name, setup := range repoLayouts() {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.stub("gitleaks", gitleaksStub)
			repo := setup(h)
			if out, err := h.runScript(repo, "scripts/install-hooks.sh"); err != nil {
				t.Fatalf("install-hooks.sh failed: %v\n%s", err, out)
			}
			const envBody = "OPENAI_API_KEY=keep-me\n"
			writeFile(t, filepath.Join(repo, ".env"), envBody, 0o600)
			before := h.git(repo, "status", "--porcelain", "--untracked-files=all")

			out, err := h.runScript(repo, "scripts/test-hooks.sh")
			if err != nil {
				t.Fatalf("test-hooks.sh failed: %v\n%s", err, out)
			}
			if b, _ := os.ReadFile(h.log); !bytes.Contains(b, []byte("gitleaks git --staged")) {
				t.Errorf("installed pre-commit hook never ran the scan; stub log:\n%s", b)
			}
			if got := h.git(repo, "status", "--porcelain", "--untracked-files=all"); got != before {
				t.Errorf("test-hooks.sh changed the checkout:\nbefore:\n%s\nafter:\n%s", before, got)
			}
			if b, err := os.ReadFile(filepath.Join(repo, ".env")); err != nil || string(b) != envBody {
				t.Errorf(".env clobbered: %q, %v", b, err)
			}
		})
	}
}

// TestTestHooksScriptRequiresInstall checks test-hooks.sh refuses to run
// when core.hooksPath does not point at .githooks.
func TestTestHooksScriptRequiresInstall(t *testing.T) {
	requireTools(t)
	h := newHarness(t)
	h.stub("gitleaks", gitleaksStub)
	repo := h.newRepo("clone", hookScriptFiles...)
	out, err := h.runScript(repo, "scripts/test-hooks.sh")
	if err == nil {
		t.Fatalf("test-hooks.sh passed without installed hooks:\n%s", out)
	}
	if !strings.Contains(out, "make install-hooks") {
		t.Errorf("want a pointer to make install-hooks, got:\n%s", out)
	}
}

func requireTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"bash", "git", "make"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
}

func repoLayouts() map[string]func(h *harness) string {
	return map[string]func(h *harness) string{
		"clone": func(h *harness) string { return h.newRepo("clone", hookScriptFiles...) },
		"linked worktree": func(h *harness) string {
			hub := h.newRepo("hub", hookScriptFiles...)
			wt := filepath.Join(h.root, "wt")
			h.git(hub, "worktree", "add", "-q", "-b", "wt", wt)
			if fi, err := os.Lstat(filepath.Join(wt, ".git")); err != nil || fi.IsDir() {
				h.t.Fatalf("worktree .git should be a file: %v", err)
			}
			return wt
		},
	}
}

// stub adds (or replaces) a logging stub on the harness PATH.
func (h *harness) stub(name, body string) {
	h.t.Helper()
	script := "#!/bin/sh\necho \"" + name + " $*\" >> \"$STUB_LOG\"\n" + body + "\n"
	writeFile(h.t, filepath.Join(h.root, "stubs", name), script, 0o755)
}

// runScript runs a repo-relative script from an unrelated cwd.
func (h *harness) runScript(repo, rel string) (string, error) {
	h.t.Helper()
	cmd := exec.Command("bash", filepath.Join(repo, rel))
	cmd.Dir = h.root
	cmd.Env = h.env
	cmd.Stdin = strings.NewReader("")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

// assertHooksWired checks git resolves hooks to the tracked .githooks
// and nothing was written into the legacy hooks dir.
func (h *harness) assertHooksWired(repo string) {
	h.t.Helper()
	if got := h.git(repo, "rev-parse", "--git-path", "hooks"); got != ".githooks" {
		h.t.Errorf("git hooks path = %q, want .githooks", got)
	}
	legacy := filepath.Join(h.git(repo, "rev-parse", "--path-format=absolute", "--git-common-dir"), "hooks", "pre-commit")
	if _, err := os.Stat(legacy); err == nil {
		h.t.Errorf("hook written into legacy dir %s", legacy)
	}
}
