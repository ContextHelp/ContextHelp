package cmd

import (
	"fmt"
	"os"
	"sort"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage focus profiles",
	Long: `Manage focus profiles for contextual filtering and reranking.

Focus profiles allow you to tailor ContextHelp's behavior to specific
roles or projects (e.g., Founder, Engineer, Research).

Examples:
  # List all profiles
  ctxt profile list

  # View profile details
  ctxt profile view founder

  # Create a new profile
  ctxt profile create myproject

  # Make a profile the default
  ctxt profile set founder

  # Clear the default profile
  ctxt profile unset founder

  # Remove a profile
  ctxt profile rm myproject --confirm=yes`,
}

var profileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all profiles",
	Long: `List every focus profile defined in the active config plus the
currently-selected default. Read-only; the profile store is not
modified.`,
	RunE: runProfileList,
}

var profileViewCmd = &cobra.Command{
	Use:   "view <profile>",
	Short: "View profile details",
	Long: `Display the description, tags, mention namespaces, and rerank
boosts for a single focus profile. Read-only; no config writes.`,
	Args: cobra.ExactArgs(1),
	RunE: runProfileView,
}

var profileCreateCmd = &cobra.Command{
	Use:   "create <profile>",
	Short: "Create a new profile",
	Long: `Add a new focus profile to the active config with a placeholder
description. Fails if a profile of the same name already exists.
Writes back to the resolved config file.`,
	Args: cobra.ExactArgs(1),
	RunE: runProfileCreate,
}

var profileRmCmd = &cobra.Command{
	Use:   "rm <profile>",
	Short: "Remove a profile",
	Long: `Remove a focus profile from the active config. If the removed
profile was the default, the default is also cleared. The change is
persisted by rewriting the resolved config file.

Requires --confirm=yes (or --confirm=prompt for an interactive confirmation).`,
	Args: cobra.ExactArgs(1),
	RunE: runProfileRm,
}

var profileSetCmd = &cobra.Command{
	Use:   "set <profile>",
	Short: "Set the default profile",
	Long: `Pin the named profile as the default for filtering and
reranking. The profile must already exist. The change is persisted by
rewriting the resolved config file.

Use 'ctxt profile unset <profile>' to clear the default again.`,
	Args: cobra.ExactArgs(1),
	RunE: runProfileSet,
}

var profileUnsetCmd = &cobra.Command{
	Use:   "unset <profile>",
	Short: "Clear the default profile",
	Long: `Clear the default focus profile. The profile name is required
and must be the one currently pinned as default: naming it keeps the
invocation self-documenting in scripts and turns a stale command into
an error instead of silently clearing a default somebody else moved.

The profile itself is untouched — only the 'default' pointer is
cleared. Use 'ctxt profile rm <profile>' to remove the profile.`,
	Args: cobra.ExactArgs(1),
	RunE: runProfileUnset,
}

// Deprecated verb shims. The canonical surface is set/unset/view/list/rm
// (see the target-surface rationale: "default" is a noun, not a verb).
// ctxt is a published CLI, so the pre-rename spellings keep working and
// only emit a notice. Cobra writes the Deprecated banner through
// Command.Print → OutOrStderr(), i.e. stderr, which is what keeps
// `--format json` on stdout machine-parseable.
//
// Each shim delegates to the canonical RunE rather than re-implementing
// the behavior, so the two spellings can never drift.

var profileShowDeprecatedCmd = &cobra.Command{
	Use:        "show <profile>",
	Short:      "[deprecated] use 'ctxt profile view'",
	Long:       `Deprecated spelling of 'ctxt profile view'. Behaves identically and emits a deprecation notice on stderr.`,
	Deprecated: "use 'ctxt profile view' instead",
	Hidden:     true,
	Args:       cobra.ExactArgs(1),
	RunE:       runProfileView,
}

var profileDeleteDeprecatedCmd = &cobra.Command{
	Use:        "delete <profile>",
	Short:      "[deprecated] use 'ctxt profile rm'",
	Long:       `Deprecated spelling of 'ctxt profile rm'. Behaves identically and emits a deprecation notice on stderr.`,
	Deprecated: "use 'ctxt profile rm' instead",
	Hidden:     true,
	Args:       cobra.ExactArgs(1),
	RunE:       runProfileRm,
}

// profileDefaultDeprecatedCmd keeps the old dual-purpose semantics: with
// an argument it pins the default, without one it clears the default.
// The canonical replacement splits those two jobs across set/unset.
var profileDefaultDeprecatedCmd = &cobra.Command{
	Use:   "default [profile]",
	Short: "[deprecated] use 'ctxt profile set' / 'ctxt profile unset'",
	Long: `Deprecated spelling that both pins and clears the default
profile. With an argument it behaves like 'ctxt profile set <profile>';
with no argument it clears the default like 'ctxt profile unset'.
Emits a deprecation notice on stderr.`,
	Deprecated: "use 'ctxt profile set <profile>' to pin, or 'ctxt profile unset <profile>' to clear",
	Hidden:     true,
	Args:       cobra.MaximumNArgs(1),
	RunE:       runProfileDefaultDeprecated,
}

func init() {
	rootCmd.AddCommand(profileCmd)

	profileCmd.AddCommand(profileListCmd)
	profileCmd.AddCommand(profileViewCmd)
	profileCmd.AddCommand(profileCreateCmd)
	profileCmd.AddCommand(profileRmCmd)
	profileCmd.AddCommand(profileSetCmd)
	profileCmd.AddCommand(profileUnsetCmd)

	profileCmd.AddCommand(profileShowDeprecatedCmd)
	profileCmd.AddCommand(profileDeleteDeprecatedCmd)
	profileCmd.AddCommand(profileDefaultDeprecatedCmd)

	cliconv.WithSideEffect(profileListCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(profileListCmd, []cliconv.Example{
		{Title: "List all profiles", Command: "ctxt profile list"},
		{Title: "List profiles as JSON", Command: "ctxt profile list --format json"},
	})

	cliconv.WithSideEffect(profileViewCmd, cliconv.SideEffectRead)
	// "view" is absent from kit's default idempotency table (which knows
	// list/show/get/...), so the read class has to be stated outright.
	cliconv.WithIdempotency(profileViewCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(profileViewCmd, []cliconv.Example{
		{Title: "View a profile's details", Command: "ctxt profile view founder"},
		{Title: "View a profile as JSON", Command: "ctxt profile view founder --format json"},
	})

	cliconv.WithSideEffect(profileCreateCmd, cliconv.SideEffectWrite)
	cliconv.WithExamples(profileCreateCmd, []cliconv.Example{
		{Title: "Create a profile", Command: "ctxt profile create myproject"},
		{Title: "Create then inspect", Command: "ctxt profile create research && ctxt profile view research"},
	})
	cliconv.WithNextSteps(profileCreateCmd, []cliconv.NextStep{
		{Suggest: "ctxt profile view <profile>", Reason: "verify the new profile and its default fields"},
		{Suggest: "ctxt profile set <profile>", Reason: "promote the new profile to be the active default"},
	})

	cliconv.WithSideEffect(profileRmCmd, cliconv.SideEffectDestructive)
	cliconv.WithDestructiveToken(profileRmCmd)
	// "rm" is not in kit's default idempotency table ("delete" is), so the
	// replay class is declared here: removing an absent profile is an
	// error, but re-running against the same end state is safe.
	cliconv.WithIdempotency(profileRmCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(profileRmCmd, []cliconv.Example{
		{Title: "Remove a profile", Command: "ctxt profile rm myproject --confirm=yes"},
		{Title: "Remove with interactive confirmation", Command: "ctxt profile rm myproject --confirm=prompt"},
	})
	cliconv.WithNextSteps(profileRmCmd, []cliconv.NextStep{
		{Suggest: "ctxt profile list", Reason: "confirm the profile is gone and review remaining ones"},
		{When: "if it was the default", Suggest: "ctxt profile set <profile>", Reason: "the default was cleared; pin a new one"},
	})

	cliconv.WithSideEffect(profileSetCmd, cliconv.SideEffectWrite)
	// "set"/"unset" are absent from kit's default idempotency table.
	// Both converge on a fixed end state, so replay is safe.
	cliconv.WithIdempotency(profileSetCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(profileSetCmd, []cliconv.Example{
		{Title: "Pin a profile as default", Command: "ctxt profile set founder"},
		{Title: "Create then pin", Command: "ctxt profile create research && ctxt profile set research"},
	})
	cliconv.WithNextSteps(profileSetCmd, []cliconv.NextStep{
		{Suggest: "ctxt profile list", Reason: "confirm the default marker moved (* prefix)"},
		{Suggest: "ctxt profile view <profile>", Reason: "review the now-active profile schema and boosts"},
	})

	cliconv.WithSideEffect(profileUnsetCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(profileUnsetCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(profileUnsetCmd, []cliconv.Example{
		{Title: "Clear the default profile", Command: "ctxt profile unset founder"},
		{Title: "Clear then confirm", Command: "ctxt profile unset founder && ctxt profile list"},
	})
	cliconv.WithNextSteps(profileUnsetCmd, []cliconv.NextStep{
		{Suggest: "ctxt profile list", Reason: "confirm no profile carries the default (* prefix) marker"},
		{Suggest: "ctxt profile set <profile>", Reason: "pin a different profile as the new default"},
	})

	// The deprecated shims stay in the tree, so the strict 12fcc gates
	// apply to them too: side-effect, idempotency and guidance mirror
	// the canonical verb each one forwards to, and the examples point at
	// the replacement spelling so `--help` teaches the new surface.
	cliconv.WithSideEffect(profileShowDeprecatedCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(profileShowDeprecatedCmd, []cliconv.Example{
		{Title: "Deprecated; prefer view", Command: "ctxt profile view founder"},
		{Title: "Deprecated spelling", Command: "ctxt profile show founder"},
	})

	cliconv.WithSideEffect(profileDeleteDeprecatedCmd, cliconv.SideEffectDestructive)
	cliconv.WithDestructiveToken(profileDeleteDeprecatedCmd)
	cliconv.WithExamples(profileDeleteDeprecatedCmd, []cliconv.Example{
		{Title: "Deprecated; prefer rm", Command: "ctxt profile rm myproject --confirm=yes"},
		{Title: "Deprecated spelling", Command: "ctxt profile delete myproject --confirm=yes"},
	})
	cliconv.WithNextSteps(profileDeleteDeprecatedCmd, []cliconv.NextStep{
		{Suggest: "ctxt profile rm <profile> --confirm=yes", Reason: "move scripts to the canonical destructive verb"},
	})

	cliconv.WithSideEffect(profileDefaultDeprecatedCmd, cliconv.SideEffectWrite)
	cliconv.WithExamples(profileDefaultDeprecatedCmd, []cliconv.Example{
		{Title: "Deprecated; prefer set", Command: "ctxt profile set founder"},
		{Title: "Deprecated; prefer unset", Command: "ctxt profile unset founder"},
	})
	cliconv.WithNextSteps(profileDefaultDeprecatedCmd, []cliconv.NextStep{
		{When: "to pin a default", Suggest: "ctxt profile set <profile>", Reason: "move scripts to the canonical verb"},
		{When: "to clear the default", Suggest: "ctxt profile unset <profile>", Reason: "move scripts to the canonical verb"},
	})
}

func configPath() string {
	if cfgFile != "" {
		return cfgFile
	}
	return config.GetConfigPath(binName)
}

func runProfileList(cmd *cobra.Command, args []string) error {
	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"default":  cfg.Profile.Default,
			"profiles": cfg.Profile.Profiles,
		})
	}

	// csv/text/table project from profileRow's tags. The default
	// profile travels as a DEFAULT column rather than the human view's
	// "*" prefix: a marker glued onto the name would corrupt the name
	// field for anything parsing it.
	if cliformat.Rows() {
		return cliformat.DispatchRows(cmd, cmd.OutOrStdout(), profileRows())
	}

	fmt.Println("Focus Profiles:")
	fmt.Println()
	defaultProfile := cfg.Profile.Default
	if defaultProfile == "" {
		defaultProfile = "(none)"
	}
	fmt.Printf("  Default profile: %s\n", defaultProfile)
	fmt.Println()

	if len(cfg.Profile.Profiles) == 0 {
		fmt.Println("  No profiles defined.")
	} else {
		names := make([]string, 0, len(cfg.Profile.Profiles))
		for name := range cfg.Profile.Profiles {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			p := cfg.Profile.Profiles[name]
			prefix := "  "
			if name == cfg.Profile.Default {
				prefix = "* "
			}
			fmt.Printf("%s%s", prefix, name)
			if p.Description != "" {
				fmt.Printf(" - %s", p.Description)
			}
			fmt.Println()
		}
	}

	fmt.Println()
	fmt.Println("  Configure profiles in your config file:")
	fmt.Printf("  %s\n", config.GetConfigPath(binName))
	return nil
}

// Tabular views spell a boolean this way, and have since before the
// non-human formats existed; the constants keep the two spellings in
// one place.
const (
	tabularYes = "yes"
	tabularNo  = "no"
)

// yesNo renders a boolean in that spelling.
func yesNo(b bool) string {
	if b {
		return tabularYes
	}
	return tabularNo
}

// profileRow is the row shape the tabular formats project from.
type profileRow struct {
	Name        string `table:"NAME"`
	Default     string `table:"DEFAULT"`
	Description string `table:"DESCRIPTION"`
}

// profileRows projects configured profiles onto the row shape, sorted
// by name so repeated runs are byte-identical — Go map iteration is
// randomized, and an unstable row order would make the output
// undiffable.
func profileRows() []profileRow {
	names := make([]string, 0, len(cfg.Profile.Profiles))
	for name := range cfg.Profile.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := make([]profileRow, 0, len(names))
	for _, name := range names {
		rows = append(rows, profileRow{
			Name:        name,
			Default:     yesNo(name == cfg.Profile.Default),
			Description: cfg.Profile.Profiles[name].Description,
		})
	}
	return rows
}

func runProfileView(cmd *cobra.Command, args []string) error {
	name := args[0]
	p, ok := cfg.Profile.Profiles[name]
	if !ok {
		return fmt.Errorf("profile not found: %s", name)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"name":       name,
			"is_default": cfg.Profile.Default == name,
			"profile":    p,
		})
	}

	fmt.Printf("Profile: %s\n", name)
	if cfg.Profile.Default == name {
		fmt.Println("  (default profile)")
	}
	if p.Description != "" {
		fmt.Printf("  Description: %s\n", p.Description)
	}
	if len(p.Tags) > 0 {
		fmt.Printf("  Tags: %v\n", p.Tags)
	}
	if len(p.MentionNamespaces) > 0 {
		fmt.Printf("  Mention Namespaces: %v\n", p.MentionNamespaces)
	}
	if len(p.RerankBoosts) > 0 {
		fmt.Println("  Rerank Boosts:")
		keys := make([]string, 0, len(p.RerankBoosts))
		for k := range p.RerankBoosts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("    %s: %.2f\n", k, p.RerankBoosts[k])
		}
	}
	return nil
}

func runProfileCreate(cmd *cobra.Command, args []string) error {
	name := args[0]

	if cfg.Profile.Profiles == nil {
		cfg.Profile.Profiles = make(map[string]config.FocusProfile)
	}

	if _, exists := cfg.Profile.Profiles[name]; exists {
		// The name is taken: kit's CONFLICT (exit 4, permanent). A
		// retry of the identical command cannot clear it, so the
		// envelope names the two ways out instead.
		e := output.ConflictError(fmt.Sprintf("profile already exists: %s", name))
		e.SuggestedFix = "choose a different name, or run `ctxt profile show " +
			name + "` to inspect the existing one"
		return e
	}

	cfg.Profile.Profiles[name] = config.FocusProfile{
		Description: fmt.Sprintf("Profile for %s", name),
	}

	if err := config.WriteBack(cfg, configPath()); err != nil {
		return fmt.Errorf("failed to save profile: %w", err)
	}

	// A structured caller gets the identity of what was just created.
	// The confirmation sentence is unparseable to the caller that asked
	// for a document, and it forces the name to be recovered by regex
	// from prose the renderer is free to reword.
	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"name":    name,
			"created": true,
		})
	}

	fmt.Printf("Created profile: %s\n", name)
	return nil
}

func runProfileRm(cmd *cobra.Command, args []string) error {
	name := args[0]

	if _, exists := cfg.Profile.Profiles[name]; !exists {
		return fmt.Errorf("profile not found: %s", name)
	}

	delete(cfg.Profile.Profiles, name)
	if cfg.Profile.Default == name {
		cfg.Profile.Default = ""
	}

	if err := config.WriteBack(cfg, configPath()); err != nil {
		return fmt.Errorf("failed to remove profile: %w", err)
	}

	// Same contract as create: the write path answers in the form it
	// was asked for, naming the object it acted on.
	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"name":    name,
			"removed": true,
		})
	}

	fmt.Printf("Removed profile: %s\n", name)
	return nil
}

func runProfileSet(cmd *cobra.Command, args []string) error {
	name := args[0]
	if _, exists := cfg.Profile.Profiles[name]; !exists {
		return fmt.Errorf("profile not found: %s", name)
	}
	return writeDefaultProfile(name)
}

// runProfileUnset clears the default pointer. The named profile must be
// the one currently pinned: a mismatch means the caller's assumption
// about the config is stale, and clearing anyway would silently discard
// a default they never meant to touch.
func runProfileUnset(cmd *cobra.Command, args []string) error {
	name := args[0]
	if _, exists := cfg.Profile.Profiles[name]; !exists {
		return fmt.Errorf("profile not found: %s", name)
	}
	if cfg.Profile.Default != name {
		if cfg.Profile.Default == "" {
			return fmt.Errorf(
				"profile %s is not the default: no default profile is set", name)
		}
		return fmt.Errorf(
			"profile %s is not the default: the default is %s",
			name, cfg.Profile.Default)
	}
	return writeDefaultProfile("")
}

// runProfileDefaultDeprecated preserves the pre-rename dual behavior of
// `ctxt profile default [name]`: pin with an argument, clear without.
func runProfileDefaultDeprecated(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return writeDefaultProfile("")
	}
	return runProfileSet(cmd, args)
}

// writeDefaultProfile persists cfg.Profile.Default and reports the
// change. An empty name clears the default.
func writeDefaultProfile(name string) error {
	cfg.Profile.Default = name

	if err := config.WriteBack(cfg, configPath()); err != nil {
		return fmt.Errorf("failed to set default profile: %w", err)
	}

	// The resulting selection travels on the same `default` field the
	// list view reports it on, so a reconciler can re-assert and
	// compare the answer without knowing which verb produced it. The
	// cleared case is the empty string rather than an omitted field:
	// "no default" is an answer, and dropping the key would make it
	// indistinguishable from a command that reported nothing.
	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"default": name,
		})
	}

	if name == "" {
		fmt.Println("Cleared default profile")
	} else {
		fmt.Printf("Set default profile to: %s\n", name)
	}
	return nil
}
