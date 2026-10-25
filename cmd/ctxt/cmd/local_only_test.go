package cmd

import (
	"strings"
	"testing"

	"hop.top/kit/go/console/output"
)

// localOnlyCases are the commands that change the local store. --server
// names a remote instance they would never touch, so they refuse it
// rather than change the local one under that flag.
var localOnlyCases = [][]string{
	{"upgrade", "plan"},
	{"upgrade", "run", "--all"},
	{"registry", "add", "r", "https://registry.example"},
	{"registry", "delete", "r"},
	{"registry", "sync", "r"},
	{"registry", "approve", "pe_1"},
	{"registry", "reject", "pe_1"},
	{"remind", "set", "obj_1", "tomorrow"},
	{"remind", "clear", "obj_1"},
	{"resurface", "refresh"},
	{"resurface", "dismiss", "rq_1"},
	{"embeddings", "register", "m"},
	{"embeddings", "set-default", "m"},
	{"embeddings", "deprecate", "m"},
	{"embeddings", "purge", "m"},
	{"embeddings", "migrate", "--to", "m"},
	{"ingest", "--source", "s"},
}

// TestLocalOnlyCommandsRefuseServer: --server on a local-store command
// is a usage error (exit 2), raised before anything runs.
func TestLocalOnlyCommandsRefuseServer(t *testing.T) {
	db := setupTestDB(t)
	for _, args := range localOnlyCases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, err := db.exec(append(args, "--server", "https://prod.example")...)
			if got := exitOf(err); got != output.ExitUsage {
				t.Fatalf("exit %d (%v), want %d", got, err, output.ExitUsage)
			}
			if !strings.Contains(err.Error(), "--server") {
				t.Errorf("err = %v, want it to name --server", err)
			}
		})
	}
}
