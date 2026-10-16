package watcher

import (
	"context"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// StoreRecords returns FileRecords over a dpkms WatchStore: the records
// of dpkms's own server-side watches (/api/v1/watches), kept in its
// database. ctxt never uses it.
//
// The table's object_id column holds the job ID enqueue returned. The
// object is resolved from the job when the file is deleted, because the
// job has not produced it yet when the record is written.
func StoreRecords(ws storage.WatchStore) FileRecords { return storeRecords{ws} }

type storeRecords struct{ ws storage.WatchStore }

func (s storeRecords) GetFile(ctx context.Context, watchID, path string) (FileRecord, bool, error) {
	// The drivers report a missing row as a plain error, not
	// storage.ErrNotFound, so any failure reads as "no record": the file
	// is enqueued again, and dpkms's content-hash dedup catches a repeat.
	r, err := s.ws.GetFileRecord(ctx, watchID, path)
	if err != nil {
		return FileRecord{}, false, nil //nolint:nilerr // see above
	}
	return FileRecord{Path: r.FilePath, ContentHash: r.ContentHash, JobID: r.ObjectID, LastSeen: r.LastSeen}, true, nil
}

func (s storeRecords) PutFile(ctx context.Context, watchID string, rec FileRecord) error {
	return s.ws.UpsertFileRecord(ctx, &storage.WatchFileRecord{
		WatchID: watchID, FilePath: rec.Path, ObjectID: rec.JobID,
		ContentHash: rec.ContentHash, LastSeen: rec.LastSeen,
	})
}

func (s storeRecords) DeleteFile(ctx context.Context, watchID, path string) error {
	return s.ws.DeleteFileRecord(ctx, watchID, path)
}

func (s storeRecords) SetLastError(ctx context.Context, watchID, msg string) error {
	wc, err := s.ws.GetWatch(ctx, watchID)
	if err != nil {
		return err
	}
	wc.LastError = msg
	wc.UpdatedAt = time.Now()
	return s.ws.UpdateWatch(ctx, wc)
}
