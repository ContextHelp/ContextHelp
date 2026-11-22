package storage

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The steward entries of the read audit form a hash chain (ADR-083 §5,
// tamper evidence): each one stores the digest of the entry before it
// and its own digest over that and its canonical encoding, in append
// order, one chain per instance. A deleted or edited entry breaks the
// chain at that entry, which VerifyAccessChain reports. It does not
// stop a writer with database access from rewriting the whole chain;
// the signed export manifest commits to the head for that.
//
// Canonical encoding, version ctxt-access-chain/v1. One line per field,
// each ending in "\n", in this order; text values are Go-quoted
// (strconv.Quote), so no value can end its line early:
//
//	ctxt-access-chain/v1
//	prev <quoted prev_digest>
//	seq <decimal>
//	id <quoted>
//	at <Unix milliseconds, decimal>
//	principal <quoted>
//	identity <quoted>
//	route <quoted>
//	decision <quoted>
//	operation <quoted operation digest>
//	policy <quoted policy versions>
//	steward_grant <quoted>
//	objects <decimal object count> <quoted objects digest>
//	entities <decimal entity count>
//	checks <decimal number of checks>
//	check <quoted id> <quoted outcome> <quoted reason>   (one per check, in trail order)
//
// The entry digest is DigestPrefix and the lowercase hex SHA-256 of
// those bytes: the previous digest comes first, so an entry's digest
// commits to every entry before it. The first entry's prev is "".
//
// The objects digest (ctxt-access-objects/v1) is DigestPrefix and the
// hex SHA-256 of the line "ctxt-access-objects/v1" followed by one
// line per object id, quoted, sorted and without repeats. The entry
// stores it, so the chain still verifies once retention has pruned the
// list itself.
//
// Times are kept to the millisecond, the resolution both stores read
// back exactly: LinkAccessEntry truncates At before it hashes.
const (
	accessChainVersion   = "ctxt-access-chain/v1"
	accessObjectsVersion = "ctxt-access-objects/v1"
)

// AccessChainLink is a steward entry's place in the chain.
type AccessChainLink struct {
	// Seq is the entry's position, from 1, in append order.
	Seq int64 `json:"seq"`
	// PrevDigest is the entry digest of the entry at Seq-1; "" at 1.
	PrevDigest string `json:"prev_digest"`
	// EntryDigest is the digest of PrevDigest and the canonical entry.
	EntryDigest string `json:"entry_digest"`
	// ObjectsDigest is the digest of the entry's object id list.
	ObjectsDigest string `json:"objects_digest"`
}

// AccessChainHead is the last link of the chain: its sequence number
// and entry digest. The zero head is the empty chain.
type AccessChainHead struct {
	Seq    int64  `json:"seq"`
	Digest string `json:"digest"`
}

// AccessObjectsDigest is the digest of an object id list: sorted, each
// id once (see the encoding above).
func AccessObjectsDigest(ids []string) string {
	sorted := slices.Clone(ids)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)
	var b strings.Builder
	b.WriteString(accessObjectsVersion)
	b.WriteByte('\n')
	for _, id := range sorted {
		b.WriteString(strconv.Quote(id))
		b.WriteByte('\n')
	}
	return digestOf(b.String())
}

// AccessEntryDigest is the digest of e at position seq after prev, with
// objectsDigest standing for its object id list (see the encoding
// above).
func AccessEntryDigest(prev string, seq int64, e *AccessEntry, objectsDigest string) string {
	return digestOf(canonicalAccessEntry(prev, seq, e, objectsDigest))
}

func canonicalAccessEntry(prev string, seq int64, e *AccessEntry, objectsDigest string) string {
	var b strings.Builder
	line := func(name string, values ...string) {
		b.WriteString(name)
		for _, v := range values {
			b.WriteByte(' ')
			b.WriteString(v)
		}
		b.WriteByte('\n')
	}
	q := strconv.Quote
	b.WriteString(accessChainVersion)
	b.WriteByte('\n')
	line("prev", q(prev))
	line("seq", strconv.FormatInt(seq, 10))
	line("id", q(e.ID))
	line("at", strconv.FormatInt(e.At.UnixMilli(), 10))
	line("principal", q(e.PrincipalID))
	line("identity", q(e.IdentityID))
	line("route", q(e.Route))
	line("decision", q(string(e.Decision)))
	line("operation", q(e.OperationDigest))
	line("policy", q(e.PolicyVersions))
	line("steward_grant", q(e.StewardGrantID))
	line("objects", strconv.Itoa(e.ObjectCount), q(objectsDigest))
	line("entities", strconv.Itoa(e.EntityCount))
	line("checks", strconv.Itoa(len(e.Checks)))
	for _, c := range e.Checks {
		line("check", q(c.Check), q(string(c.Outcome)), q(c.Reason))
	}
	return b.String()
}

func digestOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return DigestPrefix + hex.EncodeToString(sum[:])
}

// LinkAccessEntry places e after head: At kept to the millisecond, its
// objects digest and its entry digest computed, e.Chain set. It returns
// the new head. Drivers call it inside the transaction that holds the
// chain head, after PrepareAccessEntry.
func LinkAccessEntry(head AccessChainHead, e *AccessEntry) AccessChainHead {
	e.At = e.At.UTC().Truncate(time.Millisecond)
	link := &AccessChainLink{Seq: head.Seq + 1, PrevDigest: head.Digest, ObjectsDigest: AccessObjectsDigest(e.ObjectIDs)}
	link.EntryDigest = AccessEntryDigest(link.PrevDigest, link.Seq, e, link.ObjectsDigest)
	e.Chain = link
	return AccessChainHead{Seq: link.Seq, Digest: link.EntryDigest}
}

// PrepareStewardEntry fills the defaults of a steward entry about to be
// appended and validates it, as AccessLogStore.AppendSteward documents.
func PrepareStewardEntry(e *AccessEntry, now time.Time) error {
	if e == nil {
		return errors.New("access entry: nil")
	}
	if e.StewardGrantID == "" {
		return errors.New("access entry: a steward entry names its steward grant")
	}
	if len(e.ObjectIDs) > 0 {
		ids := slices.Clone(e.ObjectIDs)
		slices.Sort(ids)
		e.ObjectIDs = slices.Compact(ids)
	}
	return prepareAccessEntry(e, now)
}

// AccessChainBreak is the first place the chain does not hold.
type AccessChainBreak struct {
	// Seq is the position the break is at.
	Seq int64
	// EntryID is the entry at Seq, "" when no entry holds it.
	EntryID string
	// Reason says what does not hold.
	Reason string
}

func (b *AccessChainBreak) Error() string {
	if b.EntryID == "" {
		return fmt.Sprintf("access chain broken at %d: %s", b.Seq, b.Reason)
	}
	return fmt.Sprintf("access chain broken at %d (%s): %s", b.Seq, b.EntryID, b.Reason)
}

// VerifyAccessChain walks the steward entries in append order and
// returns the first *AccessChainBreak, or nil when the chain holds:
// positions run from 1 without a gap or a repeat, each entry names the
// digest of the one before it, each digest recomputes from the entry as
// stored, each retained object id list hashes to the entry's objects
// digest, and head is the last entry's. entries are what
// AccessLogStore.StewardChain returns, in any order.
//
// keptSince is where steward retention (steward.audit_retention) cut
// the object id lists: an entry older than it may have an empty list,
// pruned. The zero time keeps every list, so every list must be whole.
func VerifyAccessChain(head AccessChainHead, entries []*AccessEntry, keptSince time.Time) error {
	sorted := slices.Clone(entries)
	for _, e := range sorted {
		if e.Chain == nil {
			return &AccessChainBreak{EntryID: e.ID, Reason: "steward entry is not in the chain"}
		}
	}
	slices.SortFunc(sorted, func(a, b *AccessEntry) int { return cmp.Compare(a.Chain.Seq, b.Chain.Seq) })
	prev := ""
	for i, e := range sorted {
		seq := int64(i + 1)
		link := e.Chain
		switch {
		case link.Seq > seq:
			return &AccessChainBreak{Seq: seq, Reason: "no entry holds this position"}
		case link.Seq < seq:
			return &AccessChainBreak{Seq: link.Seq, EntryID: e.ID, Reason: "two entries hold this position"}
		case link.PrevDigest != prev:
			return &AccessChainBreak{Seq: seq, EntryID: e.ID, Reason: "prev_digest is not the digest of the entry before"}
		}
		pruned := !keptSince.IsZero() && e.At.Before(keptSince) && len(e.ObjectIDs) == 0
		if !pruned && AccessObjectsDigest(e.ObjectIDs) != link.ObjectsDigest {
			return &AccessChainBreak{Seq: seq, EntryID: e.ID, Reason: "object id list does not match its digest"}
		}
		if AccessEntryDigest(prev, seq, e, link.ObjectsDigest) != link.EntryDigest {
			return &AccessChainBreak{Seq: seq, EntryID: e.ID, Reason: "entry does not match its digest"}
		}
		prev = link.EntryDigest
	}
	n := int64(len(sorted))
	if head.Seq != n || head.Digest != prev {
		return &AccessChainBreak{Seq: max(head.Seq, n), Reason: fmt.Sprintf("chain head (%d) is not the last entry (%d)", head.Seq, n)}
	}
	return nil
}
