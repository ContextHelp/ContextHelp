//go:build unix

package scripts

import (
	"os/exec"
	"path/filepath"
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
		{"tool version exact", "release.yml", "      - uses: goreleaser/goreleaser-action@v7\n        with:\n          version: 'v2.18.2'\n", true},
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

// release.yml publishes a tag, so it must gate on at least what ci.yml
// gates a merge on. Its integration job once ran only test/integration/...
// while CI also ran the Postgres driver, embeddings registry and job-queue
// suites, so a release could ship code whose Postgres path CI had never
// seen pass. Every command ci.yml's integration job runs must also run in
// release.yml's; release may run more.
func TestCheckToolchainPins_ReleaseIntegrationParity(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	mise := readMise(t)

	wf := func(job, steps string) string {
		return "name: t\non: push\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    steps:\n" +
			"      - run: go test ./lint/...\n" +
			"  " + job + ":\n    runs-on: ubuntu-latest\n    steps:\n" + steps +
			"  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: go build ./...\n"
	}
	drop := "      - name: Drop local-path replaces\n        run: go mod edit -dropreplace=hop.top/kit\n"
	folded := "      - name: Run integration tests\n        run: >-\n          go test -v -tags=integration,fts5\n          ./test/integration/...\n          ./internal/storage/postgres/...\n        env:\n          POSTGRES_HOST: localhost\n"
	foldedReflowed := "      - name: Integration\n        run: >-\n          go test -v -tags=integration,fts5 ./test/integration/...\n          ./internal/storage/postgres/...\n"
	short := "      - name: Run integration tests\n        run: go test -v -tags=integration,fts5 ./test/integration/...\n"
	jobs := "      # Postgres job-queue scenarios\n      - run: >-\n          go test -tags=integration,fts5 -run 'TestClaim_.*_Postgres' ./internal/jobs/\n"
	ci := wf("integration", drop+folded+jobs)

	cases := []struct {
		name, ci, release, want string
	}{
		{"mirrored", ci, wf("integration", drop+folded+jobs), ""},
		{"reflowed folded scalar", ci, wf("integration", drop+foldedReflowed+jobs), ""},
		{"release runs more", ci, wf("integration", drop+folded+jobs+"      - run: go test ./extra/...\n"), ""},
		{"no integration job in ci", wf("other", short), wf("integration", short), ""},
		{"no release workflow", ci, "", ""},
		{"release runs a subset", ci, wf("integration", short), "./internal/storage/postgres/..."},
		{"release lacks drop step", ci, wf("integration", folded+jobs), "go mod edit -dropreplace=hop.top/kit"},
		{"release lacks job-queue suite", ci, wf("integration", drop+folded), "TestClaim_.*_Postgres"},
		{"suite in another release job", ci, strings.Replace(wf("integration", drop+folded), "go build ./...", "go test -tags=integration,fts5 -run 'TestClaim_.*_Postgres' ./internal/jobs/", 1), "TestClaim_.*_Postgres"},
		{"release has no integration job", ci, wf("other", drop+folded+jobs), "release.yml has no integration job"},
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
			if !strings.Contains(string(out), "FAIL: .github/workflows/release.yml") || !strings.Contains(string(out), tc.want) {
				t.Errorf("failure does not name release.yml and %q:\n%s", tc.want, out)
			}
		})
	}
}

// The parity check skips when ci.yml has no integration job, so renaming
// that job would silently switch it off. On this checkout it must have
// compared real commands.
func TestCheckToolchainPins_RepoIntegrationParityRuns(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	out, err := exec.Command("bash", "check-toolchain-pins.sh").CombinedOutput()
	if err != nil {
		t.Fatalf("check-toolchain-pins.sh: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok: .github/workflows/release.yml integration job runs all ") {
		t.Errorf("parity check did not run against the repo's workflows:\n%s", out)
	}
}
