package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"hop.top/uri"
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
	if mentions := viper.GetString("edit.mentions"); mentions != "" {
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
		rawMentions := strings.Fields(v)
		uris := make([]uri.URI, 0, len(rawMentions))
		for _, m := range rawMentions {
			m = strings.TrimPrefix(m, "@")
			parts := strings.SplitN(m, ".", 2)
			if len(parts) == 2 {
				uris = append(uris, uri.URI{Scheme: "ctxt", Space: "entity", ID: parts[0] + "/" + parts[1]})
			} else {
				uris = append(uris, uri.URI{Scheme: "ctxt", Space: "entity", ID: m})
			}
		}
		obj.MentionURIs = uris
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
