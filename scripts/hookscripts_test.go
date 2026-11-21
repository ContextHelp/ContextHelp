//go:build unix

package scripts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

const zeroSHA = "0000000000000000000000000000000000000000"

// prePushRun is the outcome of one pre-push hook invocation.
type prePushRun struct {
	out   string // hook output plus the stub log
	gin   string // stdin the chained global hook received ("" if it never ran)
	err   error  // hook exit error
	repo  string
	runs  []string // directories make ran in, in order
	h     *harness
	stdin string
}

// prePushEnv is a fixture repo with stub make, mise and global hook, in
// which the hook can run more than once.
type prePushEnv struct {
	t         *testing.T
	h         *harness
	repo      string
	gitconfig string
	stdinLog  string
}

func newPrePushEnv(t *testing.T, makeBody string, setup func(h *harness, repo string)) *prePushEnv {
	t.Helper()
	h := newHarness(t)
	repo := h.newRepo("clone", append([]string{"mise.toml"}, hookScriptFiles...)...)
	h.stub("make", "echo \"make-cwd $(pwd -P)\" >> \"$STUB_LOG\"\n"+makeBody)
	// mise trust --show (run in the checkout): "<dir>: trusted|untrusted"
	h.stub("mise", `case "$MISE_STUB" in untrusted) echo "$PWD: untrusted" ;; *) echo "$PWD: trusted" ;; esac`)
	if setup != nil {
		setup(h, repo)
	}
	globalDir := filepath.Join(h.root, "global-hooks")
	e := &prePushEnv{t: t, h: h, repo: repo, gitconfig: filepath.Join(h.root, "gitconfig"), stdinLog: filepath.Join(h.root, "global-stdin.log")}
	writeFile(t, filepath.Join(globalDir, "pre-push"),
		"#!/bin/sh\necho \"global $*\" >> \"$STUB_LOG\"\ncat > \""+e.stdinLog+"\"\nexit ${GLOBAL_EXIT:-0}\n", 0o755)
	writeFile(t, e.gitconfig, "[core]\n\thooksPath = "+globalDir+"\n", 0o644)
	return e
}

// run invokes the hook once; refs builds its stdin from the repo (so tests
// can use real shas).
func (e *prePushEnv) run(refs func(h *harness, repo string) string, env ...string) prePushRun {
	e.t.Helper()
	_ = os.Remove(e.h.log)
	_ = os.Remove(e.stdinLog)
	stdin := refs(e.h, e.repo)
	cmd := exec.Command("sh", filepath.Join(e.repo, ".githooks", "pre-push"), "origin", "git@example.com:x/y.git")
	cmd.Dir = e.repo
	cmd.Env = append(append([]string{}, e.h.env...), append([]string{"GIT_CONFIG_GLOBAL=" + e.gitconfig}, env...)...)
	cmd.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	got, _ := os.ReadFile(e.stdinLog)
	logb, _ := os.ReadFile(e.h.log)
	var runs []string
	for _, l := range strings.Split(string(logb), "\n") {
		if d, ok := strings.CutPrefix(l, "make-cwd "); ok {
			runs = append(runs, d)
		}
	}
	return prePushRun{out: out.String() + "\n--stub log--\n" + string(logb), gin: string(got), err: err, repo: e.repo, runs: runs, h: e.h, stdin: stdin}
}

// lintWorktreeOf is the hook-owned checkout: <cache>/ctxt-pre-push/<key>/lint-worktree,
// where cache is the harness XDG_CACHE_HOME and key the first 12 hex of the
// sha256 of the git common dir.
func lintWorktreeOf(h *harness, repo string) string {
	return lintWorktreeIn(h.root, h, repo)
}

// lintWorktreeIn is lintWorktreeOf with the cache home taken from another
// harness root (to share one cache between fixtures).
func lintWorktreeIn(cacheRoot string, h *harness, repo string) string {
	common := h.git(repo, "rev-parse", "--path-format=absolute", "--git-common-dir")
	sum := sha256.Sum256([]byte(common))
	root, _ := filepath.EvalSymlinks(cacheRoot)
	return filepath.Join(root, "cache", "ctxt-pre-push", hex.EncodeToString(sum[:])[:12], "lint-worktree")
}

// runPrePush runs the hook once in a fresh fixture. setup may dirty the
// tree or stage state first.
func runPrePush(t *testing.T, makeBody string, setup func(h *harness, repo string), refs func(h *harness, repo string) string, env ...string) prePushRun {
	t.Helper()
	return newPrePushEnv(t, makeBody, setup).run(refs, env...)
}

func pushLine(ref, sha string) string {
	return "refs/heads/" + ref + " " + sha + " refs/heads/" + ref + " " + zeroSHA + "\n"
}

func headRefs(h *harness, repo string) string {
	return pushLine("a", h.git(repo, "rev-parse", "HEAD"))
}

// otherCommit makes a commit on a side branch and returns its sha, leaving
// HEAD on main.
func otherCommit(h *harness, repo string) string {
	h.git(repo, "checkout", "-q", "-b", "side")
	writeFile(h.t, filepath.Join(repo, "side.txt"), "side\n", 0o644)
	h.git(repo, "add", "side.txt")
	h.git(repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "side")
	sha := h.git(repo, "rev-parse", "HEAD")
	h.git(repo, "checkout", "-q", "main")
	return sha
}

func (r prePushRun) inPlace() bool {
	if len(r.runs) != 1 {
		return false
	}
	want, _ := filepath.EvalSymlinks(r.repo)
	return r.runs[0] == want
}

// assertLintWorktree checks every checkout-based run happened in the one
// stable lint worktree, which is kept (so mise trust persists) and whose
// lock was released.
func (r prePushRun) assertLintWorktree(t *testing.T) {
	t.Helper()
	lw := lintWorktreeOf(r.h, r.repo)
	repo, _ := filepath.EvalSymlinks(r.repo)
	n := 0
	for _, d := range r.runs {
		if d == repo {
			continue
		}
		n++
		if d != lw {
			t.Errorf("lint ran in %s, want the stable lint worktree %s", d, lw)
		}
	}
	if n == 0 {
		t.Errorf("no run in the lint worktree: %v\n%s", r.runs, r.out)
	}
	if wt := r.h.git(r.repo, "worktree", "list", "--porcelain"); strings.Count(wt, "worktree ") != 2 {
		t.Errorf("want main + one lint worktree registered:\n%s", wt)
	}
	if _, err := os.Stat(filepath.Join(lw, ".git")); err != nil {
		t.Errorf("lint worktree not kept: %v", err)
	}
	common, _ := filepath.EvalSymlinks(r.h.git(r.repo, "rev-parse", "--path-format=absolute", "--git-common-dir"))
	if strings.HasPrefix(lw, common) {
		t.Errorf("lint worktree %s is inside the git dir %s", lw, common)
	}
	if _, err := os.Stat(lw + ".lock"); err == nil {
		t.Errorf("lock not released")
	}
}

// prePushDeleteRefs is the stdin of a push that only deletes a ref.
func prePushDeleteRefs(h *harness, repo string) string {
	return "(delete) " + zeroSHA + " refs/heads/a " + h.git(repo, "rev-parse", "HEAD") + "\n"
}

// dirtyTracked modifies a tracked file in the fixture's working tree.
func dirtyTracked(h *harness, repo string) {
	writeFile(h.t, filepath.Join(repo, "Makefile"), "# dirty\n", 0o644)
}

// TestPrePushHeadCleanLintsInPlace: HEAD with clean tree lints in place and chains refs.
func TestPrePushHeadCleanLintsInPlace(t *testing.T) {
	requireTools(t)
	r := runPrePush(t, "exit 0", nil, headRefs)
	if r.err != nil {
		t.Fatalf("hook failed: %v\n%s", r.err, r.out)
	}
	if r.gin != r.stdin {
		t.Errorf("global hook stdin = %q, want %q", r.gin, r.stdin)
	}
	if !strings.Contains(r.out, "global origin git@example.com:x/y.git") {
		t.Errorf("global hook did not get args:\n%s", r.out)
	}
	if !r.inPlace() {
		t.Errorf("want one in-place run, got %v\n%s", r.runs, r.out)
	}
	if !strings.Contains(r.out, "make ci-lint ci-security") {
		t.Errorf("make ci-lint ci-security not run:\n%s", r.out)
	}
}

// TestPrePushUntrackedFilesStayInPlace: untracked files do not force the lint worktree.
func TestPrePushUntrackedFilesStayInPlace(t *testing.T) {
	requireTools(t)
	r := runPrePush(t, "exit 0", func(h *harness, repo string) {
		writeFile(h.t, filepath.Join(repo, "scratch.txt"), "x\n", 0o644)
	}, headRefs)
	if r.err != nil || !r.inPlace() {
		t.Errorf("want in-place pass, err=%v runs=%v\n%s", r.err, r.runs, r.out)
	}
}

// TestPrePushDirtyTreeUsesLintWorktree: dirty tree lints the sha in the lint worktree.
func TestPrePushDirtyTreeUsesLintWorktree(t *testing.T) {
	requireTools(t)
	r := runPrePush(t, "exit 0", dirtyTracked, headRefs)
	if r.err != nil {
		t.Fatalf("hook failed: %v\n%s", r.err, r.out)
	}
	if len(r.runs) != 1 || r.inPlace() {
		t.Fatalf("want one temp-worktree run, got %v\n%s", r.runs, r.out)
	}
	r.assertLintWorktree(t)
}

// TestPrePushStagedChangeCountsAsDirty: staged change counts as dirty.
func TestPrePushStagedChangeCountsAsDirty(t *testing.T) {
	requireTools(t)
	r := runPrePush(t, "exit 0", func(h *harness, repo string) {
		dirtyTracked(h, repo)
		h.git(repo, "add", "Makefile")
	}, headRefs)
	if r.err != nil || len(r.runs) != 1 || r.inPlace() {
		t.Errorf("want temp-worktree run, err=%v runs=%v\n%s", r.err, r.runs, r.out)
	}
}

// TestPrePushNonHeadShaUsesLintWorktree: non-HEAD sha lints in the lint worktree.
func TestPrePushNonHeadShaUsesLintWorktree(t *testing.T) {
	requireTools(t)
	var side string
	r := runPrePush(t, "exit 0", nil, func(h *harness, repo string) string {
		side = otherCommit(h, repo)
		return pushLine("side", side)
	})
	if r.err != nil {
		t.Fatalf("hook failed: %v\n%s", r.err, r.out)
	}
	if len(r.runs) != 1 || r.inPlace() {
		t.Fatalf("want one temp-worktree run, got %v\n%s", r.runs, r.out)
	}
	if !strings.Contains(r.out, side) {
		t.Errorf("output does not name %s:\n%s", side, r.out)
	}
	r.assertLintWorktree(t)
}

// TestPrePushSameShaLintsOnce: two refs with the same sha lint once.
func TestPrePushSameShaLintsOnce(t *testing.T) {
	requireTools(t)
	r := runPrePush(t, "exit 0", nil, func(h *harness, repo string) string {
		sha := h.git(repo, "rev-parse", "HEAD")
		return pushLine("a", sha) + pushLine("b", sha)
	})
	if r.err != nil || len(r.runs) != 1 {
		t.Errorf("want exactly one run, err=%v runs=%v\n%s", r.err, r.runs, r.out)
	}
}

// TestPrePushDistinctShasLintEachOnce: distinct shas lint each once.
func TestPrePushDistinctShasLintEachOnce(t *testing.T) {
	requireTools(t)
	r := runPrePush(t, "exit 0", nil, func(h *harness, repo string) string {
		return pushLine("a", h.git(repo, "rev-parse", "HEAD")) + pushLine("side", otherCommit(h, repo))
	})
	if r.err != nil || len(r.runs) != 2 {
		t.Errorf("want two runs, err=%v runs=%v\n%s", r.err, r.runs, r.out)
	}
	r.assertLintWorktree(t)
}

// TestPrePushLintFailureBlocksAndNamesSha: lint failure blocks, names the sha, keeps the lint worktree.
func TestPrePushLintFailureBlocksAndNamesSha(t *testing.T) {
	requireTools(t)
	var side string
	r := runPrePush(t, "exit 1", nil, func(h *harness, repo string) string {
		side = otherCommit(h, repo)
		return pushLine("side", side)
	})
	if r.err == nil {
		t.Fatalf("hook passed despite lint failure:\n%s", r.out)
	}
	for _, want := range []string{"make ci-lint ci-security", "SKIP_LINT=1", "failed for " + side} {
		if !strings.Contains(r.out, want) {
			t.Errorf("failure output lacks %q:\n%s", want, r.out)
		}
	}
	r.assertLintWorktree(t)
}

// TestPrePushInPlaceLintFailureBlocks: in-place lint failure blocks.
func TestPrePushInPlaceLintFailureBlocks(t *testing.T) {
	requireTools(t)
	r := runPrePush(t, "exit 1", nil, headRefs)
	if r.err == nil {
		t.Fatalf("hook passed despite lint failure:\n%s", r.out)
	}
}

// TestPrePushGlobalHookFailureBlocks: global hook failure blocks and skips lint.
func TestPrePushGlobalHookFailureBlocks(t *testing.T) {
	requireTools(t)
	r := runPrePush(t, "exit 0", nil, headRefs, "GLOBAL_EXIT=3")
	if r.err == nil {
		t.Fatalf("hook passed despite global hook failure:\n%s", r.out)
	}
	if len(r.runs) != 0 {
		t.Errorf("lint ran after global hook failed:\n%s", r.out)
	}
}

// TestPrePushSkipLintBypasses: SKIP_LINT bypasses lint.
func TestPrePushSkipLintBypasses(t *testing.T) {
	requireTools(t)
	r := runPrePush(t, "exit 1", nil, headRefs, "SKIP_LINT=1")
	if r.err != nil || len(r.runs) != 0 {
		t.Errorf("SKIP_LINT=1 did not bypass: err=%v runs=%v\n%s", r.err, r.runs, r.out)
	}
}

// TestPrePushDeleteOnlyPushSkipsLint: delete-only push skips lint.
func TestPrePushDeleteOnlyPushSkipsLint(t *testing.T) {
	requireTools(t)
	r := runPrePush(t, "exit 1", nil, prePushDeleteRefs)
	if r.err != nil || len(r.runs) != 0 {
		t.Errorf("delete-only push linted/blocked: err=%v runs=%v\n%s", r.err, r.runs, r.out)
	}
	if r.gin != r.stdin {
		t.Errorf("global hook stdin = %q, want %q", r.gin, r.stdin)
	}
}

// TestPrePushLintWorktreeReusedAndCleaned: lint worktree is reused and cleaned between pushes.
func TestPrePushLintWorktreeReusedAndCleaned(t *testing.T) {
	requireTools(t)
	e := newPrePushEnv(t, "exit 0", nil)
	var first, second string
	r1 := e.run(func(h *harness, repo string) string {
		first = otherCommit(h, repo)
		return pushLine("side", first)
	})
	if r1.err != nil {
		t.Fatalf("first push failed: %v\n%s", r1.err, r1.out)
	}
	lw := lintWorktreeOf(e.h, e.repo)
	writeFile(t, filepath.Join(lw, "leftover.txt"), "x\n", 0o644)
	writeFile(t, filepath.Join(lw, "side.txt"), "edited in the lint checkout\n", 0o644)
	r2 := e.run(func(h *harness, repo string) string {
		h.git(repo, "checkout", "-q", "side")
		writeFile(h.t, filepath.Join(repo, "side2.txt"), "2\n", 0o644)
		h.git(repo, "add", "side2.txt")
		h.git(repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "side2")
		second = h.git(repo, "rev-parse", "HEAD")
		h.git(repo, "checkout", "-q", "main")
		return pushLine("side", second)
	})
	if r2.err != nil {
		t.Fatalf("second push failed: %v\n%s", r2.err, r2.out)
	}
	r2.assertLintWorktree(t)
	if got := e.h.git(lw, "rev-parse", "HEAD"); got != second {
		t.Errorf("lint worktree at %s, want %s", got, second)
	}
	if _, err := os.Stat(filepath.Join(lw, "leftover.txt")); err == nil {
		t.Errorf("untracked leftover survived the reuse")
	}
	if st := e.h.git(lw, "status", "--porcelain"); st != "" {
		t.Errorf("tracked edit survived the reuse:\n%s", st)
	}
}

// TestPrePushUntrustedMiseBlocksThenPasses: untrusted mise config blocks with the trust command, then passes once trusted.
func TestPrePushUntrustedMiseBlocksThenPasses(t *testing.T) {
	requireTools(t)
	e := newPrePushEnv(t, "exit 0", nil)
	refs := func(h *harness, repo string) string { return pushLine("side", otherCommit(h, repo)) }
	r := e.run(refs, "MISE_STUB=untrusted")
	if r.err == nil {
		t.Fatalf("hook passed with untrusted mise config:\n%s", r.out)
	}
	lw := lintWorktreeOf(e.h, e.repo)
	_ = lw
	for _, want := range []string{"  mise trust ", "/ctxt-pre-push/", "lint-worktree/mise.toml", "SKIP_LINT=1"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("output lacks %q:\n%s", want, r.out)
		}
	}
	if len(r.runs) != 0 {
		t.Errorf("lint ran despite untrusted config: %v", r.runs)
	}
	r = e.run(func(h *harness, repo string) string { return pushLine("side", h.git(repo, "rev-parse", "side")) })
	if r.err != nil || len(r.runs) != 1 {
		t.Errorf("after trust: err=%v runs=%v\n%s", r.err, r.runs, r.out)
	}
}

// TestPrePushHeldLockBlocks: held lock blocks with a clear message.
func TestPrePushHeldLockBlocks(t *testing.T) {
	requireTools(t)
	r := runPrePush(t, "exit 0", func(h *harness, repo string) {
		lock := lintWorktreeOf(h, repo) + ".lock"
		writeFile(h.t, filepath.Join(lock, "pid"), strconv.Itoa(os.Getpid())+"\n", 0o644)
	}, func(h *harness, repo string) string { return pushLine("side", otherCommit(h, repo)) }, "PRE_PUSH_LOCK_WAIT=1")
	if r.err == nil || !strings.Contains(r.out, "another push is using the lint checkout") {
		t.Errorf("want blocked by lock, err=%v\n%s", r.err, r.out)
	}
	if len(r.runs) != 0 {
		t.Errorf("lint ran under a held lock: %v", r.runs)
	}
}

// TestPrePushStaleLockTakenOver: stale lock from a dead process is taken over.
func TestPrePushStaleLockTakenOver(t *testing.T) {
	requireTools(t)
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Skip("cannot spawn a short-lived process")
	}
	r := runPrePush(t, "exit 0", func(h *harness, repo string) {
		lock := lintWorktreeOf(h, repo) + ".lock"
		writeFile(h.t, filepath.Join(lock, "pid"), strconv.Itoa(dead.Process.Pid)+"\n", 0o644)
	}, func(h *harness, repo string) string { return pushLine("side", otherCommit(h, repo)) }, "PRE_PUSH_LOCK_WAIT=1")
	if r.err != nil || len(r.runs) != 1 {
		t.Errorf("stale lock not taken over: err=%v runs=%v\n%s", r.err, r.runs, r.out)
	}
}

// TestPrePushClonesGetDistinctCheckouts: two clones get two distinct lint checkouts in a shared cache.
func TestPrePushClonesGetDistinctCheckouts(t *testing.T) {
	requireTools(t)
	e1 := newPrePushEnv(t, "exit 0", nil)
	e2 := newPrePushEnv(t, "exit 0", nil)
	cache := "XDG_CACHE_HOME=" + filepath.Join(e1.h.root, "cache")
	refs := func(h *harness, repo string) string { return pushLine("side", otherCommit(h, repo)) }
	r1 := e1.run(refs, cache)
	r2 := e2.run(refs, cache)
	if r1.err != nil || r2.err != nil {
		t.Fatalf("pushes failed: %v / %v\n%s\n%s", r1.err, r2.err, r1.out, r2.out)
	}
	lw1 := lintWorktreeIn(e1.h.root, e1.h, e1.repo)
	lw2 := lintWorktreeIn(e1.h.root, e2.h, e2.repo)
	if lw1 == lw2 {
		t.Fatalf("clones share a lint checkout: %s", lw1)
	}
	if len(r1.runs) != 1 || r1.runs[0] != lw1 || len(r2.runs) != 1 || r2.runs[0] != lw2 {
		t.Errorf("runs = %v / %v, want %s / %s", r1.runs, r2.runs, lw1, lw2)
	}
}

// TestPrePushCacheHomeAndFallback: honours XDG_CACHE_HOME and falls back to HOME/.cache.
func TestPrePushCacheHomeAndFallback(t *testing.T) {
	requireTools(t)
	e := newPrePushEnv(t, "exit 0", nil)
	home := filepath.Join(e.h.root, "home")
	r := e.run(func(h *harness, repo string) string { return pushLine("side", otherCommit(h, repo)) }, "XDG_CACHE_HOME=")
	if r.err != nil || len(r.runs) != 1 || !strings.Contains(r.runs[0], filepath.Join(home, ".cache", "ctxt-pre-push")) {
		if h, _ := filepath.EvalSymlinks(home); r.err != nil || len(r.runs) != 1 || !strings.Contains(r.runs[0], filepath.Join(h, ".cache", "ctxt-pre-push")) {
			t.Errorf("want a run under HOME/.cache/ctxt-pre-push: err=%v runs=%v\n%s", r.err, r.runs, r.out)
		}
	}
}

// TestPrePushStaleRegistrationRecovered: stale worktree registration (checkout dir gone) is pruned and recreated.
func TestPrePushStaleRegistrationRecovered(t *testing.T) {
	requireTools(t)
	e := newPrePushEnv(t, "exit 0", nil)
	refs := func(h *harness, repo string) string { return pushLine("side", otherCommit(h, repo)) }
	if r := e.run(refs); r.err != nil {
		t.Fatalf("first push failed: %v\n%s", r.err, r.out)
	}
	if err := os.RemoveAll(lintWorktreeOf(e.h, e.repo)); err != nil {
		t.Fatal(err)
	}
	r := e.run(func(h *harness, repo string) string { return pushLine("side", h.git(repo, "rev-parse", "side")) })
	if r.err != nil || len(r.runs) != 1 {
		t.Errorf("stale registration not recovered: err=%v runs=%v\n%s", r.err, r.runs, r.out)
	}
	r.assertLintWorktree(t)
}
