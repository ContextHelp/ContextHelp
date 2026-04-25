package cmd

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
)

var detectorCmd = &cobra.Command{
	Use:   "detector",
	Short: "Manage pipeline detectors",
	Long: `Add, list, remove, enable, and disable pipeline detectors.

Detectors determine which pipeline is selected for incoming content.

Examples:
  # Add an extension-based detector
  dpkms detector add --kind extension --pattern .pdf --pipeline doc.pdf --name "PDF files"

  # Add a URL-pattern detector
  dpkms detector add --kind url_pattern --pattern "github\\.com" --pipeline doc.code --name "GitHub URLs"

  # List all detectors
  dpkms detector list

  # Disable a detector
  dpkms detector disable <id>

  # Enable a detector
  dpkms detector enable <id>

  # Remove a detector
  dpkms detector remove <id>`,
}

var detectorAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a detector",
	Long:  `Add a new pipeline detector.`,
	RunE:  runDetectorAdd,
}

var detectorListCmd = &cobra.Command{
	Use:   "list",
	Short: "List detectors",
	Long:  `List all pipeline detectors.`,
	RunE:  runDetectorList,
}

var detectorRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Remove a detector",
	Long:  `Remove a detector by ID.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runDetectorRemove,
}

var detectorEnableCmd = &cobra.Command{
	Use:   "enable <id>",
	Short: "Enable a detector",
	Long:  `Enable a previously disabled detector.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runDetectorEnable,
}

var detectorDisableCmd = &cobra.Command{
	Use:   "disable <id>",
	Short: "Disable a detector",
	Long:  `Disable a detector without removing it.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runDetectorDisable,
}

func init() {
	rootCmd.AddCommand(detectorCmd)

	detectorCmd.AddCommand(detectorAddCmd)
	detectorCmd.AddCommand(detectorListCmd)
	detectorCmd.AddCommand(detectorRemoveCmd)
	detectorCmd.AddCommand(detectorEnableCmd)
	detectorCmd.AddCommand(detectorDisableCmd)

	detectorAddCmd.Flags().String("kind", "",
		"detector kind: extension, url_pattern, content_test (required)")
	detectorAddCmd.Flags().String("pattern", "",
		"pattern to match (extension like .pdf, or regexp)")
	detectorAddCmd.Flags().String("pipeline", "",
		"pipeline name to route to (required)")
	detectorAddCmd.Flags().String("name", "", "human-readable label")
	detectorAddCmd.Flags().Int("priority", 100,
		"priority (lower = higher, default 100)")
	detectorAddCmd.MarkFlagRequired("kind")
	detectorAddCmd.MarkFlagRequired("pipeline")

	detectorListCmd.Flags().String("kind", "", "filter by kind")
	detectorListCmd.Flags().Bool("enabled", false, "show only enabled detectors")
	detectorListCmd.Flags().Bool("disabled", false, "show only disabled detectors")
}

func runDetectorAdd(cmd *cobra.Command, _ []string) error {
	kind, _ := cmd.Flags().GetString("kind")
	pattern, _ := cmd.Flags().GetString("pattern")
	pipelineName, _ := cmd.Flags().GetString("pipeline")
	name, _ := cmd.Flags().GetString("name")
	priority, _ := cmd.Flags().GetInt("priority")

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	d, err := svc.CreateDetector(context.Background(), service.DetectorCreateRequest{
		Kind:         storage.DetectorKind(kind),
		Name:         name,
		PipelineName: pipelineName,
		Pattern:      pattern,
		Priority:     priority,
	})
	if err != nil {
		return fmt.Errorf("add detector: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), d)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Detector ID: %s\n", d.ID)
	return nil
}

func runDetectorList(cmd *cobra.Command, _ []string) error {
	kindStr, _ := cmd.Flags().GetString("kind")
	showEnabled, _ := cmd.Flags().GetBool("enabled")
	showDisabled, _ := cmd.Flags().GetBool("disabled")

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	filter := storage.DetectorFilter{}
	if kindStr != "" {
		filter.Kind = storage.DetectorKind(kindStr)
	}
	if showEnabled {
		t := true
		filter.Enabled = &t
	} else if showDisabled {
		f := false
		filter.Enabled = &f
	}

	detectors, err := svc.ListDetectors(context.Background(), filter)
	if err != nil {
		return fmt.Errorf("list detectors: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), detectors)
	}

	headers := []string{"ID", "KIND", "NAME", "PIPELINE", "PATTERN", "PRIORITY", "ENABLED"}
	rows := make([][]string, 0, len(detectors))
	for _, d := range detectors {
		enabled := "yes"
		if !d.Enabled {
			enabled = "no"
		}
		rows = append(rows, []string{
			d.ID, string(d.Kind), d.Name, d.PipelineName, d.Pattern,
			fmt.Sprintf("%d", d.Priority), enabled,
		})
	}
	printAdminTable(cmd.OutOrStdout(), headers, rows)
	return nil
}

func runDetectorRemove(cmd *cobra.Command, args []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	if err := svc.DeleteDetector(context.Background(), args[0]); err != nil {
		return fmt.Errorf("remove detector: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed detector %s\n", args[0])
	return nil
}

func runDetectorEnable(cmd *cobra.Command, args []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	if err := svc.EnableDetector(context.Background(), args[0]); err != nil {
		return fmt.Errorf("enable detector: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Enabled detector %s\n", args[0])
	return nil
}

func runDetectorDisable(cmd *cobra.Command, args []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	if err := svc.DisableDetector(context.Background(), args[0]); err != nil {
		return fmt.Errorf("disable detector: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Disabled detector %s\n", args[0])
	return nil
}
