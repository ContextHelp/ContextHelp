package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/accessmarker"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/federation"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the dpkms configuration",
	Long: `Validate the effective dpkms configuration and list every error.

Besides the checks every config gets, pipelines.overrides is checked
against the pipelines and the steps installed in the configured store:
each overridden pipeline exists, each skip_steps name and each
insert_steps before/after anchor is one of that pipeline's steps, and
each inserted step is built in or installed. Each federation entry
given a path must point at a private database: a protected or public
target is reached through its daemon's URL. dpkms serve refuses to
start on the same errors.

Validation does not need an openable store. With no database yet,
overrides are checked against the built-in steps. When the store
cannot be read (a protected or public instance, another schema
version), installed steps are not checked and a warning says so.`,
	Args: cobra.NoArgs,
	RunE: runConfigValidate,
}

func init() {
	configCmd.AddCommand(configValidateCmd)
	cliconv.WithSideEffect(configValidateCmd, cliconv.SideEffectRead)
}

func runConfigValidate(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	errs := config.Validate(cfg)
	for _, e := range federation.ValidateLocalTargets(*cfg) {
		errs = append(errs, config.ValidationError{Field: "federations", Message: e.Error()})
	}

	has, warning, err := installedSteps(cmd)
	if err != nil {
		return err
	}
	if warning != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: installed steps not checked: %s\n", warning)
		fmt.Fprintln(out, "note: installed steps not checked (store unavailable); overrides checked against built-in steps")
	}
	errs = append(errs, builtins.ValidateOverrides(cfg.Pipelines, cfg.Duplicates, has)...)

	if len(errs) == 0 {
		fmt.Fprintln(out, "✓ Configuration is valid")
		return nil
	}
	fmt.Fprintf(out, "✗ %d validation error(s):\n", len(errs))
	for _, e := range errs {
		fmt.Fprintf(out, "    %s\n", e)
	}
	return fmt.Errorf("configuration is invalid: %d validation error(s)", len(errs))
}

// installedSteps returns the installed-step lookup overrides are checked
// against. Validation never needs an openable store: with no database
// yet nothing is installed; a store refused by its access class or at
// another schema version cannot be read, so an override naming a step
// that is not built in is let through and warning says why.
func installedSteps(cmd *cobra.Command) (func(string) bool, string, error) {
	driver, cleanup, err := openStore(cmd)
	switch {
	case err == nil:
	case errors.Is(err, storageutil.ErrNoSchema):
		return func(string) bool { return false }, "no database yet, so none are installed", nil
	case errors.Is(err, storageutil.ErrSchemaMismatch), errors.Is(err, accessmarker.ErrRefused):
		return func(string) bool { return true }, err.Error(), nil
	default:
		return nil, "", fmt.Errorf("open store to list installed steps: %w", err)
	}
	defer cleanup()
	catalog, err := steps.LoadCatalog(asSystem(context.Background()), driver, steps.Runners{})
	if err != nil {
		return nil, "", err
	}
	return catalog.Has, "", nil
}
