package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/cli"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var showCmd = &cobra.Command{
	Use:   "show [id]",
	Short: "Display knowledge object details",
	Long: `Display detailed information about a knowledge object, read from
the dpkms instance ctxt resolves, with the objects related to it through
shared mentions.

If no ID is provided, it will check the clipboard for an ID.

Examples:
  # Show object details
  ctxt show obj_12345678

  # View object using ID from clipboard
  ctxt show

  # Show raw object data
  ctxt show obj_12345678 --raw`,
	RunE: runShow,
}

func init() {
	rootCmd.AddCommand(showCmd)
	cliconv.WithSideEffect(showCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(showCmd, []cliconv.Example{
		{Title: "Show object details", Command: "ctxt show obj_12345678"},
		{Title: "Show ID picked from the clipboard", Command: "ctxt show"},
		{Title: "Show raw object data", Command: "ctxt show obj_12345678 --raw"},
	})

	// Display flags
	showCmd.Flags().Bool("raw", false, "show raw object data")

	// Bind flags to viper
	viper.BindPFlag("show.raw", showCmd.Flags().Lookup("raw"))
}

func runShow(cmd *cobra.Command, args []string) error {
	objectID, source, err := cli.GetInput(args)
	if err != nil {
		return err
	}

	if source == cli.SourceClipboard {
		fmt.Fprintf(os.Stderr, "Opening object ID from clipboard: %q\n", objectID)
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	obj, err := client.GetObject(ctx, objectID)
	if err != nil {
		return err
	}

	if isJSONOutput() || viper.GetBool("show.raw") {
		return outputJSON(os.Stdout, obj)
	}

	// See Also: related objects via shared mention targets (depth 1, at
	// most 5), fetched before anything prints.
	related, err := client.RelatedObjects(ctx, obj.ID, 1, 5)
	if err != nil {
		return err
	}

	fmt.Printf("Knowledge Object: %s\n\n", obj.ID)
	fmt.Printf("Type:      %s\n", obj.Type)
	if obj.Subtype != "" {
		fmt.Printf("Subtype:   %s\n", obj.Subtype)
	}
	fmt.Printf("Pipeline:  %s\n", obj.Pipeline)
	fmt.Printf("Created:   %s\n", obj.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Updated:   %s\n", obj.UpdatedAt.Format("2006-01-02 15:04:05"))
	if obj.Source != "" {
		fmt.Printf("Source:    %s\n", obj.Source)
	}
	fmt.Println()

	// Use projection helpers — never access KO fields ad hoc.
	docProj := projection.ProjectDocument(obj)
	idxProj := projection.ProjectIndex(obj)

	if len(idxProj.Tags) > 0 {
		var labels []string
		for _, t := range idxProj.Tags {
			labels = append(labels, t.Label)
		}
		fmt.Printf("Tags:      %s\n", strings.Join(labels, ", "))
	}
	if len(idxProj.Mentions) > 0 {
		fmt.Printf("Mentions:  %s\n", strings.Join(idxProj.Mentions, ", "))
	}
	fmt.Println()

	if len(obj.Summaries) > 0 {
		fmt.Println("Summary:")
		fmt.Printf("  %s\n", obj.Summaries[0])
		fmt.Println()
	}

	if len(docProj.Sections) > 0 {
		fmt.Println("Sections:")
		for _, s := range docProj.Sections {
			if s.Title != "" {
				fmt.Printf("  [%s]\n", s.Title)
			}
			if s.Content != "" {
				body := s.Content
				if len(body) > 120 {
					body = body[:117] + "..."
				}
				fmt.Printf("    %s\n", body)
			}
		}
		fmt.Println()
	} else if docProj.Body != "" {
		fmt.Println("Body:")
		body := docProj.Body
		if len(body) > 240 {
			body = body[:237] + "..."
		}
		fmt.Printf("  %s\n", body)
		fmt.Println()
	}

	if len(obj.Decisions) > 0 {
		fmt.Println("Decisions:")
		for _, d := range obj.Decisions {
			fmt.Printf("  - %s [%s, %s]\n", d.Title, d.Status, d.Impact)
		}
	}

	if len(related) > 0 {
		fmt.Println()
		fmt.Println("See Also:")
		for _, r := range related {
			label := koLabel(r)
			fmt.Printf("  %s\n", label)
		}
	}

	return nil
}
