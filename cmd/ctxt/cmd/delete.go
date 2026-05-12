package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete knowledge objects",
	Long: `Delete knowledge objects using IDs or filters.

WARNING: This operation is irreversible. Use with caution.

Examples:
  # Delete specific object
  ctxt delete --id obj_12345678

  # Delete by tag
  ctxt delete --tagged temporary

  # Delete by mention
  ctxt delete --mention @project.archived

  # Delete all (requires confirmation)
  ctxt delete --all`,
	RunE: runDelete,
}

func init() {
	rootCmd.AddCommand(deleteCmd)
	cliconv.WithSideEffect(deleteCmd, cliconv.SideEffectDestructive)
	// "delete" is in kit's defaultIdempotency table (yes); deleting an
	// already-deleted object is a no-op, so the verb default matches.

	// Filter flags
	deleteCmd.Flags().String("id", "", "delete specific knowledge object")
	deleteCmd.Flags().String("index", "", "delete by list index (comma-separated)")
	deleteCmd.Flags().String("tagged", "", "delete by tag (comma-separated)")
	deleteCmd.Flags().String("hint", "", "delete by hint")
	deleteCmd.Flags().String("mention", "", "delete by mention")
	deleteCmd.Flags().String("type", "", "delete by type")
	deleteCmd.Flags().String("subtype", "", "delete by subtype")
	deleteCmd.Flags().Bool("all", false, "delete all (requires confirmation)")

	// Confirmation flags
	deleteCmd.Flags().BoolP("yes", "y", false, "skip confirmation prompt")

	// Bind flags to viper
	viper.BindPFlag("delete.id", deleteCmd.Flags().Lookup("id"))
	viper.BindPFlag("delete.index", deleteCmd.Flags().Lookup("index"))
	viper.BindPFlag("delete.tagged", deleteCmd.Flags().Lookup("tagged"))
	viper.BindPFlag("delete.hint", deleteCmd.Flags().Lookup("hint"))
	viper.BindPFlag("delete.mention", deleteCmd.Flags().Lookup("mention"))
	viper.BindPFlag("delete.type", deleteCmd.Flags().Lookup("type"))
	viper.BindPFlag("delete.subtype", deleteCmd.Flags().Lookup("subtype"))
	viper.BindPFlag("delete.all", deleteCmd.Flags().Lookup("all"))
	viper.BindPFlag("delete.yes", deleteCmd.Flags().Lookup("yes"))
}

func runDelete(cmd *cobra.Command, args []string) error {
	id := viper.GetString("delete.id")
	tag := viper.GetString("delete.tagged")
	mention := viper.GetString("delete.mention")
	typ := viper.GetString("delete.type")
	deleteAll := viper.GetBool("delete.all")
	skipConfirmation := viper.GetBool("delete.yes")

	if id == "" && tag == "" && mention == "" && typ == "" && !deleteAll {
		return fmt.Errorf("no filter specified; use --id, --tagged, --mention, --type, or --all")
	}

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()

	// If deleting by specific ID, just delete it directly
	if id != "" {
		if !skipConfirmation {
			fmt.Printf("Delete object %s? (y/N): ", id)
			var response string
			fmt.Scanln(&response)
			if response != "y" && response != "Y" {
				fmt.Println("Deletion cancelled.")
				return nil
			}
		}
		if err := svc.DeleteObject(ctx, id); err != nil {
			return fmt.Errorf("delete: %w", err)
		}
		fmt.Printf("Deleted %s\n", id)
		return nil
	}

	// Otherwise, list matching objects first
	filter := storage.ObjectFilter{
		Type:    typ,
		Tag:     tag,
		Mention: mention,
		Limit:   1000,
	}
	if deleteAll {
		filter = storage.ObjectFilter{Limit: 10000}
	}

	objects, total, err := svc.ListObjects(ctx, filter)
	if err != nil {
		return fmt.Errorf("list objects: %w", err)
	}

	if total == 0 {
		fmt.Println("No matching objects found.")
		return nil
	}

	fmt.Printf("Found %d objects to delete.\n", total)
	if !skipConfirmation {
		fmt.Print("Proceed? (y/N): ")
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Deletion cancelled.")
			return nil
		}
	}

	deleted := 0
	for _, obj := range objects {
		if err := svc.DeleteObject(ctx, obj.ID); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: failed to delete %s: %v\n", obj.ID, err)
			continue
		}
		deleted++
	}
	fmt.Printf("%d objects deleted\n", deleted)
	return nil
}
