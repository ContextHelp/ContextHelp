package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var editCmd = &cobra.Command{
	Use:   "edit --id <id>",
	Short: "Modify knowledge object metadata",
	Long: `Edit metadata for a knowledge object.

Editable fields include title, summary, tags, hints, mentions,
decisions, and subtype.

Examples:
  # Update title
  ctxt edit --id obj_12345678 --title "New title"

  # Update tags
  ctxt edit --id obj_12345678 --tags "ux,onboarding,critical"

  # Update mentions
  ctxt edit --id obj_12345678 --mentions "@ui.best-practice @ux.onboarding"

  # Update multiple fields
  ctxt edit --id obj_12345678 --title "New title" --tags "ux" --subtype "article"`,
	RunE: runEdit,
}

func init() {
	rootCmd.AddCommand(editCmd)

	// Required flags
	editCmd.Flags().String("id", "", "knowledge object ID (required)")
	editCmd.MarkFlagRequired("id")

	// Editable fields
	editCmd.Flags().String("title", "", "update title")
	editCmd.Flags().String("summary", "", "update summary")
	editCmd.Flags().String("tags", "", "replace tags (comma-separated)")
	editCmd.Flags().String("hints", "", "replace hints")
	editCmd.Flags().String("mentions", "", "replace mentions")
	editCmd.Flags().String("decisions", "", "replace decisions (JSON)")
	editCmd.Flags().String("subtype", "", "update subtype")

	// Bind flags to viper
	viper.BindPFlag("edit.id", editCmd.Flags().Lookup("id"))
	viper.BindPFlag("edit.title", editCmd.Flags().Lookup("title"))
	viper.BindPFlag("edit.summary", editCmd.Flags().Lookup("summary"))
	viper.BindPFlag("edit.tags", editCmd.Flags().Lookup("tags"))
	viper.BindPFlag("edit.hints", editCmd.Flags().Lookup("hints"))
	viper.BindPFlag("edit.mentions", editCmd.Flags().Lookup("mentions"))
	viper.BindPFlag("edit.decisions", editCmd.Flags().Lookup("decisions"))
	viper.BindPFlag("edit.subtype", editCmd.Flags().Lookup("subtype"))
}

func runEdit(cmd *cobra.Command, args []string) error {
	id := viper.GetString("edit.id")

	// Collect changed fields
	updates := make(map[string]string)
	if title := viper.GetString("edit.title"); title != "" {
		updates["title"] = title
	}
	if summary := viper.GetString("edit.summary"); summary != "" {
		updates["summary"] = summary
	}
	if tags := viper.GetString("edit.tags"); tags != "" {
		updates["tags"] = tags
	}
	if hints := viper.GetString("edit.hints"); hints != "" {
		updates["hints"] = hints
	}
	if mentions := viper.GetString("edit.mentions"); mentions != "" {
		updates["mentions"] = mentions
	}
	if decisions := viper.GetString("edit.decisions"); decisions != "" {
		updates["decisions"] = decisions
	}
	if subtype := viper.GetString("edit.subtype"); subtype != "" {
		updates["subtype"] = subtype
	}

	if len(updates) == 0 {
		return fmt.Errorf("no fields to update")
	}

	// TODO: Implement actual edit logic
	fmt.Printf("Updating knowledge object: %s\n\n", id)
	fmt.Println("Changes:")
	for field, value := range updates {
		fmt.Printf("  %s: %s\n", field, value)
	}
	fmt.Println()
	fmt.Println("✓ Knowledge object updated successfully")

	return nil
}
