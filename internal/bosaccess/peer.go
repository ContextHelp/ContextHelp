package bosaccess

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store keeps the peer's state durably: the last set and what the records
// stand under. Load returns (nil, nil) when nothing was saved.
type Store interface {
	Load() ([]byte, error)
	Save([]byte) error
}

// MemStore is a Store in memory, for tests.
type MemStore struct {
	mu sync.Mutex
	b  []byte
}

// Load implements Store.
func (m *MemStore) Load() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.b...), nil
}

// Save implements Store.
func (m *MemStore) Save(b []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.b = append([]byte(nil), b...)
	return nil
}

// FileStore is a Store in one file, replaced atomically on every save.
type FileStore struct{ Path string }

// Load implements Store.
func (f FileStore) Load() ([]byte, error) {
	b, err := os.ReadFile(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

// Save implements Store.
func (f FileStore) Save(b []byte) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.Path), ".bosaccess-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return errors.Join(werr, cerr)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), f.Path)
}

// stored is what the peer keeps durably.
type stored struct {
	Held *Held      `json:"held,omitempty"`
	Book RecordBook `json:"book"`
}

// Peer is the access state of one scope: the set it holds, what the records
// stand under, and the clock it enforces expiry and staleness by. All
// methods are safe for concurrent use.
type Peer struct {
	prof  Profile
	store Store
	now   func() time.Time

	mu   sync.Mutex
	held *Held
	book RecordBook
	// rev counts the changes a read could see; Revision exposes it as a
	// cache key.
	rev int64
}

// Open returns a peer with the profile it declares, reading back what store
// kept. A nil store keeps nothing across restarts. A nil clock is time.Now.
func Open(prof Profile, store Store, now func() time.Time) (*Peer, error) {
	if now == nil {
		now = time.Now
	}
	p := &Peer{prof: prof, store: store, now: now}
	if err := p.load(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Peer) load() error {
	p.held, p.book = nil, RecordBook{}
	if p.store == nil {
		return nil
	}
	b, err := p.store.Load()
	if err != nil {
		return fmt.Errorf("bosaccess: load: %w", err)
	}
	if len(b) == 0 {
		return nil
	}
	var s stored
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("bosaccess: load: %w", err)
	}
	p.held, p.book = s.Held, s.Book
	return nil
}

// persist writes the state; the caller holds the lock. A failed write fails
// the call: a peer must not acknowledge a set it could not keep.
func (p *Peer) persist() error {
	if p.store == nil {
		return nil
	}
	if p.held == nil && len(p.book.Records) == 0 {
		return p.store.Save(nil)
	}
	b, err := json.Marshal(stored{Held: p.held, Book: p.book})
	if err != nil {
		return err
	}
	return p.store.Save(b)
}

// Profile returns the profile the peer declares.
func (p *Peer) Profile() Profile { return p.prof }

func (p *Peer) own() time.Duration {
	return time.Duration(p.prof.Limits.MaxStalenessSeconds) * time.Second
}

// Revision changes whenever the set, the feed state or the catch-up time
// changes; a cache of decisions keys on it (with the time, for entry dates).
func (p *Peer) Revision() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rev
}

// Apply applies a set (full or delta) and returns the acknowledgement. A
// refusal is an *Error and leaves what the peer holds unchanged.
func (p *Peer) Apply(set Set) (Ack, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	res, err := Apply(p.held, set, p.now(), p.prof)
	if err != nil {
		return Ack{}, err
	}
	prev := p.held
	p.held = res.Held
	if err := p.persist(); err != nil {
		p.held = prev
		return Ack{}, &Error{Code: CodeUnavailable, Message: err.Error()}
	}
	p.rev++
	return res.Ack, nil
}

// State is what the peer holds now.
func (p *Peer) State() Report {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.held.Report(p.own())
}

// Consume applies one change event of the feed, in order.
func (p *Peer) Consume(ev Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.book.Consume(ev)
	p.rev++
	return p.persist()
}

// CatchUp records that the peer completed a catch-up of the feed now: the
// record basis its record-level reads need fresh.
func (p *Peer) CatchUp() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.held != nil {
		p.held.RecordBasisAt = p.now()
	}
	p.rev++
	return p.persist()
}

// Reload forgets what is in memory and reads the store back: a restart.
func (p *Peer) Reload() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rev++
	return p.load()
}

// Reset forgets everything, the store included.
func (p *Peer) Reset() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.held, p.book = nil, RecordBook{}
	p.rev++
	return p.persist()
}

// Decide decides a read now. A query that carries no record under is
// completed from the feed state.
func (p *Peer) Decide(q Query) Verdict {
	p.mu.Lock()
	defer p.mu.Unlock()
	if q.Record != nil && q.Record.Under == nil && !q.Record.UnderTruncated {
		rec := p.book.Query(q.Record.Address)
		q.Record = &rec
	}
	return p.held.Decide(q, p.now(), p.own())
}

// RecordTypeKey is the record type of a bos address: bos://<type_key>/<id>.
func RecordTypeKey(address string) string {
	rest, ok := strings.CutPrefix(address, "bos://")
	if !ok {
		return ""
	}
	typ, _, _ := strings.Cut(rest, "/")
	return typ
}

// Readable lists the addresses of the records the principal may read now
// through record entries (and any type grant the profile lets the peer
// enforce), sorted. It is the read half of enforcement: every condition of
// Decide holds for each address, evaluated at the peer's clock, so an entry
// that has not started or has ended, a stale set, a stale catch-up, a record
// whose under is unknown or truncated all leave the record out. A peer
// holding no set lists nothing.
func (p *Peer) Readable(principal string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	h := p.held
	if h == nil || h.Set.Epoch == 0 || principal == "" {
		return nil
	}
	now, own := p.now(), p.own()
	// the anchors of the principal's entries: a record outside them can only
	// be read through a type grant, which the entries path does not cover
	anchors := map[string]bool{}
	for _, e := range h.Set.RecordAccess {
		if e.Subject == principal {
			anchors[e.Anchor] = true
		}
	}
	if len(anchors) == 0 {
		return nil
	}
	var out []string
	for addr, rec := range p.book.Records {
		if !anchors[addr] && !anyIn(rec.Under, anchors) {
			continue
		}
		q := Query{Principal: principal, Scope: rec.Scope, TypeKey: RecordTypeKey(addr), Sensitivity: rec.Sensitivity,
			Record: &RecordQuery{Address: addr, Under: rec.Under, UnderTruncated: rec.Truncated}}
		if h.Decide(q, now, own).Allow {
			out = append(out, addr)
		}
	}
	sort.Strings(out)
	return out
}

func anyIn(ss []string, set map[string]bool) bool {
	for _, s := range ss {
		if set[s] {
			return true
		}
	}
	return false
}
