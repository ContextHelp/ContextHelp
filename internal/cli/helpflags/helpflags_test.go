package helpflags

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
)

func noop(*cobra.Command, []string) error { return nil }

// tree builds app with a persistent --format, a "get" leaf declaring
// --id, and a "rm" leaf carrying kit/destructive-token whose Long names
// --confirm-token. Each test adds the help text under test.
func tree(t *testing.T) (root, get, rm *cobra.Command) {
	t.Helper()
	root = &cobra.Command{Use: "app"}
	root.PersistentFlags().String("format", "", "")
	root.PersistentFlags().Bool("dry-run", false, "")
	get = &cobra.Command{Use: "get", RunE: noop}
	get.Flags().String("id", "", "")
	rm = &cobra.Command{Use: "rm", RunE: noop, Long: "Needs --confirm-token=<sha>."}
	kitcli.SetDestructiveToken(rm)
	root.AddCommand(get, rm)
	return root, get, rm
}

func examples(t *testing.T, c *cobra.Command, cmds ...string) {
	t.Helper()
	var ex []kitcli.Example
	for _, s := range cmds {
		ex = append(ex, kitcli.Example{Title: "x", Command: s})
	}
	if err := kitcli.SetExamples(c, ex); err != nil {
		t.Fatal(err)
	}
}

func wantProblems(t *testing.T, root *cobra.Command, subs ...string) {
	t.Helper()
	got := Check(root)
	if len(got) != len(subs) {
		t.Fatalf("got %d problems, want %d:\n%s", len(got), len(subs), strings.Join(got, "\n"))
	}
	for i, s := range subs {
		if !strings.Contains(got[i], s) {
			t.Errorf("problem %d = %q, want it to mention %q", i, got[i], s)
		}
	}
}

func TestCheck_CleanTree(t *testing.T) {
	root, get, rm := tree(t)
	examples(t, get, "app get --id 1 --format json", "app get --id=1 && app get --help")
	examples(t, rm, "app rm --confirm-token=<sha>", "app rm --dry-run")
	get.Long = "Pass --id.\n\nExamples:\n  app get --id 1\n  $ app get --format=json # as JSON"
	root.Long = "Run `app get --id 1`; see \"systemctl --user status\"."
	wantProblems(t, root)
}

func TestCheck_UndeclaredFlagInExample(t *testing.T) {
	root, get, _ := tree(t)
	examples(t, get, "app get --json")
	wantProblems(t, root, `passes --json to "app get"`)
}

func TestCheck_UndeclaredFlagInIndentedLong(t *testing.T) {
	root, get, _ := tree(t)
	get.Long = "Examples:\n  app get --json"
	wantProblems(t, root, `"app get" Long passes --json`)
}

func TestCheck_UndeclaredFlagInNextStep(t *testing.T) {
	root, get, _ := tree(t)
	if err := kitcli.SetNextSteps(get, []kitcli.NextStep{{Suggest: "app get --json"}}); err != nil {
		t.Fatal(err)
	}
	wantProblems(t, root, `kit/next-steps passes --json`)
}

func TestCheck_UndeclaredFlagInQuotedProse(t *testing.T) {
	root, get, _ := tree(t)
	get.Long = "Run `app get --json` to script it."
	wantProblems(t, root, `passes --json to "app get"`)
}

func TestCheck_UnknownFlagInProse(t *testing.T) {
	root, get, _ := tree(t)
	get.Long = "With --json the output is JSON; --id and --<name> are fine."
	wantProblems(t, root, `mentions --json, which no app command declares`)
}

func TestCheck_UnknownCommand(t *testing.T) {
	root, get, _ := tree(t)
	examples(t, get, "app gte --id 1")
	wantProblems(t, root, `runs "app gte --id 1", which is not a command`)
}

func TestCheck_UnknownSubcommandOfGroup(t *testing.T) {
	root, get, _ := tree(t)
	grp := &cobra.Command{Use: "config"}
	grp.AddCommand(&cobra.Command{Use: "path", RunE: noop})
	root.AddCommand(grp)
	get.Long = `Use "app config restore" first.`
	wantProblems(t, root, `"app config" has no subcommand "restore"`)
}

func TestCheck_DestructiveExampleWithoutToken(t *testing.T) {
	root, _, rm := tree(t)
	examples(t, rm, "app rm --confirm=yes")
	// --confirm is not declared in this tree either.
	wantProblems(t, root, `passes --confirm to "app rm"`, `runs "app rm" without --confirm-token`)
}

func TestCheck_DestructiveLongWithoutTokenMention(t *testing.T) {
	root, _, rm := tree(t)
	rm.Long = "Requires --dry-run first."
	wantProblems(t, root, `"app rm" Long does not mention --confirm-token`)
}

func TestCheck_SkipsHiddenAndDeprecated(t *testing.T) {
	root, _, _ := tree(t)
	hidden := &cobra.Command{Use: "old", Hidden: true, RunE: noop, Long: "Use --json."}
	dep := &cobra.Command{Use: "older", Deprecated: "gone", RunE: noop, Long: "Use --json."}
	kitcli.SetDestructiveToken(dep)
	root.AddCommand(hidden, dep)
	wantProblems(t, root)
}

func TestCheck_ConfirmTokenOnlyOnTokenCommands(t *testing.T) {
	root, get, _ := tree(t)
	examples(t, get, "app get --confirm-token=abc")
	wantProblems(t, root, `passes --confirm-token to "app get"`)
}
