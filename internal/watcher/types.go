// Package watcher turns local activity into dpkms ingest requests: directory
// watches (Manager), the clipboard and screen captures. Each hands content
// to an Analyzer or Ingester and never links the service layer itself.
package watcher

import (
	"context"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// WatchConfig is an alias for storage.WatchConfig, defined here for convenience
// in the watcher package.
type WatchConfig = storage.WatchConfig

// Request is one piece of content a watcher hands to dpkms for analysis.
// The package keeps its own type so a watcher links neither the service
// layer nor the HTTP client: the Ingester behind it decides.
type Request struct {
	Content   string
	Type      string
	Source    string
	Pipeline  string
	KnownHash string
}

// Analyzer enqueues content and returns the ID of the dpkms job that will
// process it.
type Analyzer interface {
	Analyze(ctx context.Context, req Request) (jobID string, err error)
}

// Ingester is what a directory watch needs from dpkms: enqueue a file,
// find the object a job produced, and delete that object when the file
// goes away.
type Ingester interface {
	Analyzer
	// JobResult returns the ID of the object the job produced, or ""
	// while the job has none (pending, running or failed).
	JobResult(ctx context.Context, jobID string) (objectID string, err error)
	// DeleteObject deletes one object. An object that is already gone
	// is not an error.
	DeleteObject(ctx context.Context, id string) error
}

// FileRecord is the last content of one watched file handed to dpkms.
type FileRecord struct {
	// Path is the file's absolute path.
	Path string
	// ContentHash is the hex SHA-256 of the content that was enqueued.
	ContentHash string
	// JobID is the job that enqueue created. The object it produces is
	// resolved through it when the file is deleted.
	JobID string
	// ObjectID is the object the job produced, when already known.
	ObjectID string
	// LastSeen is when the content was enqueued.
	LastSeen time.Time
}

// FileRecords is a watch's dedup state: which content of which file was
// already handed off. ctxt keeps it in a local state file per dpkms
// instance; dpkms keeps its own server-side watches' records in its
// database. Implementations must be safe for concurrent use.
type FileRecords interface {
	// GetFile returns the record for path under watchID, and whether
	// there is one.
	GetFile(ctx context.Context, watchID, path string) (FileRecord, bool, error)
	// PutFile stores rec under watchID, replacing any record for its path.
	PutFile(ctx context.Context, watchID string, rec FileRecord) error
	// DeleteFile forgets path under watchID.
	DeleteFile(ctx context.Context, watchID, path string) error
	// SetLastError records the watch's most recent failure.
	SetLastError(ctx context.Context, watchID, msg string) error
}

// ConfigStore persists watch configurations. Only dpkms's server-side
// watches have one; ctxt's watches come from its config file.
type ConfigStore interface {
	CreateWatch(ctx context.Context, w *WatchConfig) error
	GetWatch(ctx context.Context, id string) (*WatchConfig, error)
	ListWatches(ctx context.Context, status string) ([]*WatchConfig, error)
	UpdateWatch(ctx context.Context, w *WatchConfig) error
	DeleteWatch(ctx context.Context, id string) error
}
