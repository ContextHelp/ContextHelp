package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/accessmarker"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/directopen"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// markerDBPath returns the SQLite file whose access marker c governs, and
// false for a store with no file (Postgres, in-memory SQLite).
func markerDBPath(c *config.Config) (string, bool) {
	typ, path := serveStorageTarget(c)
	if typ != storageTypeSQLite || sqlite.IsMemoryPath(path) {
		return "", false
	}
	return path, true
}

// blobDirOf returns the local blob directory c configures, "" for a
// remote backend.
func blobDirOf(c *config.Config) string {
	switch c.Storage.Blob.Backend {
	case "", "local":
		return c.Storage.Blob.Local.Path
	}
	return ""
}

// recordServeAccess writes the access marker serve owes its database:
// a protected or public instance records its class (and, once its
// listener is bound, addr) before the database is opened, so a direct
// open is refused even after the daemon stops. A private serve writes
// nothing and never lowers a recorded class; that takes
// `dpkms instance set-access private`. It notes such a mismatch on warn.
func recordServeAccess(warn io.Writer, c *config.Config, instance, access, addr string) error {
	dbPath, ok := markerDBPath(c)
	if !ok {
		return nil
	}
	if access == config.AccessPrivate {
		m, found, err := accessmarker.Read(dbPath)
		if err != nil {
			return fmt.Errorf("access marker: %w", err)
		}
		if found && !m.Private() {
			_, _ = fmt.Fprintf(warn, "note: %s records access %q; direct opens stay refused until `dpkms instance set-access private`\n",
				accessmarker.Path(dbPath), m.Access)
		}
		return nil
	}
	return accessmarker.Write(dbPath, accessmarker.Marker{Access: access, Instance: instance, DaemonAddr: addr})
}

// checkServePermissions is serve's file-mode preflight: on a protected or
// public instance, a database file, WAL, sidecar or blob directory
// other users can reach is a warning on warn, and a refusal when strict.
// Private instances and platforms without file modes are not checked.
func checkServePermissions(warn io.Writer, c *config.Config, access string, strict bool) error {
	dbPath, ok := markerDBPath(c)
	if !ok || access == config.AccessPrivate {
		return nil
	}
	findings, err := accessmarker.CheckPermissions(dbPath, blobDirOf(c))
	if errors.Is(err, accessmarker.ErrModesUnsupported) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, f := range findings {
		_, _ = fmt.Fprintf(warn, "warning: %s instance: %s\n", access, f)
	}
	if strict && len(findings) > 0 {
		return fmt.Errorf("%d instance file(s) readable by other users on a %s instance; chmod them (0600 files, 0700 directories) or run without --strict-permissions",
			len(findings), access)
	}
	return nil
}

var instanceCmd = &cobra.Command{
	Use:   "instance",
	Short: "Manage the local instance's database",
}

var instanceSetAccessCmd = &cobra.Command{
	Use:   "set-access <private|protected|public>",
	Short: "Record the access class direct opens are gated on",
	Long: `Write the access class into the access marker beside the configured
SQLite database (<storage.path>.access).

Commands that open the database directly (dpkms job, detector, session,
housekeeping, backup) refuse a protected or public instance; its daemon's
API is the way in. dpkms serve records protected and public itself, and
never lowers the class. This command is how an operator lowers it:
set-access private lets direct opens through again.

The daemon must be stopped: the command takes the database lock. Run it
as the user that owns the database; the change is written to the
instance's audit log (access.set) before the marker changes.

Examples:
  dpkms instance set-access private`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{config.AccessPrivate, config.AccessProtected, config.AccessPublic},
	RunE:      runInstanceSetAccess,
}

func init() {
	rootCmd.AddCommand(instanceCmd)
	instanceCmd.AddCommand(instanceSetAccessCmd)
	// Rewrites the marker in place; set-access back restores it.
	cliconv.WithSideEffect(instanceSetAccessCmd, cliconv.SideEffectWrite)
}

func runInstanceSetAccess(cmd *cobra.Command, args []string) error {
	access := args[0]
	switch access {
	case config.AccessPrivate, config.AccessProtected, config.AccessPublic:
	default:
		return fmt.Errorf("invalid argument %q: want private, protected or public", access)
	}
	dbPath, ok := markerDBPath(cfg)
	if !ok {
		return errors.New("set-access needs a SQLite database file: storage.type postgres and in-memory SQLite have no access marker")
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	t := storeTarget("dpkms instance set-access")
	t.Path = dbPath
	prev, err := directopen.SetAccess(ctx, t, access, instanceNameFromDBPath(dbPath))
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Access for %s set to %s (was %s)\n", dbPath, access, orUnset(prev.Access))
	return nil
}

// orUnset renders an empty class.
func orUnset(s string) string {
	if s == "" {
		return "unset"
	}
	return s
}
