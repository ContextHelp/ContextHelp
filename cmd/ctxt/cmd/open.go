package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/cli"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var openCmd = &cobra.Command{
	Use:   "open [id]",
	Short: "Display knowledge object details",
	Long: `Display detailed information about a knowledge object.

If no ID is provided, it will check the clipboard for an ID.

Examples:
  # View object details
  ctxt open obj_12345678

  # View object using ID from clipboard
  ctxt open

  # View raw object data
  ctxt open obj_12345678 --raw`,
	RunE: runOpen,
}

func init() {
	rootCmd.AddCommand(openCmd)

	// Display flags
	openCmd.Flags().Bool("raw", false, "show raw object data")

	// Bind flags to viper
	viper.BindPFlag("open.raw", openCmd.Flags().Lookup("raw"))
}

func runOpen(cmd *cobra.Command, args []string) error {
	objectID, source, err := cli.GetInput(args)
	if err != nil {
		return err
	}

	if source == "clipboard" {
		fmt.Fprintf(os.Stderr, "Opening object ID from clipboard: %q\n", objectID)
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

	if isJSONOutput() || viper.GetBool("open.raw") {
		return outputJSON(os.Stdout, obj)
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

	if len(obj.Tags) > 0 {
		var labels []string
		for _, t := range obj.Tags {
			labels = append(labels, t.Label)
		}
		fmt.Printf("Tags:      %s\n", strings.Join(labels, ", "))
	}
	if len(obj.Mentions) > 0 {
		mentionStrs := make([]string, len(obj.Mentions))
		for i, u := range obj.Mentions {
			mentionStrs[i] = u.String()
		}
		fmt.Printf("Mentions:  %s\n", strings.Join(mentionStrs, ", "))
	}
	fmt.Println()

	if len(obj.Summaries) > 0 {
		fmt.Println("Summary:")
		fmt.Printf("  %s\n", obj.Summaries[0])
		fmt.Println()
	}

	if len(obj.Decisions) > 0 {
		fmt.Println("Decisions:")
		for _, d := range obj.Decisions {
			fmt.Printf("  - %s [%s, %s]\n", d.Title, d.Status, d.Impact)
		}
	}

	// See Also: related objects via shared mention targets (depth=1, max 5).
	related, err := svc.RelatedObjects(ctx, obj.ID, 1, 5)
	if err == nil && len(related) > 0 {
		fmt.Println()
		fmt.Println("See Also:")
		for _, r := range related {
			label := r.ID
			if len(r.Summaries) > 0 && r.Summaries[0] != "" {
				summary := r.Summaries[0]
				if len(summary) > 60 {
					summary = summary[:57] + "..."
				}
				label = fmt.Sprintf("%s  %s", r.ID, summary)
			}
			fmt.Printf("  %s\n", label)
		}
	}

	return nil
}
