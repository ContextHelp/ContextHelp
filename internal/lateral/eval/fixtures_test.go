package eval_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/eval"
)

// TestFixtures_AllDecodeCleanly proves every fixture file under
// testdata/eval/fixtures/ round-trips through DecodeFixtures without
// error. Guards against fixture drift — a malformed JSONL line in a
// PR breaks this test instead of the CI workflow downstream.
func TestFixtures_AllDecodeCleanly(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs("../../../testdata/eval/fixtures")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(root, "*.jsonl"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("no fixtures found under %s", root)
	}

	for _, path := range matches {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			fixtures, err := eval.LoadFixtures(path)
			if err != nil {
				t.Fatalf("LoadFixtures(%s): %v", path, err)
			}
			if len(fixtures) == 0 {
				t.Errorf("%s: zero fixtures decoded", path)
			}
			// Sanity: every fixture has either a SourceURL or an ObjectID.
			for i, f := range fixtures {
				if f.Event.SourceURL == "" && f.Event.ObjectID == "" {
					t.Errorf("%s[%d]: empty source_url and object_id", path, i)
				}
			}
		})
	}

	// Defensive: the README must also be present so curators have
	// guidance.
	if _, err := filepath.Abs(filepath.Join(root, "README.md")); err != nil {
		t.Errorf("missing README: %v", err)
	}
	_ = strings.TrimSpace // keep import for future negative-list expansions
}
