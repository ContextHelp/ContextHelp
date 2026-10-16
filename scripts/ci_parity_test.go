//go:build unix

package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The repo's ci.yml and Makefile agree: every CI check step is a make
// target and each ci-<job> runs that job's targets in order. CI's
// toolchain job runs this same gate.
func TestCheckCIParity_RepoAgrees(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	out, err := exec.Command("bash", "check-ci-parity.sh").CombinedOutput()
	if err != nil {
		t.Fatalf("check-ci-parity.sh: %v\n%s", err, out)
	}
	for _, job := range []string{"toolchain", "lint", "test", "integration", "build"} {
		if !strings.Contains(string(out), "ok: ci-"+job+" == ") {
			t.Errorf("gate did not compare job %s:\n%s", job, out)
		}
	}
}

// Any way ci.yml and the local entry points can drift apart fails the
// gate: a step that runs a command inline, env only CI sets, a reordered
// or dropped step, a local target that runs more (or less) than CI, a job
// make ci does not know, a CI-only make variable, a check hidden in a
// provisioning step, an unpinned lint path, a tool version that differs.
func TestCheckCIParity_Drift(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	ci := readRepoFile(t, ".github/workflows/ci.yml")
	mk := readRepoFile(t, "Makefile")

	type edit struct{ file, old, new string }
	cases := []struct {
		name  string
		edits []edit
		extra map[string]string
		want  string // "" = passes
	}{
		{name: "unchanged"},
		{name: "renamed step keeps its target", edits: []edit{{"ci", "name: Run go vet\n", "name: Vet everything\n"}}},
		{
			name:  "inline command",
			edits: []edit{{"ci", "run: make ci-vet\n", "run: go vet -tags fts5 ./...\n"}},
			want:  "job lint step 'Run go vet' runs 'go vet -tags fts5 ./...' inline",
		},
		{
			name:  "inline block",
			edits: []edit{{"ci", "run: make ci-gofmt\n", "run: |\n          gofmt -l .\n"}},
			want:  "job lint step 'Check formatting' runs 'gofmt -l .' inline",
		},
		{
			name:  "flag added in CI only",
			edits: []edit{{"ci", "run: make ci-test-unit\n", "run: make ci-test-unit && go test -count=1 ./...\n"}},
			want:  "passes ' && go test -count=1 ./...' to make",
		},
		{
			name:  "CI-only make variable",
			edits: []edit{{"ci", "run: make ci-test-unit\n", "run: make ci-test-unit GOFLAGS=-count=1\n"}},
			want:  "only LINT_BASE may differ",
		},
		{
			name:  "step env",
			edits: []edit{{"ci", "        run: make ci-test-unit\n", "        env:\n          CGO_ENABLED: '0'\n        run: make ci-test-unit\n"}},
			want:  "step 'Run unit tests' sets env:",
		},
		{
			name:  "job env",
			edits: []edit{{"ci", "  build:\n    name: Build\n", "  build:\n    name: Build\n    env:\n      GOFLAGS: -mod=mod\n"}},
			want:  "job build sets env: or defaults: at job level",
		},
		{
			name:  "step dropped from CI",
			edits: []edit{{"ci", "      - name: Run go vet\n        run: make ci-vet\n", ""}},
			want:  "Makefile ci-lint runs [ci-drop-replaces ci-lint-full ci-gofmt ci-vet] but .github/workflows/ci.yml job lint runs [ci-drop-replaces ci-lint-full ci-gofmt]",
		},
		{
			name:  "step dropped locally",
			edits: []edit{{"mk", "\t$(CI_STEP) ci-test-e2e\n", ""}},
			want:  "Makefile ci-test runs [ci-drop-replaces ci-test-unit] but",
		},
		{
			name:  "steps reordered locally",
			edits: []edit{{"mk", "\t$(CI_STEP) ci-gofmt\n\t$(CI_STEP) ci-vet\n", "\t$(CI_STEP) ci-vet\n\t$(CI_STEP) ci-gofmt\n"}},
			want:  "Makefile ci-lint runs [ci-drop-replaces ci-lint-full ci-vet ci-gofmt]",
		},
		{
			name:  "blocking step made advisory in CI",
			edits: []edit{{"ci", "        run: make ci-lint-full\n", "        continue-on-error: true\n        run: make ci-lint-full\n"}},
			want:  "job lint runs [ci-drop-replaces -ci-lint-full ci-gofmt ci-vet]",
		},
		{
			name:  "test job left out of make test",
			edits: []edit{{"mk", "CI_TEST_JOBS := test viewer integration\n", "CI_TEST_JOBS := test viewer\n"}},
			want:  "Makefile CI_TEST_JOBS is 'test viewer' but the .github/workflows/ci.yml jobs that run a test target (ci-test*, ci-integration*, test-*) are 'test viewer integration'",
		},
		{
			name:  "blocking step made advisory locally",
			edits: []edit{{"mk", "\t$(CI_STEP) ci-lint-full\n", "\t-$(CI_STEP) ci-lint-full\n"}},
			want:  "Makefile ci-lint runs [ci-drop-replaces -ci-lint-full ci-gofmt ci-vet]",
		},
		{
			name:  "local target runs its own command",
			edits: []edit{{"mk", "\t$(CI_STEP) ci-test-e2e\n", "\t$(CI_STEP) ci-test-e2e\n\t$(GO) test -run Nothing ./...\n"}},
			want:  "Makefile ci-test runs '$(GO) test -run Nothing ./...'",
		},
		{
			name:  "local target hides prerequisites",
			edits: []edit{{"mk", "ci-webui:\n", "ci-webui: build-ui\n"}},
			want:  "Makefile ci-webui has prerequisites (build-ui)",
		},
		{
			name:  "new CI job without a local target",
			edits: []edit{{"ci", "  build:\n    name: Build\n", "  docs:\n    name: Docs\n    runs-on: ubuntu-latest\n    steps:\n      - run: make docs\n\n  build:\n    name: Build\n"}},
			want:  "Makefile has no target ci-docs for .github/workflows/ci.yml job docs",
		},
		{
			name:  "CI_JOBS misses a job",
			edits: []edit{{"mk", "CI_JOBS := toolchain secret-scan lint windows", "CI_JOBS := toolchain lint windows"}},
			want:  "CI_JOBS is 'toolchain lint windows",
		},
		{
			name:  "step calls an undefined target",
			edits: []edit{{"ci", "run: make ci-vet\n", "run: make ci-vett\n"}, {"mk", "\t$(CI_STEP) ci-vet\n", "\t$(CI_STEP) ci-vett\n"}},
			want:  "runs make ci-vett, which Makefile does not define",
		},
		{
			name:  "check hidden in a provisioning step",
			edits: []edit{{"ci", "run: sudo apt-get install -y gcc-mingw-w64-x86-64\n", "run: sudo apt-get install -y gcc-mingw-w64-x86-64 && go vet ./...\n"}},
			want:  "is a provisioning step but runs a check (go vet)",
		},
		{
			name:  "unpinned golangci-lint in the Taskfile",
			extra: map[string]string{"Taskfile.yml": "tasks:\n  lint:\n    cmds:\n      - golangci-lint run --timeout=5m\n"},
			want:  "Taskfile.yml:4: - golangci-lint run --timeout=5m runs golangci-lint without scripts/ci/tools.sh",
		},
		{
			name:  "unpinned golangci-lint in a hook",
			extra: map[string]string{"scripts/hooks/lint.sh": "#!/bin/sh\n[ -n \"$1\" ] && golangci-lint run; true\n"},
			want:  "scripts/hooks/lint.sh:2: ",
		},
		{
			name:  "pinned golangci-lint in a hook",
			extra: map[string]string{"scripts/hooks/lint.sh": "#!/bin/sh\n\"$tools\" golangci-lint,go -- golangci-lint run --new-from-rev=HEAD\n# golangci-lint run is fine in a comment\necho \"run: golangci-lint run\"\n"},
		},
		{
			name:  "gosec version differs in another workflow",
			extra: map[string]string{".github/workflows/nightly.yml": "jobs:\n  g:\n    steps:\n      - run: go install github.com/securego/gosec/v2/cmd/gosec@v2.0.0\n"},
			want:  "installs github.com/securego/gosec/v2/cmd/gosec@v2.0.0 but Makefile pins",
		},
		{
			name:  "govulncheck version differs in the Taskfile",
			extra: map[string]string{"Taskfile.yml": "tasks:\n  t:\n    cmds:\n      - go install golang.org/x/vuln/cmd/govulncheck@v1.0.0\n"},
			want:  "installs golang.org/x/vuln/cmd/govulncheck@v1.0.0 but Makefile pins",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{"ci": ci, "mk": mk}
			for _, e := range tc.edits {
				if !strings.Contains(files[e.file], e.old) {
					t.Fatalf("fixture drifted: %s has no %q", e.file, e.old)
				}
				files[e.file] = strings.Replace(files[e.file], e.old, e.new, 1)
			}
			repo := t.TempDir()
			writePinsFile(t, filepath.Join(repo, "scripts", "check-ci-parity.sh"), readRepoFile(t, "scripts/check-ci-parity.sh"))
			writePinsFile(t, filepath.Join(repo, ".github", "workflows", "ci.yml"), files["ci"])
			writePinsFile(t, filepath.Join(repo, "Makefile"), files["mk"])
			for name, body := range tc.extra {
				writePinsFile(t, filepath.Join(repo, name), body)
			}

			out, err := exec.Command("bash", filepath.Join(repo, "scripts", "check-ci-parity.sh")).CombinedOutput()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("refused: %v\n%s", err, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("drift passed the gate:\n%s", out)
			}
			if !strings.Contains(string(out), "FAIL: ") || !strings.Contains(string(out), tc.want) {
				t.Errorf("failure does not say %q:\n%s", tc.want, out)
			}
		})
	}
}

// readRepoFile reads a file relative to the repo root (tests run in
// scripts/).
func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
