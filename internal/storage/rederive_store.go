package storage

import (
	"context"
	"time"
)

// Skip reasons: why an automatic re-derive left an object as stored. The
// set is fixed so a status or report can group by it.
const (
	// SkipRefused: the pipeline or the object cannot be re-derived (a
	// container pipeline, a lost original, encryption, a hold).
	SkipRefused = "refused"
	// SkipRefreshRejected: a re-fetch returned something other than the
	// stored page and the guard kept the stored object, or the stored text
	// was too short to compare with a refresh (no fetch was made).
	SkipRefreshRejected = "refresh_rejected"
	// SkipFetchFailed: the re-fetch failed (not found, DNS, timeout).
	SkipFetchFailed = "fetch_failed"
	// SkipProviderFailed: a provider call (llm, vision or embedding) kept
	// failing for this object after the provider was shown to work.
	SkipProviderFailed = "provider_failed"
	// SkipUnusableReply: a provider answered, and a step could not use
	// the reply (empty, not the structure it requires), 3 strikes (they add up across runs).
	SkipUnusableReply = "unusable_reply"
	// SkipFailed: a failure of no known kind, 3 strikes (they add up across runs).
	SkipFailed = "failed"
)

// StaleGroup is the stale rows that share a stored pipeline stamp and
// signature: what the re-derive job decides once for all of them.
type StaleGroup struct {
	Pipeline string
	Sig      string
	// Objects counts the group's rows that are not skipped for a current
	// signature.
	Objects int
}

// RederiveCursor is a position in a group's processing order: fewest
// tries first, then ID. The zero value is the start.
type RederiveCursor struct {
	Tries int
	ID    string
}

// RederiveQuery selects one page of a group's rows.
type RederiveQuery struct {
	Pipeline, Sig string
	// Target is the signature the group is brought to: skips and
	// attempts are read for it.
	Target string
	After  RederiveCursor
	Limit  int
}

// StaleObject is a row of a group, with the tries it has for the group's
// target.
type StaleObject struct {
	ID       string
	Pipeline string
	Sig      string
	Tries    int
}

// RederiveSkip records that an object was not re-derived to TargetSig and
// why. A skip holds for its target only: a later change of the target
// makes the object eligible again.
type RederiveSkip struct {
	ObjectID  string
	TargetSig string
	Reason    string
	// Detail is the error text, truncated and without content or
	// credentials.
	Detail string
	At     time.Time
}

// RederiveStore holds what the automatic re-derive job reads and writes
// besides the objects themselves: the entries behind each derivation
// signature, the tries and strikes per object and target, and the objects
// it skipped. Both drivers' ObjectStore implement it.
type RederiveStore interface {
	// UpsertDerivationSignature records the entries a signature hashes,
	// replacing an earlier record of the same signature.
	UpsertDerivationSignature(ctx context.Context, sig, pipeline string, entries []string) error
	// DerivationSignatureEntries returns the entries recorded for sig;
	// ok is false when none were.
	DerivationSignatureEntries(ctx context.Context, sig string) (entries []string, ok bool, err error)
	// ListStaleGroups counts the stale rows (signature non-empty and not
	// in current) by stored pipeline and signature, leaving out rows with
	// a skip for a signature in current, and discarded objects. The other
	// statuses (active, inbox, raw) stay eligible. One query, whatever the
	// rows.
	ListStaleGroups(ctx context.Context, current []string) ([]StaleGroup, error)
	// ListRederivable returns a page of one group's rows, fewest tries
	// first then by ID, after the cursor, leaving out rows with a skip for
	// the target and discarded objects.
	ListRederivable(ctx context.Context, q RederiveQuery) ([]StaleObject, error)
	// StampGroup sets the signature of every row of the group to to, except
	// discarded ones (like the selection), and reports how many. Nothing is re-derived: the rows need no change.
	StampGroup(ctx context.Context, pipeline, from, to string) (int, error)
	// RecordRederiveTry, RecordRederiveStrike and RecordRederiveSkip wrap
	// ErrNotFound when the object is gone (it was deleted since it was
	// listed): its rows cannot exist without it.
	//
	// RecordRederiveTry counts one more failed try of an object for a
	// target. Tries only order the rows: fewest first, so an object that
	// keeps failing never holds back the rest.
	RecordRederiveTry(ctx context.Context, objectID, target string) error
	// RecordRederiveStrike counts one more strike: a failure the run
	// showed to be the object's, and returns the count. Strikes alone
	// lead to a durable skip.
	RecordRederiveStrike(ctx context.Context, objectID, target string) (int, error)
	// ClearRederiveAttempts forgets an object's tries and strikes for a
	// target (it succeeded, or was skipped).
	ClearRederiveAttempts(ctx context.Context, objectID, target string) error
	// PruneRederiveRows deletes the attempts and skips whose target is not
	// in keep (a signature no definition produces any more: a target that
	// comes back later starts clean) and does not belong, by the pipeline
	// recorded with its entries, to one of keepPipelines (definitions that
	// cannot be built at this start, whose current signature is unknown).
	// It reports how many rows went. With nothing in keep it deletes
	// nothing.
	PruneRederiveRows(ctx context.Context, keep, keepPipelines []string) (int, error)
	// RecordRederiveSkip stores a skip, replacing one for the same object
	// and target.
	RecordRederiveSkip(ctx context.Context, skip RederiveSkip) error
}
