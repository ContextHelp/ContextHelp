package storage

import "context"

// DerivationStore reads and writes objects.derivation_sig, the signature
// (pipeline.Signature) of the pipeline and step versions that derived each
// object. Create writes it from KnowledgeObject.DerivationSig; Update never
// touches it. A row with an empty signature is never stale: one created
// through the API, or with no pipeline. A row federated in carries none
// either, unless it names a local pipeline: the baseline stamp signs it
// like any local row of that pipeline. Both drivers' ObjectStore
// implement it.
//
// The signature is only ever written alone, by Create, StampDerivation and
// the baseline stamp: the change_seq triggers skip the bump for an UPDATE
// that changes derivation_sig, so an UPDATE changing it together with
// another column would not be pushed to federation targets.
type DerivationStore interface {
	// StampDerivationBaseline sets the signature of every row whose
	// signature is empty and whose pipeline is a key of baseline, matching
	// the bare name and its "@v0" form. It reports the rows stamped and is
	// idempotent: stamped rows are no longer empty.
	StampDerivationBaseline(ctx context.Context, baseline map[string]string) (int, error)
	// CountStaleDerivations counts rows whose signature is non-empty and
	// not in current.
	CountStaleDerivations(ctx context.Context, current []string) (int, error)
	// ListStaleDerivations returns up to limit stale object IDs greater
	// than after, in ID order.
	ListStaleDerivations(ctx context.Context, current []string, after string, limit int) ([]string, error)
	// StampDerivation sets object id's signature, leaving updated_at alone.
	// A missing object wraps ErrNotFound.
	StampDerivation(ctx context.Context, id, sig string) error
}
