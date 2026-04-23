package cmd

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/spf13/cobra"
)

var profileSchemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Manage per-profile schema vocabulary",
	Long: `Manage entity types, topic vocabulary, and classification rules
for a focus profile's metadata extraction schema.

Examples:
  ctxt profile schema show founder
  ctxt profile schema add-type founder decision
  ctxt profile schema add-topic founder security
  ctxt profile schema add-rule founder "(?i)deploy" task
  ctxt profile schema remove-type founder decision
  ctxt profile schema remove-topic founder security
  ctxt profile schema evolve founder`,
}

var schemaShowCmd = &cobra.Command{
	Use:   "show <profile>",
	Short: "Display current schema for a profile",
	Args:  cobra.ExactArgs(1),
	RunE:  runSchemaShow,
}

var schemaAddTypeCmd = &cobra.Command{
	Use:   "add-type <profile> <type>",
	Short: "Add an entity type to the profile schema",
	Args:  cobra.ExactArgs(2),
	RunE:  runSchemaAddType,
}

var schemaAddTopicCmd = &cobra.Command{
	Use:   "add-topic <profile> <type>",
	Short: "Add a topic to the profile schema vocabulary",
	Args:  cobra.ExactArgs(2),
	RunE:  runSchemaAddTopic,
}

var schemaAddRuleCmd = &cobra.Command{
	Use:   "add-rule <profile> <pattern> <type>",
	Short: "Add a classification rule (regex pattern -> type)",
	Args:  cobra.ExactArgs(3),
	RunE:  runSchemaAddRule,
}

var schemaRemoveTypeCmd = &cobra.Command{
	Use:   "remove-type <profile> <type>",
	Short: "Remove an entity type from the profile schema",
	Args:  cobra.ExactArgs(2),
	RunE:  runSchemaRemoveType,
}

var schemaRemoveTopicCmd = &cobra.Command{
	Use:   "remove-topic <profile> <topic>",
	Short: "Remove a topic from the profile schema vocabulary",
	Args:  cobra.ExactArgs(2),
	RunE:  runSchemaRemoveTopic,
}

var schemaEvolveCmd = &cobra.Command{
	Use:   "evolve <profile>",
	Short: "Suggest schema improvements from recent ingestions",
	Args:  cobra.ExactArgs(1),
	RunE:  runSchemaEvolve,
}

func init() {
	profileCmd.AddCommand(profileSchemaCmd)
	profileSchemaCmd.AddCommand(schemaShowCmd)
	profileSchemaCmd.AddCommand(schemaAddTypeCmd)
	profileSchemaCmd.AddCommand(schemaAddTopicCmd)
	profileSchemaCmd.AddCommand(schemaAddRuleCmd)
	profileSchemaCmd.AddCommand(schemaRemoveTypeCmd)
	profileSchemaCmd.AddCommand(schemaRemoveTopicCmd)
	profileSchemaCmd.AddCommand(schemaEvolveCmd)
}

func getProfile(name string) (*config.FocusProfile, error) {
	p, ok := cfg.Profile.Profiles[name]
	if !ok {
		return nil, fmt.Errorf("profile not found: %s", name)
	}
	return &p, nil
}

func saveProfile(name string, p config.FocusProfile) error {
	if cfg.Profile.Profiles == nil {
		cfg.Profile.Profiles = make(map[string]config.FocusProfile)
	}
	cfg.Profile.Profiles[name] = p
	return config.WriteBack(cfg, configPath())
}

func runSchemaShow(cmd *cobra.Command, args []string) error {
	name := args[0]
	p, err := getProfile(name)
	if err != nil {
		return err
	}

	s := p.Schema
	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"profile": name,
			"schema":  s,
		})
	}

	fmt.Printf("Schema for profile %q (version %d):\n\n", name, s.Version)

	fmt.Println("  Entity Types:")
	if len(s.EntityTypes) == 0 {
		fmt.Println("    (none — uses global defaults)")
	} else {
		sorted := make([]string, len(s.EntityTypes))
		copy(sorted, s.EntityTypes)
		sort.Strings(sorted)
		for _, t := range sorted {
			fmt.Printf("    - %s\n", t)
		}
	}

	fmt.Println()
	fmt.Println("  Topic Vocabulary:")
	if len(s.TopicVocabulary) == 0 {
		fmt.Println("    (none — unconstrained)")
	} else {
		sorted := make([]string, len(s.TopicVocabulary))
		copy(sorted, s.TopicVocabulary)
		sort.Strings(sorted)
		for _, t := range sorted {
			fmt.Printf("    - %s\n", t)
		}
	}

	fmt.Println()
	fmt.Println("  Classification Rules:")
	if len(s.ClassificationRules) == 0 {
		fmt.Println("    (none)")
	} else {
		for _, r := range s.ClassificationRules {
			fmt.Printf("    - %s -> %s\n", r.Pattern, r.Type)
		}
	}

	return nil
}

func runSchemaAddType(cmd *cobra.Command, args []string) error {
	name, typ := args[0], strings.ToLower(args[1])
	p, err := getProfile(name)
	if err != nil {
		return err
	}

	for _, t := range p.Schema.EntityTypes {
		if t == typ {
			return fmt.Errorf("entity type %q already exists in profile %q", typ, name)
		}
	}

	p.Schema.EntityTypes = append(p.Schema.EntityTypes, typ)
	p.Schema.Version++

	if err := saveProfile(name, *p); err != nil {
		return fmt.Errorf("failed to save schema: %w", err)
	}

	fmt.Printf("Added entity type %q to profile %q (v%d)\n",
		typ, name, p.Schema.Version)
	return nil
}

func runSchemaAddTopic(cmd *cobra.Command, args []string) error {
	name, topic := args[0], strings.ToLower(args[1])
	p, err := getProfile(name)
	if err != nil {
		return err
	}

	for _, t := range p.Schema.TopicVocabulary {
		if t == topic {
			return fmt.Errorf("topic %q already exists in profile %q", topic, name)
		}
	}

	p.Schema.TopicVocabulary = append(p.Schema.TopicVocabulary, topic)
	p.Schema.Version++

	if err := saveProfile(name, *p); err != nil {
		return fmt.Errorf("failed to save schema: %w", err)
	}

	fmt.Printf("Added topic %q to profile %q (v%d)\n",
		topic, name, p.Schema.Version)
	return nil
}

func runSchemaAddRule(cmd *cobra.Command, args []string) error {
	name, pattern, typ := args[0], args[1], strings.ToLower(args[2])
	p, err := getProfile(name)
	if err != nil {
		return err
	}

	// Validate pattern compiles.
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("invalid regex pattern %q: %w", pattern, err)
	}

	p.Schema.ClassificationRules = append(p.Schema.ClassificationRules,
		config.ClassificationRule{Pattern: pattern, Type: typ})
	p.Schema.Version++

	if err := saveProfile(name, *p); err != nil {
		return fmt.Errorf("failed to save schema: %w", err)
	}

	fmt.Printf("Added classification rule %q -> %q to profile %q (v%d)\n",
		pattern, typ, name, p.Schema.Version)
	return nil
}

func runSchemaRemoveType(cmd *cobra.Command, args []string) error {
	name, typ := args[0], strings.ToLower(args[1])
	p, err := getProfile(name)
	if err != nil {
		return err
	}

	found := false
	filtered := p.Schema.EntityTypes[:0]
	for _, t := range p.Schema.EntityTypes {
		if t == typ {
			found = true
			continue
		}
		filtered = append(filtered, t)
	}
	if !found {
		return fmt.Errorf("entity type %q not found in profile %q", typ, name)
	}

	p.Schema.EntityTypes = filtered
	p.Schema.Version++

	if err := saveProfile(name, *p); err != nil {
		return fmt.Errorf("failed to save schema: %w", err)
	}

	fmt.Printf("Removed entity type %q from profile %q (v%d)\n",
		typ, name, p.Schema.Version)
	return nil
}

func runSchemaRemoveTopic(cmd *cobra.Command, args []string) error {
	name, topic := args[0], strings.ToLower(args[1])
	p, err := getProfile(name)
	if err != nil {
		return err
	}

	found := false
	filtered := p.Schema.TopicVocabulary[:0]
	for _, t := range p.Schema.TopicVocabulary {
		if t == topic {
			found = true
			continue
		}
		filtered = append(filtered, t)
	}
	if !found {
		return fmt.Errorf("topic %q not found in profile %q", topic, name)
	}

	p.Schema.TopicVocabulary = filtered
	p.Schema.Version++

	if err := saveProfile(name, *p); err != nil {
		return fmt.Errorf("failed to save schema: %w", err)
	}

	fmt.Printf("Removed topic %q from profile %q (v%d)\n",
		topic, name, p.Schema.Version)
	return nil
}

func runSchemaEvolve(cmd *cobra.Command, args []string) error {
	name := args[0]
	p, err := getProfile(name)
	if err != nil {
		return err
	}

	svc, cleanup, err := newService()
	if err != nil {
		return fmt.Errorf("failed to init service: %w", err)
	}
	defer cleanup()

	suggestions, err := svc.SuggestSchemaEvolution(
		cmd.Context(), name, p.Schema)
	if err != nil {
		return fmt.Errorf("evolution analysis failed: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, suggestions)
	}

	if len(suggestions.NewTypes) == 0 &&
		len(suggestions.NewTopics) == 0 {
		fmt.Printf("No schema suggestions for profile %q.\n", name)
		return nil
	}

	fmt.Printf("Schema evolution suggestions for profile %q:\n\n", name)

	if len(suggestions.NewTypes) > 0 {
		fmt.Println("  Suggested entity types:")
		for _, s := range suggestions.NewTypes {
			fmt.Printf("    - %s (seen %d times)\n", s.Value, s.Count)
		}
	}

	if len(suggestions.NewTopics) > 0 {
		fmt.Println("  Suggested topics:")
		for _, s := range suggestions.NewTopics {
			fmt.Printf("    - %s (seen %d times)\n", s.Value, s.Count)
		}
	}

	fmt.Println()
	fmt.Println("  Apply with:")
	for _, s := range suggestions.NewTypes {
		fmt.Printf("    ctxt profile schema add-type %s %s\n", name, s.Value)
	}
	for _, s := range suggestions.NewTopics {
		fmt.Printf("    ctxt profile schema add-topic %s %s\n", name, s.Value)
	}

	return nil
}
