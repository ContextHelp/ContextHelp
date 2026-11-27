package cmd

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
)

// derivationBaselineID is the index_signatures key whose presence records
// that the derivation baseline was stamped.
const derivationBaselineID = "derivation_baseline"

// stampDerivationBaseline stamps the baseline signature of each pipeline
// definition, available or not, on the existing rows that carry none,
// once: later starts read the flag and return. The flag is written after
// the stamp, and the stamp is idempotent, so a start cut short repeats it
// harmlessly. It reports the rows stamped.
func stampDerivationBaseline(ctx context.Context, db indexsig.DBTX, d indexsig.Dialect, store storage.DerivationStore, reg pipeline.Registry) (int, error) {
	flag, err := indexsig.Load(ctx, db, d, derivationBaselineID)
	if err != nil {
		return 0, err
	}
	if flag != nil {
		return 0, nil
	}
	n, err := store.StampDerivationBaseline(ctx, pipeline.Baselines(reg))
	if err != nil {
		return 0, err
	}
	if err := indexsig.Upsert(ctx, db, d, derivationBaselineID, "1", fmt.Sprintf("stamped %d rows", n)); err != nil {
		return n, err
	}
	return n, nil
}
