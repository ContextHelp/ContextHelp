package storage

// SchemaStatus is a read-only look at a database's schema version,
// taken without opening the store normally (opening migrates). Each
// driver's CheckSchema fills it.
type SchemaStatus struct {
	// Current is the newest version the schema_version ledger records;
	// 0 when there is no ledger.
	Current int `json:"current"`
	// Latest is the newest schema version this binary migrates to.
	Latest int `json:"latest"`
	// Target is the version the next open migrates to; 0 when nothing
	// is pending (schema current, or newer than this binary).
	Target int `json:"target,omitempty"`
	// New reports that no data is at stake: no database yet, or one
	// with no tables of its own. The next open creates the schema and
	// takes no snapshot.
	New bool `json:"new,omitempty"`
}

// Pending reports whether the next open applies migrations.
func (s SchemaStatus) Pending() bool { return s.Target != 0 }

// Ahead reports whether the database records a schema newer than this
// binary knows.
func (s SchemaStatus) Ahead() bool { return s.Current > s.Latest }
