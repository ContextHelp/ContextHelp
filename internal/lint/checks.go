package lint

import (
	"context"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// checkOrphans finds objects with no tags, no mentions, no edges, no metadata.
func (l *Linter) checkOrphans(ctx context.Context) ([]Issue, error) {
	objs, err := l.listObjects(ctx)
	if err != nil {
		return nil, err
	}

	edgeStore := l.driver.Edges()
	var issues []Issue

	for _, obj := range objs {
		if len(obj.Tags) > 0 || len(obj.Mentions) > 0 || len(obj.Metadata) > 0 {
			continue
		}
		// check edges
		from, err := edgeStore.ListFrom(ctx, "object", obj.ID)
		if err != nil {
			return nil, err
		}
		to, err := edgeStore.ListTo(ctx, "object", obj.ID)
		if err != nil {
			return nil, err
		}
		if len(from) > 0 || len(to) > 0 {
			continue
		}

		issues = append(issues, Issue{
			Check:    "orphans",
			Severity: SeverityWarning,
			ObjectID: obj.ID,
			Message:  "object has no tags, mentions, edges, or metadata",
		})
	}
	return issues, nil
}

// checkMissingMetadata finds objects without structured_metadata enrichment.
func (l *Linter) checkMissingMetadata(ctx context.Context) ([]Issue, error) {
	objs, err := l.listObjects(ctx)
	if err != nil {
		return nil, err
	}

	var issues []Issue
	for _, obj := range objs {
		if _, ok := obj.Metadata["enrichment.structured_metadata"]; ok {
			continue
		}
		issues = append(issues, Issue{
			Check:    "missing_metadata",
			Severity: SeverityInfo,
			ObjectID: obj.ID,
			Message:  "object has no structured_metadata enrichment",
		})
	}
	return issues, nil
}

// checkDuplicates finds near-duplicate objects using vector similarity.
func (l *Linter) checkDuplicates(ctx context.Context) ([]Issue, error) {
	objs, err := l.driver.Objects().ListWithEmbeddings(ctx)
	if err != nil {
		return nil, err
	}

	if len(objs) < 2 {
		return nil, nil
	}

	thresh := l.cfg.DuplicateThresh
	seen := map[string]bool{}
	var issues []Issue

	for _, obj := range objs {
		if len(obj.Embeddings) == 0 {
			continue
		}
		// profile filter
		if l.cfg.ProfileID != "" && obj.ProfileID != l.cfg.ProfileID {
			continue
		}

		hits, err := l.driver.Vectors().Search(ctx, obj.Embeddings, 5)
		if err != nil {
			return nil, err
		}

		for _, hit := range hits {
			if hit.ID == obj.ID {
				continue
			}
			pairKey := pairKey(obj.ID, hit.ID)
			if seen[pairKey] {
				continue
			}
			if hit.Score >= thresh {
				seen[pairKey] = true
				issues = append(issues, Issue{
					Check:    "duplicates",
					Severity: SeverityWarning,
					ObjectID: obj.ID,
					Message: "near-duplicate of " + hit.ID +
						" (similarity " + formatScore(hit.Score) + ")",
				})
			}
		}
	}
	return issues, nil
}

// checkStale finds objects not updated in StaleDays with no inbound edges.
func (l *Linter) checkStale(ctx context.Context) ([]Issue, error) {
	objs, err := l.listObjects(ctx)
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().AddDate(0, 0, -l.cfg.StaleDays)
	edgeStore := l.driver.Edges()
	var issues []Issue

	for _, obj := range objs {
		if obj.UpdatedAt.After(cutoff) {
			continue
		}

		inbound, err := edgeStore.ListTo(ctx, "object", obj.ID)
		if err != nil {
			return nil, err
		}
		if len(inbound) > 0 {
			continue
		}

		issues = append(issues, Issue{
			Check:    "stale",
			Severity: SeverityInfo,
			ObjectID: obj.ID,
			Message:  "not updated since " + obj.UpdatedAt.Format("2006-01-02") + " with no inbound edges",
		})
	}
	return issues, nil
}

// listObjects returns all objects, optionally scoped to a profile.
func (l *Linter) listObjects(ctx context.Context) ([]*storage.KnowledgeObject, error) {
	filter := storage.ObjectFilter{
		Status:    "all",
		ProfileID: l.cfg.ProfileID,
		Limit:     0,
	}
	objs, _, err := l.driver.Objects().List(ctx, filter)
	return objs, err
}

func pairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}

func formatScore(s float64) string {
	return fmt.Sprintf("%.4f", s)
}
