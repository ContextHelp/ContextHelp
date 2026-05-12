package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cursor"
	"github.com/spf13/cobra"
)

var cursorCmd = &cobra.Command{
	Use:   "cursor",
	Short: "Manage named cursors for incremental list retrieval (US-0409)",
	Long: `Named cursors mark a per-viewer position over ctxt list results.

A cursor stores last_seen_at + last_object_id + a query snapshot.
Use ` + "`ctxt list --cursor <name> --advance`" + ` to fetch only items added
after the last advance.

Storage: ` + cursor.DefaultFileName + ` under XDG config dir;
override with ` + cursor.EnvFile + `.`,
}

var cursorListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all named cursors",
	Long: `Print every named cursor known to the local cursor store.

For each cursor the table shows name, LastSeenAt timestamp, LastObjectID, and
the UpdatedAt for the cursor record itself. Useful before calling
ctxt cursor reset / set / delete on a specific cursor.`,
	RunE: runCursorList,
}

var cursorShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show a cursor's state and query snapshot",
	Long: `Inspect a single named cursor.

Prints the cursor name, LastSeenAt timestamp, LastObjectID, creation /
update times, and the captured query snapshot (mention, tag, profile, q,
type, after). Use this to verify the cursor before advancing it.`,
	Args: cobra.ExactArgs(1),
	RunE: runCursorShow,
}

var cursorResetCmd = &cobra.Command{
	Use:   "reset <name>",
	Short: "Rewind a cursor to epoch 0",
	Long: `Rewind the named cursor so the next advance returns the full result set.

LastSeenAt is set to the zero time and LastObjectID is cleared. The query
snapshot is preserved. Use this to re-process every matching object from
the beginning without dropping the cursor itself.`,
	Args: cobra.ExactArgs(1),
	RunE: runCursorReset,
}

var cursorSetCmd = &cobra.Command{
	Use:   "set <name> --to <ts>",
	Short: "Jump cursor to a specific timestamp (RFC3339 or signed duration)",
	Long: `Jump the named cursor's LastSeenAt to a specific moment.

--to accepts either an RFC3339 timestamp (e.g. 2026-04-28T14:22:11Z) or a
signed duration relative to now (e.g. -7d, +1h). Use this when you need
to replay a known window or skip past a noisy region of the timeline.`,
	Args: cobra.ExactArgs(1),
	RunE: runCursorSet,
}

var cursorDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Remove a cursor",
	Long: `Delete the named cursor from the local cursor store.

The cursor's state (LastSeenAt, LastObjectID, query snapshot) is dropped.
Re-creating the cursor on the next ctxt list --cursor invocation will
start a fresh epoch-0 cursor with the new query snapshot.`,
	Args: cobra.ExactArgs(1),
	RunE: runCursorDelete,
}

func init() {
	rootCmd.AddCommand(cursorCmd)
	cursorCmd.AddCommand(cursorListCmd)
	cursorCmd.AddCommand(cursorShowCmd)
	cursorCmd.AddCommand(cursorResetCmd)
	cursorCmd.AddCommand(cursorSetCmd)
	cursorCmd.AddCommand(cursorDeleteCmd)

	// 12fcc conformance: side-effect + idempotency annotations.
	// list/show are read; reset/set mutate cursor state (write-local);
	// delete removes a cursor record (destructive-local).
	cliconv.WithSideEffect(cursorListCmd, cliconv.SideEffectRead)
	cliconv.WithSideEffect(cursorShowCmd, cliconv.SideEffectRead)
	cliconv.WithSideEffect(cursorResetCmd, cliconv.SideEffectWriteLocal)
	cliconv.WithSideEffect(cursorSetCmd, cliconv.SideEffectWriteLocal)
	cliconv.WithSideEffect(cursorDeleteCmd, cliconv.SideEffectDestructiveLocal)
	// 12fcc strict-gate: cursor delete drops local cursor state; opt
	// into kit's typed-token confirmation flow.
	cliconv.WithDestructiveToken(cursorDeleteCmd)

	// Kit verb defaults already cover list/show/delete (Yes).
	// "reset" and "set" are not in the default table; tag them
	// explicitly (both are idempotent: same args produce same state).
	cliconv.WithIdempotency(cursorResetCmd, cliconv.IdempotencyYes)
	cliconv.WithIdempotency(cursorSetCmd, cliconv.IdempotencyYes)

	cursorSetCmd.Flags().String("to", "",
		"target timestamp: RFC3339 (2026-04-28T14:22:11Z) or signed duration (-7d, +1h)")
	_ = cursorSetCmd.MarkFlagRequired("to")
}

func newCursorManager() (*cursor.Manager, error) {
	return cursor.New()
}

func runCursorList(cmd *cobra.Command, args []string) error {
	m, err := newCursorManager()
	if err != nil {
		return err
	}
	cs, err := m.List()
	if err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{"cursors": cs})
	}
	if len(cs) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No cursors. Create one via `ctxt list --cursor <name> --advance`.")
		return nil
	}
	headers := []string{"Name", "LastSeenAt", "LastObjectID", "Updated"}
	var rows [][]string
	for _, c := range cs {
		rows = append(rows, []string{
			c.Name,
			fmtTime(c.LastSeenAt),
			c.LastObjectID,
			fmtTime(c.UpdatedAt),
		})
	}
	printTable(cmd.OutOrStdout(), headers, rows)
	return nil
}

func runCursorShow(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := cursor.ValidateName(name); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(2)
	}
	m, err := newCursorManager()
	if err != nil {
		return err
	}
	c, err := m.Get(name)
	if err != nil {
		if errors.Is(err, cursor.ErrUnknownCursor) {
			fmt.Fprintf(os.Stderr, "cursor %q not found. Try `ctxt cursor list`.\n", name)
			os.Exit(2)
		}
		return err
	}
	if isJSONOutput() {
		return outputJSON(os.Stdout, c)
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Cursor:        %s\n", c.Name)
	fmt.Fprintf(out, "LastSeenAt:    %s\n", fmtTime(c.LastSeenAt))
	fmt.Fprintf(out, "LastObjectID:  %s\n", c.LastObjectID)
	fmt.Fprintf(out, "CreatedAt:     %s\n", fmtTime(c.CreatedAt))
	fmt.Fprintf(out, "UpdatedAt:     %s\n", fmtTime(c.UpdatedAt))
	fmt.Fprintln(out, "Query snapshot:")
	fmt.Fprintf(out, "  mention:  %v\n", c.Query.Mention)
	fmt.Fprintf(out, "  tag:      %v\n", c.Query.Tag)
	fmt.Fprintf(out, "  profile:  %s\n", c.Query.Profile)
	fmt.Fprintf(out, "  q:        %s\n", c.Query.Q)
	fmt.Fprintf(out, "  type:     %s\n", c.Query.Type)
	fmt.Fprintf(out, "  after:    %s\n", c.Query.After)
	return nil
}

func runCursorReset(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := cursor.ValidateName(name); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(2)
	}
	m, err := newCursorManager()
	if err != nil {
		return err
	}
	c, err := m.Reset(name)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Cursor %q rewound to epoch 0.\n", c.Name)
	return nil
}

func runCursorSet(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := cursor.ValidateName(name); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(2)
	}
	to, _ := cmd.Flags().GetString("to")
	ts, err := cursor.ParseTimestamp(to)
	if err != nil {
		return err
	}
	m, err := newCursorManager()
	if err != nil {
		return err
	}
	c, err := m.SetTo(name, ts)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Cursor %q set to %s.\n", c.Name, fmtTime(c.LastSeenAt))
	return nil
}

func runCursorDelete(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := cursor.ValidateName(name); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(2)
	}
	m, err := newCursorManager()
	if err != nil {
		return err
	}
	if err := m.Delete(name); err != nil {
		if errors.Is(err, cursor.ErrUnknownCursor) {
			fmt.Fprintf(os.Stderr, "cursor %q not found.\n", name)
			os.Exit(2)
		}
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Cursor %q deleted.\n", name)
	return nil
}

func fmtTime(t interface{ IsZero() bool }) string {
	type stringer interface{ Format(string) string }
	if t == nil || t.IsZero() {
		return "(epoch 0)"
	}
	if s, ok := t.(stringer); ok {
		return s.Format("2006-01-02T15:04:05Z07:00")
	}
	return fmt.Sprintf("%v", t)
}
