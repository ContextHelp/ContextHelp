//go:build e2e && unix

// Package searchgraph_test drives `ctxt find --graph` end to end: a ctxt
// binary built once per run, a fixture corpus seeded through the binary's
// own commands, and black-box assertions on exit codes, stdout, stderr,
// written files and the ephemeral viewer server. The dpkms-hosted viewer
// runs against a private `dpkms serve` over the same corpus, with a dpkms
// binary built on first use.
//
// Run with:
//
//	go test -count=1 -tags 'fts5 e2e' ./test/e2e/searchgraph/...
//
// Regenerate the JGF goldens after an intended document change:
//
//	go test -count=1 -tags 'fts5 e2e' ./test/e2e/searchgraph/... -run Golden -update
//
// Every process runs with a throwaway HOME and XDG roots, a config file
// pointing storage at a per-test copy of the seeded database, and the
// dpkms server plus embedding endpoint routed to closed loopback ports,
// so nothing reads or writes the developer's real data and no search
// ever reaches a running daemon or model server.
package searchgraph_test

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite JGF goldens under testdata/golden")

// suite is the per-run shared state TestMain prepares.
var suite struct {
	work   string // per-run scratch directory
	bin    string // built ctxt binary
	corpus *corpus
}

func TestMain(m *testing.M) {
	flag.Parse()
	code, err := setup(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "searchgraph e2e: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func setup(m *testing.M) (int, error) {
	work, err := os.MkdirTemp("", "ctxt-searchgraph-e2e-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(work)

	root, err := repoRoot()
	if err != nil {
		return 0, err
	}
	suite.work = work
	suite.bin = filepath.Join(work, "ctxt")
	build := exec.Command("go", "build", "-tags", "fts5", "-buildvcs=false", "-o", suite.bin, "./cmd/ctxt")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=1")
	if out, err := build.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("go build ./cmd/ctxt: %w\n%s", err, out)
	}

	c, err := seedCorpus(suite.bin, filepath.Join(work, "corpus"))
	if err != nil {
		return 0, fmt.Errorf("seed corpus: %w", err)
	}
	suite.corpus = c
	return m.Run(), nil
}

// repoRoot walks up from the test's working directory to go.mod.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}
