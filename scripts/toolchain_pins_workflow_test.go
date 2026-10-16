//go:build unix

package scripts

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A workflow that names a branch (@master, @main), @latest or a version
// range (~> v2, version: latest) runs whatever upstream publishes on the
// day the job starts. A release built from it cannot be rebuilt, and a
// compromised or broken upstream release lands without a diff. The gate
// refuses every such reference in workflows.
func TestCheckToolchainPins_WorkflowFloatingRef(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	mise := readMise(t)

	cases := []struct {
		name, file, steps string
		ok                bool
	}{
		{"action release tag", "ci.yml", "      - uses: aquasecurity/trivy-action@v0.36.0\n", true},
		{"action major tag", "ci.yml", "      - uses: actions/checkout@v7\n", true},
		{"action commit sha", "ci.yml", "      - uses: aquasecurity/trivy-action@" + strings.Repeat("a", 40) + "\n", true},
		{"go install exact", "ci.yml", "      - run: go install golang.org/x/vuln/cmd/govulncheck@v1.8.0\n", true},
		{"go install derived", "ci.yml", "      - run: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v${{ needs.toolchain.outputs.golangci-version }}\n", true},
		{"tool version derived", "release.yml", "      - uses: goreleaser/goreleaser-action@v7\n        with:\n          version: v${{ needs.ci.outputs.goreleaser-version }}\n", true},
		{"image ref latest is a scan target", "trivy.yml", "      - uses: aquasecurity/trivy-action@v0.36.0\n        with:\n          image-ref: ghcr.io/o/i:latest\n", true},
		{"main branch filter", "ci.yml", "      - run: echo main\n", true},
		{"commented out", "ci.yml", "      # - uses: aquasecurity/trivy-action@master\n      - run: true\n", true},
		{"trailing comment", "ci.yml", "      - uses: aquasecurity/trivy-action@v0.36.0 # was @master\n", true},
		{"action master", "ci.yml", "      - uses: aquasecurity/trivy-action@master\n", false},
		{"action main", "trivy.yml", "      - uses: some/action@main\n", false},
		{"action quoted master", "ci.yml", "      - uses: 'aquasecurity/trivy-action@master'\n", false},
		{"action latest", "ci.yml", "      - uses: some/action@latest\n", false},
		{"go install latest", "ci.yml", "      - run: go install golang.org/x/vuln/cmd/govulncheck@latest\n", false},
		{"go install latest in block", "vuln-scan.yml", "      - run: |\n          go install github.com/sonatype-nexus-community/nancy@latest\n          nancy --version\n", false},
		{"tool version range", "release.yml", "      - uses: goreleaser/goreleaser-action@v7\n        with:\n          version: '~> v2'\n", false},
		{"tool version range unquoted", "release.yml", "      - uses: goreleaser/goreleaser-action@v7\n        with:\n          version: ~> v2\n", false},
		{"tool version latest", "release.yml", "      - uses: goreleaser/goreleaser-action@v7\n        with:\n          version: latest\n", false},
		{"yaml extension", "nightly.yaml", "      - uses: aquasecurity/trivy-action@master\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := pinsRepo(t, mise)
			wf := filepath.Join(".github", "workflows", tc.file)
			body := "name: t\non: push\njobs:\n  j:\n    runs-on: ubuntu-latest\n    steps:\n" + tc.steps
			writePinsFile(t, filepath.Join(repo, wf), body)

			out, err := exec.Command("bash", filepath.Join(repo, "scripts", "check-toolchain-pins.sh")).CombinedOutput()
			if tc.ok {
				if err != nil {
					t.Fatalf("pinned workflow refused: %v\n%s\n%s", err, body, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("%s passed the gate:\n%s\n%s", wf, body, out)
			}
			if !strings.Contains(string(out), "FAIL: "+wf+":") || !strings.Contains(string(out), "floating") {
				t.Errorf("failure does not name %s as floating:\n%s", wf, out)
			}
		})
	}
}

// release.yml publishes with goreleaser-action and ci.yml checks
// .goreleaser.yaml with it. Both must take the version from the mise.toml
// pin (the toolchain job's output); a literal, even today's pin, lets the
// release run a GoReleaser the config was never checked against.
func TestCheckToolchainPins_WorkflowGoreleaserVersion(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	mise := readMise(t)
	pin := regexp.MustCompile(`(?m)^goreleaser = "(.+)"$`).FindSubmatch(mise)
	if pin == nil {
		t.Fatal("mise.toml has no goreleaser pin")
	}
	action := "      - uses: goreleaser/goreleaser-action@v7\n        with:\n"

	cases := []struct {
		name, steps string
		ok          bool
	}{
		{"needs output", action + "          version: v${{ needs.ci.outputs.goreleaser-version }}\n", true},
		{"step output quoted", action + "          version: 'v${{ steps.pins.outputs.goreleaser-version }}'\n", true},
		{"install only derived", action + "          install-only: true\n          version: v${{ steps.pins.outputs.goreleaser-version }}\n", true},
		{"another action's version", "      - uses: some/action@v1\n        with:\n          version: 1.2.3\n" + action + "          version: v${{ needs.ci.outputs.goreleaser-version }}\n", true},
		{"commented literal", action + "          # version: 'v1.0.0'\n          version: v${{ needs.ci.outputs.goreleaser-version }}\n", true},
		{"literal pin quoted", action + "          version: 'v" + string(pin[1]) + "'\n", false},
		{"literal unquoted", action + "          version: v2.0.0\n", false},
		{"literal without v", action + "          args: release --clean\n          version: \"" + string(pin[1]) + "\"\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := pinsRepo(t, mise)
			wf := filepath.Join(".github", "workflows", "release.yml")
			body := "name: t\non: push\njobs:\n  j:\n    runs-on: ubuntu-latest\n    steps:\n" + tc.steps
			writePinsFile(t, filepath.Join(repo, wf), body)

			out, err := exec.Command("bash", filepath.Join(repo, "scripts", "check-toolchain-pins.sh")).CombinedOutput()
			if tc.ok {
				if err != nil {
					t.Fatalf("derived version refused: %v\n%s\n%s", err, body, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("hardcoded version passed the gate:\n%s\n%s", body, out)
			}
			if !strings.Contains(string(out), "hardcodes a GoReleaser version") || !strings.Contains(string(out), wf+":") {
				t.Errorf("failure does not name %s as hardcoding GoReleaser:\n%s", wf, out)
			}
		})
	}
}

// release.yml publishes a tag, so it must gate on at least what ci.yml
// gates a merge on. Hand-kept copies of ci.yml's jobs drifted (Linux-only
// unit tests, no e2e run, no drop-replaces step), so release.yml runs
// ci.yml itself as a reusable workflow. The gate holds what that depends
// on: ci.yml is callable, none of its jobs can be skipped in a called run,
// and nothing in release.yml starts before the ci.yml job passes.
func TestCheckToolchainPins_ReleaseRunsCI(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	mise := readMise(t)

	ciJobs := "jobs:\n" +
		"  lint:\n    runs-on: ubuntu-latest\n    steps:\n      - run: go vet ./...\n" +
		"  test:\n    runs-on: ${{ matrix.os }}\n    steps:\n" +
		"      - if: matrix.os == 'ubuntu-latest'\n        run: go test ./...\n"
	ciOn := "name: CI\non:\n  push:\n    branches: [main]\n  workflow_call:\n    outputs:\n      go-version:\n        value: x\n"
	ci := ciOn + ciJobs
	caller := "  ci:\n    uses: ./.github/workflows/ci.yml\n"
	gate := func(needs string) string {
		return "  trivy:\n" + needs + "    runs-on: ubuntu-latest\n    steps:\n      - run: trivy fs .\n"
	}
	publish := func(needs string) string {
		return "  goreleaser:\n    runs-on: ubuntu-latest\n" + needs +
			"    steps:\n      - uses: goreleaser/goreleaser-action@v7\n        with:\n          version: v${{ needs.ci.outputs.goreleaser-version }}\n"
	}
	rel := func(jobs ...string) string {
		return "name: Release\non:\n  push:\n    tags: ['v*']\njobs:\n" + strings.Join(jobs, "\n")
	}

	cases := []struct {
		name, ci, release, want string
	}{
		{"flow list needs", ci, rel(caller, gate("    needs: ci\n"), publish("    needs: [ci, trivy]\n")), ""},
		{"block list needs", ci, rel(caller, gate("    needs:\n      - ci\n"), publish("    needs:\n      - trivy\n      - ci\n")), ""},
		{"gated through another job", ci, rel(caller, gate("    needs: ci\n"), publish("    needs: trivy\n")), ""},
		{"quoted caller", ci, rel("  ci:\n    uses: './.github/workflows/ci.yml'\n", publish("    needs: ci\n")), ""},
		{"inline on list", "name: CI\non: [push, workflow_call]\n" + ciJobs, rel(caller, publish("    needs: ci\n")), ""},
		{"no release workflow", ci, "", ""},
		{"publish job ungated", ci, rel(caller, gate("    needs: ci\n"), publish("")), "job goreleaser does not need ci"},
		{"needs only an ungated job", ci, rel(caller, gate(""), publish("    needs: trivy\n")), "job goreleaser does not need ci"},
		{"needs commented out", ci, rel(caller, publish("    # needs: ci\n")), "job goreleaser does not need ci"},
		{"release copies ci jobs", ci, rel("  lint:\n    runs-on: ubuntu-latest\n    steps:\n      - run: go vet ./...\n", publish("    needs: lint\n")), "has no job that runs .github/workflows/ci.yml"},
		{"release calls another workflow", ci, rel("  ci:\n    uses: ./.github/workflows/lint.yml\n", publish("    needs: ci\n")), "has no job that runs .github/workflows/ci.yml"},
		{"ci not callable", "name: CI\non:\n  push:\n    branches: [main]\n" + ciJobs, rel(caller, publish("    needs: ci\n")), "ci.yml has no workflow_call trigger"},
		{"ci job-level if", ciOn + ciJobs + "  integration:\n    if: github.ref == 'refs/heads/main'\n    runs-on: ubuntu-latest\n    steps:\n      - run: go test -tags=integration ./...\n",
			rel(caller, publish("    needs: ci\n")), "ci.yml job integration has a job-level if:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := pinsRepo(t, mise)
			writePinsFile(t, filepath.Join(repo, ".github", "workflows", "ci.yml"), tc.ci)
			if tc.release != "" {
				writePinsFile(t, filepath.Join(repo, ".github", "workflows", "release.yml"), tc.release)
			}

			out, err := exec.Command("bash", filepath.Join(repo, "scripts", "check-toolchain-pins.sh")).CombinedOutput()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("refused: %v\nci:\n%s\nrelease:\n%s\n%s", err, tc.ci, tc.release, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("passed the gate:\nci:\n%s\nrelease:\n%s\n%s", tc.ci, tc.release, out)
			}
			if !strings.Contains(string(out), "FAIL: .github/workflows/") || !strings.Contains(string(out), tc.want) {
				t.Errorf("failure does not say %q:\n%s", tc.want, out)
			}
		})
	}
}

// The release check skips when either workflow is missing, so renaming
// one would silently switch it off. On this checkout it must have checked
// the real pair.
func TestCheckToolchainPins_RepoReleaseRunsCI(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	out, err := exec.Command("bash", "check-toolchain-pins.sh").CombinedOutput()
	if err != nil {
		t.Fatalf("check-toolchain-pins.sh: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok: .github/workflows/release.yml runs .github/workflows/ci.yml (job ") {
		t.Errorf("release check did not run against the repo's workflows:\n%s", out)
	}
}

// make vuln-scan, task install:tools and scripts/dev-setup.sh install the
// same tools CI does. @latest there hands each developer whatever was
// newest the day they ran it, so a local scan or lint run stops being
// evidence about CI. golangci-lint must also be the mise.toml pin on the
// v2 module path.
func TestCheckToolchainPins_DevToolInstalls(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	mise := readMise(t)
	m := regexp.MustCompile(`(?m)^"go:.*golangci-lint" = "(.+)"$`).FindSubmatch(mise)
	if m == nil {
		t.Fatal("mise.toml has no golangci-lint pin")
	}
	lint := string(m[1])

	cases := []struct {
		name, file, body, want string
	}{
		{"pinned", "Makefile", "vuln:\n\tgo install golang.org/x/vuln/cmd/govulncheck@v1.8.0\n", ""},
		{"comment", "Makefile", "# go install golang.org/x/vuln/cmd/govulncheck@latest\n", ""},
		{"prose without a package", "Makefile", "lint:\n\techo \"go install @latest drifts\"\n", ""},
		{"test fixture", "scripts/x_test.go", "package scripts\n\nconst s = \"go install a.b/c@latest\"\n", ""},
		{"lint pin", "Taskfile.yml", "      - go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v" + lint + "\n", ""},
		{"lint derived", "scripts/dev-setup.sh", "go install \"github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v${v}\"\n", ""},
		{"makefile latest", "Makefile", "vuln:\n\tgo install golang.org/x/vuln/cmd/govulncheck@latest; \\\n", "Makefile:2: go install golang.org/x/vuln/cmd/govulncheck@latest"},
		{"taskfile latest", "Taskfile.yml", "      - go install github.com/go-task/task/v3/cmd/task@latest\n", "Taskfile.yml:1: "},
		{"script latest", "scripts/dev-setup.sh", "if true; then\n    go install golang.org/x/vuln/cmd/govulncheck@latest\nfi\n", "scripts/dev-setup.sh:2: "},
		{"script branch", "scripts/install.ps1", "go install example.com/tool@main && ok\n", "scripts/install.ps1:1: "},
		{"nested script", "scripts/hooks/pre-commit", "go run example.com/tool@latest ./...\n", "scripts/hooks/pre-commit:1: "},
		{"go get latest", "Makefile", "deps:\n\tgo get example.com/lib@latest\n", "Makefile:2: "},
		{"lint other version", "Taskfile.yml", "      - go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.0.0\n", "but mise.toml pins golangci-lint " + lint},
		{"lint v1 module", "Taskfile.yml", "      - go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8\n", "the v1 module"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := pinsRepo(t, mise)
			writePinsFile(t, filepath.Join(repo, tc.file), tc.body)

			out, err := exec.Command("bash", filepath.Join(repo, "scripts", "check-toolchain-pins.sh")).CombinedOutput()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("refused: %v\n%s\n%s", err, tc.body, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("%s passed the gate:\n%s\n%s", tc.file, tc.body, out)
			}
			if !strings.Contains(string(out), "FAIL: "+tc.file) || !strings.Contains(string(out), tc.want) {
				t.Errorf("failure does not say %q:\n%s", tc.want, out)
			}
		})
	}
}
