package service

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// RestoreOpts configures a restore operation.
type RestoreOpts struct {
	Source      string // .tar.gz file or extracted directory
	DBPath      string // target DB path
	ConfigDir   string // target config directory
	SkipConfigs bool
	DryRun      bool
	// Blobs receives the backup's blobs through its own Put, so they
	// land wherever storage.blob puts them, in its layout. Required
	// when the backup holds blobs, except for a dry run.
	Blobs storage.BlobStore
}

// RestoreResult summarises a completed restore.
type RestoreResult struct {
	DBSize      int64
	ConfigCount int
	BlobCount   int
}

// Restore reads a backup archive (.tar.gz) or directory and writes
// DB and config files to their target locations and blobs to
// opts.Blobs.
func Restore(ctx context.Context, opts RestoreOpts) (RestoreResult, error) {
	info, err := os.Stat(opts.Source)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore: %w", err)
	}

	if info.IsDir() {
		return restoreFromDir(ctx, opts)
	}
	return restoreFromArchive(ctx, opts)
}

// restoreBlob writes one backed-up blob, named by its key relative to
// the backup's blobs/ directory, to opts.Blobs.
func restoreBlob(ctx context.Context, opts RestoreOpts, rel string, r io.Reader, size int64) error {
	key := filepath.ToSlash(rel)
	// Backup names each blob by its bare key; anything with a path in
	// it would let the archive write outside the store.
	if key == "" || key == "." || key == ".." || strings.ContainsAny(key, `/\`) {
		return fmt.Errorf("restore: blob %q: not a blob key", rel)
	}
	if opts.DryRun {
		return nil
	}
	if opts.Blobs == nil {
		return fmt.Errorf("restore: blob %s: no blob store to restore into", key)
	}
	if err := opts.Blobs.Put(ctx, key, r, storage.BlobMeta{Size: size}); err != nil {
		return fmt.Errorf("restore: write blob %s: %w", key, err)
	}
	return nil
}

func restoreFromArchive(ctx context.Context, opts RestoreOpts) (RestoreResult, error) {
	f, err := os.Open(opts.Source)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore: open archive: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore: gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	var result RestoreResult
	var manifest map[string]any

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return RestoreResult{}, fmt.Errorf("restore: read tar: %w", err)
		}

		// Strip the ctxt-backup/ prefix.
		name := strings.TrimPrefix(hdr.Name, "ctxt-backup/")

		switch {
		case name == "manifest.json":
			data, _ := io.ReadAll(tr)
			// A corrupt manifest must not be swallowed: leaving it nil
			// skips the schema-version guard below, restoring an
			// unsupported backup as if it were supported.
			if err := json.Unmarshal(data, &manifest); err != nil {
				return RestoreResult{}, fmt.Errorf("restore: parse manifest: %w", err)
			}

		case name == "ctxt.db":
			if err := restoreArchiveDB(opts, hdr, tr); err != nil {
				return RestoreResult{}, err
			}
			result.DBSize = hdr.Size

		case strings.HasPrefix(name, "config/"):
			if opts.SkipConfigs {
				continue
			}
			if err := restoreArchiveConfig(opts, strings.TrimPrefix(name, "config/"), hdr, tr); err != nil {
				return RestoreResult{}, err
			}
			result.ConfigCount++

		case strings.HasPrefix(name, "blobs/") && hdr.Typeflag == tar.TypeReg:
			if err := restoreBlob(ctx, opts, strings.TrimPrefix(name, "blobs/"), tr, hdr.Size); err != nil {
				return RestoreResult{}, err
			}
			result.BlobCount++
		}
	}

	if err := checkManifest(manifest); err != nil {
		return RestoreResult{}, err
	}
	return result, nil
}

// checkManifest refuses a backup schema newer than this build reads and
// notes the embedding model the backup was made with. A nil manifest
// passes.
func checkManifest(manifest map[string]any) error {
	if manifest == nil {
		return nil
	}
	if v, ok := manifest["schema_version"].(float64); ok && v > 2 {
		return fmt.Errorf("restore: unsupported backup schema version %.0f (max 2)", v)
	}
	// Warn if embedding model differs from backup.
	if emb, ok := manifest["embedding"].(map[string]any); ok {
		model, _ := emb["model"].(string)
		if model != "" {
			fmt.Fprintf(os.Stderr,
				"note: backup was created with embedding model %q; "+
					"reindex vectors if your current model differs\n", model)
		}
	}
	return nil
}

func restoreArchiveDB(opts RestoreOpts, hdr *tar.Header, r io.Reader) error {
	if opts.DryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(opts.DBPath), 0750); err != nil {
		return fmt.Errorf("restore: mkdir for db: %w", err)
	}
	if err := writeFile(opts.DBPath, r, hdr.FileInfo().Mode()); err != nil {
		return fmt.Errorf("restore: write db: %w", err)
	}
	return nil
}

func restoreArchiveConfig(opts RestoreOpts, rel string, hdr *tar.Header, r io.Reader) error {
	if opts.DryRun {
		return nil
	}
	target := filepath.Join(filepath.Dir(opts.ConfigDir), rel)
	if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
		return fmt.Errorf("restore: mkdir config: %w", err)
	}
	if err := writeFile(target, r, hdr.FileInfo().Mode()); err != nil {
		return fmt.Errorf("restore: write config %s: %w", rel, err)
	}
	return nil
}

func restoreFromDir(ctx context.Context, opts RestoreOpts) (RestoreResult, error) {
	var result RestoreResult
	base := opts.Source

	// DB
	dbSrc := filepath.Join(base, "ctxt.db")
	if info, err := os.Stat(dbSrc); err == nil {
		result.DBSize = info.Size()
		if !opts.DryRun {
			if err := os.MkdirAll(filepath.Dir(opts.DBPath), 0750); err != nil {
				return RestoreResult{}, fmt.Errorf("restore: mkdir db: %w", err)
			}
			if err := copyFile(dbSrc, opts.DBPath); err != nil {
				return RestoreResult{}, fmt.Errorf("restore: copy db: %w", err)
			}
		}
	}

	// Configs
	configSrc := filepath.Join(base, "config")
	if !opts.SkipConfigs {
		if info, err := os.Stat(configSrc); err == nil && info.IsDir() {
			filepath.Walk(configSrc, func(path string, fi os.FileInfo, werr error) error {
				if werr != nil || fi.IsDir() {
					return werr
				}
				rel, _ := filepath.Rel(configSrc, path)
				target := filepath.Join(filepath.Dir(opts.ConfigDir), rel)
				result.ConfigCount++
				if opts.DryRun {
					return nil
				}
				os.MkdirAll(filepath.Dir(target), 0750)
				return copyFile(path, target)
			})
		}
	}

	// Blobs
	blobSrc := filepath.Join(base, "blobs")
	if info, err := os.Stat(blobSrc); err == nil && info.IsDir() {
		n, err := restoreBlobDir(ctx, opts, blobSrc)
		if err != nil {
			return RestoreResult{}, err
		}
		result.BlobCount = n
	}

	return result, nil
}

// restoreBlobDir restores every file under dir as a blob named by its
// path relative to dir, read through an os.Root so a symlink cannot
// reach outside it.
func restoreBlobDir(ctx context.Context, opts RestoreOpts, dir string) (int, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return 0, fmt.Errorf("restore: open blobs: %w", err)
	}
	defer func() { _ = root.Close() }()
	n := 0
	err = fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return werr
		}
		f, err := root.Open(rel)
		if err != nil {
			return fmt.Errorf("restore: open blob %s: %w", rel, err)
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil {
			return fmt.Errorf("restore: stat blob %s: %w", rel, err)
		}
		if err := restoreBlob(ctx, opts, filepath.FromSlash(rel), f, fi.Size()); err != nil {
			return err
		}
		n++
		return nil
	})
	return n, err
}

func writeFile(path string, r io.Reader, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return writeFile(dst, in, 0644)
}
