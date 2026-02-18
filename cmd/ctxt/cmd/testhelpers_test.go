package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// testDB holds the temp database state for a test.
type testDB struct {
	Driver     storage.StorageDriver
	ConfigPath string
}

// setupTestDB creates a temp SQLite database and a config file that points to it.
// The config path must be passed as "--config", configPath to executeCommand args.
func setupTestDB(t *testing.T) *testDB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	driver, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	ctx := context.Background()
	if err := driver.Init(ctx); err != nil {
		t.Fatalf("init sqlite: %v", err)
	}
	t.Cleanup(func() { driver.Close(ctx) })

	configPath := filepath.Join(dir, "config.yaml")
	configContent := fmt.Sprintf("storage:\n  type: sqlite\n  path: %s\n", dbPath)
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return &testDB{Driver: driver, ConfigPath: configPath}
}

// exec runs a command with the test database config prepended.
func (db *testDB) exec(args ...string) (string, error) {
	fullArgs := append([]string{"--config", db.ConfigPath}, args...)
	return executeCommand(fullArgs...)
}
