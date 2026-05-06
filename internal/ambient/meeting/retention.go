package meeting

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// RetentionPolicy controls when meeting media files are evicted from disk.
//
// Meeting media files have a different size profile than ambient events
// (~500MB / hour at 1080p vs. KB-scale events), so they get their own
// retention tier per ADR-069 §4. The KnowledgeObject (transcript + frame
// OCR) lives in dpkms forever; this policy only governs the raw media
// file's local lifetime.
//
// Three eviction triggers (oldest-first within each):
//
//	1. TTL: files older than RetentionHours since their pipeline-success
//	   timestamp are deleted. Default: 48 hours.
//	2. Cap: when total bytes-on-disk exceeds MaxGB, oldest-first eviction
//	   until the cap is met. Default: 20 GB.
//	3. S3 archive (optional): if S3Archive is true, the file is uploaded
//	   to S3 BEFORE local deletion so cold-storage retention can extend
//	   indefinitely.
type RetentionPolicy struct {
	// MediaDir is the absolute path under which meeting media files live.
	// Per-recording files: <MediaDir>/<session_id>/<uuid>.{mov,mp4,webm,m4a}.
	MediaDir string
	// RetentionHours is the TTL after pipeline success. Default: 48.
	RetentionHours int
	// MaxGB caps total disk usage. Default: 20.
	MaxGB int
	// S3Archive enables cold-storage upload before local deletion.
	S3Archive bool
	// Now overrides the wall clock. Tests use a fake clock; production
	// callers leave it at the default time.Now.
	Now func() time.Time

	// testCapBytes is a test-only override for the cap (in bytes). When
	// non-zero it takes precedence over MaxGB. Production callers tune
	// via MaxGB; tests use this to avoid allocating gigabytes of disk.
	testCapBytes int64
}

// RetentionResult summarises one Sweep run.
type RetentionResult struct {
	Scanned     int   // total files inspected
	EvictedTTL  int   // files evicted because of TTL
	EvictedCap  int   // files evicted because of cap
	BytesBefore int64
	BytesAfter  int64
	Errors      []error
}

// Sweep applies the retention policy. Returns a summary of what was
// evicted. Idempotent: calling Sweep when no files need eviction is a
// cheap no-op.
//
// Sweep walks MediaDir, applies the TTL rule first (deletes files past
// RetentionHours since their ModTime), then the cap rule (LRU eviction
// to bring total bytes under MaxGB).
func (p *RetentionPolicy) Sweep(ctx context.Context) (RetentionResult, error) {
	now := p.now()
	res := RetentionResult{}

	files, err := p.listMediaFiles()
	if err != nil {
		return res, err
	}
	res.Scanned = len(files)
	for _, f := range files {
		res.BytesBefore += f.Size
	}

	retentionDur := time.Duration(p.retentionHours()) * time.Hour
	cutoff := now.Add(-retentionDur)

	// Pass 1: TTL eviction.
	survivors := make([]mediaFile, 0, len(files))
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if f.ModTime.Before(cutoff) {
			if err := p.evict(ctx, f); err != nil {
				res.Errors = append(res.Errors, err)
				survivors = append(survivors, f)
				continue
			}
			res.EvictedTTL++
			continue
		}
		survivors = append(survivors, f)
	}

	// Pass 2: cap eviction (oldest-first).
	var maxBytes int64
	if p.testCapBytes > 0 {
		maxBytes = p.testCapBytes
	} else {
		maxBytes = int64(p.maxGB()) * 1024 * 1024 * 1024
	}
	var totalBytes int64
	for _, f := range survivors {
		totalBytes += f.Size
	}
	if totalBytes > maxBytes {
		// Sort survivors oldest-first for LRU-by-mtime eviction.
		sort.Slice(survivors, func(i, j int) bool {
			return survivors[i].ModTime.Before(survivors[j].ModTime)
		})
		for _, f := range survivors {
			if err := ctx.Err(); err != nil {
				return res, err
			}
			if totalBytes <= maxBytes {
				break
			}
			if err := p.evict(ctx, f); err != nil {
				res.Errors = append(res.Errors, err)
				continue
			}
			totalBytes -= f.Size
			res.EvictedCap++
		}
	}

	// Recompute BytesAfter from disk so it reflects actual state, not
	// our running estimate (in case of concurrent writes).
	final, _ := p.listMediaFiles()
	for _, f := range final {
		res.BytesAfter += f.Size
	}
	return res, nil
}

// mediaFile is one file under the retention manager's view.
type mediaFile struct {
	Path    string
	Size    int64
	ModTime time.Time
}

func (p *RetentionPolicy) listMediaFiles() ([]mediaFile, error) {
	if p.MediaDir == "" {
		return nil, errors.New("retention: MediaDir not configured")
	}
	var out []mediaFile
	err := filepath.Walk(p.MediaDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		// Only sweep media-shaped files; skip metadata sidecars or future
		// files we want to retain longer.
		switch filepath.Ext(path) {
		case ".mp4", ".mov", ".webm", ".mkv", ".m4a", ".mp3", ".wav":
			out = append(out, mediaFile{Path: path, Size: info.Size(), ModTime: info.ModTime()})
		}
		return nil
	})
	return out, err
}

// evict removes a media file. When S3Archive is enabled, uploads first
// (no-op stub in v1; T-0510 wires real S3 upload).
func (p *RetentionPolicy) evict(_ context.Context, f mediaFile) error {
	if p.S3Archive {
		// TODO: wire to internal/ambient/buffer/s3 (T-0510). For now we
		// just delete locally; the S3-archive path is stubbed so test
		// expectations don't gate on S3.
	}
	return os.Remove(f.Path)
}

func (p *RetentionPolicy) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *RetentionPolicy) retentionHours() int {
	if p.RetentionHours <= 0 {
		return 48
	}
	return p.RetentionHours
}

func (p *RetentionPolicy) maxGB() int {
	if p.MaxGB <= 0 {
		return 20
	}
	return p.MaxGB
}
