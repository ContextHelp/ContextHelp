package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// testDB holds the temp database state for a test.
type testDB struct {
	Driver     storage.StorageDriver
	ConfigPath string
	// Server is the in-process dpkms serving Driver.
	Server *dpkmstest.Server
	// ClientConfigPath is the user-level ctxt config (the cascade's user
	// slot under the test's XDG_CONFIG_HOME) routing commands to Server.
	ClientConfigPath string
}

// setupTestDB creates a temp SQLite database and a config file that points
// to it, and starts an in-process dpkms over the same database (see
// internal/dpkmstest). Seed through Driver; commands reach the data
// directly or over the API.
//
// server.url (plus server.token for a protected instance) goes into the
// user-level config, which --config layers over: a test that writes its
// own server section into ConfigPath still wins. opts configure the
// instance: dpkmstest.WithStaticTokens() makes it protected and the
// client authenticates as admin (see useRole); dpkmstest.Unreachable()
// routes the client to a closed port.
// The config path must be passed as "--config", configPath to executeCommand args.
func setupTestDB(t *testing.T, opts ...dpkmstest.Option) *testDB {
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
	//   - CTXT_DATA_DIR sets storage.data_dir, the base of the run/ dir,
	//     over the XDG default, so a dev with it exported in their shell
	//     would still hit the prod run/ dir.
	//   - activeInstanceName() reads CTXT_INSTANCE (via viper.BindEnv on
	//     "instance" in root.go) before the state file, so a dev with
	//     CTXT_INSTANCE=work exported would still route to that instance.
	//
	// An empty env value counts as unset, so the XDG default applies.
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("CTXT_DATA_DIR", "")
	t.Setenv("CTXT_INSTANCE", "")

	driver, err := sqlite.New(dbPath, storageutil.NewTestBlobStore(t))
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
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	db := &testDB{
		Driver:           driver,
		ConfigPath:       configPath,
		Server:           dpkmstest.Start(t, driver, opts...),
		ClientConfigPath: filepath.Join(dir, "contexthelp", "ctxt.yaml"),
	}
	db.useRole(t, dpkmstest.RoleAdmin)
	return db
}

// useRole rewrites the client config so commands authenticate as role
// (dpkmstest.RoleAdmin, RoleWriter, RoleReader) against a protected
// instance. A role the instance has no token for sends none.
func (db *testDB) useRole(t *testing.T, role string) {
	t.Helper()
	body := "server:\n  url: " + db.Server.URL + "\n"
	if tok := db.Server.Token(role); tok != "" {
		body += "  token: " + tok + "\n"
	}
	if err := os.MkdirAll(filepath.Dir(db.ClientConfigPath), 0o700); err != nil {
		t.Fatalf("client config dir: %v", err)
	}
	if err := os.WriteFile(db.ClientConfigPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write client config: %v", err)
	}
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

// testDataDir is the data dir a CLI run under setupTestDB resolves:
// CTXT_DATA_DIR is cleared and no config sets storage.data_dir, so it is
// the XDG default inside the test's tempdir.
func testDataDir(t *testing.T) string {
	t.Helper()
	dir, err := config.DefaultDataDir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
