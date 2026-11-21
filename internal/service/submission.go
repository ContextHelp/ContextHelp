package service

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Size limits on what a capture carries beside its content. A transcript
// of a long video is a few hundred KB, so the note limit leaves room; the
// data is metadata, not a document.
const (
	MaxAnalyzeNoteBytes = 2 << 20   // 2 MiB
	MaxAnalyzeDataBytes = 256 << 10 // 256 KiB encoded
	// MaxAnalyzeMetadataBytes bounds the client metadata, like the data.
	MaxAnalyzeMetadataBytes = MaxAnalyzeDataBytes
	// MaxAnalyzeAmbientIDBytes bounds ambient_source, fingerprint and
	// session_id: short identifiers, not payloads.
	MaxAnalyzeAmbientIDBytes = 512
)

// ErrInvalidSubmission marks an analyze or enqueue request whose note or
// data is refused. Surfaces map it to a client error (HTTP 400).
var ErrInvalidSubmission = errors.New("invalid submission")

// validateSubmission checks the note, data, metadata and ambient id limits.
func validateSubmission(req AnalyzeRequest) error {
	if len(req.Note) > MaxAnalyzeNoteBytes {
		return fmt.Errorf("%w: note is %d bytes, the limit is %d", ErrInvalidSubmission, len(req.Note), MaxAnalyzeNoteBytes)
	}
	if len(req.Data) > 0 {
		b, err := json.Marshal(req.Data)
		if err != nil {
			return fmt.Errorf("%w: data is not JSON: %w", ErrInvalidSubmission, err)
		}
		if len(b) > MaxAnalyzeDataBytes {
			return fmt.Errorf("%w: data is %d bytes encoded, the limit is %d", ErrInvalidSubmission, len(b), MaxAnalyzeDataBytes)
		}
	}
	for name, v := range map[string]string{
		"ambient_source": req.AmbientSource,
		"fingerprint":    req.Fingerprint,
		"session_id":     req.SessionID,
	} {
		if len(v) > MaxAnalyzeAmbientIDBytes {
			return fmt.Errorf("%w: %s is %d bytes, the limit is %d", ErrInvalidSubmission, name, len(v), MaxAnalyzeAmbientIDBytes)
		}
	}
	if len(req.Metadata) > 0 {
		b, err := json.Marshal(req.Metadata)
		if err != nil {
			return fmt.Errorf("%w: metadata is not JSON: %w", ErrInvalidSubmission, err)
		}
		if len(b) > MaxAnalyzeMetadataBytes {
			return fmt.Errorf("%w: metadata is %d bytes encoded, the limit is %d", ErrInvalidSubmission, len(b), MaxAnalyzeMetadataBytes)
		}
	}
	return nil
}

// ambientContext is the job-record form of the request's ambient fields;
// nil when the request sent none.
func (r AnalyzeRequest) ambientContext() *storage.AmbientContext {
	a := &storage.AmbientContext{
		Source: r.AmbientSource, Fingerprint: r.Fingerprint,
		SessionID: r.SessionID, Metadata: r.Metadata,
	}
	if a.Empty() {
		return nil
	}
	return a
}

// updatesStored reports whether the request is an update of an object
// already stored rather than a new capture to drop as a duplicate: it
// names a source key or carries a note or data to apply.
func (r AnalyzeRequest) updatesStored() bool {
	return r.SourceKey != "" || r.Note != "" || len(r.Data) > 0
}
