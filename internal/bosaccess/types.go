// Package bosaccess holds the access sets an external authority (bos,
// contract bos.access/v1, document 1.1) provisions, and enforces the
// record-level entries in them on knowledge reads.
//
// It is the peer side of the contract: apply a full set or a delta by epoch
// and digest, keep it durably, take what each record stands under from the
// change feed, decide a read, and list the records a person may read now.
// The semantics follow the contract text and its conformance fixtures
// (testdata/fixtures); nothing here imports the authority.
package bosaccess

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// Contract is the id of the access provisioning contract, major included.
const Contract = "bos.access/v1"

// Modes of an access set.
const (
	ModeFull  = "full"
	ModeDelta = "delta"
)

// Error codes of the access contract.
const (
	CodeEpochStale     = "epoch_stale"
	CodeBaseEpoch      = "base_epoch_mismatch"
	CodeDigestMismatch = "digest_mismatch"
	CodeSetInvalid     = "set_invalid"
	CodeUnavailable    = "unavailable"
)

// Grant shapes a profile may list.
const (
	ShapeTypeGrant      = "type_grant"
	ShapeFieldExclusion = "field_exclusion"
	ShapeDirectionLimit = "direction_limit"
	ShapeExpiry         = "expiry"
	ShapeRowRelation    = "row_relation"
	ShapeRecordGrant    = "record_grant"
	ShapeOperation      = "operation_grant"
)

// FeatureRecordAccess is the feature of the contract a peer lists when it
// holds record entries and the under of each record.
const FeatureRecordAccess = "record_access"

// FeaturePrincipalIdentities is the feature a peer lists to be told the
// external identities (issuer, subject) of the principals a set names, so it
// can map its own sign-ins to them.
const FeaturePrincipalIdentities = "principal_identities"

// Vias of a record entry.
const (
	ViaAssignment  = "assignment"
	ViaRecordGrant = "record_grant"
)

// Error is an error in the vocabulary of the access contract.
type Error struct {
	Code         string
	Message      string
	AppliedEpoch int64
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Policy is what a set tells the peer about the numbers it enforces.
type Policy struct {
	MaxStalenessSeconds            int64 `json:"max_staleness_seconds"`
	ReconcileIntervalSeconds       int64 `json:"reconcile_interval_seconds"`
	RecordBasisMaxStalenessSeconds int64 `json:"record_basis_max_staleness_seconds,omitempty"`
}

// RecordEntry is one record-level entry of a set: a person in relation to an
// anchor, for a scope and a type, during a period. A property this version
// does not know is kept in Extra; the entry is then one the peer cannot
// represent and denies.
type RecordEntry struct {
	ID         string                     `json:"id"`
	Subject    string                     `json:"subject"`
	Anchor     string                     `json:"anchor"`
	Scope      string                     `json:"scope"`
	TypeKey    *string                    `json:"type_key"`
	Via        string                     `json:"via"`
	Grant      string                     `json:"grant"`
	ValidFrom  *string                    `json:"valid_from,omitempty"`
	ValidUntil *string                    `json:"valid_until,omitempty"` // exclusive
	Extra      map[string]json.RawMessage `json:"-"`
}

var knownEntryKeys = map[string]bool{"id": true, "subject": true, "anchor": true, "scope": true,
	"type_key": true, "via": true, "grant": true, "valid_from": true, "valid_until": true}

// Unknown lists the properties of the entry this version does not know.
func (e RecordEntry) Unknown() []string { return sortedKeys(e.Extra) }

type entryAlias RecordEntry

// UnmarshalJSON reads an entry and notes the properties it does not know.
func (e *RecordEntry) UnmarshalJSON(b []byte) error {
	var a entryAlias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	a.Extra = extras(b, knownEntryKeys)
	*e = RecordEntry(a)
	return nil
}

// MarshalJSON writes the entry with the properties it was read with.
func (e RecordEntry) MarshalJSON() ([]byte, error) {
	return withExtras(entryAlias(e), e.Extra)
}

// Scope is a scope of the set with its parent (nil for the root).
type Scope struct {
	ID     string  `json:"id"`
	Parent *string `json:"parent"`
}

// Principal is a principal the set names.
type Principal struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	// Identities are the identities the principal has outside the authority,
	// sent to a peer that lists the feature principal_identities. They grant
	// nothing: a grant does.
	Identities []Identity `json:"identities,omitempty"`
}

// Identity is one external identity of a principal: the issuer that vouches
// for it and the subject the issuer gives the principal.
type Identity struct {
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
}

// Member is a member of an audience.
type Member struct {
	Principal  string  `json:"principal"`
	ValidUntil *string `json:"valid_until,omitempty"`
}

// Audience is an identity group with its flattened members.
type Audience struct {
	ID      string   `json:"id"`
	Members []Member `json:"members"`
}

// Target is what a grant covers: a scope (and below) and a type.
type Target struct {
	Scope   string  `json:"scope"`
	TypeKey *string `json:"type_key"`
}

// Grant is one read grant of a set. A property this version does not know is
// kept in Extra; the grant is then one the peer cannot represent.
type Grant struct {
	ID             string                     `json:"id"`
	Subject        string                     `json:"subject"`
	Target         Target                     `json:"target"`
	Permission     string                     `json:"permission"`
	ExcludedFields []string                   `json:"excluded_fields,omitempty"`
	Direction      string                     `json:"direction,omitempty"`
	ValidUntil     *string                    `json:"valid_until,omitempty"`
	Extra          map[string]json.RawMessage `json:"-"`
}

var knownGrantKeys = map[string]bool{"id": true, "subject": true, "target": true, "permission": true,
	"excluded_fields": true, "direction": true, "valid_until": true}

// Unknown lists the properties of the grant this version does not know.
func (g Grant) Unknown() []string { return sortedKeys(g.Extra) }

type grantAlias Grant

// UnmarshalJSON reads a grant and notes the properties it does not know.
func (g *Grant) UnmarshalJSON(b []byte) error {
	var a grantAlias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	a.Extra = extras(b, knownGrantKeys)
	*g = Grant(a)
	return nil
}

// MarshalJSON writes the grant with the properties it was read with.
func (g Grant) MarshalJSON() ([]byte, error) { return withExtras(grantAlias(g), g.Extra) }

// Operation is one operation grant of a set. The peer does not represent
// operation grants: it holds them for the digest, denies them and reports
// them unrepresentable.
type Operation struct {
	ID         string  `json:"id"`
	Subject    string  `json:"subject"`
	Scope      string  `json:"scope"`
	Operation  string  `json:"operation"`
	ValidUntil *string `json:"valid_until,omitempty"`
}

// Set is the document the authority pushes: who may read what in one scope.
type Set struct {
	Contract        string      `json:"contract"`
	Epoch           int64       `json:"epoch"`
	Mode            string      `json:"mode"`
	BaseEpoch       int64       `json:"base_epoch,omitempty"`
	Digest          string      `json:"digest"`
	Scope           string      `json:"scope"`
	GeneratedAt     string      `json:"generated_at"`
	FeedSeq         int64       `json:"feed_seq"`
	Policy          Policy      `json:"policy"`
	Scopes          []Scope     `json:"scopes"`
	Principals      []Principal `json:"principals"`
	Audiences       []Audience  `json:"audiences"`
	RestrictedTypes []string    `json:"restricted_types"`
	Grants          []Grant     `json:"grants"`
	// Operations are replaced whole in a delta.
	Operations          []Operation   `json:"operations,omitempty"`
	Removed             []string      `json:"removed,omitempty"`
	RecordAccess        []RecordEntry `json:"record_access,omitempty"`
	RecordAccessRemoved []string      `json:"record_access_removed,omitempty"`
}

// Normalize sorts every list the digest is defined over and gives empty
// lists as empty, not null.
func (s *Set) Normalize() {
	sort.Slice(s.Scopes, func(i, j int) bool { return s.Scopes[i].ID < s.Scopes[j].ID })
	sort.Slice(s.Principals, func(i, j int) bool { return s.Principals[i].ID < s.Principals[j].ID })
	for i := range s.Principals {
		ids := s.Principals[i].Identities
		sort.Slice(ids, func(a, b int) bool {
			if ids[a].Issuer != ids[b].Issuer {
				return ids[a].Issuer < ids[b].Issuer
			}
			return ids[a].Subject < ids[b].Subject
		})
	}
	sort.Slice(s.Audiences, func(i, j int) bool { return s.Audiences[i].ID < s.Audiences[j].ID })
	for i := range s.Audiences {
		m := s.Audiences[i].Members
		sort.Slice(m, func(a, b int) bool { return m[a].Principal < m[b].Principal })
		if m == nil {
			s.Audiences[i].Members = []Member{}
		}
	}
	sort.Strings(s.RestrictedTypes)
	sort.Slice(s.Grants, func(i, j int) bool { return s.Grants[i].ID < s.Grants[j].ID })
	for i := range s.Grants {
		sort.Strings(s.Grants[i].ExcludedFields)
	}
	sort.Slice(s.Operations, func(i, j int) bool { return s.Operations[i].ID < s.Operations[j].ID })
	sort.Strings(s.Removed)
	sort.Slice(s.RecordAccess, func(i, j int) bool { return s.RecordAccess[i].ID < s.RecordAccess[j].ID })
	sort.Strings(s.RecordAccessRemoved)
	if s.Scopes == nil {
		s.Scopes = []Scope{}
	}
	if s.Principals == nil {
		s.Principals = []Principal{}
	}
	if s.Audiences == nil {
		s.Audiences = []Audience{}
	}
	if s.RestrictedTypes == nil {
		s.RestrictedTypes = []string{}
	}
	if s.Grants == nil {
		s.Grants = []Grant{}
	}
}

// content is what the digest is defined over.
type content struct {
	Audiences       []Audience    `json:"audiences"`
	Grants          []Grant       `json:"grants"`
	Principals      []Principal   `json:"principals"`
	RestrictedTypes []string      `json:"restricted_types"`
	Scopes          []Scope       `json:"scopes"`
	Operations      []Operation   `json:"operations,omitempty"`
	RecordAccess    []RecordEntry `json:"record_access,omitempty"`
}

// ContentDigest is the digest of the content of the set (the full state it
// produces): sha256 of the canonical JSON of {audiences, grants, principals,
// record_access, restricted_types, scopes}, lists sorted as Normalize does,
// operations and record_access only when not empty. The digest of a delta is
// the one of the result.
func ContentDigest(s Set) string {
	c := s
	c.Normalize()
	return digestOf(content{c.Audiences, c.Grants, c.Principals, c.RestrictedTypes, c.Scopes, c.Operations, c.RecordAccess})
}

// RecordAccessDigest is the digest of a list of entries: sha256 of the
// canonical JSON of the array sorted by id.
func RecordAccessDigest(entries []RecordEntry) string {
	es := append([]RecordEntry(nil), entries...)
	sort.Slice(es, func(i, j int) bool { return es[i].ID < es[j].ID })
	if es == nil {
		es = []RecordEntry{}
	}
	return digestOf(es)
}

func digestOf(v any) string {
	raw, _ := marshalPlain(v)
	var anyv any
	_ = json.Unmarshal(raw, &anyv)
	canon, _ := marshalPlain(anyv) // maps marshal with sorted keys
	sum := sha256.Sum256(canon)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func marshalPlain(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func sortedKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func extras(b []byte, known map[string]bool) map[string]json.RawMessage {
	var raw map[string]json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		return nil
	}
	var out map[string]json.RawMessage
	for k, v := range raw {
		if !known[k] {
			if out == nil {
				out = map[string]json.RawMessage{}
			}
			out[k] = v
		}
	}
	return out
}

func withExtras(v any, extra map[string]json.RawMessage) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil || len(extra) == 0 {
		return b, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for k, v := range extra {
		m[k] = v
	}
	return json.Marshal(m)
}

// checkRefs: every grant subject is a principal or audience of the set,
// every audience member a principal, every target a scope of the set, every
// id unique, one root scope.
func (s Set) checkRefs() error {
	subjects, principals, err := s.checkSubjects()
	if err != nil {
		return err
	}
	scopes, err := s.checkScopes()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, g := range s.Grants {
		if seen[g.ID] {
			return fmt.Errorf("grant %s listed twice", g.ID)
		}
		seen[g.ID] = true
		if !subjects[g.Subject] {
			return fmt.Errorf("grant %s names subject %s that is not in the set", g.ID, g.Subject)
		}
		if !scopes[g.Target.Scope] {
			return fmt.Errorf("grant %s targets scope %s that is not in the set", g.ID, g.Target.Scope)
		}
	}
	entries := map[string]bool{}
	for _, e := range s.RecordAccess {
		if entries[e.ID] {
			return fmt.Errorf("record entry %s listed twice", e.ID)
		}
		entries[e.ID] = true
		if !principals[e.Subject] {
			return fmt.Errorf("record entry %s names subject %s that is not a principal of the set", e.ID, e.Subject)
		}
		if !scopes[e.Scope] {
			return fmt.Errorf("record entry %s targets scope %s that is not in the set", e.ID, e.Scope)
		}
	}
	for _, o := range s.Operations {
		if seen[o.ID] {
			return fmt.Errorf("grant %s listed twice", o.ID)
		}
		seen[o.ID] = true
		if !subjects[o.Subject] {
			return fmt.Errorf("operation grant %s names subject %s that is not in the set", o.ID, o.Subject)
		}
		if !scopes[o.Scope] {
			return fmt.Errorf("operation grant %s targets scope %s that is not in the set", o.ID, o.Scope)
		}
	}
	return nil
}

// checkSubjects returns the subjects (principals and audiences) and the
// principals of the set, refusing a duplicate or a member that is no
// principal.
func (s Set) checkSubjects() (subjects, principals map[string]bool, err error) {
	subjects, principals = map[string]bool{}, map[string]bool{}
	for _, p := range s.Principals {
		if principals[p.ID] {
			return nil, nil, fmt.Errorf("principal %s listed twice", p.ID)
		}
		principals[p.ID], subjects[p.ID] = true, true
	}
	for _, a := range s.Audiences {
		if subjects[a.ID] {
			return nil, nil, fmt.Errorf("audience %s listed twice or also a principal", a.ID)
		}
		subjects[a.ID] = true
		for _, m := range a.Members {
			if !principals[m.Principal] {
				return nil, nil, fmt.Errorf("audience %s member %s is not a principal of the set", a.ID, m.Principal)
			}
		}
	}
	return subjects, principals, nil
}

// checkScopes returns the scopes of the set, refusing a duplicate, a parent
// outside the set, and a tree without exactly one root, the set's scope.
func (s Set) checkScopes() (map[string]bool, error) {
	scopes := map[string]bool{}
	roots := 0
	for _, sc := range s.Scopes {
		if scopes[sc.ID] {
			return nil, fmt.Errorf("scope %s listed twice", sc.ID)
		}
		scopes[sc.ID] = true
		if sc.Parent == nil {
			roots++
		}
	}
	for _, sc := range s.Scopes {
		if sc.Parent != nil && !scopes[*sc.Parent] {
			return nil, fmt.Errorf("scope %s has a parent outside the set", sc.ID)
		}
	}
	if roots != 1 || !scopes[s.Scope] {
		return nil, fmt.Errorf("the scopes of the set need exactly one root, the scope %s", s.Scope)
	}
	return scopes, nil
}

// Ack is a peer's answer to an applied set.
type Ack struct {
	Contract        string      `json:"contract"`
	Epoch           int64       `json:"epoch"`
	State           string      `json:"state"`
	Digest          string      `json:"digest"`
	AppliedAt       string      `json:"applied_at"`
	Unrepresentable []AckUnrep  `json:"unrepresentable"`
	RecordAccess    *AckRecords `json:"record_access,omitempty"`
}

// AckRecords is a peer's report of the record entries it holds: how many,
// their digest, and the ones it cannot express by id.
type AckRecords struct {
	Count           int             `json:"count"`
	Digest          string          `json:"digest"`
	Unrepresentable []AckUnrepEntry `json:"unrepresentable"`
}

// AckUnrepEntry is a record entry the peer reports it cannot express.
type AckUnrepEntry struct {
	Entry  string `json:"entry"`
	Reason string `json:"reason,omitempty"`
}

// AckUnrep is a grant the peer reports it cannot express.
type AckUnrep struct {
	Grant  string `json:"grant"`
	Reason string `json:"reason,omitempty"`
}

// Report is what a peer holds for a scope now (the state route).
type Report struct {
	Contract            string      `json:"contract"`
	AppliedEpoch        *int64      `json:"applied_epoch"`
	Digest              *string     `json:"digest"`
	AppliedAt           *string     `json:"applied_at"`
	MaxStalenessSeconds int64       `json:"max_staleness_seconds"`
	Unrepresentable     []AckUnrep  `json:"unrepresentable"`
	RecordAccess        *AckRecords `json:"record_access,omitempty"`
}
