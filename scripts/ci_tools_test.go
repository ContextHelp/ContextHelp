//go:build unix

package scripts

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// toolsRepo lays out what scripts/ci/tools.sh and the lint hook need: the
// scripts, the repo's mise.toml, and a stub dir per name in dirs. It
// returns the repo root and the pinned go and golangci-lint versions.
func toolsRepo(t *testing.T) (repo, goPin, lintPin string) {
	t.Helper()
	mise := readRepoFile(t, "mise.toml")
	goPin = regexp.MustCompile(`(?m)^go = "(.+)"$`).FindStringSubmatch(mise)[1]
	lintPin = regexp.MustCompile(`(?m)^"go:.*golangci-lint" = "(.+)"$`).FindStringSubmatch(mise)[1]
	repo = t.TempDir()
	writePinsFile(t, filepath.Join(repo, "mise.toml"), mise)
	for _, f := range []string{"scripts/ci/tools.sh", "scripts/hooks/golangci-staged.sh"} {
		writeExec(t, filepath.Join(repo, f), readRepoFile(t, f))
	}
	return repo, goPin, lintPin
}

// stubGo writes a go that reports version v and logs any other call.
func stubGo(t *testing.T, dir, v string) {
	t.Helper()
	writeExec(t, filepath.Join(dir, "go"), `#!/bin/sh
[ "$1 $2" = "env GOVERSION" ] && { echo go`+v+`; exit 0; }
echo "go `+v+` ran: $*"
`)
}

// stubLint writes a golangci-lint that reports version v and logs runs.
func stubLint(t *testing.T, dir, v string) {
	t.Helper()
	writeExec(t, filepath.Join(dir, "golangci-lint"), `#!/bin/sh
[ "$1" = version ] && { echo "golangci-lint has version `+v+` built with go1.26.8 from (unknown)"; exit 0; }
echo "golangci-lint `+v+` ran: $* (go: $(go env GOVERSION))"
`)
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	writePinsFile(t, path, body)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// runIn runs argv in dir with PATH=<path>:/usr/bin:/bin (no mise, so tools
// resolve from PATH as on a CI runner) and a throwaway HOME.
func runIn(t *testing.T, dir, path string, extraEnv []string, argv ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append([]string{"PATH=" + path + ":/usr/bin:/bin", "HOME=" + t.TempDir()}, extraEnv...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

// tools.sh runs a command only with the pinned versions: a missing tool or
// any other version is an error naming the pin, never a warning, and the
// command sees the pinned go even when another is first on PATH.
func TestCIToolsPinnedOrFail(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	repo, goPin, lintPin := toolsRepo(t)
	tools := filepath.Join(repo, "scripts", "ci", "tools.sh")
	stubs := func(goV, lintV string) string {
		d := t.TempDir()
		if goV != "" {
			stubGo(t, d, goV)
		}
		if lintV != "" {
			stubLint(t, d, lintV)
		}
		return d
	}

	cases := []struct {
		name, path string
		argv       []string
		want       string
		ok         bool
	}{
		{"pinned lint runs with pinned go", stubs(goPin, "v"+lintPin), []string{"golangci-lint,go", "--", "golangci-lint", "run"}, "golangci-lint v" + lintPin + " ran: run (go: go" + goPin + ")", true},
		{"check only", stubs(goPin, lintPin), []string{"golangci-lint,go"}, "", true},
		{"lint other version", stubs(goPin, "2.11.4"), []string{"golangci-lint,go", "--", "golangci-lint", "run"}, "is 2.11.4; this repo pins " + lintPin, false},
		{"lint missing", stubs(goPin, ""), []string{"golangci-lint,go", "--", "golangci-lint", "run"}, "golangci-lint not found; this repo pins " + lintPin, false},
		{"go other version", stubs("1.20.1", lintPin), []string{"go", "--", "go", "test", "./..."}, "is 1.20.1; this repo pins " + goPin, false},
		{"go missing", stubs("", lintPin), []string{"golangci-lint,go"}, "go not found; this repo pins " + goPin, false},
		{"unknown tool", stubs(goPin, lintPin), []string{"golangci", "--", "true"}, "unknown tool 'golangci'", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runIn(t, repo, tc.path, nil, append([]string{"bash", tools}, tc.argv...)...)
			if tc.ok != (err == nil) {
				t.Fatalf("err = %v, want ok=%v\n%s", err, tc.ok, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("output does not say %q:\n%s", tc.want, out)
			}
		})
	}

	// golangci-lint's directory also holds a stale go (mise puts
	// go-installed binaries next to Go): listed first, it would shadow the
	// pinned go golangci-lint shells out to.
	t.Run("shadowed go", func(t *testing.T) {
		lintDir := stubs("1.20.1", lintPin)
		goDir := stubs(goPin, "")
		out, err := runIn(t, repo, goDir+":"+lintDir, nil, "bash", tools, "golangci-lint,go", "--", "golangci-lint", "run")
		if err == nil || !strings.Contains(out, "shadows the pinned go") {
			t.Fatalf("shadowed go not refused: %v\n%s", err, out)
		}
	})

	// CI has no go.work and a stale GOROOT breaks a switched go.
	t.Run("env", func(t *testing.T) {
		out, err := runIn(t, repo, stubs(goPin, ""), []string{"GOROOT=/stale", "GOWORK=/some/go.work"},
			"bash", tools, "go", "--", "sh", "-c", `echo "GOROOT=${GOROOT-unset} GOWORK=$GOWORK"`)
		if err != nil || !strings.Contains(out, "GOROOT=unset GOWORK=off") {
			t.Fatalf("env not normalized: %v\n%s", err, out)
		}
	})
}

// The pre-commit lint hook fails, loudly, when golangci-lint is missing or
// not the pin. It used to print "skipping" and exit 0 when missing, and only
// warn on another version, so a commit passed a gate that never ran (or ran
// rules CI does not).
func TestGolangciStagedHookRequiresPin(t *testing.T) {
	for _, tool := range []string{"bash", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
	gitPath, _ := exec.LookPath("git")
	cases := []struct {
		name, lint, want string
	}{
		{"missing", "", "golangci-lint not found; this repo pins "},
		{"other version", "2.11.4", "is 2.11.4; this repo pins "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, goPin, _ := toolsRepo(t)
			if out, err := exec.Command(gitPath, "-C", repo, "init", "-q").CombinedOutput(); err != nil {
				t.Fatalf("git init: %v\n%s", err, out)
			}
			writePinsFile(t, filepath.Join(repo, "pkg", "a.go"), "package pkg\n")
			stubs := t.TempDir()
			stubGo(t, stubs, goPin)
			if tc.lint != "" {
				stubLint(t, stubs, tc.lint)
			}
			// git alone, not its directory, which may hold a golangci-lint.
			if err := os.Symlink(gitPath, filepath.Join(stubs, "git")); err != nil {
				t.Fatal(err)
			}
			out, err := runIn(t, repo, stubs, nil, "bash", "scripts/hooks/golangci-staged.sh", "pkg/a.go")
			if err == nil {
				t.Fatalf("hook passed without the pinned golangci-lint:\n%s", out)
			}
			if !strings.Contains(out, "ERROR: golangci-lint") || !strings.Contains(out, tc.want) {
				t.Errorf("hook failure does not say %q:\n%s", tc.want, out)
			}
			if strings.Contains(out, "ran:") {
				t.Errorf("hook ran a linter it should have refused:\n%s", out)
			}
		})
	}
}
