package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// opRepo is a throwaway git repository laid out like this one: the script
// under scripts/ci and the release notes under docs/release-notes.
type opRepo struct {
	t    *testing.T
	dir  string
	base string
}

func newOpRepo(t *testing.T) *opRepo {
	t.Helper()
	dir := t.TempDir()
	src, err := os.ReadFile("ci/check-operator-impact.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "scripts", "ci"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "ci", "check-operator-impact.sh"), src, 0o755); err != nil {
		t.Fatal(err)
	}
	r := &opRepo{t: t, dir: dir}
	r.git("init", "-q", "-b", "main")
	r.git("add", "-A")
	r.git("commit", "-q", "-m", "chore: base")
	r.base = r.head()
	return r
}

func (r *opRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	// Scrub inherited GIT_* (hooks export GIT_DIR) so the commands act on the
	// temp repository, and pin an identity.
	env := []string{}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	env = append(env, "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid", "GIT_CONFIG_GLOBAL=/dev/null")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *opRepo) head() string { return r.git("rev-parse", "HEAD") }

// commit writes files (path → content) and commits them with message.
func (r *opRepo) commit(message string, files map[string]string) string {
	r.t.Helper()
	for p, c := range files {
		full := filepath.Join(r.dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			r.t.Fatal(err)
		}
	}
	r.git("add", "-A")
	r.git("commit", "-q", "--allow-empty", "-m", message)
	return r.head()
}

func (r *opRepo) check() (string, bool) {
	r.t.Helper()
	cmd := exec.Command("bash", "scripts/ci/check-operator-impact.sh", r.base, "HEAD")
	cmd.Dir = r.dir
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

func TestCheckOperatorImpact(t *testing.T) {
	t.Run("absent trailer is none", func(t *testing.T) {
		r := newOpRepo(t)
		r.commit("feat: thing", map[string]string{"a.txt": "a"})
		if out, ok := r.check(); !ok {
			t.Fatalf("want pass, got:\n%s", out)
		}
	})

	t.Run("free-text trailer fails", func(t *testing.T) {
		r := newOpRepo(t)
		sha := r.commit("feat: thing\n\nOperator-Impact: automatic on first open", nil)
		out, ok := r.check()
		if ok || !strings.Contains(out, sha+" has invalid Operator-Impact trailer") {
			t.Fatalf("want an invalid-trailer failure naming %s, got ok=%v:\n%s", sha, ok, out)
		}
	})

	t.Run("correction replaces a bad trailer", func(t *testing.T) {
		r := newOpRepo(t)
		sha := r.commit("feat: thing\n\nOperator-Impact: automatic on first open", nil)
		r.commit("docs: correct it", map[string]string{
			"docs/release-notes/operator-impact-corrections.tsv": "# header\n" + sha + "\tnone\ttrailer was free text\n",
		})
		if out, ok := r.check(); !ok {
			t.Fatalf("want pass with the correction, got:\n%s", out)
		}
	})

	t.Run("correction needs a full SHA and a reason", func(t *testing.T) {
		r := newOpRepo(t)
		sha := r.commit("feat: thing\n\nOperator-Impact: bogus", nil)
		r.commit("docs: correct it", map[string]string{
			"docs/release-notes/operator-impact-corrections.tsv": sha[:7] + "\tnone\tshort sha\n",
		})
		if out, ok := r.check(); ok || !strings.Contains(out, "not a full 40-character SHA") {
			t.Fatalf("want a full-SHA refusal, got ok=%v:\n%s", ok, out)
		}
	})

	t.Run("non-none bucket needs a row naming SHA and bucket", func(t *testing.T) {
		r := newOpRepo(t)
		sha := r.commit("fix: reindex\n\nOperator-Impact: reindex_auto", map[string]string{
			"docs/release-notes/2026-01-01.md": "| fix | `reindex_auto` | row without the sha |\n",
		})
		if out, ok := r.check(); ok || !strings.Contains(out, sha[:7]) {
			t.Fatalf("want a missing-row failure naming %s, got ok=%v:\n%s", sha[:7], ok, out)
		}
		r.commit("docs: cite it", map[string]string{
			"docs/release-notes/2026-01-01.md": "| fix (`" + sha[:7] + "`) | `reindex_auto` | row |\n",
		})
		if out, ok := r.check(); !ok {
			t.Fatalf("want pass once the row names the SHA, got:\n%s", out)
		}
	})
}
