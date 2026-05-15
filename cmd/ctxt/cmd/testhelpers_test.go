package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

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

	// Hermeticize env so the CLI's instance-state lookup (RunDir → XDG_DATA_HOME)
	// can't read the developer's real ~/.local/share/contexthelp/run/current-instance,
	// which would silently override --config and route the test at the production DB
	// (T-0186: data-safety hazard, not just a test-correctness bug).
	//
	// XDG_* + HOME redirect file-based lookups into the per-test tempdir.
	// CTXT_DATA_DIR and CTXT_INSTANCE are explicitly cleared because both
	// take precedence over the XDG path:
	//   - config.RunDir() reads $CTXT_DATA_DIR before $XDG_DATA_HOME, so a
	//     dev with CTXT_DATA_DIR exported in their shell would still hit
	//     the prod run/ dir.
	//   - activeInstanceName() reads CTXT_INSTANCE (via viper.BindEnv on
	//     "instance" in root.go) before the state file, so a dev with
	//     CTXT_INSTANCE=work exported would still route to that instance.
	//
	// Setting CTXT_DATA_DIR="" works (vs the obvious worry that it would
	// clobber storage.path) because viper's bound-env-var lookup treats an
	// empty env value as unset — see cascade_test.go:98 which uses the same
	// pattern to let file values win in TestLoad_PicksUpUserConfigPerBinary.
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("CTXT_DATA_DIR", "")
	t.Setenv("CTXT_INSTANCE", "")

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

// seedJob inserts a job record into the test database.
func seedJob(t *testing.T, db *testDB, id, typ, pipeline string, status storage.JobStatus) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	job := &storage.Job{
		ID:         id,
		Type:       typ,
		Status:     status,
		Pipeline:   pipeline,
		MaxRetries: 3,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := db.Driver.Jobs().Create(context.Background(), job); err != nil {
		t.Fatalf("seed job %s: %v", id, err)
	}
}
