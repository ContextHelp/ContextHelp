package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/keys"
)

var (
	// ErrEncryptionKeyRequired is returned when a SQLite database has a
	// sidecar key envelope (<db>.key) indicating it is encrypted, but no
	// keys.Provider was supplied to unlock it.
	ErrEncryptionKeyRequired = errors.New("sqlite: database is encrypted; configure storage.encryption key provider")

	// ErrEncryptionUnsupported is returned when storage encryption is requested
	// on a binary compiled without the SQLite3MultipleCiphers codec
	// (rebuild with -tags "libsqlite3 fts5" using scripts/ci/sqlite3mc.sh).
	ErrEncryptionUnsupported = errors.New("sqlite: encryption codec not compiled into this binary")
)

const (
	sqliteCipherName     = "chacha20"
	sqliteKeyFileVersion = 1
)

// KeyEnvelopePath returns the sidecar path holding the KEK-wrapped SQLite DEK.
func KeyEnvelopePath(dbPath string) string { return dbPath + ".key" }

type dbKeyEnvelope struct {
	Version    int    `json:"version"`
	Cipher     string `json:"cipher"`
	KeyID      string `json:"key_id"`
	WrappedDEK string `json:"wrapped_dek"`
}

func dbKeyAAD(dbPath string) []byte {
	return []byte("sqlite-db:" + filepath.Base(dbPath))
}

// HasKeyEnvelope reports whether dbPath has a sidecar <db>.key envelope
// indicating an encrypted SQLite database.
func HasKeyEnvelope(dbPath string) bool {
	if IsMemoryPath(dbPath) {
		return false
	}
	_, err := os.Stat(KeyEnvelopePath(dbPath))
	return err == nil
}

func hasKeyEnvelope(dbPath string) bool { return HasKeyEnvelope(dbPath) }

func readKeyEnvelope(dbPath string) (dbKeyEnvelope, error) {
	raw, err := os.ReadFile(KeyEnvelopePath(dbPath))
	if err != nil {
		return dbKeyEnvelope{}, err
	}
	var env dbKeyEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return dbKeyEnvelope{}, fmt.Errorf("parse sqlite key envelope: %w", err)
	}
	if env.Version != sqliteKeyFileVersion || env.Cipher == "" || env.KeyID == "" || env.WrappedDEK == "" {
		return dbKeyEnvelope{}, fmt.Errorf("invalid sqlite key envelope: %w", keys.ErrUnwrap)
	}
	return env, nil
}

func writeKeyEnvelopeAtomic(dbPath string, env dbKeyEnvelope) error {
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sqlite key envelope: %w", err)
	}
	b = append(b, '\n')
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create key envelope dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".db-key-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp key envelope: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, KeyEnvelopePath(dbPath)); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("publish sqlite key envelope: %w", err)
	}
	return nil
}

func unwrapDBKey(ctx context.Context, dbPath string, p keys.Provider) (hexKey, cipher string, err error) {
	env, err := readKeyEnvelope(dbPath)
	if err != nil {
		return "", "", err
	}
	wrapped, err := base64.StdEncoding.DecodeString(env.WrappedDEK)
	if err != nil {
		return "", "", fmt.Errorf("decode wrapped sqlite dek: %w", keys.ErrUnwrap)
	}
	dek, err := p.Unwrap(ctx, env.KeyID, wrapped, dbKeyAAD(dbPath))
	if err != nil {
		return "", "", fmt.Errorf("unwrap sqlite dek: %w", err)
	}
	if len(dek) != keys.MaterialSize {
		return "", "", fmt.Errorf("invalid sqlite dek length %d: %w", len(dek), keys.ErrUnwrap)
	}
	return hex.EncodeToString(dek), env.Cipher, nil
}

func createAndWrapDBKey(ctx context.Context, dbPath string, p keys.Provider) (hexKey string, env dbKeyEnvelope, err error) {
	dek := make([]byte, keys.MaterialSize)
	if _, err := rand.Read(dek); err != nil {
		return "", dbKeyEnvelope{}, fmt.Errorf("generate sqlite dek: %w", err)
	}
	wrapped, usedID, err := p.Wrap(ctx, "", dek, dbKeyAAD(dbPath))
	if err != nil {
		return "", dbKeyEnvelope{}, fmt.Errorf("wrap sqlite dek: %w", err)
	}
	return hex.EncodeToString(dek), dbKeyEnvelope{
		Version:    sqliteKeyFileVersion,
		Cipher:     sqliteCipherName,
		KeyID:      usedID,
		WrappedDEK: base64.StdEncoding.EncodeToString(wrapped),
	}, nil
}

// resolveSQLiteDSN applies SQLite encryption URI parameters when keyProvider
// is set and enforces fail-closed behavior if a .key sidecar exists without a
// keyProvider or if an existing plaintext database is opened with encryption
// before running EncryptDatabase.
func resolveSQLiteDSN(ctx context.Context, path, baseDSN string, readOnly bool, p keys.Provider) (dsn string, createdKey bool, err error) {
	if IsMemoryPath(path) {
		return baseDSN, false, nil
	}
	keyExists := hasKeyEnvelope(path)
	if p == nil {
		if keyExists {
			return "", false, fmt.Errorf("%w: %s", ErrEncryptionKeyRequired, KeyEnvelopePath(path))
		}
		return baseDSN, false, nil
	}

	var (
		hexKey string
		cipher string
	)
	if keyExists {
		hexKey, cipher, err = unwrapDBKey(ctx, path, p)
		if err != nil {
			return "", false, err
		}
	} else {
		if readOnly {
			return "", false, fmt.Errorf("open encrypted sqlite %s read-only: missing %s: %w", path, KeyEnvelopePath(path), keys.ErrUnavailable)
		}
		// Refuse to silently open an existing non-empty plaintext DB under
		// encryption: require explicit migration via EncryptDatabase.
		if info, serr := os.Stat(path); serr == nil && info.Size() > 0 {
			return "", false, fmt.Errorf("sqlite database %s is plaintext; run `dpkms storage encrypt` to migrate it before opening with encryption", path)
		}
		var env dbKeyEnvelope
		hexKey, env, err = createAndWrapDBKey(ctx, path, p)
		if err != nil {
			return "", false, err
		}
		if err := writeKeyEnvelopeAtomic(path, env); err != nil {
			return "", false, err
		}
		cipher = env.Cipher
		createdKey = true
	}

	return injectCipherParams(baseDSN, cipher, hexKey), createdKey, nil
}

// injectCipherParams prepends cipher=<cipher>&hexkey=<hexKey> to the query
// string of a file: SQLite URI so SQLite3MultipleCiphers keys the connection
// inside sqlite3_open_v2 before mattn/go-sqlite3 executes connection pragmas.
func injectCipherParams(baseDSN, cipher, hexKey string) string {
	prefix := "cipher=" + cipher + "&hexkey=" + hexKey
	if idx := strings.Index(baseDSN, "?"); idx >= 0 {
		return baseDSN[:idx+1] + prefix + "&" + baseDSN[idx+1:]
	}
	return baseDSN + "?" + prefix
}

// verifySQLiteCodec confirms the linked SQLite engine supports SQLite3MultipleCiphers
// encryption and activated the expected cipher (fail closed if linked against
// stock unencrypted SQLite).
func verifySQLiteCodec(ctx context.Context, db *sql.DB) error {
	var cipher string
	if err := db.QueryRowContext(ctx, `PRAGMA cipher`).Scan(&cipher); err != nil || strings.TrimSpace(cipher) == "" {
		return fmt.Errorf("%w: PRAGMA cipher unavailable", ErrEncryptionUnsupported)
	}
	return nil
}

// EncryptDatabase migrates an existing plaintext SQLite database at dbPath to
// an encrypted ChaCha20-Poly1305 SQLite database using a freshly generated DEK
// wrapped by p. The migration uses crash-safe VACUUM INTO + verification +
// atomic rename. Returns true if migrated, false if already encrypted.
func EncryptDatabase(ctx context.Context, dbPath string, p keys.Provider) (bool, error) {
	if p == nil {
		return false, fmt.Errorf("encrypt database: %w", keys.ErrUnavailable)
	}
	if IsMemoryPath(dbPath) {
		return false, errors.New("encrypt database: cannot encrypt in-memory database")
	}
	if hasKeyEnvelope(dbPath) {
		return false, nil
	}
	if _, err := os.Stat(dbPath); err != nil {
		return false, fmt.Errorf("encrypt database: stat %s: %w", dbPath, err)
	}

	hexKey, env, err := createAndWrapDBKey(ctx, dbPath, p)
	if err != nil {
		return false, err
	}

	plainDB, err := sql.Open("sqlite3", "file:"+dbPath+"?_busy_timeout=5000")
	if err != nil {
		return false, fmt.Errorf("encrypt database: open plaintext: %w", err)
	}
	defer func() { _ = plainDB.Close() }()

	if err := verifySQLiteCodec(ctx, plainDB); err != nil {
		return false, err
	}

	tmpEnc := dbPath + ".encrypting.tmp"
	_ = os.Remove(tmpEnc)
	defer func() { _ = os.Remove(tmpEnc) }()

	targetURI := fmt.Sprintf("file:%s?cipher=%s&hexkey=%s", tmpEnc, env.Cipher, hexKey)
	if _, err := plainDB.ExecContext(ctx, `VACUUM INTO ?`, targetURI); err != nil {
		return false, fmt.Errorf("encrypt database: vacuum into encrypted copy: %w", err)
	}
	if err := plainDB.Close(); err != nil {
		return false, fmt.Errorf("encrypt database: close plaintext: %w", err)
	}

	// Verify the encrypted copy opens and passes quick_check before swapping.
	if err := verifyEncryptedFile(ctx, tmpEnc, env.Cipher, hexKey); err != nil {
		return false, err
	}

	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
	if err := writeKeyEnvelopeAtomic(dbPath, env); err != nil {
		return false, err
	}
	if err := os.Rename(tmpEnc, dbPath); err != nil {
		return false, fmt.Errorf("encrypt database: publish encrypted db: %w", err)
	}
	return true, nil
}

// RewrapDatabaseKey rotates the KEK wrapping <dbPath>.key without rewriting
// database pages (O(1) key rotation).
func RewrapDatabaseKey(ctx context.Context, dbPath string, p keys.Provider, toKeyID string) (string, error) {
	if p == nil {
		return "", fmt.Errorf("rewrap database key: %w", keys.ErrUnavailable)
	}
	env, err := readKeyEnvelope(dbPath)
	if err != nil {
		return "", fmt.Errorf("rewrap database key: %w", err)
	}
	wrapped, err := base64.StdEncoding.DecodeString(env.WrappedDEK)
	if err != nil {
		return "", fmt.Errorf("rewrap database key: %w", keys.ErrUnwrap)
	}
	nextWrapped, usedID, err := p.Rewrap(ctx, env.KeyID, wrapped, dbKeyAAD(dbPath), toKeyID)
	if err != nil {
		return "", fmt.Errorf("rewrap database key: %w", err)
	}
	env.KeyID = usedID
	env.WrappedDEK = base64.StdEncoding.EncodeToString(nextWrapped)
	if err := writeKeyEnvelopeAtomic(dbPath, env); err != nil {
		return "", err
	}
	return usedID, nil
}

// RekeyDatabase rotates the underlying SQLite DEK itself via crash-safe
// VACUUM INTO + atomic rename and updates <dbPath>.key.
func RekeyDatabase(ctx context.Context, dbPath string, p keys.Provider) error {
	if p == nil {
		return fmt.Errorf("rekey database: %w", keys.ErrUnavailable)
	}
	oldHex, oldCipher, err := unwrapDBKey(ctx, dbPath, p)
	if err != nil {
		return fmt.Errorf("rekey database: unwrap current key: %w", err)
	}
	newHex, newEnv, err := createAndWrapDBKey(ctx, dbPath, p)
	if err != nil {
		return err
	}

	srcDSN := fmt.Sprintf("file:%s?cipher=%s&hexkey=%s&_busy_timeout=5000", dbPath, oldCipher, oldHex)
	srcDB, err := sql.Open("sqlite3", srcDSN)
	if err != nil {
		return fmt.Errorf("rekey database: open: %w", err)
	}
	defer func() { _ = srcDB.Close() }()

	if err := verifySQLiteCodec(ctx, srcDB); err != nil {
		return err
	}

	tmpPath := dbPath + ".rekey.tmp"
	_ = os.Remove(tmpPath)
	defer func() { _ = os.Remove(tmpPath) }()

	dstURI := fmt.Sprintf("file:%s?cipher=%s&hexkey=%s", tmpPath, newEnv.Cipher, newHex)
	if _, err := srcDB.ExecContext(ctx, `VACUUM INTO ?`, dstURI); err != nil {
		return fmt.Errorf("rekey database: vacuum into: %w", err)
	}
	if err := srcDB.Close(); err != nil {
		return fmt.Errorf("rekey database: close: %w", err)
	}
	if err := verifyEncryptedFile(ctx, tmpPath, newEnv.Cipher, newHex); err != nil {
		return err
	}

	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
	if err := writeKeyEnvelopeAtomic(dbPath, newEnv); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dbPath); err != nil {
		return fmt.Errorf("rekey database: publish: %w", err)
	}
	return nil
}

func verifyEncryptedFile(ctx context.Context, path, cipher, hexKey string) error {
	vdb, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?cipher=%s&hexkey=%s&mode=ro", path, cipher, hexKey))
	if err != nil {
		return fmt.Errorf("verify encrypted db: open: %w", err)
	}
	defer func() { _ = vdb.Close() }()
	var check string
	if err := vdb.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&check); err != nil {
		return fmt.Errorf("verify encrypted db: quick_check: %w", err)
	}
	if !strings.EqualFold(check, "ok") {
		return fmt.Errorf("verify encrypted db: quick_check returned %q", check)
	}
	return nil
}

// SnapshotDatabase writes a consistent VACUUM INTO copy of srcPath to dstPath.
// When srcPath has a <db>.key envelope, p is required to unwrap the database
// key; the snapshot is written with the same cipher so plaintext never touches
// the temporary file on disk.
func SnapshotDatabase(ctx context.Context, srcPath, dstPath string, p keys.Provider) error {
	dsn := "file:" + srcPath + "?mode=ro&_busy_timeout=5000"
	if HasKeyEnvelope(srcPath) {
		if p == nil {
			return fmt.Errorf("%w: %s", ErrEncryptionKeyRequired, KeyEnvelopePath(srcPath))
		}
		hexKey, cipher, err := unwrapDBKey(ctx, srcPath, p)
		if err != nil {
			return fmt.Errorf("snapshot encrypted db: %w", err)
		}
		dsn = injectCipherParams(dsn, cipher, hexKey)
	}
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return fmt.Errorf("snapshot: open: %w", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, dstPath); err != nil {
		return fmt.Errorf("snapshot: vacuum into: %w", err)
	}
	return nil
}

// CodecSupported reports whether this binary was compiled with the
// SQLite3MultipleCiphers encryption codec (-tags "libsqlite3 fts5").
func CodecSupported(ctx context.Context) bool {
	db, err := sql.Open("sqlite3", "file::memory:")
	if err != nil {
		return false
	}
	defer func() { _ = db.Close() }()
	return verifySQLiteCodec(ctx, db) == nil
}

// cleanupCreatedKeyEnvelope removes an orphan key file and freshly created
// database files when an initial encrypted open fails before returning a driver.
func cleanupCreatedKeyEnvelope(dbPath string, created bool) {
	if !created {
		return
	}
	_ = os.Remove(KeyEnvelopePath(dbPath))
	_ = os.Remove(dbPath)
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
}
