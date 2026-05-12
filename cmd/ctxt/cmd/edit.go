package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/mentions"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var editCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Modify knowledge object metadata",
	Long: `Edit metadata for a knowledge object.

Editable fields include title, summary, tags, hints, mentions,
decisions, and subtype.

Examples:
  # Update title
  ctxt edit obj_12345678 --title "New title"

  # Update tags
  ctxt edit obj_12345678 --tags "ux,onboarding,critical"

  # Update mentions
  ctxt edit obj_12345678 --mention "@ui.best-practice @ux.onboarding"

  # Update multiple fields
  ctxt edit obj_12345678 --title "New title" --tags "ux" --subtype "article"`,
	Args: cobra.ExactArgs(1),
	RunE: runEdit,
}

func init() {
	rootCmd.AddCommand(editCmd)
	cliconv.WithSideEffect(editCmd, cliconv.SideEffectWrite)
	cliconv.WithExamples(editCmd, []cliconv.Example{
		{Title: "Update the title", Command: "ctxt edit obj_12345678 --title \"New title\""},
		{Title: "Replace tags", Command: "ctxt edit obj_12345678 --tags ux,onboarding,critical"},
		{Title: "Update multiple fields", Command: "ctxt edit obj_12345678 --title \"X\" --subtype article"},
	})
	cliconv.WithNextSteps(editCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt show <id>", Reason: "confirm the updated metadata"},
	})
	// "edit" is in kit's defaultIdempotency table (yes); the verb default
	// matches the field-update semantics (re-applying the same update
	// converges), so no override needed.

	// Editable fields
	editCmd.Flags().String("title", "", "update title")
	editCmd.Flags().String("summary", "", "update summary")
	editCmd.Flags().String("tags", "", "replace tags (comma-separated)")
	editCmd.Flags().String("hints", "", "replace hints")
	editCmd.Flags().String("mention", "", "replace mentions")
	editCmd.Flags().String("decisions", "", "replace decisions (JSON)")
	editCmd.Flags().String("subtype", "", "update subtype")

	viper.BindPFlag("edit.title", editCmd.Flags().Lookup("title"))
	viper.BindPFlag("edit.summary", editCmd.Flags().Lookup("summary"))
	viper.BindPFlag("edit.tags", editCmd.Flags().Lookup("tags"))
	viper.BindPFlag("edit.hints", editCmd.Flags().Lookup("hints"))
	viper.BindPFlag("edit.mention", editCmd.Flags().Lookup("mention"))
	viper.BindPFlag("edit.decisions", editCmd.Flags().Lookup("decisions"))
	viper.BindPFlag("edit.subtype", editCmd.Flags().Lookup("subtype"))
}

func runEdit(cmd *cobra.Command, args []string) error {
	id := args[0]

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
	if mentions := viper.GetString("edit.mention"); mentions != "" {
		updates["mentions"] = mentions
	}
	if subtype := viper.GetString("edit.subtype"); subtype != "" {
		updates["subtype"] = subtype
	}

	if len(updates) == 0 {
		return fmt.Errorf("no fields to update")
	}

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	obj, err := svc.GetObject(ctx, id)
	if err != nil {
		return fmt.Errorf("get object: %w", err)
	}

	// Apply updates
	if v, ok := updates["title"]; ok {
		obj.Summaries = []string{v}
	}
	if v, ok := updates["summary"]; ok {
		if len(obj.Summaries) > 0 {
			obj.Summaries[0] = v
		} else {
			obj.Summaries = []string{v}
		}
	}
	if v, ok := updates["tags"]; ok {
		var tags []storage.Tag
		for _, label := range strings.Split(v, ",") {
			label = strings.TrimSpace(label)
			if label != "" {
				tags = append(tags, storage.Tag{Label: label, Source: "manual"})
			}
		}
		obj.Tags = tags
	}
	if v, ok := updates["mentions"]; ok {
		obj.Mentions = mentions.ParseSlice(strings.Fields(v))
	}
	if v, ok := updates["subtype"]; ok {
		obj.Subtype = v
	}

	obj.UpdatedAt = time.Now().Truncate(time.Second)

	if err := svc.UpdateObject(ctx, obj); err != nil {
		return fmt.Errorf("update object: %w", err)
	}

	fmt.Printf("Updated %s\n", id)
	for field, value := range updates {
		fmt.Printf("  %s: %s\n", field, value)
	}
	return nil
}
