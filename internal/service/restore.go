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
	"strings"
)

// RestoreOpts configures a restore operation.
type RestoreOpts struct {
	Source      string // .tar.gz file or extracted directory
	DBPath      string // target DB path
	ConfigDir   string // target config directory
	SkipConfigs bool
	DryRun      bool
}

// RestoreResult summarises a completed restore.
type RestoreResult struct {
	DBSize      int64
	ConfigCount int
	BlobCount   int
}

// Restore reads a backup archive (.tar.gz) or directory and writes
// DB, config, and blob files to their target locations.
func Restore(_ context.Context, opts RestoreOpts) (RestoreResult, error) {
	info, err := os.Stat(opts.Source)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore: %w", err)
	}

	if info.IsDir() {
		return restoreFromDir(opts)
	}
	return restoreFromArchive(opts)
}

func restoreFromArchive(opts RestoreOpts) (RestoreResult, error) {
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
			json.Unmarshal(data, &manifest)

		case name == "ctxt.db":
			if opts.DryRun {
				result.DBSize = hdr.Size
				continue
			}
			if err := os.MkdirAll(filepath.Dir(opts.DBPath), 0750); err != nil {
				return RestoreResult{}, fmt.Errorf("restore: mkdir for db: %w", err)
			}
			if err := writeFile(opts.DBPath, tr, hdr.FileInfo().Mode()); err != nil {
				return RestoreResult{}, fmt.Errorf("restore: write db: %w", err)
			}
			result.DBSize = hdr.Size

		case strings.HasPrefix(name, "config/"):
			if opts.SkipConfigs {
				continue
			}
			rel := strings.TrimPrefix(name, "config/")
			target := filepath.Join(filepath.Dir(opts.ConfigDir), rel)
			if opts.DryRun {
				result.ConfigCount++
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
				return RestoreResult{}, fmt.Errorf("restore: mkdir config: %w", err)
			}
			if err := writeFile(target, tr, hdr.FileInfo().Mode()); err != nil {
				return RestoreResult{}, fmt.Errorf("restore: write config %s: %w", rel, err)
			}
			result.ConfigCount++

		case strings.HasPrefix(name, "blobs/"):
			// Blob restore: write to same relative path under data dir.
			rel := strings.TrimPrefix(name, "blobs/")
			blobDir := filepath.Join(filepath.Dir(opts.DBPath), "blobs")
			target := filepath.Join(blobDir, rel)
			if opts.DryRun {
				result.BlobCount++
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
				return RestoreResult{}, fmt.Errorf("restore: mkdir blob: %w", err)
			}
			if err := writeFile(target, tr, 0644); err != nil {
				return RestoreResult{}, fmt.Errorf("restore: write blob %s: %w", rel, err)
			}
			result.BlobCount++
		}
	}

	// Validate manifest.
	if manifest != nil {
		if v, ok := manifest["schema_version"].(float64); ok && v > 2 {
			return RestoreResult{},
				fmt.Errorf("restore: unsupported backup schema version %.0f (max 2)", v)
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
	}

	return result, nil
}

func restoreFromDir(opts RestoreOpts) (RestoreResult, error) {
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
		blobDir := filepath.Join(filepath.Dir(opts.DBPath), "blobs")
		filepath.Walk(blobSrc, func(path string, fi os.FileInfo, werr error) error {
			if werr != nil || fi.IsDir() {
				return werr
			}
			rel, _ := filepath.Rel(blobSrc, path)
			target := filepath.Join(blobDir, rel)
			result.BlobCount++
			if opts.DryRun {
				return nil
			}
			os.MkdirAll(filepath.Dir(target), 0750)
			return copyFile(path, target)
		})
	}

	return result, nil
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
