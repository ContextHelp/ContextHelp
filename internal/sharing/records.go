package sharing

import (
	"context"
	"log/slog"
	"slices"

	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Record-level reads for an external authority (bos.access/v1 record
// entries). The authority provisions who stands in relation to which
// engagement, project or client file; ctxt holds the entries and what each
// record stands under (internal/bosaccess) and decides, at the moment of
// each read, which of the records ingested from the authority a principal
// may read. Those records join the principal's grants as one more predicate,
// so the scoped driver, the render plan, the ceiling and the key policy
// apply to them like to any grant.

const (
	// RecordSourceName is the source ingested authority records carry: the
	// objects whose source_key is the record's address.
	RecordSourceName = "bos"
	// RecordGrantID names the grant a principal's record-level reads compile
	// to. It sorts after every grant id the store issues (grt_...).
	RecordGrantID = "~record-access"
	// MaxRecordAddresses bounds the records one viewer's predicate names;
	// past it the first addresses in order are kept and the rest are left out
	// (a read denied, never one widened).
	MaxRecordAddresses = 5000
	// recordChunk is how many addresses one IN comparison binds.
	recordChunk = 500
)

// RecordSource says which records an external authority lets a principal
// read now: the addresses (source keys), sorted. An unknown principal reads
// none.
type RecordSource interface {
	ReadableRecords(ctx context.Context, principalID string) ([]string, error)
}

// recordGrantPolicy is the policy a principal's record-level reads render
// with: every field of the record, the search modes that filter rows, and
// entities, export and MCP through the records that were released. The
// authority's entries carry no field limits (a grant that would is not
// provisioned as an entry), so none apply here.
func recordGrantPolicy() Policy {
	return Policy{
		Name:         "bos-record-access",
		Version:      1,
		Fields:       map[string][]string{"*": {FieldAll}},
		Capabilities: Capabilities{Search: []string{SearchFTS, SearchRSQL}, Entities: true, Export: true, MCP: true},
	}
}

// withRecordGrant returns the grants of in plus, when it carries records, the
// grant they compile to.
func withRecordGrant(in Input) []Grant {
	grants := slices.Clone(in.Grants)
	if len(in.Records) == 0 {
		return grants
	}
	return append(grants, Grant{
		ID: RecordGrantID, Audience: RecordSourceName, Policy: recordGrantPolicy(),
		Node: recordNode(in.Records),
	})
}

// recordNode matches the objects ingested from the authority whose source
// key is one of addresses.
func recordNode(addresses []string) search.Node {
	if len(addresses) > MaxRecordAddresses {
		addresses = addresses[:MaxRecordAddresses]
	}
	chunks := make([]search.Node, 0, len(addresses)/recordChunk+1)
	for chunk := range slices.Chunk(addresses, recordChunk) {
		chunks = append(chunks, search.ComparisonNode{Field: "source_key", Operator: search.OpIn, Value: slices.Clone(chunk)})
	}
	var keys search.Node = search.OrNode{Children: chunks}
	if len(chunks) == 1 {
		keys = chunks[0]
	}
	return search.AndNode{Children: []search.Node{
		search.ComparisonNode{Field: "source", Operator: search.OpEq, Value: RecordSourceName},
		keys,
	}}
}

// Holder is what the record source asks of the held access state: the
// records a person of the authority may read now.
type Holder interface {
	// Resolve maps an external identity (issuer, subject) to the principal of
	// the held set that carries it.
	Resolve(issuer, subject string) (string, bool)
	Readable(authorityPrincipal string) []string
}

// AuthorityRecords is a RecordSource over a held access state: it maps a
// ctxt principal to its principal at the authority through its active oidc
// identities (issuer and subject, as the authority lists them in the set),
// and lists what the state lets that principal read now. An identity no
// principal of the set carries, or that two carry, maps to nothing.
type AuthorityRecords struct {
	Holder     Holder
	Identities storage.IdentityStore
}

// ReadableRecords implements RecordSource.
func (a AuthorityRecords) ReadableRecords(ctx context.Context, principalID string) ([]string, error) {
	if a.Holder == nil || a.Identities == nil || principalID == "" {
		return nil, nil
	}
	ids, err := a.Identities.ListByPrincipal(ctx, principalID)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, id := range ids {
		if !id.Active() || id.Method != storage.IdentityMethodOIDC {
			continue
		}
		if who, ok := a.Holder.Resolve(id.Issuer, id.Subject); ok {
			out = append(out, a.Holder.Readable(who)...)
		}
	}
	slices.Sort(out)
	out = slices.Compact(out)
	if len(out) > MaxRecordAddresses {
		slog.Warn("record-level reads over the bound: the rest are left out",
			"principal", principalID, "readable", len(out), "bound", MaxRecordAddresses)
		out = out[:MaxRecordAddresses]
	}
	return out, nil
}

// KVCursor keeps a feed cursor in the instance key-value store, under Key.
type KVCursor struct {
	KV  storage.KVStore
	Key string
}

// Load returns the cursor saved, or "" when none.
func (k KVCursor) Load() (string, error) {
	b, ok, err := k.KV.Get(context.Background(), k.Key)
	if err != nil || !ok {
		return "", err
	}
	return string(b), nil
}

// Save replaces the cursor.
func (k KVCursor) Save(v string) error {
	return k.KV.Put(context.Background(), k.Key, []byte(v))
}

// KVState keeps the held access state in the instance database's key-value
// store, under Key, so it survives a restart with the rest of the instance.
type KVState struct {
	KV  storage.KVStore
	Key string
}

// Load returns the state saved, or nil when none.
func (k KVState) Load() ([]byte, error) {
	b, ok, err := k.KV.Get(context.Background(), k.Key)
	if err != nil || !ok {
		return nil, err
	}
	return b, nil
}

// Save replaces the state; nil or empty deletes it.
func (k KVState) Save(b []byte) error {
	if len(b) == 0 {
		return k.KV.Delete(context.Background(), k.Key)
	}
	return k.KV.Put(context.Background(), k.Key, b)
}
