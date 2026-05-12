package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/cli"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var exportCmd = &cobra.Command{
	Use:   "export [id]",
	Short: "Export a knowledge object using an output-generator plugin",
	Long: `Export a knowledge object to an external format via an installed output-generator plugin.

The --format flag selects the generator (e.g. "obsidian-md"). If no matching plugin
is loaded, the command falls back to JSON output.

Examples:
  # Export to Obsidian Markdown (requires markdown-export plugin)
  ctxt export obj_12345678 --format obsidian-md

  # Export, writing to the configured vault path
  ctxt export obj_12345678 --format obsidian-md --dest ~/Documents/ObsidianVault

  # Print raw JSON when no format plugin is needed
  ctxt export obj_12345678`,
	RunE: runExport,
}

func init() {
	rootCmd.AddCommand(exportCmd)
	cliconv.WithSideEffect(exportCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(exportCmd, []cliconv.Example{
		{Title: "Print raw JSON for an object", Command: "ctxt export obj_12345678"},
		{Title: "Export via a format plugin", Command: "ctxt export obj_12345678 --format obsidian-md"},
		{Title: "Write to a destination path", Command: "ctxt export obj_12345678 --format obsidian-md --dest ~/Vault"},
	})
	// "export" is not in kit's defaultIdempotency table. Exporting an
	// object is naturally idempotent — the same input + format yields the
	// same output and no source-of-truth mutation.
	cliconv.WithIdempotency(exportCmd, cliconv.IdempotencyYes)

	// --format is inherited from the kit-owned persistent flag set; do not
	// re-register it locally. export reads it via cmd.Flags().GetString("format")
	// at run time (cobra resolves inherited persistent flags through Flags()).
	exportCmd.Flags().String("dest", "", "destination path hint passed to the generator")

	viper.BindPFlag("export.dest", exportCmd.Flags().Lookup("dest"))
}

func runExport(cmd *cobra.Command, args []string) error {
	objectID, source, err := cli.GetInput(args)
	if err != nil {
		return err
	}

	if source == "clipboard" {
		fmt.Fprintf(os.Stderr, "Exporting object ID from clipboard: %q\n", objectID)
	}

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	obj, err := svc.GetObject(ctx, objectID)
	if err != nil {
		return fmt.Errorf("get object: %w", err)
	}

	// --format is owned by kit as a persistent root flag; cmd.Flags()
	// resolves inherited persistent flags transparently.
	format, _ := cmd.Flags().GetString("format")
	if format == "" {
		// No format requested — fall back to JSON.
		return outputJSON(os.Stdout, obj)
	}

	gen := findOutputGenerator(svc.PluginRegistry, format)
	if gen == nil {
		return fmt.Errorf("no output-generator plugin loaded for format %q", format)
	}

	if !gen.Accepts(*obj) {
		return fmt.Errorf("plugin %q does not accept objects of type %q", format, obj.Type)
	}

	opts := pluginapi.OutputOptions{
		Destination: viper.GetString("export.dest"),
	}

	out, err := gen.Generate(ctx, *obj, opts)
	if err != nil {
		return fmt.Errorf("generate %q: %w", format, err)
	}

	_, err = os.Stdout.Write(out)
	return err
}
