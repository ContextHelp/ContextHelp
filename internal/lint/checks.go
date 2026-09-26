package lint

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
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

// checkDuplicates finds near-duplicate objects by comparing each object's
// vectors under the default embedding model with that model's index. The
// store reports cosine distance; the threshold is a similarity, so a pair is
// a near-duplicate when 1 - distance >= DuplicateThresh.
func (l *Linter) checkDuplicates(ctx context.Context) ([]Issue, error) {
	models, err := registry.ForDriver(l.driver)
	if err != nil {
		return nil, err
	}
	model, err := models.Default(ctx)
	if errors.Is(err, registry.ErrNoDefaultModel) {
		return []Issue{duplicatesSkipped("no default embedding model to compare vectors under")}, nil
	}
	if err != nil {
		return nil, err
	}

	objs, err := l.listObjects(ctx)
	if err != nil {
		return nil, err
	}

	store := l.driver.Embeddings()
	thresh := l.cfg.DuplicateThresh
	seen := map[string]bool{}
	var issues []Issue

	for _, obj := range objs {
		chunks, err := store.Get(ctx, obj.ID, model.ModelID)
		if err != nil {
			return nil, err
		}
		for _, chunk := range chunks {
			hits, err := store.Search(ctx, storage.VectorQuery{ModelID: model.ModelID, Vector: chunk.Vector, TopK: 5})
			if errors.Is(err, storage.ErrEmbeddingIndexMissing) {
				return []Issue{duplicatesSkipped("default embedding model " + model.ModelID + " has no vector index")}, nil
			}
			if err != nil {
				return nil, err
			}
			for _, hit := range hits {
				if hit.ObjectID == obj.ID {
					continue
				}
				key := pairKey(obj.ID, hit.ObjectID)
				if seen[key] {
					continue
				}
				similarity := 1 - hit.Distance
				if similarity < thresh {
					continue
				}
				seen[key] = true
				issues = append(issues, Issue{
					Check:    "duplicates",
					Severity: SeverityWarning,
					ObjectID: obj.ID,
					Message: "near-duplicate of " + hit.ObjectID +
						" (similarity " + formatScore(similarity) + ")",
				})
			}
		}
	}
	return issues, nil
}

// duplicatesSkipped reports that the duplicates check could not run.
func duplicatesSkipped(reason string) Issue {
	return Issue{
		Check:    "duplicates",
		Severity: SeverityInfo,
		Message:  "duplicates check skipped: " + reason,
	}
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
