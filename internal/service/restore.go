package service

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/safefs"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// RestoreOpts configures a restore operation.
type RestoreOpts struct {
	Source string // .tar.gz file or extracted directory
	DBPath string // target DB path
	// ConfigDir is the directory config entries restore into. Every
	// one must name a path inside it; required when the backup holds
	// config entries, unless SkipConfigs or DryRun.
	ConfigDir string
	// ConfigFile is the name, inside ConfigDir, of the config file the
	// target runs with (dpkms.yaml for dpkms). The backup's config.yaml
	// entry, a verbatim copy of the config file the backup ran with,
	// restores to it. Required when the backup holds that entry, unless
	// SkipConfigs or DryRun.
	ConfigFile string
	// SkipConfigs skips every config entry: config.yaml and config/.
	SkipConfigs bool
	DryRun      bool
	// RunDir is the data dir's run directory, where every running dpkms
	// instance keeps its pidfile. When set, restore refuses, before
	// anything is read or written, while a live instance there serves
	// DBPath. Dry runs included: the real restore would be refused.
	RunDir string
	// Blobs receives the backup's blobs, each with its backed-up
	// metadata, through its own Put, so they land wherever
	// storage.blob puts them, in its layout. Required
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
//
// The database is replaced atomically: written to a temporary file
// beside DBPath, fsynced, renamed over it, and the old database's
// -wal, -shm and -journal files removed, so a failed restore leaves the
// old database intact and SQLite never replays the old WAL onto the
// restored one.
//
// The whole backup is checked before anything is written: an entry
// that is not a regular file (symlink, hard link, device, fifo), a
// config entry naming a path outside opts.ConfigDir, a blob entry that
// is not a bare key, or an unsupported manifest refuses the restore
// with nothing written.
//
// Restore refuses, dry runs included, while a process holds the
// database lock on DBPath (every dpkms serve holds it on its database)
// and, when opts.RunDir is set, while a live pidfile there names
// DBPath. The writing pass holds that lock itself.
func Restore(ctx context.Context, opts RestoreOpts) (RestoreResult, error) {
	if opts.RunDir != "" {
		if err := refuseWhileServed(opts.RunDir, opts.DBPath); err != nil {
			return RestoreResult{}, err
		}
	}
	if err := refuseWhileLocked(opts.DBPath); err != nil {
		return RestoreResult{}, err
	}
	info, err := os.Stat(opts.Source)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore: %w", err)
	}

	if info.IsDir() {
		return restoreFromDir(ctx, opts)
	}
	return restoreFromArchive(ctx, opts)
}

// lockForWrite takes the database lock for the writing pass when it
// replaces the database; a dry run, or a backup without one, takes
// none. Call the returned func when done.
func (opts RestoreOpts) lockForWrite(has entrySet) (func(), error) {
	if opts.DryRun || !has.db {
		return func() {}, nil
	}
	l, err := acquireRestoreLock(opts.DBPath)
	if err != nil {
		return nil, err
	}
	return func() { _ = l.Release() }, nil
}

// entryKind is what a backup entry restores as.
type entryKind int

const (
	entrySkip entryKind = iota // not restored (directories, unknown names)
	entryManifest
	entryDB
	entryConfig
	entryActiveConfig // config.yaml: the config file the backup ran with
	entryBlob
	entryBlobMeta
)

// blobMetaDir holds one <key>.json BlobMeta sidecar per backed-up blob.
const blobMetaDir = "blobmeta/"

// backupEntry is one classified entry: rel is the config path (OS
// form, relative to the config dir) or the blob key.
type backupEntry struct {
	kind entryKind
	rel  string
}

// classifyEntry names what the backup entry name (slash-separated,
// relative to the backup root) restores as, and refuses any entry
// restore would write that is not a regular file or does not name a
// path inside its target.
func classifyEntry(name string, regular bool) (backupEntry, error) {
	var e backupEntry
	switch {
	case name == "manifest.json":
		e = backupEntry{kind: entryManifest}
	case name == "ctxt.db":
		e = backupEntry{kind: entryDB}
	case name == "config.yaml":
		e = backupEntry{kind: entryActiveConfig}
	case strings.HasPrefix(name, "config/"):
		rel, err := safefs.LocalName(strings.TrimPrefix(name, "config/"))
		if err != nil {
			return e, fmt.Errorf("restore: config entry %q: %w", name, err)
		}
		e = backupEntry{kind: entryConfig, rel: rel}
	case strings.HasPrefix(name, "blobs/"):
		key := strings.TrimPrefix(name, "blobs/")
		if !isBlobKey(key) {
			return e, fmt.Errorf("restore: blob %q: not a blob key", key)
		}
		e = backupEntry{kind: entryBlob, rel: key}
	case strings.HasPrefix(name, blobMetaDir):
		key, ok := strings.CutSuffix(strings.TrimPrefix(name, blobMetaDir), ".json")
		if !ok || !isBlobKey(key) {
			return e, fmt.Errorf("restore: blob metadata %q: not <blob key>.json", name)
		}
		e = backupEntry{kind: entryBlobMeta, rel: key}
	default:
		return backupEntry{kind: entrySkip}, nil
	}
	if !regular {
		return e, fmt.Errorf("restore: entry %q: not a regular file", name)
	}
	return e, nil
}

// isBlobKey reports whether key is a bare blob key. Backup names each
// blob by its bare key; anything with a path in it would let the
// archive write outside the store.
func isBlobKey(key string) bool {
	return key != "" && key != "." && key != ".." && !strings.ContainsAny(key, "/\\\x00")
}

// parseBlobMeta decodes one blob's metadata sidecar.
func parseBlobMeta(key string, r io.Reader) (storage.BlobMeta, error) {
	var meta storage.BlobMeta
	data, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return meta, fmt.Errorf("restore: read blob metadata %s: %w", key, err)
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return meta, fmt.Errorf("restore: parse blob metadata %s: %w", key, err)
	}
	return meta, nil
}

// preflight refuses a restore whose entries have nowhere to go, before
// anything is written.
func preflight(opts RestoreOpts, has entrySet) error {
	if opts.DryRun {
		return nil
	}
	if (has.configs || has.activeConfig) && !opts.SkipConfigs && opts.ConfigDir == "" {
		return errors.New("restore: backup holds config files but no config dir was given")
	}
	if has.activeConfig && !opts.SkipConfigs {
		if opts.ConfigFile == "" {
			return errors.New("restore: backup holds the config file but no config file name was given")
		}
		if _, err := safefs.LocalName(filepath.ToSlash(opts.ConfigFile)); err != nil {
			return fmt.Errorf("restore: config file: %w", err)
		}
	}
	if has.blobs && opts.Blobs == nil {
		return errors.New("restore: backup holds blobs but there is no blob store to restore into")
	}
	return nil
}

// entrySet records which kinds of entry a backup holds.
type entrySet struct {
	db, configs, activeConfig, blobs bool
}

func (s *entrySet) add(k entryKind) {
	switch k {
	case entryDB:
		s.db = true
	case entryConfig:
		s.configs = true
	case entryActiveConfig:
		s.activeConfig = true
	case entryBlob:
		s.blobs = true
	}
}

// staleDBSuffixes name the files SQLite keeps beside a database: a WAL
// or hot journal left from the old database would be replayed onto the
// restored one.
var staleDBSuffixes = []string{"-wal", "-shm", "-journal"}

// dbSourceHook, when set (tests only), wraps the database entry's
// reader.
var dbSourceHook func(io.Reader) io.Reader

// restorer writes classified entries to their targets.
type restorer struct {
	ctx  context.Context
	opts RestoreOpts
	// blobMeta is each blob's backed-up metadata, by key, read in the
	// check pass so a blob restores with it whatever the entry order.
	blobMeta map[string]storage.BlobMeta
	result   RestoreResult
}

func (r *restorer) write(e backupEntry, src io.Reader, size int64, perm fs.FileMode) error {
	switch e.kind {
	case entryDB:
		r.result.DBSize = size
		if r.opts.DryRun {
			return nil
		}
		if dbSourceHook != nil {
			src = dbSourceHook(src)
		}
		dir, name := filepath.Split(r.opts.DBPath)
		if dir == "" {
			dir = "."
		}
		if err := safefs.ReplaceFile(dir, name, src, perm, staleDBSuffixes...); err != nil {
			return fmt.Errorf("restore: write db: %w", err)
		}
	case entryConfig:
		if r.opts.SkipConfigs {
			return nil
		}
		r.result.ConfigCount++
		if r.opts.DryRun {
			return nil
		}
		if err := safefs.WriteFile(r.opts.ConfigDir, e.rel, src, perm); err != nil {
			return fmt.Errorf("restore: write config %s: %w", e.rel, err)
		}
	case entryActiveConfig:
		if r.opts.SkipConfigs {
			return nil
		}
		r.result.ConfigCount++
		if r.opts.DryRun {
			return nil
		}
		if err := safefs.WriteFile(r.opts.ConfigDir, r.opts.ConfigFile, src, perm); err != nil {
			return fmt.Errorf("restore: write config file %s: %w", r.opts.ConfigFile, err)
		}
	case entryBlob:
		r.result.BlobCount++
		if r.opts.DryRun {
			return nil
		}
		meta := r.blobMeta[e.rel]
		meta.Size = size
		if err := r.opts.Blobs.Put(r.ctx, e.rel, src, meta); err != nil {
			return fmt.Errorf("restore: write blob %s: %w", e.rel, err)
		}
	}
	return nil
}

func openArchive(source string) (*tar.Reader, func(), error) {
	f, err := os.Open(source)
	if err != nil {
		return nil, nil, fmt.Errorf("restore: open archive: %w", err)
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		_ = f.Close()
		return nil, nil, fmt.Errorf("restore: gzip reader: %w", err)
	}
	return tar.NewReader(gz), func() { _ = gz.Close(); _ = f.Close() }, nil
}

// walkArchive calls fn with every entry of the archive, classified.
// Directory and other non-restored entries are skipped.
func walkArchive(source string, fn func(e backupEntry, hdr *tar.Header, r io.Reader) error) error {
	tr, closeFn, err := openArchive(source)
	if err != nil {
		return err
	}
	defer closeFn()
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("restore: read tar: %w", err)
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		name := strings.TrimPrefix(hdr.Name, "ctxt-backup/")
		e, err := classifyEntry(name, hdr.Typeflag == tar.TypeReg)
		if err != nil {
			return err
		}
		if e.kind == entrySkip {
			continue
		}
		if err := fn(e, hdr, tr); err != nil {
			return err
		}
	}
}

func restoreFromArchive(ctx context.Context, opts RestoreOpts) (RestoreResult, error) {
	// Pass 1: check every entry and the manifest; write nothing.
	var has entrySet
	var manifest map[string]any
	blobMeta := map[string]storage.BlobMeta{}
	err := walkArchive(opts.Source, func(e backupEntry, _ *tar.Header, r io.Reader) error {
		switch e.kind {
		case entryManifest:
			m, err := parseManifest(r)
			if err != nil {
				return err
			}
			manifest = m
		case entryBlobMeta:
			meta, err := parseBlobMeta(e.rel, r)
			if err != nil {
				return err
			}
			blobMeta[e.rel] = meta
		default:
			has.add(e.kind)
		}
		return nil
	})
	if err != nil {
		return RestoreResult{}, err
	}
	if err := checkManifest(manifest); err != nil {
		return RestoreResult{}, err
	}
	if err := preflight(opts, has); err != nil {
		return RestoreResult{}, err
	}

	// Pass 2: write. Each entry is classified again, so an archive
	// that changed since pass 1 is still held to the same rules.
	release, err := opts.lockForWrite(has)
	if err != nil {
		return RestoreResult{}, err
	}
	defer release()
	rs := &restorer{ctx: ctx, opts: opts, blobMeta: blobMeta}
	err = walkArchive(opts.Source, func(e backupEntry, hdr *tar.Header, r io.Reader) error {
		return rs.write(e, r, hdr.Size, hdr.FileInfo().Mode())
	})
	if err != nil {
		return RestoreResult{}, err
	}
	return rs.result, nil
}

// parseManifest decodes a backup manifest. A corrupt manifest must not
// be swallowed: leaving it nil skips the schema-version guard,
// restoring an unsupported backup as if it were supported.
func parseManifest(r io.Reader) (map[string]any, error) {
	data, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("restore: read manifest: %w", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("restore: parse manifest: %w", err)
	}
	return manifest, nil
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

// restoreFromDir restores an extracted backup. Every read goes through
// an os.Root on the backup directory, so a symlink cannot reach
// outside it, and every entry restore writes must be a regular file.
func restoreFromDir(ctx context.Context, opts RestoreOpts) (RestoreResult, error) {
	root, err := os.OpenRoot(opts.Source)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore: open backup dir: %w", err)
	}
	defer func() { _ = root.Close() }()

	// Pass 1: collect and check every entry; write nothing.
	type dirEntry struct {
		backupEntry
		name string
	}
	var entries []dirEntry
	var manifest map[string]any
	var has entrySet
	blobMeta := map[string]storage.BlobMeta{}
	err = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return werr
		}
		e, err := classifyEntry(name, d.Type().IsRegular())
		if err != nil {
			return err
		}
		switch e.kind {
		case entrySkip:
			return nil
		case entryManifest:
			f, err := root.Open(name)
			if err != nil {
				return fmt.Errorf("restore: open manifest: %w", err)
			}
			defer f.Close()
			m, err := parseManifest(f)
			if err != nil {
				return err
			}
			manifest = m
			return nil
		case entryBlobMeta:
			f, err := root.Open(name)
			if err != nil {
				return fmt.Errorf("restore: open blob metadata: %w", err)
			}
			defer f.Close()
			meta, err := parseBlobMeta(e.rel, f)
			if err != nil {
				return err
			}
			blobMeta[e.rel] = meta
			return nil
		}
		has.add(e.kind)
		entries = append(entries, dirEntry{backupEntry: e, name: name})
		return nil
	})
	if err != nil {
		return RestoreResult{}, err
	}
	if err := checkManifest(manifest); err != nil {
		return RestoreResult{}, err
	}
	if err := preflight(opts, has); err != nil {
		return RestoreResult{}, err
	}

	// Pass 2: write.
	release, err := opts.lockForWrite(has)
	if err != nil {
		return RestoreResult{}, err
	}
	defer release()
	rs := &restorer{ctx: ctx, opts: opts, blobMeta: blobMeta}
	for _, e := range entries {
		if err := restoreDirEntry(rs, root, e.backupEntry, e.name); err != nil {
			return RestoreResult{}, err
		}
	}
	return rs.result, nil
}

func restoreDirEntry(rs *restorer, root *os.Root, e backupEntry, name string) error {
	// Lstat again: the entry must still be a regular file, not one
	// swapped for a symlink since pass 1.
	li, err := root.Lstat(name)
	if err != nil {
		return fmt.Errorf("restore: stat %s: %w", name, err)
	}
	if !li.Mode().IsRegular() {
		return fmt.Errorf("restore: entry %q: not a regular file", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return fmt.Errorf("restore: open %s: %w", name, err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("restore: stat %s: %w", name, err)
	}
	return rs.write(e, f, fi.Size(), fi.Mode())
}
