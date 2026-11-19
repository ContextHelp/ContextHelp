package bosaccess

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Held is the access set a peer holds for a scope: what it applied, when it
// last counted the set as confirmed by the authority (its clock), and what
// it cannot express.
type Held struct {
	Set       Set
	AppliedAt time.Time
	// Unrep maps the id of a grant (or operation grant) the peer denies to
	// the reason.
	Unrep map[string]string
	// UnrepEntries maps the id of a record entry the peer denies to the reason.
	UnrepEntries map[string]string
	// RecordBasisAt is when the peer last completed a catch-up of the change
	// feed, by its clock; zero when it never did. It belongs to the feed, not
	// to a set: a set replaces what is held and keeps it.
	RecordBasisAt time.Time
}

// Result is the outcome of Apply.
type Result struct {
	Ack  Ack
	Held *Held
}

// Apply is what a peer does with an incoming set: it refuses an older epoch
// (epoch_stale), answers a replay of the held epoch and digest by changing
// nothing but the time it counts the set's age from, refuses a delta whose
// base is not the held epoch (base_epoch_mismatch) or whose result has
// another digest than it carries (digest_mismatch), and lists what it cannot
// express (a shape its profile does not list, a property it does not know)
// and denies it. held is nil when the peer holds nothing; it is not changed.
func Apply(held *Held, set Set, now time.Time, prof Profile) (Result, error) {
	e := func(code, msg string, applied int64) error {
		return &Error{Code: code, Message: msg, AppliedEpoch: applied}
	}
	var heldEpoch int64
	if held != nil {
		heldEpoch = held.Set.Epoch
	}
	if heldEpoch > 0 && set.Epoch < heldEpoch {
		return Result{}, e(CodeEpochStale, fmt.Sprintf("epoch %d is older than the applied %d", set.Epoch, heldEpoch), heldEpoch)
	}
	if heldEpoch > 0 && set.Epoch == heldEpoch {
		if held.Set.Digest != set.Digest {
			return Result{}, e(CodeEpochStale, fmt.Sprintf("epoch %d is held with another digest", set.Epoch), heldEpoch)
		}
		h := *held
		h.AppliedAt = now
		return Result{Ack: h.ack("replayed"), Held: &h}, nil
	}
	next := set
	if set.Mode == ModeDelta {
		if heldEpoch == 0 || set.BaseEpoch != heldEpoch {
			return Result{}, e(CodeBaseEpoch, fmt.Sprintf("delta applies to epoch %d, the peer holds %d", set.BaseEpoch, heldEpoch), heldEpoch)
		}
		next = mergeDelta(held.Set, set)
	}
	if err := next.checkRefs(); err != nil {
		return Result{}, e(CodeSetInvalid, err.Error(), heldEpoch)
	}
	if got := ContentDigest(next); got != set.Digest {
		return Result{}, e(CodeDigestMismatch, fmt.Sprintf("the result has digest %s, the set says %s", got, set.Digest), heldEpoch)
	}
	next.Mode, next.BaseEpoch, next.Removed, next.RecordAccessRemoved = ModeFull, 0, nil, nil
	h := &Held{Set: next, AppliedAt: now, Unrep: map[string]string{}, UnrepEntries: map[string]string{}}
	if held != nil {
		h.RecordBasisAt = held.RecordBasisAt
	}
	for _, g := range next.Grants {
		if reason := cannotExpress(g, prof); reason != "" {
			h.Unrep[g.ID] = reason
		}
	}
	for _, o := range next.Operations {
		if !prof.Expresses(ShapeOperation) {
			h.Unrep[o.ID] = "the peer does not represent " + ShapeOperation
		}
	}
	for _, en := range next.RecordAccess {
		if reason := cannotExpressEntry(en, prof); reason != "" {
			h.UnrepEntries[en.ID] = reason
		}
	}
	return Result{Ack: h.ack("applied"), Held: h}, nil
}

func (h *Held) ack(state string) Ack {
	a := Ack{Contract: Contract, Epoch: h.Set.Epoch, State: state, Digest: h.Set.Digest,
		AppliedAt: h.AppliedAt.UTC().Format(time.RFC3339Nano), Unrepresentable: []AckUnrep{}}
	for _, id := range sortedIDs(h.Unrep) {
		a.Unrepresentable = append(a.Unrepresentable, AckUnrep{Grant: id, Reason: h.Unrep[id]})
	}
	a.RecordAccess = h.recordAck()
	return a
}

// recordAck is what the peer reports of the record entries it holds; nil
// when it holds none.
func (h *Held) recordAck() *AckRecords {
	if len(h.Set.RecordAccess) == 0 {
		return nil
	}
	r := &AckRecords{Count: len(h.Set.RecordAccess), Digest: RecordAccessDigest(h.Set.RecordAccess), Unrepresentable: []AckUnrepEntry{}}
	for _, id := range sortedIDs(h.UnrepEntries) {
		r.Unrepresentable = append(r.Unrepresentable, AckUnrepEntry{Entry: id, Reason: h.UnrepEntries[id]})
	}
	return r
}

// Report is what the peer serves at the state route. h may be nil.
func (h *Held) Report(own time.Duration) Report {
	r := Report{Contract: Contract, Unrepresentable: []AckUnrep{}}
	if h == nil || h.Set.Epoch == 0 {
		r.MaxStalenessSeconds = int64(own.Seconds())
		return r
	}
	ep, dg, at := h.Set.Epoch, h.Set.Digest, h.AppliedAt.UTC().Format(time.RFC3339Nano)
	r.AppliedEpoch, r.Digest, r.AppliedAt = &ep, &dg, &at
	r.MaxStalenessSeconds = int64(h.Limit(own).Seconds())
	ack := h.ack("applied")
	r.Unrepresentable, r.RecordAccess = ack.Unrepresentable, ack.RecordAccess
	return r
}

func sortedIDs(m map[string]string) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// cannotExpressEntry returns why the peer cannot enforce the record entry as
// written, "" when it can: a property it does not know, or a shape or the
// feature its profile does not list.
func cannotExpressEntry(e RecordEntry, prof Profile) string {
	if u := e.Unknown(); len(u) > 0 {
		return "unknown entry property " + strings.Join(u, ", ")
	}
	if !prof.HasFeature(Contract, FeatureRecordAccess) {
		return "the peer does not list the feature " + FeatureRecordAccess
	}
	var need string
	switch e.Via {
	case ViaAssignment:
		need = ShapeRowRelation
	case ViaRecordGrant:
		need = ShapeRecordGrant
	default:
		return "unknown entry via " + e.Via
	}
	if !prof.Expresses(need) {
		return "the peer does not represent " + need
	}
	return ""
}

// cannotExpress returns why the peer cannot enforce the grant as written, ""
// when it can.
func cannotExpress(g Grant, prof Profile) string {
	if u := g.Unknown(); len(u) > 0 {
		return "unknown grant property " + strings.Join(u, ", ")
	}
	need := []string{ShapeTypeGrant}
	if g.ValidUntil != nil {
		need = append(need, ShapeExpiry)
	}
	if len(g.ExcludedFields) > 0 {
		need = append(need, ShapeFieldExclusion)
	}
	if g.Direction != "" {
		need = append(need, ShapeDirectionLimit)
	}
	for _, s := range need {
		if !prof.Expresses(s) {
			return "the peer does not represent " + s
		}
	}
	return ""
}

// mergeDelta applies a delta to the held set: principals, audiences, scopes
// and restricted types are replaced whole, grants and record entries are
// added or replaced by id, removed ones are dropped.
func mergeDelta(base, d Set) Set {
	out := d
	byID := map[string]Grant{}
	for _, g := range base.Grants {
		byID[g.ID] = g
	}
	for _, id := range d.Removed {
		delete(byID, id)
	}
	for _, g := range d.Grants {
		byID[g.ID] = g
	}
	out.Grants = make([]Grant, 0, len(byID))
	for _, g := range byID {
		out.Grants = append(out.Grants, g)
	}
	entries := map[string]RecordEntry{}
	for _, e := range base.RecordAccess {
		entries[e.ID] = e
	}
	for _, id := range d.RecordAccessRemoved {
		delete(entries, id)
	}
	for _, e := range d.RecordAccess {
		entries[e.ID] = e
	}
	out.RecordAccess = nil
	for _, e := range entries {
		out.RecordAccess = append(out.RecordAccess, e)
	}
	out.Normalize()
	return out
}
