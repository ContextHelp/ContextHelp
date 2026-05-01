package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/cli"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var showCmd = &cobra.Command{
	Use:   "show [id]",
	Short: "Display knowledge object details",
	Long: `Display detailed information about a knowledge object.

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

	// Display flags
	showCmd.Flags().Bool("raw", false, "show raw object data")
	showCmd.Flags().String("format", "", "output-generator plugin name (e.g. obsidian-md)")

	// Bind flags to viper
	viper.BindPFlag("show.raw", showCmd.Flags().Lookup("raw"))
	viper.BindPFlag("show.format", showCmd.Flags().Lookup("format"))
}

func runShow(cmd *cobra.Command, args []string) error {
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

	// Delegate to output-generator plugin when --format is specified.
	if format := viper.GetString("show.format"); format != "" {
		gen := findOutputGenerator(svc.PluginRegistry, format)
		if gen == nil {
			return fmt.Errorf("no output-generator plugin loaded for format %q", format)
		}
		if !gen.Accepts(*obj) {
			return fmt.Errorf("plugin %q does not accept objects of type %q", format, obj.Type)
		}
		out, err := gen.Generate(ctx, *obj, pluginapi.OutputOptions{})
		if err != nil {
			return fmt.Errorf("generate %q: %w", format, err)
		}
		_, err = os.Stdout.Write(out)
		return err
	}

	if isJSONOutput() || viper.GetBool("show.raw") {
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

	// See Also: related objects via shared mention targets (depth=1, max 5).
	related, err := svc.RelatedObjects(ctx, obj.ID, 1, 5)
	if err == nil && len(related) > 0 {
		fmt.Println()
		fmt.Println("See Also:")
		for _, r := range related {
			label := koLabel(r)
			fmt.Printf("  %s\n", label)
		}
	}

	return nil
}
