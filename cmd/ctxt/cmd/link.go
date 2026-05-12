package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/graph"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// --- ctxt link {create,list,delete} ---

var linkCmd = &cobra.Command{
	Use:   "link",
	Short: "Manage typed links between knowledge objects",
	Long: `Manage associative typed links between knowledge objects.

Links are directed edges with a kit-defined type (extends, contradicts,
supersedes, supports, related-to, derived-from). Reverse edges are
maintained automatically except for symmetric types.

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
	_ = linkCreateCmd.MarkFlagRequired("type")

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
	ltStr, _ := cmd.Flags().GetString("type")
	ctx, _ := cmd.Flags().GetString("context")

	lt := graph.LinkType(ltStr)
	if !graph.ValidLinkType(lt) {
		return fmt.Errorf("invalid link type %q; valid types: %s",
			ltStr, strings.Join(graph.UserLinkTypeStrings(), ", "))
	}

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	bg := context.Background()

	// validate both objects exist
	if _, err := svc.GetObject(bg, sourceID); err != nil {
		return fmt.Errorf("source object %q: %w", sourceID, err)
	}
	if _, err := svc.GetObject(bg, targetID); err != nil {
		return fmt.Errorf("target object %q: %w", targetID, err)
	}

	now := time.Now().Truncate(time.Second)
	var meta map[string]any
	if ctx != "" {
		meta = map[string]any{"context": ctx}
	}

	// forward edge
	fwd := &storage.Edge{
		ID:        uuid.New().String(),
		FromType:  "object",
		FromID:    sourceID,
		ToType:    "object",
		ToID:      targetID,
		EdgeType:  string(lt),
		Weight:    1.0,
		Metadata:  meta,
		CreatedAt: now,
	}
	if err := svc.Store.Edges().Create(bg, fwd); err != nil {
		return fmt.Errorf("create forward edge: %w", err)
	}

	// reverse edge (skip if symmetric and same pair)
	inv, _ := graph.InverseLinkType(lt)
	if !graph.IsSymmetric(lt) {
		rev := &storage.Edge{
			ID:        uuid.New().String(),
			FromType:  "object",
			FromID:    targetID,
			ToType:    "object",
			ToID:      sourceID,
			EdgeType:  string(inv),
			Weight:    1.0,
			Metadata:  meta,
			CreatedAt: now,
		}
		if err := svc.Store.Edges().Create(bg, rev); err != nil {
			return fmt.Errorf("create reverse edge: %w", err)
		}
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
	if follow > 3 {
		follow = 3
	}

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	bg := context.Background()

	if follow > 0 {
		return printLinksTraversal(cmd, svc.Store.Edges(), objectID, typeFilter, follow)
	}

	outbound, err := svc.Store.Edges().ListFrom(bg, "object", objectID)
	if err != nil {
		return fmt.Errorf("list outbound: %w", err)
	}
	inbound, err := svc.Store.Edges().ListTo(bg, "object", objectID)
	if err != nil {
		return fmt.Errorf("list inbound: %w", err)
	}

	// filter to object-object link edges only
	var links []*storage.Edge
	for _, e := range outbound {
		if e.ToType != "object" {
			continue
		}
		if typeFilter != "" && e.EdgeType != typeFilter {
			continue
		}
		links = append(links, e)
	}
	for _, e := range inbound {
		if e.FromType != "object" {
			continue
		}
		if typeFilter != "" && e.EdgeType != typeFilter {
			continue
		}
		links = append(links, e)
	}

	if len(links) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No links found.")
		return nil
	}

	for _, e := range links {
		dir := "→"
		other := e.ToID
		if e.ToID == objectID {
			dir = "←"
			other = e.FromID
		}
		meta := ""
		if c, ok := e.Metadata["context"]; ok {
			meta = fmt.Sprintf(" (%s)", c)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %s [%s] %s%s\n", dir, e.EdgeType, other, meta)
	}
	return nil
}

// printLinksTraversal does BFS up to maxDepth.
func printLinksTraversal(
	cmd *cobra.Command,
	edges storage.EdgeStore,
	rootID, typeFilter string,
	maxDepth int,
) error {
	bg := context.Background()
	type queueItem struct {
		id    string
		depth int
	}

	visited := map[string]bool{rootID: true}
	queue := []queueItem{{rootID, 0}}

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		if item.depth >= maxDepth {
			continue
		}

		outbound, err := edges.ListFrom(bg, "object", item.id)
		if err != nil {
			return err
		}
		inbound, err := edges.ListTo(bg, "object", item.id)
		if err != nil {
			return err
		}

		var neighbors []struct {
			edgeType string
			other    string
			dir      string
		}
		for _, e := range outbound {
			if e.ToType != "object" {
				continue
			}
			if typeFilter != "" && e.EdgeType != typeFilter {
				continue
			}
			neighbors = append(neighbors, struct {
				edgeType string
				other    string
				dir      string
			}{e.EdgeType, e.ToID, "→"})
		}
		for _, e := range inbound {
			if e.FromType != "object" {
				continue
			}
			if typeFilter != "" && e.EdgeType != typeFilter {
				continue
			}
			neighbors = append(neighbors, struct {
				edgeType string
				other    string
				dir      string
			}{e.EdgeType, e.FromID, "←"})
		}

		indent := strings.Repeat("  ", item.depth)
		for _, n := range neighbors {
			if visited[n.other] {
				continue
			}
			visited[n.other] = true
			fmt.Fprintf(cmd.OutOrStdout(), "%s%s [%s] %s\n",
				indent, n.dir, n.edgeType, n.other)
			queue = append(queue, queueItem{n.other, item.depth + 1})
		}
	}

	if len(visited) == 1 {
		fmt.Fprintln(cmd.OutOrStdout(), "No links found.")
	}
	return nil
}

// --- ctxt link delete <source_id> <target_id> ---

var linkDeleteCmd = &cobra.Command{
	Use:   "delete <source_id> <target_id>",
	Short: "Remove links between two objects",
	Long: `Remove all associative links between two knowledge objects.
Both forward and reverse edges are removed.

Examples:
  ctxt link delete obj_abc obj_xyz`,
	Args: cobra.ExactArgs(2),
	RunE: runLinkDelete,
}

func init() {
	linkCmd.AddCommand(linkDeleteCmd)

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
		{Title: "Remove every link between two objects", Command: "ctxt link delete obj_abc obj_xyz"},
	})
	cliconv.WithNextSteps(linkDeleteCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt link list obj_abc", Reason: "verify the forward + reverse edges are gone"},
	})
}

func runLinkDelete(cmd *cobra.Command, args []string) error {
	sourceID, targetID := args[0], args[1]

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	bg := context.Background()
	deleted := 0

	// find and delete forward edges (source -> target)
	outbound, err := svc.Store.Edges().ListFrom(bg, "object", sourceID)
	if err != nil {
		return fmt.Errorf("list outbound: %w", err)
	}
	for _, e := range outbound {
		if e.ToType == "object" && e.ToID == targetID &&
			graph.ValidLinkType(graph.LinkType(e.EdgeType)) {
			if err := svc.Store.Edges().Delete(bg, e.ID); err != nil {
				return fmt.Errorf("delete edge %s: %w", e.ID, err)
			}
			deleted++
		}
	}

	// find and delete reverse edges (target -> source)
	outbound2, err := svc.Store.Edges().ListFrom(bg, "object", targetID)
	if err != nil {
		return fmt.Errorf("list reverse outbound: %w", err)
	}
	for _, e := range outbound2 {
		if e.ToType == "object" && e.ToID == sourceID &&
			graph.ValidLinkType(graph.LinkType(e.EdgeType)) {
			if err := svc.Store.Edges().Delete(bg, e.ID); err != nil {
				return fmt.Errorf("delete edge %s: %w", e.ID, err)
			}
			deleted++
		}
	}

	if deleted == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No links found between the two objects.")
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Removed %d link(s) between %s and %s\n",
			deleted, sourceID, targetID)
	}
	return nil
}
