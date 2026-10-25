package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/graph"
)

// --- ctxt link {create,list,delete} ---

var linkCmd = &cobra.Command{
	Use:   "link",
	Short: "Manage typed links between knowledge objects",
	Long: `Manage associative typed links between knowledge objects.

Links are directed edges with a kit-defined type (extends, contradicts,
supersedes, supports, related-to, derived-from). The dpkms instance stores
each link with its reverse edge, except for symmetric types, and deletes
the two together.

create needs a token with write:objects (writer or admin), list one with
read:objects, and delete one with delete:edges (admin only).

Subcommands:
  create  Create a typed link between two objects.
  list    List all links for an object, optionally with traversal.
  delete  Remove every link between two objects.`,
}

var linkCreateCmd = &cobra.Command{
	Use:   "create <source_id> <target_id>",
	Short: "Create a typed link between two objects",
	Long: `Create a typed associative link between two knowledge objects.

Valid link types: extends, contradicts, supersedes, supports, related-to, derived-from.
A reverse link is created automatically (e.g. extends → extended-by).

Examples:
  ctxt link create obj_abc obj_xyz --type extends
  ctxt link create obj_abc obj_xyz --type contradicts --context "newer research"`,
	Args: cobra.ExactArgs(2),
	RunE: runLinkCreate,
}

func init() {
	rootCmd.AddCommand(linkCmd)
	linkCmd.AddCommand(linkCreateCmd)
	linkCreateCmd.Flags().StringP("type", "t", "", "link type (required): "+
		strings.Join(graph.UserLinkTypeStrings(), ", "))
	linkCreateCmd.Flags().String("context", "", "optional reason for the link")
	linkCreateCmd.Flags().String("server", "", serverFlagUsage)
	markFlagsRequired(linkCreateCmd, "type")

	// 12fcc conformance: side-effect annotation for create.
	// list and delete are wired in their own init() blocks below; the
	// annotations live next to those wirings for locality.
	cliconv.WithSideEffect(linkCreateCmd, cliconv.SideEffectWriteShared)
	// "create" defaults to IdempotencyNo via kit verb table — no
	// explicit override needed.

	// 12fcc strict-gate: examples + next-steps on link create.
	cliconv.WithExamples(linkCreateCmd, []cliconv.Example{
		{Title: "Mark one object as extending another", Command: "ctxt link create obj_abc obj_xyz --type extends"},
		{Title: "Record a contradiction with context", Command: "ctxt link create obj_abc obj_xyz --type contradicts --context \"newer research\""},
	})
	cliconv.WithNextSteps(linkCreateCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt link list <source_id>", Reason: "confirm the forward + reverse edges landed"},
	})
}

func runLinkCreate(cmd *cobra.Command, args []string) error {
	sourceID, targetID := args[0], args[1]
	lt, _ := cmd.Flags().GetString("type")
	note, _ := cmd.Flags().GetString("context")

	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	if _, err := client.CreateLink(cmd.Context(), dpkmsclient.LinkCreate{
		Source: sourceID, Target: targetID, Type: lt, Context: note,
	}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Linked %s -[%s]-> %s\n", sourceID, lt, targetID)
	return nil
}

// --- ctxt link list <object_id> ---

var linkListCmd = &cobra.Command{
	Use:   "list <object_id>",
	Short: "List all links for an object",
	Long: `List all associative links for a knowledge object (inbound + outbound).

Examples:
  ctxt link list obj_abc
  ctxt link list obj_abc --type contradicts
  ctxt link list obj_abc --follow 2`,
	Args: cobra.ExactArgs(1),
	RunE: runLinkList,
}

func init() {
	linkCmd.AddCommand(linkListCmd)
	linkListCmd.Flags().StringP("type", "t", "", "filter by link type")
	linkListCmd.Flags().Int("follow", 0, "depth-limited traversal (max 3)")
	linkListCmd.Flags().String("server", "", serverFlagUsage)

	// 12fcc conformance: list is a pure read traversal over edges.
	cliconv.WithSideEffect(linkListCmd, cliconv.SideEffectRead)

	// 12fcc strict-gate: examples on the read leaf.
	cliconv.WithExamples(linkListCmd, []cliconv.Example{
		{Title: "List inbound + outbound links for an object", Command: "ctxt link list obj_abc"},
		{Title: "Two-hop traversal filtered by type", Command: "ctxt link list obj_abc --type contradicts --follow 2"},
	})
}

func runLinkList(cmd *cobra.Command, args []string) error {
	objectID := args[0]
	typeFilter, _ := cmd.Flags().GetString("type")
	follow, _ := cmd.Flags().GetInt("follow")
	follow = min(follow, maxLinkFollow)

	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	q := dpkmsclient.LinkQuery{Type: typeFilter}
	if follow > 0 {
		q.Depth = follow
	}
	hops, err := client.ListLinks(cmd.Context(), objectID, q)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if isJSONOutput() {
		if hops == nil {
			hops = []dpkmsclient.LinkHop{}
		}
		return outputJSON(w, map[string]any{"links": hops})
	}
	if cliformat.Rows() {
		return cliformat.DispatchRows(cmd, w, linkRows(objectID, hops))
	}
	if len(hops) == 0 {
		fmt.Fprintln(w, "No links found.")
		return nil
	}
	for _, h := range hops {
		if follow > 0 {
			fmt.Fprintf(w, "%s%s [%s] %s\n", strings.Repeat("  ", h.Depth-1), linkArrow(h.Direction), h.Edge.EdgeType, h.ObjectID)
			continue
		}
		e := h.Edge
		dir, other := "→", e.ToID
		if e.ToID == objectID {
			dir, other = "←", e.FromID
		}
		meta := ""
		if c, ok := e.Metadata["context"]; ok {
			meta = fmt.Sprintf(" (%s)", c)
		}
		fmt.Fprintf(w, "  %s [%s] %s%s\n", dir, e.EdgeType, other, meta)
	}
	return nil
}

// linkRow is the row shape `link list` projects for table, csv and text.
type linkRow struct {
	Depth     int    `table:"Depth"`
	From      string `table:"From"`
	Direction string `table:"Direction"`
	Type      string `table:"Type"`
	Object    string `table:"Object"`
	Context   string `table:"Context"`
}

func linkRows(objectID string, hops []dpkmsclient.LinkHop) []linkRow {
	out := make([]linkRow, 0, len(hops))
	for _, h := range hops {
		r := linkRow{Depth: h.Depth, From: h.From, Direction: h.Direction, Object: h.ObjectID}
		if r.From == "" {
			r.From = objectID
		}
		if e := h.Edge; e != nil {
			r.Type = e.EdgeType
			if c, ok := e.Metadata["context"]; ok {
				r.Context = fmt.Sprint(c)
			}
		}
		out = append(out, r)
	}
	return out
}

// maxLinkFollow caps --follow; a larger value walks this far.
const maxLinkFollow = 3

// linkArrow is the arrow link list prints for a hop's direction.
func linkArrow(direction string) string {
	if direction == "in" {
		return "←"
	}
	return "→"
}

// --- ctxt link delete <source_id> <target_id> ---

var linkDeleteCmd = &cobra.Command{
	Use:   "delete <source_id> <target_id>",
	Short: "Remove links between two objects",
	Long: `Remove all associative links between two knowledge objects.
Both forward and reverse edges are removed.

` + cliconv.DestructiveTokenHelp + `

Examples:
  ctxt link delete obj_abc obj_xyz --confirm-token=<sha>`,
	Args: cobra.ExactArgs(2),
	RunE: runLinkDelete,
}

func init() {
	linkCmd.AddCommand(linkDeleteCmd)
	linkDeleteCmd.Flags().String("server", "", serverFlagUsage)

	// 12fcc conformance: delete removes forward + reverse edges
	// between two objects. Classified as destructive on the shared
	// graph store.
	cliconv.WithSideEffect(linkDeleteCmd, cliconv.SideEffectDestructiveShared)
	// 12fcc strict-gate: link delete drops graph edges; opt into kit's
	// typed-token confirmation flow.
	cliconv.WithDestructiveToken(linkDeleteCmd)
	// "delete" defaults to IdempotencyYes via kit verb table — no
	// explicit override needed.

	// 12fcc strict-gate: examples + next-steps on the destructive leaf.
	cliconv.WithExamples(linkDeleteCmd, []cliconv.Example{
		{Title: "Remove every link between two objects", Command: "ctxt link delete obj_abc obj_xyz --confirm-token=<sha>"},
	})
	cliconv.WithNextSteps(linkDeleteCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt link list obj_abc", Reason: "verify the forward + reverse edges are gone"},
	})
}

func runLinkDelete(cmd *cobra.Command, args []string) error {
	sourceID, targetID := args[0], args[1]

	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	deleted, err := client.DeleteLinks(cmd.Context(), sourceID, targetID, "")
	if err != nil {
		return err
	}

	if deleted == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No links found between the two objects.")
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Removed %d link(s) between %s and %s\n",
			deleted, sourceID, targetID)
	}
	return nil
}
