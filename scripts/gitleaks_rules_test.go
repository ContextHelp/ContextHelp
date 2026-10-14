//go:build unix

package scripts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
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
	if !strings.Contains(out, "passed: 9  failed: 0") {
		t.Errorf("want all 9 scenarios to pass, got:\n%s", out)
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

// TestGitleaksRulesCaptureTheSecret checks no custom rule captures its
// keyword. gitleaks takes the first capture group as the secret when
// secretGroup is unset, so (password|pwd)\s*=... made the secret the word
// "password": --redact masked the keyword and printed the value, and
// stopwords and allowlist regexes were tested against the keyword.
func TestGitleaksRulesCaptureTheSecret(t *testing.T) {
	cfg := loadGitleaksConfig(t)
	if len(cfg.Rules) == 0 {
		t.Fatal("no [[rules]] in .gitleaks.toml")
	}
	for _, r := range cfg.Rules {
		re, err := regexp.Compile(r.Regex)
		if err != nil {
			t.Errorf("rule %s: bad regex: %v", r.ID, err)
			continue
		}
		if r.SecretGroup == 0 && re.NumSubexp() > 0 {
			t.Errorf("rule %s has %d capture group(s) and no secretGroup; the secret becomes the first group. Use (?:...)", r.ID, re.NumSubexp())
		}
	}
}

func loadGitleaksConfig(t *testing.T) gitleaksConfig {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", ".gitleaks.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg gitleaksConfig
	if err := toml.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("parse .gitleaks.toml: %v", err)
	}
	return cfg
}

// fakeKeys returns one key-shaped string per major provider, assembled at
// run time so this file holds no key-shaped literal. The fill avoids the
// placeholder shapes gitleaks skips by design (alphabet runs, EXAMPLE).
func fakeKeys() map[string]string {
	const fill = "Zq7Lw2Rk9VtXb8Nc3Hd6My"
	const b32 = "ZQ7LW2RKMY5VTXB4" // AWS key ids use A-Z and 2-7 only
	f := func(p string, n int) string { return strings.Repeat(p, n/len(p)+1)[:n] }
	return map[string]string{
		"aws":       "AKIA" + f(b32, 16),
		"github":    "ghp_" + f(fill, 36),
		"gitlab":    "glpat-" + f(fill, 20),
		"openai":    "sk-proj-" + f(fill, 40),
		"anthropic": "sk-ant-api03-" + f(fill, 93) + "AA",
		"slack":     "xoxb-" + f("1234567890", 13) + "-" + f("9876543210", 13) + "-" + f(fill, 24),
		"stripe":    "sk_live_" + f(fill, 32),
		"google":    "AIza" + f(fill, 35),
		"postgres":  "postgres://app:" + f(fill, 18) + "@db.internal:5432/app",
		// No provider prefix: only gitleaks' generic-api-key rule sees it,
		// the rule the test-file fixture allowlist targets.
		"generic": f(fill, 32),
	}
}

type gitleaksConfig struct {
	Rules []struct {
		ID          string `toml:"id"`
		Regex       string `toml:"regex"`
		SecretGroup int    `toml:"secretGroup"`
	} `toml:"rules"`
	Allowlists []struct {
		Description string   `toml:"description"`
		Paths       []string `toml:"paths"`
		Regexes     []string `toml:"regexes"`
		StopWords   []string `toml:"stopwords"`
	} `toml:"allowlists"`
}

// TestGitleaksAllowlistRegexesSpareProviderKeys checks no allowlist regex
// or stopword in .gitleaks.toml matches a key of a major provider, so an
// allowlist entry can never wave a real key through by shape alone.
func TestGitleaksAllowlistRegexesSpareProviderKeys(t *testing.T) {
	cfg := loadGitleaksConfig(t)
	if len(cfg.Allowlists) == 0 {
		t.Fatal("no [[allowlists]] in .gitleaks.toml")
	}
	keys := fakeKeys()
	for _, al := range cfg.Allowlists {
		for _, expr := range al.Regexes {
			re, err := regexp.Compile(expr)
			if err != nil {
				t.Errorf("allowlist %q: bad regex %q: %v", al.Description, expr, err)
				continue
			}
			for name, key := range keys {
				if re.MatchString(key) {
					t.Errorf("allowlist %q regex %q matches a %s key", al.Description, expr, name)
				}
			}
		}
		for _, w := range al.StopWords {
			for name, key := range keys {
				if strings.Contains(strings.ToLower(key), strings.ToLower(w)) {
					t.Errorf("allowlist %q stopword %q occurs in a %s key", al.Description, w, name)
				}
			}
		}
	}
}

// TestGitleaksReportsKeysInDocsTestsAndExamples plants every provider key
// in the file kinds the allowlists are scoped around (docs, markdown, Go
// tests, testdata, .env.example and config.*.yaml samples) and checks
// gitleaks reports each one: no path allowlist may hide a whole doc,
// fixture or sample config, and the fixture-token allowlist must need both
// a test path and the fixture shape.
func TestGitleaksReportsKeysInDocsTestsAndExamples(t *testing.T) {
	gitleaks, err := exec.LookPath("gitleaks")
	if err != nil {
		t.Skip("gitleaks not on PATH")
	}
	config, err := filepath.Abs(filepath.Join("..", ".gitleaks.toml"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	tree := filepath.Join(dir, "tree")
	keys := fakeKeys()
	// The fixture token shape is allowlisted only in test files.
	fixture := "tok-ops-fixture-0123456789abcdef0123456789"
	files := map[string]func(name, key string) string{
		"docs/setup.md":             func(n, k string) string { return fmt.Sprintf("Set the %s key: `%s`", n, k) },
		"README.md":                 func(n, k string) string { return fmt.Sprintf("    token: %s", k) },
		"testdata/fixture.env":      func(n, k string) string { return fmt.Sprintf("TOKEN=%s", k) },
		"internal/x/client_test.go": func(n, k string) string { return fmt.Sprintf("\ttoken := %q // %s", k, n) },
		// Sample configs are copied to real ones and filled in: a key
		// pasted into the sample is a leak like any other.
		".env.example":               func(n, k string) string { return fmt.Sprintf("API_TOKEN=%s", k) },
		"config/config.example.yaml": func(n, k string) string { return fmt.Sprintf("  api_key: %s", k) },
		"config/config.minimal.yaml": func(n, k string) string { return fmt.Sprintf("  token: %s", k) },
		"docker/config.docker.yaml":  func(n, k string) string { return fmt.Sprintf("  secret_key: %s", k) },
		"docs/fixture-in-prose.md":   nil,
		"internal/x/fixture_prod.go": nil,
	}
	want := map[string]int{} // file -> lines that must be reported
	for file, line := range files {
		var body []string
		if line == nil {
			body = []string{fmt.Sprintf("api_key = %q", fixture)}
		} else {
			for name, key := range keys {
				body = append(body, line(name, key))
			}
		}
		writeFile(t, filepath.Join(tree, file), strings.Join(body, "\n")+"\n", 0o644)
		want[file] = len(body)
	}

	report := filepath.Join(dir, "report.json")
	cmd := exec.Command(gitleaks, "dir", ".", "--config", config, "--no-banner",
		"--exit-code", "0", "--report-format", "json", "--report-path", report)
	cmd.Dir = tree
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gitleaks dir: %v\n%s", err, out)
	}
	b, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	var findings []struct {
		File      string
		StartLine int
	}
	if err := json.Unmarshal(b, &findings); err != nil {
		t.Fatalf("parse report: %v", err)
	}
	got := map[string]map[int]bool{}
	for _, f := range findings {
		if got[f.File] == nil {
			got[f.File] = map[int]bool{}
		}
		got[f.File][f.StartLine] = true
	}
	for file, n := range want {
		for line := 1; line <= n; line++ {
			if !got[file][line] {
				body, _ := os.ReadFile(filepath.Join(tree, file))
				t.Errorf("%s:%d not reported: %s", file, line, strings.Split(string(body), "\n")[line-1])
			}
		}
	}
}

// TestGitleaksFullTreeClean scans the checkout's file tree (tracked plus
// untracked, non-ignored files) with the checkout's .gitleaks.toml and
// expects no findings.
func TestGitleaksFullTreeClean(t *testing.T) {
	if testing.Short() {
		t.Skip("full-tree scan takes tens of seconds")
	}
	if _, err := exec.LookPath("gitleaks"); err != nil {
		t.Skip("gitleaks not on PATH")
	}
	for _, tool := range []string{"bash", "git", "tar"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
	cmd := exec.Command("bash", "secret-scan-tree.sh")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("secret-scan-tree.sh: %v\n%s", err, out.String())
	}
}

// TestGitleaksVersionPinned checks mise.toml pins gitleaks to an exact
// release and gitleaks-version-check.sh warns (exit 1) only when the
// gitleaks on PATH differs from that pin.
func TestGitleaksVersionPinned(t *testing.T) {
	requireTools(t)
	b, err := os.ReadFile(filepath.Join("..", "mise.toml"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^gitleaks = "([^"]*)"$`).FindSubmatch(b)
	if len(m) < 2 || !regexp.MustCompile(`^\d+\.\d+\.\d+$`).Match(m[1]) {
		t.Fatalf("mise.toml must pin gitleaks to an exact X.Y.Z, got %q", m)
	}
	pin := string(m[1])

	for _, tc := range []struct{ have, wantOut string }{
		{"v" + pin, ""},
		{pin, ""},
		{"8.0.0", "mise.toml pins " + pin},
	} {
		h := newHarness(t)
		h.stub("gitleaks", `[ "$1" = version ] && echo "`+tc.have+`"`)
		repo := h.newRepo("clone", "mise.toml", "scripts/gitleaks-version-check.sh")
		out, err := h.runScript(repo, "scripts/gitleaks-version-check.sh")
		if tc.wantOut == "" {
			if err != nil || out != "" {
				t.Errorf("gitleaks %s: want silent success, got %v: %q", tc.have, err, out)
			}
			continue
		}
		if err == nil || !strings.Contains(out, tc.wantOut) {
			t.Errorf("gitleaks %s: want exit 1 and %q, got %v: %q", tc.have, tc.wantOut, err, out)
		}
	}
}

// TestGitleaksBaselineEntriesNameACommit checks every .gitleaksignore entry
// is a commit-scoped fingerprint (commit:file:rule:line). gitleaks also
// takes file:rule:line, which skips that finding in every commit and in
// working-tree scans: a key pasted later on a baselined line would pass.
func TestGitleaksBaselineEntriesNameACommit(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", ".gitleaksignore"))
	if err != nil {
		t.Fatal(err)
	}
	entry := regexp.MustCompile(`^[0-9a-f]{40}:[^:]+:[a-z0-9-]+:[0-9]+$`)
	n := 0
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		n++
		if !entry.MatchString(line) {
			t.Errorf(".gitleaksignore:%d: %q is not a commit:file:rule:line fingerprint", i+1, line)
		}
	}
	if n == 0 {
		t.Error(".gitleaksignore has no entries")
	}
}

// TestGitleaksHistoryScanClean runs `make security-scan` on the checkout:
// every finding in the history of HEAD must be baselined in
// .gitleaksignore, so the scan CI runs passes.
func TestGitleaksHistoryScanClean(t *testing.T) {
	if testing.Short() {
		t.Skip("history scan takes tens of seconds")
	}
	for _, tool := range []string{"gitleaks", "git", "make"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
	cmd := exec.Command("make", "--no-print-directory", "-C", "..", "security-scan")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("make security-scan: %v\n%s", err, out.String())
	}
}
