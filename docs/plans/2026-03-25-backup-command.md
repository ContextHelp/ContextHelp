# Backup Command Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `dpkms backup` command that snapshots the SQLite DB (and optionally blobs)
into a timestamped `.tar.gz` archive, with sync (default) or async (`--async`) execution.

**Architecture:** New Cobra command in `cmd/dpkms/cmd/backup.go` calls
`service.Backup(ctx, BackupOpts)`. Service layer orchestrates SQLite `VACUUM INTO`
snapshot + optional blob walk + archive write. Async path enqueues a `"backup"` pipeline
job reusing existing job infrastructure. Config adds optional `backup.dir` field.

**Tech Stack:** Go, cobra, `archive/tar`, `compress/gzip`, `database/sql` (VACUUM INTO),
existing `storageutil`, `config`, `service`, `jobs` packages.

---

## Existing Code to Know

- `cmd/dpkms/cmd/housekeeping.go` — `openDB()` helper: opens sqlite driver from cfg;
  reference for how backup command opens storage. Note: housekeeping only supports sqlite —
  backup does the same for now.
- `cmd/dpkms/cmd/serve.go` — shows how `cfg` global is used in dpkms commands.
- `internal/storageutil/factory.go` — `NewDriver(typ, path)` — not needed directly (we
  call `openDB()` pattern), but shows the driver wiring.
- `internal/storage/blob/local/store.go` — `Store.root` is unexported; blob walk uses
  `BlobStore.List(ctx, "")` to enumerate keys, then `BlobStore.Get(ctx, key)` to stream.
- `internal/storage/blob/factory.go` — `blob.New(cfg.BlobConfig)` — needed to open blob
  store for `--include-blobs`.
- `internal/config/config.go:181` — `StorageConfig` struct; we add `BackupConfig` here.
- `internal/jobs/queue.go` — `Queue.Enqueue(ctx, *storage.Job)` — used for async path.
- `internal/storage/types.go` — `Job` struct: `Pipeline string` field used as job kind
  discriminator (set to `"backup"` for async jobs).

---

## Task 1: Add `BackupConfig` to config

**Files:**
- Modify: `internal/config/config.go:38` (Config struct)
- Modify: `internal/config/config.go:181` (after StorageConfig)

**Step 1: Write failing test**

```go
// internal/config/config_test.go  (add to existing file)
func TestBackupConfigDefaults(t *testing.T) {
    cfg := Config{}
    if cfg.Backup.Dir != "" {
        t.Fatalf("expected empty backup dir, got %q", cfg.Backup.Dir)
    }
}
```

**Step 2: Run test to verify it fails**

```
cd internal/config && go test -run TestBackupConfigDefaults -v
```
Expected: FAIL — `Config` has no `Backup` field.

**Step 3: Add BackupConfig**

In `Config` struct (after `Storage StorageConfig` line):
```go
Backup BackupConfig `mapstructure:"backup" yaml:"backup"`
```

After `StorageConfig` block, add:
```go
// BackupConfig holds settings for the backup command.
type BackupConfig struct {
    // Dir is the directory where backup archives are written.
    // Defaults to the current working directory if empty.
    Dir string `mapstructure:"dir" yaml:"dir"`
}
```

**Step 4: Run test to verify it passes**

```
cd internal/config && go test -run TestBackupConfigDefaults -v
```
Expected: PASS

**Step 5: Commit**

```
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add BackupConfig with optional backup.dir field"
```

---

## Task 2: Add SQLite snapshot helper

**Files:**
- Create: `internal/storageutil/snapshot.go`
- Create: `internal/storageutil/snapshot_test.go`

**Step 1: Write failing test**

```go
// internal/storageutil/snapshot_test.go
package storageutil_test

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func TestSQLiteSnapshot(t *testing.T) {
    // create a throwaway source db
    src := filepath.Join(t.TempDir(), "source.db")
    dst := filepath.Join(t.TempDir(), "snapshot.db")

    // write something so the file exists
    if err := os.WriteFile(src, []byte(""), 0644); err != nil {
        t.Fatal(err)
    }

    if err := storageutil.SQLiteSnapshot(src, dst); err != nil {
        t.Fatalf("snapshot: %v", err)
    }
    if _, err := os.Stat(dst); err != nil {
        t.Fatalf("snapshot file not created: %v", err)
    }
}
```

**Step 2: Run test to verify it fails**

```
cd internal/storageutil && go test -run TestSQLiteSnapshot -v
```
Expected: FAIL — `SQLiteSnapshot` not defined.

**Step 3: Implement**

```go
// internal/storageutil/snapshot.go
package storageutil

import (
    "database/sql"
    "fmt"

    _ "github.com/mattn/go-sqlite3"
)

// SQLiteSnapshot creates a consistent copy of the SQLite database at srcPath
// by executing VACUUM INTO dstPath. Safe to call while the DB is open and
// being written to.
func SQLiteSnapshot(srcPath, dstPath string) error {
    db, err := sql.Open("sqlite3", srcPath)
    if err != nil {
        return fmt.Errorf("snapshot: open: %w", err)
    }
    defer db.Close()

    if _, err := db.Exec("VACUUM INTO ?", dstPath); err != nil {
        return fmt.Errorf("snapshot: vacuum into: %w", err)
    }
    return nil
}
```

**Step 4: Run test to verify it passes**

```
cd internal/storageutil && go test -run TestSQLiteSnapshot -v
```
Expected: PASS

**Step 5: Commit**

```
git add internal/storageutil/snapshot.go internal/storageutil/snapshot_test.go
git commit -m "feat(storageutil): add SQLiteSnapshot via VACUUM INTO"
```

---

## Task 3: Add `service.Backup()`

**Files:**
- Create: `internal/service/backup.go`
- Create: `internal/service/backup_test.go`

**Step 1: Write failing test**

```go
// internal/service/backup_test.go
package service_test

import (
    "context"
    "os"
    "path/filepath"
    "strings"
    "testing"

    "github.com/ideacrafterslabs/ctxt/internal/service"
    "github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func TestBackupCreatesArchive(t *testing.T) {
    dir := t.TempDir()
    dbPath := filepath.Join(dir, "test.db")
    outDir := filepath.Join(dir, "out")
    os.MkdirAll(outDir, 0755)

    // create minimal sqlite db
    if err := storageutil.SQLiteSnapshot(dbPath, dbPath); err != nil {
        // VACUUM INTO on non-existent src fails — create empty db first
        f, _ := os.Create(dbPath)
        f.Close()
    }

    opts := service.BackupOpts{
        DBPath:       dbPath,
        BlobBackend:  "stub",
        IncludeBlobs: false,
        OutputDir:    outDir,
    }

    result, err := service.Backup(context.Background(), opts)
    if err != nil {
        t.Fatalf("Backup: %v", err)
    }
    if !strings.HasSuffix(result.Path, ".tar.gz") {
        t.Fatalf("expected .tar.gz, got %q", result.Path)
    }
    if _, err := os.Stat(result.Path); err != nil {
        t.Fatalf("archive not created: %v", err)
    }
}
```

**Step 2: Run test to verify it fails**

```
cd internal/service && go test -run TestBackupCreatesArchive -v
```
Expected: FAIL — `service.Backup`, `BackupOpts`, `BackupResult` not defined.

**Step 3: Implement**

```go
// internal/service/backup.go
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
    DBPath       string           // path to sqlite file
    BlobCfg      config.BlobConfig
    BlobBackend  string           // "local", "s3", "stub" — derived from BlobCfg if empty
    IncludeBlobs bool
    OutputDir    string           // directory to write archive; cwd if empty
    SkipBlobErrors bool
}

// BackupResult summarises a completed backup.
type BackupResult struct {
    Path      string
    DBSize    int64
    BlobCount int
    Duration  time.Duration
}

// Backup creates a timestamped .tar.gz archive of the database (and optionally
// blobs). It cleans up a partial archive on error.
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

    ts := time.Now().UTC().Format("2006-01-02T15-04-05")
    archiveName := fmt.Sprintf("ctxt-backup-%s.tar.gz", ts)
    archivePath := filepath.Join(outDir, archiveName)

    f, err := os.Create(archivePath)
    if err != nil {
        return BackupResult{}, fmt.Errorf("backup: create archive: %w", err)
    }

    var result BackupResult
    result.Path = archivePath

    writeErr := func(err error) (BackupResult, error) {
        f.Close()
        os.Remove(archivePath)
        return BackupResult{}, err
    }

    gz := gzip.NewWriter(f)
    tw := tar.NewWriter(gz)

    // 1. SQLite snapshot
    tmpDB := filepath.Join(os.TempDir(), fmt.Sprintf("ctxt-snap-%d.db", time.Now().UnixNano()))
    defer os.Remove(tmpDB)

    if err := storageutil.SQLiteSnapshot(opts.DBPath, tmpDB); err != nil {
        return writeErr(fmt.Errorf("backup: snapshot db: %w", err))
    }

    dbInfo, err := os.Stat(tmpDB)
    if err != nil {
        return writeErr(fmt.Errorf("backup: stat snapshot: %w", err))
    }
    result.DBSize = dbInfo.Size()

    if err := addFileToTar(tw, tmpDB, "ctxt-backup/ctxt.db"); err != nil {
        return writeErr(fmt.Errorf("backup: tar db: %w", err))
    }

    // 2. Optional blobs (local only)
    if opts.IncludeBlobs && opts.BlobCfg.Backend == "local" {
        bs, err := blobfactory.New(opts.BlobCfg)
        if err != nil {
            return writeErr(fmt.Errorf("backup: open blob store: %w", err))
        }
        items, err := bs.List(ctx, "")
        if err != nil {
            return writeErr(fmt.Errorf("backup: list blobs: %w", err))
        }
        for _, item := range items {
            rc, _, err := bs.Get(ctx, item.Key)
            if err != nil {
                if opts.SkipBlobErrors {
                    continue
                }
                return writeErr(fmt.Errorf("backup: get blob %q: %w", item.Key, err))
            }
            hdr := &tar.Header{
                Name:    fmt.Sprintf("ctxt-backup/blobs/%s", item.Key),
                Size:    item.Size,
                Mode:    0644,
                ModTime: item.UpdatedAt,
            }
            if err := tw.WriteHeader(hdr); err != nil {
                rc.Close()
                return writeErr(fmt.Errorf("backup: tar blob header: %w", err))
            }
            if _, err := io.Copy(tw, rc); err != nil {
                rc.Close()
                return writeErr(fmt.Errorf("backup: tar blob data: %w", err))
            }
            rc.Close()
            result.BlobCount++
        }
    }

    // 3. manifest.json (written last — signals completeness)
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
    tw.WriteHeader(hdr) //nolint:errcheck
    tw.Write(manifestBytes) //nolint:errcheck

    if err := tw.Close(); err != nil {
        return writeErr(fmt.Errorf("backup: close tar: %w", err))
    }
    if err := gz.Close(); err != nil {
        return writeErr(fmt.Errorf("backup: close gzip: %w", err))
    }
    if err := f.Close(); err != nil {
        return writeErr(fmt.Errorf("backup: close file: %w", err))
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
```

**Step 4: Run test to verify it passes**

```
cd internal/service && go test -run TestBackupCreatesArchive -v
```
Expected: PASS

**Step 5: Commit**

```
git add internal/service/backup.go internal/service/backup_test.go
git commit -m "feat(service): add Backup() — SQLite snapshot + tar.gz archive"
```

---

## Task 4: Add `dpkms backup` Cobra command

**Files:**
- Create: `cmd/dpkms/cmd/backup.go`
- Modify: `cmd/dpkms/cmd/root.go` (register command)

**Step 1: Write failing test**

```go
// cmd/dpkms/cmd/backup_test.go
package cmd_test

import (
    "os"
    "testing"
)

func TestBackupCommandRegistered(t *testing.T) {
    found := false
    for _, c := range rootCmd.Commands() {
        if c.Use == "backup" {
            found = true
            break
        }
    }
    if !found {
        t.Fatal("backup command not registered on rootCmd")
    }
}
```

**Step 2: Run test to verify it fails**

```
cd cmd/dpkms/cmd && go test -run TestBackupCommandRegistered -v
```
Expected: FAIL — `backup` not in rootCmd.

**Step 3: Implement**

```go
// cmd/dpkms/cmd/backup.go
package cmd

import (
    "context"
    "fmt"
    "os"
    "time"

    "github.com/spf13/cobra"

    blobfactory "github.com/ideacrafterslabs/ctxt/internal/storage/blob"
    "github.com/ideacrafterslabs/ctxt/internal/jobs"
    "github.com/ideacrafterslabs/ctxt/internal/service"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
    "github.com/ideacrafterslabs/ctxt/internal/storageutil"
    "github.com/google/uuid"
)

var backupCmd = &cobra.Command{
    Use:   "backup",
    Short: "Create a backup archive of user data",
    Long: `Create a timestamped .tar.gz archive containing:
  - SQLite database snapshot (always)
  - Blob files (with --include-blobs, local backend only)

Output directory defaults to backup.dir in config, then current directory.

Examples:
  dpkms backup
  dpkms backup --include-blobs
  dpkms backup --output-dir /var/backups/ctxt-now.tar.gz
  dpkms backup --async`,
    RunE: runBackup,
}

func init() {
    rootCmd.AddCommand(backupCmd)
    backupCmd.Flags().Bool("include-blobs", false, "include local blob files in archive")
    backupCmd.Flags().String("output", "", "override output archive path")
    backupCmd.Flags().Bool("async", false, "enqueue as background job and exit")
    backupCmd.Flags().Bool("skip-blob-errors", false, "skip unreadable blobs instead of failing")
}

func runBackup(cmd *cobra.Command, _ []string) error {
    includeBlobs, _ := cmd.Flags().GetBool("include-blobs")
    outputFlag, _ := cmd.Flags().GetString("output")
    async, _ := cmd.Flags().GetBool("async")
    skipBlobErrors, _ := cmd.Flags().GetBool("skip-blob-errors")

    // Resolve output dir (flag > config > cwd).
    outDir := cfg.Backup.Dir
    if outputFlag != "" {
        outDir = outputFlag
    }

    // DB path from config.
    dbPath := cfg.Storage.Path
    if dbPath == "" {
        dbPath = "dpkms.db"
    }

    if cfg.Storage.Type != "" && cfg.Storage.Type != "sqlite" {
        return fmt.Errorf("backup only supports sqlite storage (got %s)", cfg.Storage.Type)
    }

    // Async: enqueue job and exit.
    if async {
        driver, cleanup, err := openDB()
        if err != nil {
            return err
        }
        defer cleanup()

        q := jobs.NewQueue(driver.Jobs())
        job := &storage.Job{
            ID:       uuid.New().String(),
            Pipeline: "backup",
            Status:   storage.JobPending,
            Payload:  map[string]any{
                "include_blobs":    includeBlobs,
                "output_dir":       outDir,
                "skip_blob_errors": skipBlobErrors,
            },
        }
        if err := q.Enqueue(context.Background(), job); err != nil {
            return fmt.Errorf("enqueue backup job: %w", err)
        }
        fmt.Printf("Backup job enqueued: %s\n", job.ID)
        fmt.Printf("Monitor with: dpkms jobs show %s\n", job.ID)
        return nil
    }

    // Sync: run directly.
    opts := service.BackupOpts{
        DBPath:         dbPath,
        BlobCfg:        cfg.Storage.Blob,
        IncludeBlobs:   includeBlobs,
        OutputDir:      outDir,
        SkipBlobErrors: skipBlobErrors,
    }

    fmt.Fprintf(os.Stderr, "→ snapshot db...\n")
    start := time.Now()
    result, err := service.Backup(context.Background(), opts)
    if err != nil {
        return fmt.Errorf("backup: %w", err)
    }

    fmt.Printf("✓ %s (db: %s, blobs: %d, %.1fs)\n",
        result.Path,
        humanBytes(result.DBSize),
        result.BlobCount,
        result.Duration.Seconds(),
    )
    return nil
}

func humanBytes(b int64) string {
    const mb = 1024 * 1024
    if b < mb {
        return fmt.Sprintf("%d KB", b/1024)
    }
    return fmt.Sprintf("%.1f MB", float64(b)/mb)
}
```

Register in `init()` — already done via `rootCmd.AddCommand(backupCmd)` in `backup.go`.

**Step 4: Run test to verify it passes**

```
cd cmd/dpkms/cmd && go test -run TestBackupCommandRegistered -v
```
Expected: PASS

**Step 5: Commit**

```
git add cmd/dpkms/cmd/backup.go cmd/dpkms/cmd/backup_test.go
git commit -m "feat(cli): add dpkms backup command with --include-blobs and --async"
```

---

## Task 5: Async job worker support

**Files:**
- Modify: `internal/jobs/worker.go` (or equivalent worker dispatch file)

**Step 1: Find the worker dispatch**

```
grep -rn "AcquireNext\|Pipeline\|dispatch" internal/jobs/ --include="*.go" | head -20
```

Locate where acquired jobs are dispatched to pipelines. Add a case for `job.Pipeline == "backup"`.

**Step 2: Write failing test**

```go
// Find the worker test file and add:
func TestWorkerDispatchesBackupJob(t *testing.T) {
    // enqueue a backup job, run one worker tick, verify job completes
    // (use temp dir for output)
}
```

**Step 3: Implement dispatch**

In the worker's job dispatch switch/if, add:
```go
case "backup":
    payload := job.Payload
    opts := service.BackupOpts{
        DBPath:         cfg.Storage.Path,
        BlobCfg:        cfg.Storage.Blob,
        IncludeBlobs:   payload["include_blobs"] == true,
        OutputDir:      fmt.Sprintf("%v", payload["output_dir"]),
        SkipBlobErrors: payload["skip_blob_errors"] == true,
    }
    _, err = svc.Backup(ctx, opts) // TODO: svc needs Backup method wired
```

Wire `service.Backup` as a method on `*Service` or call the package function directly.

**Step 4: Run tests**

```
go test ./internal/jobs/... -v
```

**Step 5: Commit**

```
git add internal/jobs/
git commit -m "feat(jobs): dispatch backup pipeline jobs to service.Backup"
```

---

## Task 6: Integration smoke test

**Files:**
- Create: `test/integration/us0034_export_backup_test.go`

**Step 1: Write test**

```go
func TestBackupCommandProducesValidArchive(t *testing.T) {
    // 1. start dpkms with temp db
    // 2. ingest 2 objects via API
    // 3. run `dpkms backup --output-dir <tmpdir>`
    // 4. verify archive exists and contains ctxt.db + manifest.json
    // 5. verify manifest blob_count == 0, db_size > 0
}
```

Follow pattern in `test/integration/e2e_test.go` for service startup.

**Step 2: Run**

```
go test ./test/integration/ -run TestBackupCommandProducesValidArchive -v
```

**Step 3: Commit**

```
git add test/integration/us0034_export_backup_test.go
git commit -m "test(integration): add backup command smoke test (US-0034)"
```

---

## Notes

- `VACUUM INTO` requires SQLite ≥ 3.27 (2019-02-08) — already satisfied by `go-sqlite3`.
- Postgres backup is out of scope; `runBackup` returns an error for non-sqlite configs.
- `manifest.json` written last inside archive — its presence = complete backup signal.
- Future `dpkms restore` can use `manifest.json` schema_version for forward compat.
- `--output` flag accepts a full path (overrides dir resolution entirely).
