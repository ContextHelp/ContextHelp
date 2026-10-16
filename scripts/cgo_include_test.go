//go:build unix

package scripts

import (
	"bufio"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// compilesGo matches a Makefile recipe line that compiles Go or type-checks
// it (go build/test/vet/install/run, golangci-lint), directly or through the
// $(GO) and $(GOLANGCI) wrappers.
var compilesGo = regexp.MustCompile(`(^|[\s;&|(])(go|\$\(GO\))\s+(build|test|vet|install|run)\b|\$\(GOLANGCI\)|golangci-lint\s+run`)

// makeRules returns each explicit rule in the Makefile's database: target
// -> expanded prerequisites and the (unexpanded) recipe lines.
func makeRules(t *testing.T, mkdir string) map[string]struct{ prereqs, recipe []string } {
	t.Helper()
	cmd := exec.Command("make", "-pRrq", "-f", "Makefile")
	cmd.Dir = mkdir
	// -q exits 1 when a target is out of date; the database is printed anyway.
	out, _ := cmd.Output()
	rules := map[string]struct{ prereqs, recipe []string }{}
	var cur string
	files := false
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	rule := regexp.MustCompile(`^([^#\s:=][^:=]*?):(?:\s+(.*))?$`)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "# Files") {
			files = true
			continue
		}
		if !files {
			continue
		}
		if strings.HasPrefix(line, "# Not a target") {
			cur = ""
			continue
		}
		if m := rule.FindStringSubmatch(line); m != nil {
			cur = m[1]
			r := rules[cur]
			r.prereqs = strings.Fields(m[2])
			rules[cur] = r
			continue
		}
		if cur != "" && strings.HasPrefix(line, "\t") {
			r := rules[cur]
			r.recipe = append(r.recipe, line)
			rules[cur] = r
		}
		if line == "" {
			cur = ""
		}
	}
	if len(rules) == 0 {
		t.Fatalf("make -p printed no rules:\n%s", out)
	}
	return rules
}

// Every make target that compiles Go must first generate the sqlite3.h
// that CGO_CFLAGS points at: with the directory missing, the compiler
// ignores the -I and silently takes the system sqlite3.h, a different
// SQLite release from the one mattn/go-sqlite3 links.
func TestMakefileCgoTargetsGenerateSqliteHeader(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not on PATH")
	}
	repo, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	header := filepath.Join(repo, "build", "cgo-include", "sqlite3.h")
	rules := makeRules(t, repo)

	if _, ok := rules[header]; !ok {
		t.Fatalf("Makefile has no rule for %s", header)
	}
	checked := 0
	for target, r := range rules {
		compiles := false
		for _, l := range r.recipe {
			if compilesGo.MatchString(l) {
				compiles = true
				break
			}
		}
		if !compiles {
			continue
		}
		checked++
		found := false
		for _, p := range r.prereqs {
			if p == header {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("make target %s compiles Go but does not depend on %s; add it to CGO_TARGETS", target, header)
		}
	}
	// The scan itself must have found the compile targets, or a parse
	// change would pass this test vacuously.
	for _, want := range []string{"build-ctxt", "ci-vet", "ci-test-unit", "ci-lint-full", "ci-windows-build"} {
		r, ok := rules[want]
		if !ok || len(r.recipe) == 0 {
			t.Errorf("make database has no recipe for %s", want)
		}
	}
	if checked < 10 {
		t.Errorf("only %d compiling targets found; the database parse is broken", checked)
	}
}
