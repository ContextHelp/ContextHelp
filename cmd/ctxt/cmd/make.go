package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var makeCmd = &cobra.Command{
	Use:   "make <type>",
	Short: "Generate compositions from knowledge",
	Long: `Generate compositions such as briefs, plans, summaries, or drafts.

Compositions are generated from your knowledge objects using templates
and your focus profile context.

Types:
  brief   - Generate executive brief
  plan    - Generate action plan
  summary - Generate summary
  draft   - Generate publish-ready draft

Examples:
  # Generate a brief
  ctxt make brief --tag ux,onboarding

  # Generate a plan with specific mentions
  ctxt make plan --mention @project.signup-redesign

  # Generate summary since a specific date
  ctxt make summary --since 2025-01-01

  # Generate draft and save to file
  ctxt make draft --tag launch --output launch-plan.md`,
	Args: cobra.ExactArgs(1),
	RunE: runMake,
}

func init() {
	rootCmd.AddCommand(makeCmd)

	// Filter flags
	makeCmd.Flags().String("mention", "", "focus on specific mentions")
	makeCmd.Flags().String("tag", "", "focus on specific tags (comma-separated)")
	makeCmd.Flags().String("since", "", "include knowledge since date (ISO)")

	// Output flags
	makeCmd.Flags().StringP("output-file", "o", "", "write to file")

	// Bind flags to viper
	viper.BindPFlag("make.mention", makeCmd.Flags().Lookup("mention"))
	viper.BindPFlag("make.tag", makeCmd.Flags().Lookup("tag"))
	viper.BindPFlag("make.since", makeCmd.Flags().Lookup("since"))
	viper.BindPFlag("make.output-file", makeCmd.Flags().Lookup("output-file"))
}

func runMake(cmd *cobra.Command, args []string) error {
	compositionType := args[0]

	// TODO: Implement actual make logic
	fmt.Printf("Generating %s composition\n\n", compositionType)
	fmt.Printf("Profile: %s\n", viper.GetString("profile.default"))
	fmt.Printf("Mention: %s\n", viper.GetString("make.mention"))
	fmt.Printf("Tags: %s\n", viper.GetString("make.tag"))
	fmt.Printf("Since: %s\n\n", viper.GetString("make.since"))

	// Sample output
	fmt.Println("=" + string(make([]byte, 60)) + "=")
	fmt.Printf("%s Composition\n", compositionType)
	fmt.Println("=" + string(make([]byte, 60)) + "=")
	fmt.Println()
	fmt.Println("## Summary")
	fmt.Println()
	fmt.Println("Based on recent knowledge objects related to UX and onboarding...")
	fmt.Println()
	fmt.Println("## Key Points")
	fmt.Println()
	fmt.Println("1. Authentication flow improvements identified")
	fmt.Println("2. Progressive disclosure patterns recommended")
	fmt.Println("3. Mobile-first design considerations documented")

	outputFile := viper.GetString("make.output-file")
	if outputFile != "" {
		fmt.Printf("\nSaving to: %s\n", outputFile)
	}

	return nil
}
