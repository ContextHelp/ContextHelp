//go:build unix

package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestGitleaksRuleIDsPrefixed checks every custom rule in .gitleaks.toml
// has a ctxt- id. gitleaks merges a custom rule into a default rule of
// the same id (keeping the default's keywords and allowlists), so an id
// such as openai-api-key silently disables the custom regex.
func TestGitleaksRuleIDsPrefixed(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", ".gitleaks.toml"))
	if err != nil {
		t.Fatal(err)
	}
	ids := regexp.MustCompile(`(?m)^\s*id\s*=\s*"([^"]*)"`).FindAllStringSubmatch(string(b), -1)
	if len(ids) == 0 {
		t.Fatal("no rule ids found in .gitleaks.toml")
	}
	for _, m := range ids {
		if !strings.HasPrefix(m[1], "ctxt-") {
			t.Errorf("rule id %q lacks the ctxt- prefix; it may collide with a gitleaks default rule", m[1])
		}
	}
}

// TestTestHooksScriptRealGitleaks runs scripts/test-hooks.sh against the
// real gitleaks binary and the checkout's .gitleaks.toml: every fake key
// must be blocked by the installed pre-commit hook and every allowlisted
// sample must pass.
func TestTestHooksScriptRealGitleaks(t *testing.T) {
	requireTools(t)
	gitleaks, err := exec.LookPath("gitleaks")
	if err != nil {
		t.Skip("gitleaks not on PATH")
	}
	h := newHarness(t)
	h.prependPath(filepath.Dir(gitleaks))
	repo := h.newRepo("clone", hookScriptFiles...)
	if out, err := h.runScript(repo, "scripts/install-hooks.sh"); err != nil {
		t.Fatalf("install-hooks.sh failed: %v\n%s", err, out)
	}
	out, err := h.runScript(repo, "scripts/test-hooks.sh")
	if err != nil {
		t.Fatalf("test-hooks.sh failed with real gitleaks: %v\n%s", err, out)
	}
	if !strings.Contains(out, "passed: 8  failed: 0") {
		t.Errorf("want all 8 scenarios to pass, got:\n%s", out)
	}
}

// prependPath puts dir first on the harness PATH.
func (h *harness) prependPath(dir string) {
	h.t.Helper()
	for i, kv := range h.env {
		if strings.HasPrefix(kv, "PATH=") {
			h.env[i] = "PATH=" + dir + ":" + strings.TrimPrefix(kv, "PATH=")
			return
		}
	}
	h.t.Fatal("harness env has no PATH")
}
