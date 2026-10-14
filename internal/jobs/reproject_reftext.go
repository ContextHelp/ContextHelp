package jobs

import (
	"context"
	"log/slog"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
)

// repairRefText gives text back to objects whose text_content is their
// blob:// ref.
//
// Before projection v3, storage copied an externalized object's ref into
// an empty text_content, so search and embeddings had a pointer where the
// object's text belongs, and v3's projection drops refs: such an object
// was found by its summaries and sections only. Each one gets what
// externalize_content now keeps: the first TextCap bytes of its content,
// flagged a prefix when the ref was its content's. The write is
// conditional on the text still being the ref and marks the row stale, so
// the pass that follows re-indexes it.
//
// A row whose blob can't be read keeps its ref and is counted and logged.
// It does not fail the run or hold the signature: a lost blob does not
// come back, and the object is still found by its summaries. err reports
// that listing failed or the run was cut off.
func (x *reprojectRun) repairRefText(ctx context.Context) error {
	if x.RefText == nil {
		return nil
	}
	textCap := x.TextCap
	if textCap <= 0 {
		textCap = DefaultTextCap
	}
	batch := x.Batch
	if batch <= 0 {
		batch = DefaultReprojectBatch
	}
	cursor := ""
	for {
		objs, err := x.RefText.ListRefText(ctx, cursor, batch)
		if err != nil {
			return err
		}
		if len(objs) == 0 {
			return nil
		}
		for _, obj := range objs {
			if err := ctx.Err(); err != nil {
				return err
			}
			cursor = obj.ID
			wrote := false
			text, err := blob.Content(ctx, x.Blobs, obj.TextContent)
			if err == nil {
				truncated := obj.TextContent == obj.RawContent
				wrote, err = x.RefText.ReplaceRefText(ctx, obj.ID, obj.TextContent, steps.TextPrefix(text, textCap), truncated)
			}
			if err != nil {
				x.res.TextRepairFailed++
				if len(x.res.TextRepairFailedObjects) < maxFailedReprojections {
					x.res.TextRepairFailedObjects = append(x.res.TextRepairFailedObjects, obj.ID)
				}
				slog.Warn("reproject: text_content is a blob ref and could not be repaired; the object keeps it",
					"object", obj.ID, "ref", obj.TextContent, "err", err)
				continue
			}
			if wrote { // else written since it was listed: nothing to repair
				x.res.TextRepaired++
			}
		}
	}
}
