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

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	blobfactory "github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// BackupOpts configures a backup operation.
type BackupOpts struct {
	DBPath         string            // path to sqlite file
	BlobCfg        config.BlobConfig // blob backend config
	IncludeBlobs   bool
	OutputDir      string // directory to write archive; cwd if empty
	SkipBlobErrors bool
	ConfigPath     string        // optional: path to config.yaml to include in archive
	IncludeConfigs bool          // include config.yaml + keys/ + other config files
	ConfigDir      string        // config directory (e.g. ~/.config/contexthelp)
	EmbeddingInfo  EmbeddingInfo // current embedding model for manifest
}

// EmbeddingInfo records the embedding backend used when creating a backup.
type EmbeddingInfo struct {
	Backend   string `json:"backend"`
	Model     string `json:"model"`
	Dimension int    `json:"dimension"`
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

	if info, err := os.Stat(outDir); err != nil {
		if !os.IsNotExist(err) {
			return BackupResult{}, fmt.Errorf("backup: stat output dir: %w", err)
		}
		if err := os.MkdirAll(outDir, 0750); err != nil {
			return BackupResult{}, fmt.Errorf("backup: create output dir: %w", err)
		}
	} else if info.IsDir() {
		entries, _ := os.ReadDir(outDir)
		if len(entries) > 0 {
			return BackupResult{}, fmt.Errorf(
				"backup: output dir %q is not empty (%d items); use an empty or new directory",
				outDir, len(entries))
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

	// 1b. Optional config.yaml — non-fatal if missing.
	configIncluded := false
	var activeConfig os.FileInfo
	if opts.ConfigPath != "" {
		if cfgInfo, serr := os.Stat(opts.ConfigPath); serr == nil {
			if terr := addFileToTar(tw, opts.ConfigPath, "ctxt-backup/config.yaml"); terr != nil {
				return abort(fmt.Errorf("backup: tar config: %w", terr))
			}
			configIncluded = true
			activeConfig = cfgInfo
		} else {
			fmt.Fprintf(os.Stderr, "backup: config file not found, skipping: %s\n", opts.ConfigPath)
		}
	}

	// 1c. Optional full config dir (keys/, etc.). The active config
	// file, already archived as config.yaml, is skipped when it lives
	// here (dpkms's config dir is its directory): config.yaml restores
	// it to the target's own config file, whatever that is named, so a
	// second copy under config/ would only restore it twice.
	if opts.IncludeConfigs && opts.ConfigDir != "" {
		if cInfo, serr := os.Stat(opts.ConfigDir); serr == nil && cInfo.IsDir() {
			err := filepath.Walk(opts.ConfigDir, func(path string, info os.FileInfo, werr error) error {
				if werr != nil || info.IsDir() {
					return werr
				}
				if activeConfig != nil {
					if fi, serr := os.Stat(path); serr == nil && os.SameFile(fi, activeConfig) {
						return nil
					}
				}
				rel, _ := filepath.Rel(opts.ConfigDir, path)
				return addFileToTar(tw, path, filepath.Join("ctxt-backup/config", rel))
			})
			if err != nil {
				return abort(fmt.Errorf("backup: tar config dir: %w", err))
			}
		}
	}

	// 2. Optional blobs (local backend only; blob.New reads an empty
	// backend as local). Each is archived as blobs/<key>, preceded by
	// its BlobMeta as blobmeta/<key>.json, both read through the
	// store, so its on-disk layout never leaks into the archive;
	// Restore writes them back through the target's store.
	if opts.IncludeBlobs && (opts.BlobCfg.Backend == "" || opts.BlobCfg.Backend == "local") {
		bs, err := blobfactory.New(opts.BlobCfg)
		if err != nil {
			return abort(fmt.Errorf("backup: open blob store: %w", err))
		}
		items, err := bs.List(ctx, "")
		if err != nil {
			return abort(fmt.Errorf("backup: list blobs: %w", err))
		}
		for _, item := range items {
			rc, meta, err := bs.Get(ctx, item.Key)
			if err != nil {
				if opts.SkipBlobErrors {
					continue
				}
				return abort(fmt.Errorf("backup: get blob %q: %w", item.Key, err))
			}
			if merr := addBlobMeta(tw, item, meta); merr != nil {
				rc.Close()
				return abort(merr)
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
		"schema_version":  2,
		"created_at":      time.Now().UTC().Format(time.RFC3339),
		"db_size":         result.DBSize,
		"blob_count":      result.BlobCount,
		"config_included": configIncluded,
		"configs_dir":     opts.IncludeConfigs,
		"embedding":       opts.EmbeddingInfo,
	}
	manifestBytes, _ := json.Marshal(manifest)
	hdr := &tar.Header{
		Name:    "ctxt-backup/manifest.json",
		Size:    int64(len(manifestBytes)),
		Mode:    0644,
		ModTime: time.Now(),
	}
	tw.WriteHeader(hdr)     //nolint:errcheck
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

// addBlobMeta archives one blob's metadata as blobmeta/<key>.json.
func addBlobMeta(tw *tar.Writer, item storage.BlobInfo, meta storage.BlobMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("backup: marshal blob metadata %q: %w", item.Key, err)
	}
	hdr := &tar.Header{
		Name:    fmt.Sprintf("ctxt-backup/blobmeta/%s.json", item.Key),
		Size:    int64(len(data)),
		Mode:    0644,
		ModTime: item.UpdatedAt,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("backup: tar blob metadata header: %w", err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("backup: tar blob metadata: %w", err)
	}
	return nil
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
