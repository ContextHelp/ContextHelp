package service

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
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

	outDir, err := prepareBackupDir(opts.OutputDir)
	if err != nil {
		return BackupResult{}, err
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

	abort := func(cause error) error {
		f.Close()
		os.Remove(archivePath) //nolint:errcheck
		return cause
	}

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	// 1. SQLite snapshot into temp file.
	tmpDB := filepath.Join(os.TempDir(), fmt.Sprintf("ctxt-snap-%d.db", time.Now().UnixNano()))
	defer os.Remove(tmpDB) //nolint:errcheck

	if err := storageutil.SQLiteSnapshot(opts.DBPath, tmpDB); err != nil {
		return BackupResult{}, abort(fmt.Errorf("backup: snapshot db: %w", err))
	}

	dbInfo, err := os.Stat(tmpDB)
	if err != nil {
		return BackupResult{}, abort(fmt.Errorf("backup: stat snapshot: %w", err))
	}
	result.DBSize = dbInfo.Size()

	if err := addFileToTar(tw, tmpDB, "ctxt-backup/ctxt.db"); err != nil {
		return BackupResult{}, abort(fmt.Errorf("backup: tar db: %w", err))
	}

	// 1b. Optional config.yaml — non-fatal if missing.
	activeConfig, err := archiveActiveConfig(tw, opts.ConfigPath)
	if err != nil {
		return BackupResult{}, abort(err)
	}
	configIncluded := activeConfig != nil

	// 1c. Optional full config dir (keys/, etc.).
	if opts.IncludeConfigs && opts.ConfigDir != "" {
		if err := archiveConfigDir(tw, opts.ConfigDir, activeConfig); err != nil {
			return BackupResult{}, abort(err)
		}
	}

	// 2. Optional blobs (local backend only; blob.New reads an empty
	// backend as local).
	if opts.IncludeBlobs && (opts.BlobCfg.Backend == "" || opts.BlobCfg.Backend == "local") {
		n, err := archiveBlobs(ctx, tw, opts.BlobCfg, opts.SkipBlobErrors)
		if err != nil {
			return BackupResult{}, abort(err)
		}
		result.BlobCount = n
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
	if err := tw.WriteHeader(hdr); err != nil {
		return BackupResult{}, abort(fmt.Errorf("backup: tar manifest header: %w", err))
	}
	if _, err := tw.Write(manifestBytes); err != nil {
		return BackupResult{}, abort(fmt.Errorf("backup: tar manifest: %w", err))
	}

	if err := tw.Close(); err != nil {
		return BackupResult{}, abort(fmt.Errorf("backup: close tar: %w", err))
	}
	if err := gz.Close(); err != nil {
		return BackupResult{}, abort(fmt.Errorf("backup: close gzip: %w", err))
	}
	if err := f.Close(); err != nil {
		return BackupResult{}, abort(fmt.Errorf("backup: close file: %w", err))
	}

	result.Duration = time.Since(start)
	return result, nil
}

// prepareBackupDir resolves the archive's directory (the working
// directory when dir is empty), creating it when absent and refusing
// one that already holds anything.
func prepareBackupDir(dir string) (string, error) {
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("backup: resolve output dir: %w", err)
		}
	}

	if info, err := os.Stat(dir); err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("backup: stat output dir: %w", err)
		}
		if err := os.MkdirAll(dir, 0750); err != nil {
			return "", fmt.Errorf("backup: create output dir: %w", err)
		}
	} else if info.IsDir() {
		entries, _ := os.ReadDir(dir)
		if len(entries) > 0 {
			return "", fmt.Errorf(
				"backup: output dir %q is not empty (%d items); use an empty or new directory",
				dir, len(entries))
		}
	}
	return dir, nil
}

// archiveActiveConfig archives the active config file as config.yaml and
// returns its FileInfo. A missing file is noted on stderr and skipped
// (nil FileInfo, nil error); so is an empty path, silently.
func archiveActiveConfig(tw *tar.Writer, path string) (os.FileInfo, error) {
	if path == "" {
		return nil, nil
	}
	info := statOrNil(path)
	if info == nil {
		fmt.Fprintf(os.Stderr, "backup: config file not found, skipping: %s\n", path)
		return nil, nil
	}
	if err := addFileToTar(tw, path, "ctxt-backup/config.yaml"); err != nil {
		return nil, fmt.Errorf("backup: tar config: %w", err)
	}
	return info, nil
}

// archiveConfigDir archives every file under dir as config/<rel>. The
// active config file, already archived as config.yaml, is skipped when
// it lives here (dpkms's config dir is its directory): config.yaml
// restores it to the target's own config file, whatever that is named,
// so a second copy under config/ would only restore it twice. A missing
// dir, or one that is not a directory, archives nothing.
func archiveConfigDir(tw *tar.Writer, dir string, activeConfig os.FileInfo) error {
	if info := statOrNil(dir); info == nil || !info.IsDir() {
		return nil
	}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, werr error) error {
		if werr != nil || info.IsDir() {
			return werr
		}
		if activeConfig != nil {
			if fi, serr := os.Stat(p); serr == nil && os.SameFile(fi, activeConfig) {
				return nil
			}
		}
		rel, _ := filepath.Rel(dir, p)
		return addFileToTar(tw, p, path.Join("ctxt-backup/config", filepath.ToSlash(rel)))
	})
	if err != nil {
		return fmt.Errorf("backup: tar config dir: %w", err)
	}
	return nil
}

// statOrNil stats p, answering nil when it cannot: the optional config
// inputs of a backup are skipped, not failed on, when absent or
// unreadable.
func statOrNil(p string) os.FileInfo {
	info, err := os.Stat(p)
	if err != nil {
		return nil
	}
	return info
}

// archiveBlobs archives every blob of the store cfg opens as
// blobs/<key>, preceded by its BlobMeta as blobmeta/<key>.json, both
// read through the store, so its on-disk layout never leaks into the
// archive; Restore writes them back through the target's store. A blob
// that cannot be read is skipped when skipErrors is set. It returns the
// number archived.
func archiveBlobs(ctx context.Context, tw *tar.Writer, cfg config.BlobConfig, skipErrors bool) (int, error) {
	bs, err := blobfactory.New(cfg)
	if err != nil {
		return 0, fmt.Errorf("backup: open blob store: %w", err)
	}
	items, err := bs.List(ctx, "")
	if err != nil {
		return 0, fmt.Errorf("backup: list blobs: %w", err)
	}
	n := 0
	for _, item := range items {
		rc, meta, err := bs.Get(ctx, item.Key)
		if err != nil {
			if skipErrors {
				continue
			}
			return 0, fmt.Errorf("backup: get blob %q: %w", item.Key, err)
		}
		err = addBlob(tw, item, meta, rc)
		rc.Close()
		if err != nil {
			return 0, err
		}
		n++
	}
	return n, nil
}

// addBlob archives one blob's metadata, then its content from rc.
func addBlob(tw *tar.Writer, item storage.BlobInfo, meta storage.BlobMeta, rc io.Reader) error {
	if err := addBlobMeta(tw, item, meta); err != nil {
		return err
	}
	hdr := &tar.Header{
		Name:    fmt.Sprintf("ctxt-backup/blobs/%s", item.Key),
		Size:    item.Size,
		Mode:    0644,
		ModTime: item.UpdatedAt,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("backup: tar blob header: %w", err)
	}
	if _, err := io.Copy(tw, rc); err != nil {
		return fmt.Errorf("backup: tar blob data: %w", err)
	}
	return nil
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
