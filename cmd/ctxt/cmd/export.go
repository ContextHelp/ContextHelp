package cmd

import (
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/cli"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:   "export [id]",
	Short: "Export a knowledge object as JSON",
	Long: `Print a knowledge object, read from the dpkms instance ctxt resolves,
as a JSON document (YAML with --format yaml).

If no ID is provided, it will check the clipboard for an ID.

Examples:
  # Print an object as JSON
  ctxt export obj_12345678

  # As YAML
  ctxt export obj_12345678 --format yaml`,
	RunE: runExport,
}

func init() {
	rootCmd.AddCommand(exportCmd)
	cliconv.WithSideEffect(exportCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(exportCmd, []cliconv.Example{
		{Title: "Print an object as JSON", Command: "ctxt export obj_12345678"},
		{Title: "Print an object as YAML", Command: "ctxt export obj_12345678 --format yaml"},
	})
	// "export" is not in kit's defaultIdempotency table. Exporting an
	// object reads it and changes nothing.
	cliconv.WithIdempotency(exportCmd, cliconv.IdempotencyYes)

	exportCmd.Flags().String("server", "", serverFlagUsage)
}

func runExport(cmd *cobra.Command, args []string) error {
	objectID, source, err := cli.GetInput(args)
	if err != nil {
		return err
	}

	if source == cli.SourceClipboard {
		fmt.Fprintf(os.Stderr, "Exporting object ID from clipboard: %q\n", objectID)
	}

	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	obj, err := client.GetObject(cmd.Context(), objectID)
	if err != nil {
		return err
	}
	// outputJSON writes YAML under --format yaml and JSON otherwise.
	return outputJSON(os.Stdout, obj)
}
