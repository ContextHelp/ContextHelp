package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/steps"
)

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the dpkms configuration",
	Long: `Validate the effective dpkms configuration and list every error.

Besides the checks every config gets, pipelines.overrides is checked
against the pipelines and the steps installed in the configured store:
each overridden pipeline exists, each skip_steps name and each
insert_steps before/after anchor is one of that pipeline's steps, and
each inserted step is built in or installed. dpkms serve refuses to
start on the same errors.`,
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

	svc, cleanup, err := newService()
	if err != nil {
		return fmt.Errorf("open store to list installed steps: %w", err)
	}
	defer cleanup()
	catalog, err := steps.LoadCatalog(context.Background(), svc.Store, steps.Runners{})
	if err != nil {
		return err
	}
	errs = append(errs, builtins.ValidateOverrides(cfg.Pipelines, cfg.Duplicates, catalog.Has)...)

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
