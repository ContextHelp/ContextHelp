package cmd

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
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
	Long: `Print the entity types, topic vocabulary, classification rules,
and version stamp for the named profile's metadata extraction schema.
Read-only.`,
	Args: cobra.ExactArgs(1),
	RunE: runSchemaShow,
}

var schemaAddTypeCmd = &cobra.Command{
	Use:   "add-type <profile> <type>",
	Short: "Add an entity type to the profile schema",
	Long: `Append a new entity type to the profile schema's allowed
vocabulary. Rejects duplicates. The schema version is bumped and the
change is persisted by rewriting the resolved config file.`,
	Args: cobra.ExactArgs(2),
	RunE: runSchemaAddType,
}

var schemaAddTopicCmd = &cobra.Command{
	Use:   "add-topic <profile> <type>",
	Short: "Add a topic to the profile schema vocabulary",
	Long: `Append a new topic to the profile schema's topic vocabulary.
Rejects duplicates. The schema version is bumped and the change is
persisted by rewriting the resolved config file.`,
	Args: cobra.ExactArgs(2),
	RunE: runSchemaAddTopic,
}

var schemaAddRuleCmd = &cobra.Command{
	Use:   "add-rule <profile> <pattern> <type>",
	Short: "Add a classification rule (regex pattern -> type)",
	Long: `Append a classification rule that maps a regex pattern to an
entity type. The pattern is compiled before persisting; invalid
regexes are rejected. The schema version is bumped and the change is
persisted by rewriting the resolved config file.`,
	Args: cobra.ExactArgs(3),
	RunE: runSchemaAddRule,
}

var schemaRemoveTypeCmd = &cobra.Command{
	Use:   "remove-type <profile> <type>",
	Short: "Remove an entity type from the profile schema",
	Long: `Drop the named entity type from the profile schema's allowed
vocabulary. Fails when the type is not present. The schema version
is bumped and the change is persisted by rewriting the resolved
config file.

Requires --confirm=yes (or --confirm=prompt for an interactive confirmation).`,
	Args: cobra.ExactArgs(2),
	RunE: runSchemaRemoveType,
}

var schemaRemoveTopicCmd = &cobra.Command{
	Use:   "remove-topic <profile> <topic>",
	Short: "Remove a topic from the profile schema vocabulary",
	Long: `Drop the named topic from the profile schema's topic
vocabulary. Fails when the topic is not present. The schema version
is bumped and the change is persisted by rewriting the resolved
config file.

Requires --confirm=yes (or --confirm=prompt for an interactive confirmation).`,
	Args: cobra.ExactArgs(2),
	RunE: runSchemaRemoveTopic,
}

var schemaEvolveCmd = &cobra.Command{
	Use:   "evolve <profile>",
	Short: "Suggest schema improvements from recent ingestions",
	Long: `Analyse recently-ingested content and suggest new entity types
or topics worth adding to the profile schema. Read-only against the
schema itself; suggestions are printed alongside the exact
'ctxt profile schema add-*' invocations that would apply them.`,
	Args: cobra.ExactArgs(1),
	RunE: runSchemaEvolve,
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

	cliconv.WithSideEffect(schemaShowCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(schemaShowCmd, []cliconv.Example{
		{Title: "Show schema for a profile", Command: "ctxt profile schema show founder"},
		{Title: "Show schema as JSON", Command: "ctxt profile schema show founder --json"},
	})

	cliconv.WithSideEffect(schemaAddTypeCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(schemaAddTypeCmd, cliconv.IdempotencyNo)
	cliconv.WithExamples(schemaAddTypeCmd, []cliconv.Example{
		{Title: "Add an entity type", Command: "ctxt profile schema add-type founder decision"},
		{Title: "Add then inspect", Command: "ctxt profile schema add-type founder risk && ctxt profile schema show founder"},
	})
	cliconv.WithNextSteps(schemaAddTypeCmd, []cliconv.NextStep{
		{Suggest: "ctxt profile schema show <profile>", Reason: "confirm the type landed and the version bumped"},
		{Suggest: "ctxt profile schema add-rule <profile> <pattern> <type>", Reason: "attach a regex rule so the new type is auto-classified"},
	})

	cliconv.WithSideEffect(schemaAddTopicCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(schemaAddTopicCmd, cliconv.IdempotencyNo)
	cliconv.WithExamples(schemaAddTopicCmd, []cliconv.Example{
		{Title: "Add a topic", Command: "ctxt profile schema add-topic founder security"},
		{Title: "Add then inspect", Command: "ctxt profile schema add-topic founder pricing && ctxt profile schema show founder"},
	})
	cliconv.WithNextSteps(schemaAddTopicCmd, []cliconv.NextStep{
		{Suggest: "ctxt profile schema show <profile>", Reason: "verify the topic was appended and the schema version bumped"},
		{Suggest: "ctxt profile schema evolve <profile>", Reason: "ask for more topic suggestions based on recent ingestions"},
	})

	cliconv.WithSideEffect(schemaAddRuleCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(schemaAddRuleCmd, cliconv.IdempotencyNo)
	cliconv.WithExamples(schemaAddRuleCmd, []cliconv.Example{
		{Title: "Map a regex to a type", Command: "ctxt profile schema add-rule founder \"(?i)deploy\" task"},
		{Title: "Add then inspect", Command: "ctxt profile schema add-rule founder \"(?i)decision\" decision && ctxt profile schema show founder"},
	})
	cliconv.WithNextSteps(schemaAddRuleCmd, []cliconv.NextStep{
		{Suggest: "ctxt profile schema show <profile>", Reason: "verify the rule landed in the classification list"},
		{Suggest: "ctxt profile schema add-type <profile> <type>", Reason: "ensure the rule's target type is in the allowed vocabulary"},
	})

	cliconv.WithSideEffect(schemaRemoveTypeCmd, cliconv.SideEffectDestructive)
	cliconv.WithDestructiveToken(schemaRemoveTypeCmd)
	cliconv.WithIdempotency(schemaRemoveTypeCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(schemaRemoveTypeCmd, []cliconv.Example{
		{Title: "Remove an entity type", Command: "ctxt profile schema remove-type founder decision --confirm=yes"},
		{Title: "Remove with interactive confirmation", Command: "ctxt profile schema remove-type founder risk --confirm=prompt"},
	})
	cliconv.WithNextSteps(schemaRemoveTypeCmd, []cliconv.NextStep{
		{Suggest: "ctxt profile schema show <profile>", Reason: "confirm the type was dropped and the schema version bumped"},
		{Suggest: "ctxt profile schema evolve <profile>", Reason: "see whether recent ingestions suggest a replacement type"},
	})

	cliconv.WithSideEffect(schemaRemoveTopicCmd, cliconv.SideEffectDestructive)
	cliconv.WithDestructiveToken(schemaRemoveTopicCmd)
	cliconv.WithIdempotency(schemaRemoveTopicCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(schemaRemoveTopicCmd, []cliconv.Example{
		{Title: "Remove a topic", Command: "ctxt profile schema remove-topic founder security --confirm=yes"},
		{Title: "Remove with interactive confirmation", Command: "ctxt profile schema remove-topic founder pricing --confirm=prompt"},
	})
	cliconv.WithNextSteps(schemaRemoveTopicCmd, []cliconv.NextStep{
		{Suggest: "ctxt profile schema show <profile>", Reason: "confirm the topic was dropped and the schema version bumped"},
		{Suggest: "ctxt profile schema evolve <profile>", Reason: "review fresh topic suggestions before re-adding"},
	})

	cliconv.WithSideEffect(schemaEvolveCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(schemaEvolveCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(schemaEvolveCmd, []cliconv.Example{
		{Title: "Suggest schema improvements", Command: "ctxt profile schema evolve founder"},
		{Title: "Emit suggestions as JSON", Command: "ctxt profile schema evolve founder --json"},
	})
	cliconv.WithNextSteps(schemaEvolveCmd, []cliconv.NextStep{
		{When: "for each suggested type", Suggest: "ctxt profile schema add-type <profile> <type>", Reason: "apply a recommended entity type"},
		{When: "for each suggested topic", Suggest: "ctxt profile schema add-topic <profile> <topic>", Reason: "apply a recommended topic"},
	})
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
