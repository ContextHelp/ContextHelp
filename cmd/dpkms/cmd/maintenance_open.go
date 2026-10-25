package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"

	"github.com/ideacrafterslabs/ctxt/internal/accessmarker"
	"github.com/ideacrafterslabs/ctxt/internal/directopen"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// maintenanceOverrideFlag lets housekeeping and backup run on a protected
// or public instance without lowering its access class.
const maintenanceOverrideFlag = "maintenance-override"

// maintenanceLabel names housekeeping and backup in the lock body.
const maintenanceLabel = "dpkms maintenance"

// addMaintenanceOverrideFlag declares --maintenance-override on cmd.
func addMaintenanceOverrideFlag(cmd *cobra.Command, persistent bool) {
	fs := cmd.Flags()
	if persistent {
		fs = cmd.PersistentFlags()
	}
	fs.Bool(maintenanceOverrideFlag, false,
		"run on a protected or public instance: as the database owner, with the daemon stopped; audited")
}

// maintenanceOverride reports whether cmd was given --maintenance-override.
func maintenanceOverride(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	on, _ := cmd.Flags().GetBool(maintenanceOverrideFlag)
	return on
}

// openMaintenance opens the configured store for the maintenance command
// cmd: as any write (access gate, database lock), read-only for a
// --dry-run preview, or, with
// --maintenance-override on a protected or public instance, through the
// audited override.
func openMaintenance(cmd *cobra.Command) (storage.StorageDriver, func(), error) {
	ctx := context.Background()
	t := storeTarget(maintenanceLabel)
	var h *directopen.Handle
	var err error
	switch {
	case maintenanceOverride(cmd):
		h, err = directopen.Override(ctx, t, cmd.CommandPath())
	case kitcli.IsDryRun(cmd):
		// A preview only reads: no lock, so it runs beside the daemon.
		h, err = directopen.Read(ctx, t)
	default:
		h, err = directopen.Write(ctx, t)
	}
	if err != nil {
		return nil, nil, maintenanceRefusal(err)
	}
	cleanup := func() {
		if err := h.Close(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to close storage: %v\n", err)
		}
	}
	return h.Driver, cleanup, nil
}

// refusalWithOverride is an access refusal met by a maintenance
// command: its fix names the override.
type refusalWithOverride struct{ err error }

func (r refusalWithOverride) Error() string { return r.err.Error() }
func (r refusalWithOverride) Unwrap() error { return r.err }

// Fix adds the maintenance override to the refusal's own fix.
func (r refusalWithOverride) Fix() string {
	fix := ""
	var re *accessmarker.RefusedError
	if errors.As(r.err, &re) {
		// An unreadable marker refuses the override too: repair it.
		if re.Cause != nil {
			return re.Fix()
		}
		fix = re.Fix() + "; "
	}
	return fix + "or rerun with --" + maintenanceOverrideFlag + " as the database owner, with the daemon stopped (audited)"
}

// maintenanceRefusal points an access refusal at the override.
func maintenanceRefusal(err error) error {
	if errors.Is(err, accessmarker.ErrRefused) {
		return refusalWithOverride{err: err}
	}
	return err
}
