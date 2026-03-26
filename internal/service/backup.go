package service

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	blobfactory "github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// BackupOpts configures a backup operation.
type BackupOpts struct {
	DBPath         string            // path to sqlite file
	BlobCfg        config.BlobConfig // blob backend config
	IncludeBlobs   bool
	OutputDir      string // directory to write archive; cwd if empty
	SkipBlobErrors bool
}

// BackupResult summarises a completed backup.
type BackupResult struct {
	Path      string
	DBSize    int64
	BlobCount int
	Duration  time.Duration
}

// Backup creates a timestamped .tar.gz archive containing the SQLite database
// snapshot and optionally blob files. Cleans up a partial archive on error.
func Backup(ctx context.Context, opts BackupOpts) (BackupResult, error) {
	start := time.Now()

	outDir := opts.OutputDir
	if outDir == "" {
		var err error
		outDir, err = os.Getwd()
		if err != nil {
			return BackupResult{}, fmt.Errorf("backup: resolve output dir: %w", err)
		}
	}

	ts := time.Now().UTC().Format("2006-01-02T15-04-05Z")
	archiveName := fmt.Sprintf("ctxt-backup-%s.tar.gz", ts)
	archivePath := filepath.Join(outDir, archiveName)

	f, err := os.Create(archivePath)
	if err != nil {
		return BackupResult{}, fmt.Errorf("backup: create archive: %w", err)
	}

	var result BackupResult
	result.Path = archivePath

	abort := func(cause error) (BackupResult, error) {
		f.Close()
		os.Remove(archivePath) //nolint:errcheck
		return BackupResult{}, cause
	}

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	// 1. SQLite snapshot into temp file.
	tmpDB := filepath.Join(os.TempDir(), fmt.Sprintf("ctxt-snap-%d.db", time.Now().UnixNano()))
	defer os.Remove(tmpDB) //nolint:errcheck

	if err := storageutil.SQLiteSnapshot(opts.DBPath, tmpDB); err != nil {
		return abort(fmt.Errorf("backup: snapshot db: %w", err))
	}

	dbInfo, err := os.Stat(tmpDB)
	if err != nil {
		return abort(fmt.Errorf("backup: stat snapshot: %w", err))
	}
	result.DBSize = dbInfo.Size()

	if err := addFileToTar(tw, tmpDB, "ctxt-backup/ctxt.db"); err != nil {
		return abort(fmt.Errorf("backup: tar db: %w", err))
	}

	// 2. Optional blobs (local backend only).
	if opts.IncludeBlobs && opts.BlobCfg.Backend == "local" {
		bs, err := blobfactory.New(opts.BlobCfg)
		if err != nil {
			return abort(fmt.Errorf("backup: open blob store: %w", err))
		}
		items, err := bs.List(ctx, "")
		if err != nil {
			return abort(fmt.Errorf("backup: list blobs: %w", err))
		}
		for _, item := range items {
			rc, _, err := bs.Get(ctx, item.Key)
			if err != nil {
				if opts.SkipBlobErrors {
					continue
				}
				return abort(fmt.Errorf("backup: get blob %q: %w", item.Key, err))
			}
			hdr := &tar.Header{
				Name:    fmt.Sprintf("ctxt-backup/blobs/%s", item.Key),
				Size:    item.Size,
				Mode:    0644,
				ModTime: item.UpdatedAt,
			}
			if werr := tw.WriteHeader(hdr); werr != nil {
				rc.Close()
				return abort(fmt.Errorf("backup: tar blob header: %w", werr))
			}
			if _, cerr := io.Copy(tw, rc); cerr != nil {
				rc.Close()
				return abort(fmt.Errorf("backup: tar blob data: %w", cerr))
			}
			rc.Close()
			result.BlobCount++
		}
	}

	// 3. manifest.json — written last; its presence signals a complete backup.
	manifest := map[string]any{
		"schema_version": 1,
		"created_at":     time.Now().UTC().Format(time.RFC3339),
		"db_size":        result.DBSize,
		"blob_count":     result.BlobCount,
	}
	manifestBytes, _ := json.Marshal(manifest)
	hdr := &tar.Header{
		Name:    "ctxt-backup/manifest.json",
		Size:    int64(len(manifestBytes)),
		Mode:    0644,
		ModTime: time.Now(),
	}
	tw.WriteHeader(hdr)  //nolint:errcheck
	tw.Write(manifestBytes) //nolint:errcheck

	if err := tw.Close(); err != nil {
		return abort(fmt.Errorf("backup: close tar: %w", err))
	}
	if err := gz.Close(); err != nil {
		return abort(fmt.Errorf("backup: close gzip: %w", err))
	}
	if err := f.Close(); err != nil {
		return abort(fmt.Errorf("backup: close file: %w", err))
	}

	result.Duration = time.Since(start)
	return result, nil
}

func addFileToTar(tw *tar.Writer, srcPath, tarName string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	hdr := &tar.Header{
		Name:    tarName,
		Size:    info.Size(),
		Mode:    int64(info.Mode()),
		ModTime: info.ModTime(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}
