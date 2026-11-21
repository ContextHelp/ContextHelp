//go:build unix

package scripts

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// kitConformanceStub stands in for a kit binary that ships the
// conformance leaves. Each verify leaf writes a minimal report to its
// -o file and exits with the code the test sets, so a case can play
// any part of kit's exit contract. The badge leaf keeps a copy of the
// matrix it was handed and writes a placeholder badge.
const kitConformanceStub = `#!/bin/sh
[ "$1" = conformance ] || exit 64
leaf="$2"
shift 2
out=""
matrix=""
badge=""
prev=""
for a in "$@"; do
  case "$a" in
    --help) echo "  --matrix string   per-factor matrix JSON"; exit 0 ;;
    --matrix=*) matrix="${a#--matrix=}" ;;
    --output=*) badge="${a#--output=}" ;;
  esac
  [ "$prev" = "-o" ] && out="$a"
  prev="$a"
done
case "$leaf" in
  verify-no-leak)
    echo '{"scanned_files": 1, "findings": []}' >"$out"
    exit "$STUB_NO_LEAK_RC" ;;
  verify-stories)
    echo '{"scanned_files": 1, "findings": []}' >"$out"
    exit "$STUB_STORIES_RC" ;;
  badge)
    cp "$matrix" "$STUB_MATRIX_COPY"
    echo '{"label": "12-factor AI-CLI", "message": "stub", "color": "lightgrey"}' >"$badge"
    exit 0 ;;
esac
exit 64
`

// badgeRepo lays out a throwaway checkout with the badge script, one
// story, one tier-3 verdict passing F3, and the kit stub.
func badgeRepo(t *testing.T) (repo, kit string) {
	t.Helper()
	repo = t.TempDir()
	copyFile(t, "12fcc-badge.sh", filepath.Join(repo, "scripts", "12fcc-badge.sh"))
	writeFile(t, filepath.Join(repo, "e2e", "stories", "a.yaml"), "story_id: a\n", 0o644)
	writeFile(t, filepath.Join(repo, "e2e", "conformance", "verdicts", "a.json"),
		`{"scenario_id": "a", "verdict": "pass", "facets": [{"factor": 3, "status": "pass"}]}`, 0o644)
	kit = filepath.Join(t.TempDir(), "kit")
	writeExec(t, kit, kitConformanceStub)
	return repo, kit
}

type badgeMatrix struct {
	Factors []struct {
		N      int    `json:"n"`
		Status string `json:"status"`
	} `json:"factors"`
}

func readBadgeMatrix(t *testing.T, path string) map[int]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("badge leaf never received a matrix: %v", err)
	}
	var m badgeMatrix
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("matrix is not JSON: %v\n%s", err, b)
	}
	got := map[int]string{}
	for _, f := range m.Factors {
		got[f.N] = f.Status
	}
	return got
}

// TestBadgeScriptExitContract checks scripts/12fcc-badge.sh reads kit's
// conformance exit contract: 0 is clean, 66 is findings (the leaf's
// factors go red and the badge still renders), and any other code means
// the measurement broke, so the script fails without writing a badge.
// Exit 2 is kit's usage error; reading it as findings would publish a
// failed invocation as a conformance verdict.
func TestBadgeScriptExitContract(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	cases := []struct {
		name          string
		noLeak, story string
		want          map[int]string // nil: the script must fail
	}{
		{"clean", "0", "0", map[int]string{1: "pass", 2: "pass", 3: "pass", 9: "pass", 10: "pass", 12: "pass", 4: "skip"}},
		{"story findings", "0", "66", map[int]string{1: "fail", 2: "fail", 9: "fail", 12: "fail", 10: "pass", 3: "pass"}},
		{"leak findings", "66", "0", map[int]string{10: "fail", 1: "pass", 3: "pass"}},
		{"story usage error", "0", "2", nil},
		{"leak usage error", "2", "0", nil},
		{"story transient error", "0", "6", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, kit := badgeRepo(t)
			matrix := filepath.Join(t.TempDir(), "matrix.json")
			out, err := runIn(t, repo, filepath.Dir(kit), []string{
				"KIT_BIN=" + kit,
				"STUB_NO_LEAK_RC=" + tc.noLeak,
				"STUB_STORIES_RC=" + tc.story,
				"STUB_MATRIX_COPY=" + matrix,
			}, "bash", "scripts/12fcc-badge.sh")
			_, statErr := os.Stat(filepath.Join(repo, ".12fc.json"))
			if tc.want == nil {
				if err == nil {
					t.Fatalf("script succeeded on a broken measurement:\n%s", out)
				}
				if statErr == nil {
					t.Errorf("script wrote .12fc.json from a broken measurement")
				}
				return
			}
			if err != nil {
				t.Fatalf("script failed: %v\n%s", err, out)
			}
			if statErr != nil {
				t.Fatalf("no .12fc.json written: %v\n%s", statErr, out)
			}
			got := readBadgeMatrix(t, matrix)
			for n, want := range tc.want {
				if got[n] != want {
					t.Errorf("F%d = %q, want %q\n%s", n, got[n], want, out)
				}
			}
		})
	}
}
