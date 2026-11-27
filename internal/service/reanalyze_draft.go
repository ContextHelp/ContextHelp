package service

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	uri "hop.top/cite/scheme"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	psteps "github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// metaDuplicateOf names the object a re-derived object's new content
// hash collides with.
const (
	metaDuplicateOf   = "duplicate_of"
	metaDuplicateKind = "duplicate_kind"
	// duplicateKindHash marks a duplicate_of this package wrote: the
	// object's re-derived content hash is another object's. The dedup step
	// writes its own kind, which a reanalysis leaves alone.
	duplicateKindHash = "content_hash"
)

// reanalyzeDerivedMetadata are the metadata keys the content storage
// derives from where the content is kept, not from the content: a
// reanalysis starts without them so the write decides them afresh.
var reanalyzeDerivedMetadata = []string{"blob_key", psteps.MetaTextTruncated, MetaContentTruncated}

// stepTagSources are the Source values pipeline steps give the tags they
// emit: tagger ("auto", "llm"), ocr_extractor, github_repo_mapper and
// custom op steps. A new run replaces tags of these sources; a tag of any
// other source (user, manual, import, registry, none) is kept.
var stepTagSources = []string{"auto", "llm", "ocr", "github", "step"}

// refuseReanalyze reports why obj must not be re-run, or nil. An encrypted
// object holds ciphertext the steps would index as text; a held object
// may not be changed.
func (s *Service) refuseReanalyze(ctx context.Context, obj *storage.KnowledgeObject) error {
	if obj.Encrypted {
		return fmt.Errorf("reanalyze: %w: object %q is encrypted", upgrade.ErrCannotReanalyze, obj.ID)
	}
	if holds := s.Store(ctx).Holds(); holds != nil {
		held, err := holds.IsHeld(ctx, obj.ID)
		if err != nil {
			return fmt.Errorf("reanalyze: check hold of %q: %w", obj.ID, err)
		}
		if held {
			return fmt.Errorf("reanalyze: %w: object %q: %w", upgrade.ErrCannotReanalyze, obj.ID, storage.ErrObjectHeld)
		}
	}
	return nil
}

// reanalyzeDraft is the draft a reanalysis runs the steps on: a full
// copy of the stored object. A derived field is cleared only when a step
// of the pipeline about to run declares it produces it; any other field is
// left exactly as stored. produced is that set (see producedFields). With
// fetches, a fetch step will run and the content and sub types start empty.
func reanalyzeDraft(obj *storage.KnowledgeObject, versionedName string, produced map[string]bool, fetches bool) *storage.KnowledgeObject {
	d := *obj
	if fetches {
		// The fresh response decides how it is routed and cleaned, not what
		// the previous one (or a person) said its type was.
		d.ContentType, d.Subtype = "", ""
	}
	d.Metadata = storageutil.CloneMetadata(obj.Metadata)
	for _, k := range reanalyzeDerivedMetadata {
		delete(d.Metadata, k)
	}
	if d.Metadata[metaDuplicateKind] == duplicateKindHash {
		delete(d.Metadata, metaDuplicateOf)
		delete(d.Metadata, metaDuplicateKind)
	}
	if produced["TextContent"] || produced["RawContent"] {
		d.TextContent = ""
	}
	if produced["Summaries"] {
		d.Summaries = nil
	}
	if produced["Sections"] {
		d.Sections = nil
	}
	if produced["Decisions"] {
		d.Decisions = nil
	}
	if produced["Tasks"] {
		d.Tasks = nil
	}
	d.Graph = reanalyzeGraph(obj.Graph, produced)
	d.Tags = slices.Clone(obj.Tags)
	if produced["Tags"] {
		d.Tags = keptTags(obj.Tags)
	}
	d.Mentions = slices.Clone(obj.Mentions)
	if produced["Mentions"] {
		d.Mentions = keptMentions(obj)
	}
	d.Pipeline = versionedName
	return &d
}

// reanalyzeGraph returns the graph the steps start from: a copy of g with
// the nodes and edges steps rebuild dropped when a step produces the graph
// (the Graph, Sections, Tags or Mentions field). Entity-mention nodes stay
// unless a step produces Mentions: they hold the provenance of the
// mentions that stay.
//
// Nodes carry no record of what created them, so every node of a step's
// kind is dropped, whoever created it.
func reanalyzeGraph(g *storage.ObjectGraph, produced map[string]bool) *storage.ObjectGraph {
	if g == nil {
		return nil
	}
	cp := &storage.ObjectGraph{Edges: slices.Clone(g.Edges)}
	for _, n := range g.Nodes {
		n.Metadata = storageutil.CloneMetadata(n.Metadata)
		cp.Nodes = append(cp.Nodes, n)
	}
	if !produced["Graph"] && !produced["Sections"] && !produced["Tags"] && !produced["Mentions"] {
		return cp
	}
	out := &storage.ObjectGraph{}
	kept := map[string]bool{}
	for _, n := range cp.Nodes {
		if n.NodeType == storage.NodeTypeEntityMention && !produced["Mentions"] {
			out.Nodes = append(out.Nodes, n)
			kept[n.ID] = true
		}
	}
	for _, e := range cp.Edges {
		if kept[e.FromID] {
			out.Edges = append(out.Edges, e)
		}
	}
	if len(out.Nodes) == 0 {
		return nil
	}
	return out
}

// keptTags are the tags a new run does not replace: those of a source no
// step emits.
func keptTags(tags []storage.Tag) []storage.Tag {
	var out []storage.Tag
	for _, t := range tags {
		if !slices.Contains(stepTagSources, t.Source) {
			out = append(out, t)
		}
	}
	return out
}

// keptMentions are the stored mentions that were not extracted from the
// object's text: those whose provenance names anything but raw_content.
// A mention with no provenance node was set by a person or a client, or
// extracted before nodes were kept: it stays only when the stored text
// does not name it (one the text names is re-derived by the extractor).
func keptMentions(obj *storage.KnowledgeObject) []uri.URI {
	var out []uri.URI
	for _, m := range obj.Mentions {
		node := obj.Graph.MentionNode(m.String())
		switch {
		case node == nil:
			if !strings.Contains(obj.RawContent, mentionText(m)) {
				out = append(out, m)
			}
		case node.Metadata[storage.ProvenanceSourceField] != storage.MentionSourceRawContent:
			out = append(out, m)
		}
	}
	return out
}

// mentionText is the @namespace.slug form the text names a mention by.
func mentionText(m uri.URI) string {
	return "@" + strings.ReplaceAll(storage.MentionSlug(m), "/", ".")
}

// finishReanalysis applies what the steps may not decide to the derived
// draft: protected metadata, the tags and mentions the new run does not
// replace, and the content hash. A hash another object of the profile
// already holds is not taken: the draft takes no hash (the unique index
// skips an empty one) and records the other object, so both objects stay.
func (s *Service) finishReanalysis(ctx context.Context, stored, draft *storage.KnowledgeObject) error {
	// Detectors set type, subtype and content type from the content or the
	// source name; a person may have edited them. They change only with the
	// content.
	if pipeline.ContentBytes(draft) == stored.RawContent {
		draft.Type, draft.Subtype, draft.ContentType = stored.Type, stored.Subtype, stored.ContentType
	}
	draft.Metadata = storageutil.RestoreProtectedMetadata(draft.Metadata, stored.Metadata)
	storageutil.Submission{Hints: keptTags(stored.Tags), Mentions: keptMentions(stored)}.Apply(draft)

	hash := storageutil.DraftContentHash(pipeline.ContentBytes(draft), draft.Source, draft.Metadata)
	other, err := s.Store(ctx).Objects().GetByContentHash(ctx, hash, draft.ProfileID)
	if err != nil {
		return fmt.Errorf("check content hash: %w", err)
	}
	if other != nil && other.ID != draft.ID {
		if draft.Metadata == nil {
			draft.Metadata = map[string]any{}
		}
		draft.Metadata[metaDuplicateOf] = other.ID
		draft.Metadata[metaDuplicateKind] = duplicateKindHash
		draft.ContentHash = ""
		return nil
	}
	draft.ContentHash = hash
	return nil
}

// persistReanalysis writes the re-derived object through the same path
// as UpdateObject, so object.updated is emitted. The row is the result:
// what follows (mention edges, fan-out) is logged when it fails and
// never undoes or hides the write.
func (s *Service) persistReanalysis(ctx context.Context, draft *storage.KnowledgeObject) error {
	row, err := s.storable(ctx, draft)
	if err != nil {
		return err
	}
	uctx, ev := s.announce(ctx, events.TopicObjectUpdated, "service.objects", draft.ID, events.ObjectUpdatedPayload{
		ObjectID: draft.ID,
		Type:     draft.Type,
	})
	orphans, err := blob.UpdateObject(uctx, s.Store(ctx), row)
	if err != nil {
		return err
	}
	logOrphans("reanalyze", draft.ID, orphans)
	if err := s.dropStaleMentionEdges(ctx, draft); err != nil {
		slog.Warn("reanalyze: stale mention edges not dropped", "object", draft.ID, "err", err)
	}
	if err := s.writeUserMentionEdges(ctx, draft); err != nil {
		slog.Warn("reanalyze: mention edges not written", "object", draft.ID, "err", err)
	}
	if s.Cfg.FanOut.Enabled {
		if _, err := s.FanOut(ctx, draft.ID); err != nil {
			slog.Warn("reanalyze: fan-out failed", "object", draft.ID, "err", err)
		}
	}
	s.publish(ctx, ev)
	return nil
}

// dropStaleMentionEdges deletes, in both directions, the mention edges of
// obj that name an entity obj no longer mentions, whatever their source.
func (s *Service) dropStaleMentionEdges(ctx context.Context, obj *storage.KnowledgeObject) error {
	kept := make([]string, 0, len(obj.Mentions))
	for _, m := range obj.Mentions {
		kept = append(kept, storage.MentionTarget(m))
	}
	edges, err := s.Store(ctx).Edges().ListFrom(ctx, storage.EdgeEndpointObject, obj.ID)
	if err != nil {
		return fmt.Errorf("list mention edges: %w", err)
	}
	var drop []storage.EdgeMatch
	for _, e := range edges {
		if e.EdgeType != storage.EdgeTypeMentions || e.ToType != storage.EdgeEndpointEntity || slices.Contains(kept, e.ToID) {
			continue
		}
		drop = append(drop, storage.EdgeMatch{
			FromType: storage.EdgeEndpointObject, FromID: obj.ID,
			ToType: storage.EdgeEndpointEntity, ToID: e.ToID,
			EdgeTypes: []string{storage.EdgeTypeMentions},
		}, storage.EdgeMatch{
			FromType: storage.EdgeEndpointEntity, FromID: e.ToID,
			ToType: storage.EdgeEndpointObject, ToID: obj.ID,
			EdgeTypes: []string{storage.EdgeTypeMentionedIn},
		})
	}
	if len(drop) == 0 {
		return nil
	}
	if _, err := s.Store(ctx).Edges().DeleteMatching(ctx, drop); err != nil {
		return fmt.Errorf("delete stale mention edges: %w", err)
	}
	return nil
}
