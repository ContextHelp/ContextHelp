package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var openCmd = &cobra.Command{
	Use:   "open <id>",
	Short: "Display knowledge object details",
	Long: `Display detailed information about a knowledge object.

Includes content, metadata, mentions, resolved entities, and plugin data.

Examples:
  # View object details
  ctxt open obj_12345678

  # View raw object data
  ctxt open obj_12345678 --raw

  # Output as JSON
  ctxt open obj_12345678 --output json`,
	Args: cobra.ExactArgs(1),
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
	objectID := args[0]

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
		fmt.Printf("Mentions:  %s\n", strings.Join(obj.Mentions, ", "))
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

	return nil
}
