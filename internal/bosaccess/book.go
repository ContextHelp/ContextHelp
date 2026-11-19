package bosaccess

// Event is one change event of the authority's feed, as far as record-level
// access reads it.
type Event struct {
	ID      string    `json:"id,omitempty"`
	Subject string    `json:"subject"`
	Data    EventData `json:"data"`
}

// EventData is what an event says about a record: pointers and metadata.
type EventData struct {
	Change      string `json:"change"`
	Scope       string `json:"scope"`
	Sensitivity string `json:"sensitivity"`
	// Under is what the record stands under (addresses, the record's own not
	// included), on upserted, refreshed and scope_changed events sent to a
	// peer that lists the feature record_access.
	Under          []string `json:"under,omitzero"`
	UnderTruncated bool     `json:"under_truncated,omitempty"`
}

// HeldRecord is what a peer holds of one record from the feed.
type HeldRecord struct {
	// Under is nil when unknown.
	Under       []string `json:"under,omitzero"`
	Truncated   bool     `json:"truncated,omitempty"`
	Scope       string   `json:"scope,omitempty"`
	Sensitivity string   `json:"sensitivity,omitempty"`
}

// RecordBook is what a peer holds of the records from the change feed: for
// each record address, the scope and sensitivity it carries and what it
// stands under. Apply events with Consume, one at a time and in order.
type RecordBook struct {
	Records map[string]HeldRecord `json:"records,omitempty"`
}

// Consume applies one change event. upserted, refreshed and scope_changed
// carry what the record stands under now: it replaces what was held, and a
// record whose event carries none is one whose under is not known (reads of
// it are denied until an event carries it). deleted and erased drop the
// record. held, released and access_changed change nothing. A change value
// this version does not know is read as scope_changed.
func (b *RecordBook) Consume(ev Event) {
	if b.Records == nil {
		b.Records = map[string]HeldRecord{}
	}
	switch ev.Data.Change {
	case "deleted", "erased":
		delete(b.Records, ev.Subject)
	case "held", "released", "access_changed":
	default:
		b.Records[ev.Subject] = HeldRecord{
			Under: ev.Data.Under, Truncated: ev.Data.UnderTruncated,
			Scope: ev.Data.Scope, Sensitivity: ev.Data.Sensitivity,
		}
	}
}

// Query returns the record as the peer holds it. A record never heard of has
// no under.
func (b *RecordBook) Query(address string) RecordQuery {
	r := b.Records[address]
	return RecordQuery{Address: address, Under: r.Under, UnderTruncated: r.Truncated}
}
