package bosaccess

import (
	"sort"
	"strings"
	"time"
)

// SensitivityClearanceType is the record type whose read grant is the
// clearance to read a record classified special_category of its own.
const SensitivityClearanceType = "core.record_sensitivities"

// Query is one read to decide: a principal (and, for an agent acting for a
// person, the person) asking to read a record type in a scope.
type Query struct {
	Principal   string       `json:"principal"`
	OnBehalfOf  string       `json:"on_behalf_of,omitempty"`
	Scope       string       `json:"scope"`
	TypeKey     string       `json:"type_key"`
	Sensitivity string       `json:"sensitivity,omitempty"`
	Direction   string       `json:"direction,omitempty"`
	Field       string       `json:"field,omitempty"`
	Record      *RecordQuery `json:"record,omitempty"`
}

// Verdict is the decision: allow with the fields to leave out, or deny with
// the reason.
type Verdict struct {
	Allow    bool     `json:"allow"`
	Excluded []string `json:"excluded,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

// Reasons of a deny.
const (
	DenyNoSet          = "no_set"
	DenyStale          = "stale"
	DenyUnknown        = "unknown_principal"
	DenyScope          = "scope_outside_binding"
	DenyNoGrant        = "no_grant"
	DenyField          = "field_excluded"
	DenyClearance      = "needs_clearance"
	DenyOnBehalfOf     = "on_behalf_of_denied"
	DenyPrincipalOf    = "principal_denied"
	DenyRecordBasis    = "record_basis_stale"
	DenyUnderUnknown   = "under_unknown"
	DenyUnderTruncated = "under_truncated"
	DenyNoEntry        = "no_record_entry"
)

// Decide decides q at now. ownLimit is the staleness limit the peer declared
// in its profile; the one enforced is the shorter of it and the set's policy.
// A peer holding no set, or one confirmed longer ago than the limit, denies.
// When an agent acts for a person both ids must be allowed.
func (h *Held) Decide(q Query, now time.Time, ownLimit time.Duration) Verdict {
	if h == nil || h.Set.Epoch == 0 {
		return Verdict{Reason: DenyNoSet}
	}
	if lim := h.Limit(ownLimit); now.Sub(h.AppliedAt) > lim {
		return Verdict{Reason: DenyStale}
	}
	a := h.decideFor(q.Principal, q, now, ownLimit)
	if !a.Allow {
		if q.OnBehalfOf != "" {
			a.Reason = DenyPrincipalOf + ":" + a.Reason
		}
		return a
	}
	if q.OnBehalfOf == "" {
		return a
	}
	b := h.decideFor(q.OnBehalfOf, q, now, ownLimit)
	if !b.Allow {
		return Verdict{Reason: DenyOnBehalfOf + ":" + b.Reason}
	}
	ex := unionStrings(a.Excluded, b.Excluded)
	if q.Field != "" && contains(ex, q.Field) {
		return Verdict{Reason: DenyField}
	}
	return Verdict{Allow: true, Excluded: ex}
}

// Limit is the staleness limit enforced: the shorter of the peer's own and
// the set's policy.
func (h *Held) Limit(own time.Duration) time.Duration {
	lim := time.Duration(h.Set.Policy.MaxStalenessSeconds) * time.Second
	if lim <= 0 || (own > 0 && own < lim) {
		lim = own
	}
	return lim
}

func (h *Held) decideFor(principal string, q Query, now time.Time, own time.Duration) Verdict {
	known := false
	for _, p := range h.Set.Principals {
		if p.ID == principal && p.Status == "active" {
			known = true
		}
	}
	if !known {
		return Verdict{Reason: DenyUnknown}
	}
	chain := h.scopeChain(q.Scope)
	if len(chain) == 0 {
		return Verdict{Reason: DenyScope}
	}
	cover := h.covering(principal, chain, q.TypeKey, q.Direction, now)
	if len(cover) == 0 {
		if q.Record == nil {
			return Verdict{Reason: DenyNoGrant}
		}
		v := h.decideRecord(principal, chain, q, now, own)
		if !v.Allow {
			return v
		}
		if q.Sensitivity == "special_category" && len(h.covering(principal, chain, SensitivityClearanceType, "", now)) == 0 {
			return Verdict{Reason: DenyClearance}
		}
		return v
	}
	if q.Sensitivity == "special_category" && len(h.covering(principal, chain, SensitivityClearanceType, "", now)) == 0 {
		return Verdict{Reason: DenyClearance}
	}
	// a field is left out only when every covering grant leaves it out
	var ex []string
	for i, g := range cover {
		if i == 0 {
			ex = append([]string(nil), g.ExcludedFields...)
			continue
		}
		ex = intersectStrings(ex, g.ExcludedFields)
	}
	if q.Field != "" && contains(ex, q.Field) {
		return Verdict{Reason: DenyField}
	}
	return Verdict{Allow: true, Excluded: ex}
}

// scopeChain is the scope and its ancestors within the set, nearest first;
// empty when the scope is not one of the set.
func (h *Held) scopeChain(id string) []string {
	parent := map[string]*string{}
	for _, s := range h.Set.Scopes {
		parent[s.ID] = s.Parent
	}
	if _, ok := parent[id]; !ok {
		return nil
	}
	var out []string
	for cur, i := id, 0; i <= len(parent); i++ {
		out = append(out, cur)
		p := parent[cur]
		if p == nil {
			break
		}
		cur = *p
	}
	return out
}

// covering returns the grants that let principal read typeKey in the scope
// chain at now: a usable grant (not one the peer cannot express) of the
// principal or of an audience it belongs to, in force, of the scope or one
// above it, naming the type (a restricted type only by its exact key), and
// for the direction asked.
func (h *Held) covering(principal string, chain []string, typeKey, direction string, now time.Time) []Grant {
	inChain := setOf(chain)
	restricted := setOf(h.Set.RestrictedTypes)
	subjects := map[string]bool{principal: true}
	for _, a := range h.Set.Audiences {
		for _, m := range a.Members {
			if m.Principal == principal && until(m.ValidUntil, now) {
				subjects[a.ID] = true
			}
		}
	}
	out := make([]Grant, 0, len(h.Set.Grants))
	for _, g := range h.Set.Grants {
		if _, no := h.Unrep[g.ID]; no || g.Permission != "read" || !subjects[g.Subject] || !inChain[g.Target.Scope] || !until(g.ValidUntil, now) {
			continue
		}
		if !typeCovers(g.Target.TypeKey, typeKey, restricted[typeKey]) {
			continue
		}
		if g.Direction != "" && g.Direction != direction {
			continue
		}
		out = append(out, g)
	}
	return out
}

// RecordQuery is the record a read is about: its address and what the peer
// holds of what it stands under (data.under of its last event).
type RecordQuery struct {
	Address string `json:"address"`
	// Under is nil when the peer does not know what the record stands under
	// (it denies); an empty list is known: the record stands under nothing
	// but itself.
	Under []string `json:"under,omitzero"`
	// UnderTruncated: the list is incomplete (the peer denies).
	UnderTruncated bool `json:"under_truncated,omitempty"`
}

// recordLimit is the age of the last complete feed catch-up past which a
// record-level read is denied: the set's record-basis limit, else the set's
// staleness limit, and never longer than the peer's own.
func (h *Held) recordLimit(own time.Duration) time.Duration {
	lim := time.Duration(h.Set.Policy.RecordBasisMaxStalenessSeconds) * time.Second
	if lim <= 0 {
		return h.Limit(own)
	}
	if own > 0 && own < lim {
		lim = own
	}
	return lim
}

// decideRecord is the record-level path of a read the type grants did not
// cover: the floors (record basis fresh, under known and complete), then the
// entries of the principal. The clearance of a special-category record is the
// caller's.
func (h *Held) decideRecord(principal string, chain []string, q Query, now time.Time, own time.Duration) Verdict {
	rec := q.Record
	if h.RecordBasisAt.IsZero() || now.Sub(h.RecordBasisAt) > h.recordLimit(own) {
		return Verdict{Reason: DenyRecordBasis}
	}
	if rec.Under == nil {
		return Verdict{Reason: DenyUnderUnknown}
	}
	if rec.UnderTruncated {
		return Verdict{Reason: DenyUnderTruncated}
	}
	if len(h.coveringEntries(principal, chain, q.TypeKey, rec, now)) == 0 {
		return Verdict{Reason: DenyNoEntry}
	}
	return Verdict{Allow: true}
}

// coveringEntries returns the entries of the principal that let it read the
// record at now: usable, in force (started and not ended), of the scope chain
// of the record, naming its type (a restricted type only by its exact key),
// and anchored at a record the read stands under or at the record itself.
func (h *Held) coveringEntries(principal string, chain []string, typeKey string, rec *RecordQuery, now time.Time) []RecordEntry {
	inChain := setOf(chain)
	restricted := setOf(h.Set.RestrictedTypes)
	anchors := map[string]bool{rec.Address: true}
	for _, a := range rec.Under {
		anchors[a] = true
	}
	out := make([]RecordEntry, 0, len(h.Set.RecordAccess))
	for _, e := range h.Set.RecordAccess {
		if _, no := h.UnrepEntries[e.ID]; no || e.Subject != principal || !inChain[e.Scope] || !anchors[e.Anchor] {
			continue
		}
		if !typeCovers(e.TypeKey, typeKey, restricted[typeKey]) || !started(e.ValidFrom, now) || !until(e.ValidUntil, now) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// started reports whether an instant (nil: already) is at or before now. An
// unreadable start never grants.
func started(t *string, now time.Time) bool {
	if t == nil {
		return true
	}
	from, err := time.Parse(time.RFC3339Nano, *t)
	return err == nil && !now.Before(from)
}

// until reports whether an instant (nil: open) is still ahead of now. An
// unreadable end never grants.
func until(t *string, now time.Time) bool {
	if t == nil {
		return true
	}
	end, err := time.Parse(time.RFC3339Nano, *t)
	return err == nil && now.Before(end)
}

// typeCovers: nil covers every type that is not restricted; "ns.*" every
// unrestricted type of the namespace; a key only itself. A restricted type is
// covered only by its exact key.
func typeCovers(limit *string, typ string, restricted bool) bool {
	switch {
	case limit == nil:
		return !restricted
	case *limit == typ:
		return true
	case restricted:
		return false
	}
	ns, ok := strings.CutSuffix(*limit, ".*")
	return ok && typ != "" && strings.HasPrefix(typ, ns+".")
}

func setOf(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func intersectStrings(a, b []string) []string {
	var out []string
	for _, x := range a {
		if contains(b, x) {
			out = append(out, x)
		}
	}
	return out
}

func unionStrings(a, b []string) []string {
	out := append([]string(nil), a...)
	for _, x := range b {
		if !contains(out, x) {
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}
