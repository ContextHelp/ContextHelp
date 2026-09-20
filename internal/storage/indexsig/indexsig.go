// Package indexsig implements the driver-neutral half of ADR-070 index
// provenance: computing, storing, and verifying the signatures that describe
// the live search indexes so drift triggers a rebuild instead of silent
// staleness. Both storage drivers share the index_signatures table shape and
// this package's hashing; only the inputs are dialect-specific (SQLite reads
// DDL from sqlite_master, Postgres reads the generated-column expression and
// index definitions from the catalog).
package indexsig

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/search/ftsq"
)

// Dialect selects the SQL dialect for catalog reads and placeholders.
type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
)

// FTSSignatureID is the canonical signature key for the FTS index — the
// objects_fts virtual table on SQLite, the generated tsvector column plus
// GIN index on Postgres. One key for both: it names the FTS capability, not
// the storage artifact.
const FTSSignatureID = "objects_fts"

// ProjectionVersion is the build-time-known version of internal/projection
// logic that feeds the indexed FTS body. Bump when projection.ProjectIndex
// behaviour diverges in a way that should trigger reindex_auto on existing
// installations. ADR-070 §3 calls this the "projection logic version" hash
// input.
//
// v1 = behaviour after T-0565 (graph-derived FTSBody with flat-text fallback
// when the graph carries Tag/EntityMention nodes only).
const ProjectionVersion = "v1"

// SQLiteFTSTokenizer is the tokenizer in use for objects_fts. The DDL does
// not specify `tokenize=`, so FTS5 falls back to its default ("unicode61"
// with English stop-word handling). Recorded canonically so the signature
// changes when a future migration swaps tokenizers explicitly.
const SQLiteFTSTokenizer = "fts5-default"

// PostgresHNSWMaxDimension is pgvector's HNSW index ceiling; columns above
// it cannot be HNSW-indexed (halfvec extends to
// PostgresHalfvecMaxDimension, which the driver does not use yet) and fall
// back to sequential scans.
const (
	PostgresHNSWMaxDimension    = 2000
	PostgresHalfvecMaxDimension = 4000
)

// EmbeddingSignatureID returns the index_signatures row key for an embedding
// model. Mirrors FTSSignatureID for the FTS path.
func EmbeddingSignatureID(modelID string) string {
	return "embeddings_" + modelID
}

// hashInputs joins key=value parts (sorted for determinism) and hashes them.
// Adding new inputs is a deliberate signature bump — that is the point.
func hashInputs(parts []string) string {
	sorted := append([]string(nil), parts...)
	sort.Strings(sorted)
	h := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return hex.EncodeToString(h[:])
}

// HashFTSInputs hashes the SQLite FTS signature inputs. Exported for the
// driver's white-box determinism tests; the formula predates this package
// and MUST stay byte-stable — existing installations carry stored hashes.
func HashFTSInputs(tokenizer, projection, ddl string) string {
	return hashInputs([]string{
		"tokenizer=" + tokenizer,
		"projection=" + projection,
		"ddl=" + ddl,
	})
}

// ComputeFTS returns (hash, human-readable inputs summary) for the FTS index
// on the given dialect.
//
// SQLite inputs: tokenizer name, projection version, objects_fts DDL from
// sqlite_master — unchanged from the pre-package formula.
//
// Postgres inputs: tsvector regconfig (the tokenizer-analog), projection
// version, the generated column's generation expression, and the GIN index
// DDL via pg_get_indexdef. Changing any of them (a regconfig swap, a new
// projection, an index rebuild with different shape) must change the hash.
func ComputeFTS(ctx context.Context, db *sql.DB, d Dialect) (string, string, error) {
	if d == DialectPostgres {
		return computeFTSPostgres(ctx, db)
	}
	return computeFTSSQLite(ctx, db)
}

func computeFTSSQLite(ctx context.Context, db *sql.DB) (string, string, error) {
	var ddl sql.NullString
	row := db.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`,
		FTSSignatureID,
	)
	if err := row.Scan(&ddl); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", fmt.Errorf("objects_fts not present in sqlite_master")
		}
		return "", "", fmt.Errorf("read objects_fts ddl: %w", err)
	}
	ddlText := strings.TrimSpace(ddl.String)
	hash := HashFTSInputs(SQLiteFTSTokenizer, ProjectionVersion, ddlText)
	summary := fmt.Sprintf("tokenizer=%s;projection=%s;ddl-len=%d",
		SQLiteFTSTokenizer, ProjectionVersion, len(ddlText))
	return hash, summary, nil
}

func computeFTSPostgres(ctx context.Context, db *sql.DB) (string, string, error) {
	var genExpr string
	err := db.QueryRowContext(ctx, `
		SELECT pg_get_expr(ad.adbin, ad.adrelid)
		  FROM pg_attrdef ad
		  JOIN pg_attribute a ON a.attrelid = ad.adrelid AND a.attnum = ad.adnum
		 WHERE ad.adrelid = 'objects'::regclass
		   AND a.attname = 'fts' AND NOT a.attisdropped`).Scan(&genExpr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", fmt.Errorf("objects.fts generated column not present")
		}
		return "", "", fmt.Errorf("read fts generation expression: %w", err)
	}

	var indexDDL string
	err = db.QueryRowContext(ctx, `
		SELECT indexdef FROM pg_indexes
		 WHERE tablename = 'objects' AND indexname = 'idx_objects_fts'`).Scan(&indexDDL)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", fmt.Errorf("idx_objects_fts not present")
		}
		return "", "", fmt.Errorf("read fts index ddl: %w", err)
	}

	genExpr = strings.TrimSpace(genExpr)
	indexDDL = strings.TrimSpace(indexDDL)
	hash := hashInputs([]string{
		"regconfig=" + ftsq.PostgresRegconfig,
		"projection=" + ProjectionVersion,
		"genexpr=" + genExpr,
		"indexddl=" + indexDDL,
	})
	summary := fmt.Sprintf("regconfig=%s;projection=%s;genexpr-len=%d;indexddl-len=%d",
		ftsq.PostgresRegconfig, ProjectionVersion, len(genExpr), len(indexDDL))
	return hash, summary, nil
}

// ComputeEmbedding hashes an embedding index signature. Extends ADR-071's
// (model_id, provider, dimension) with the index description — method, ops
// class, and build parameters — so an index rebuilt with a different
// algorithm or tuning no longer verifies against the old stamp.
func ComputeEmbedding(modelID, provider string, dimension int, method, opsClass, buildParams string) (string, string) {
	hash := hashInputs([]string{
		"model_id=" + modelID,
		"provider=" + provider,
		fmt.Sprintf("dimension=%d", dimension),
		"method=" + method,
		"ops=" + opsClass,
		"params=" + buildParams,
	})
	summary := fmt.Sprintf("model_id=%s;provider=%s;dimension=%d;method=%s;ops=%s;params=%s",
		modelID, provider, dimension, method, opsClass, buildParams)
	return hash, summary
}

// VectorIndexDescription names the live ANN index shape for the embedding
// signature inputs.
type VectorIndexDescription struct {
	Method      string // hnsw | ivfflat | vec0 | seqscan | bruteforce
	OpsClass    string // vector_cosine_ops | cosine | l2 | ...
	BuildParams string // WITH (...) content, DDL, or "" when defaults/absent
}

// PostgresVectorIndex describes the ANN index over objects.embedding.
// When no index exists (dimension above the HNSW ceiling), the description
// is the honest fallback: sequential scan with the pinned cosine metric.
func PostgresVectorIndex(ctx context.Context, db *sql.DB) (VectorIndexDescription, error) {
	var indexDDL string
	err := db.QueryRowContext(ctx, `
		SELECT indexdef FROM pg_indexes
		 WHERE tablename = 'objects' AND indexname = 'idx_objects_embedding_hnsw'`).Scan(&indexDDL)
	if errors.Is(err, sql.ErrNoRows) {
		return VectorIndexDescription{Method: "seqscan", OpsClass: "cosine"}, nil
	}
	if err != nil {
		return VectorIndexDescription{}, fmt.Errorf("read embedding index ddl: %w", err)
	}

	desc := VectorIndexDescription{Method: "hnsw", OpsClass: "vector_cosine_ops"}
	if i := strings.Index(indexDDL, "USING "); i >= 0 {
		rest := indexDDL[i+len("USING "):]
		if j := strings.IndexByte(rest, ' '); j > 0 {
			desc.Method = rest[:j]
		}
	}
	if i := strings.Index(indexDDL, "WITH ("); i >= 0 {
		rest := indexDDL[i+len("WITH ("):]
		if j := strings.IndexByte(rest, ')'); j >= 0 {
			desc.BuildParams = rest[:j]
		}
	}
	return desc, nil
}

// SQLiteVectorIndex describes the vec0 virtual table backing ANN search.
// When the table is absent the driver brute-forces over stored embeddings,
// still under the pinned cosine contract.
func SQLiteVectorIndex(ctx context.Context, db *sql.DB) (VectorIndexDescription, error) {
	var ddl sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='vec_objects'`).Scan(&ddl)
	if errors.Is(err, sql.ErrNoRows) {
		return VectorIndexDescription{Method: "bruteforce", OpsClass: "cosine"}, nil
	}
	if err != nil {
		return VectorIndexDescription{}, fmt.Errorf("read vec_objects ddl: %w", err)
	}
	desc := VectorIndexDescription{
		Method:      "vec0",
		OpsClass:    "l2", // vec0's default when distance_metric is unspecified
		BuildParams: strings.TrimSpace(ddl.String),
	}
	if strings.Contains(ddl.String, "distance_metric=cosine") {
		desc.OpsClass = "cosine"
	}
	return desc, nil
}

// Row is the on-disk representation of a row in index_signatures.
type Row struct {
	SignatureID   string
	SignatureHash string
	ComputedAt    time.Time
	InputsSummary string
}

// Load returns the stored signature row for signatureID.
// Returns (nil, nil) when no row exists yet (first-boot case).
func Load(ctx context.Context, db *sql.DB, d Dialect, signatureID string) (*Row, error) {
	q := `SELECT signature_id, signature_hash, computed_at, inputs_summary
	        FROM index_signatures WHERE signature_id = ?`
	if d == DialectPostgres {
		q = strings.Replace(q, "?", "$1", 1)
	}
	var (
		row Row
		ts  any
	)
	err := db.QueryRowContext(ctx, q, signatureID).Scan(
		&row.SignatureID, &row.SignatureHash, &ts, &row.InputsSummary,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load index signature %q: %w", signatureID, err)
	}
	// computed_at is TEXT (RFC3339) on SQLite and TIMESTAMPTZ on Postgres.
	// Unparsable values keep the row with a zero time: callers depend on it
	// for observability, not correctness.
	switch v := ts.(type) {
	case time.Time:
		row.ComputedAt = v.UTC()
	case string:
		if t, perr := time.Parse(time.RFC3339, v); perr == nil {
			row.ComputedAt = t
		}
	case []byte:
		if t, perr := time.Parse(time.RFC3339, string(v)); perr == nil {
			row.ComputedAt = t
		}
	}
	return &row, nil
}

// Upsert writes (or replaces) the stored signature for signatureID.
func Upsert(ctx context.Context, db *sql.DB, d Dialect, signatureID, hash, inputsSummary string) error {
	now := time.Now().UTC()
	var err error
	if d == DialectPostgres {
		_, err = db.ExecContext(ctx, `
			INSERT INTO index_signatures (signature_id, signature_hash, computed_at, inputs_summary)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (signature_id) DO UPDATE SET
			    signature_hash = excluded.signature_hash,
			    computed_at    = excluded.computed_at,
			    inputs_summary = excluded.inputs_summary`,
			signatureID, hash, now, inputsSummary,
		)
	} else {
		_, err = db.ExecContext(ctx, `
			INSERT INTO index_signatures (signature_id, signature_hash, computed_at, inputs_summary)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(signature_id) DO UPDATE SET
			    signature_hash = excluded.signature_hash,
			    computed_at    = excluded.computed_at,
			    inputs_summary = excluded.inputs_summary`,
			signatureID, hash, now.Format(time.RFC3339), inputsSummary,
		)
	}
	if err != nil {
		return fmt.Errorf("upsert index signature %q: %w", signatureID, err)
	}
	return nil
}

// VerifyResult describes the outcome of VerifyFTS.
type VerifyResult struct {
	// Match is true when the stored signature equals the freshly computed
	// one. First-boot installs (no row yet) report Match=false, OldHash="".
	Match bool
	// SignatureID is the row key (always FTSSignatureID for the FTS path).
	SignatureID string
	// OldHash is the previously stored hash, or "" on first boot.
	OldHash string
	// NewHash is the freshly computed hash.
	NewHash string
	// InputsSummary is the human-readable hash-input summary for the fresh
	// computation. Used in startup logs / bus payloads.
	InputsSummary string
	// FirstBoot is true when no prior signature row existed (the mismatch
	// is expected; rebuild workers gate on this to skip the rebuild path).
	FirstBoot bool
}

// VerifyFTS computes the current FTS signature, compares it against the
// stored row, persists the freshly computed signature on mismatch, and
// returns a VerifyResult describing what happened.
//
// Detection-only per ADR-070 §3: callers may log and / or publish a bus
// event on Match=false; the reindex worker acts separately.
func VerifyFTS(ctx context.Context, db *sql.DB, d Dialect) (*VerifyResult, error) {
	newHash, summary, err := ComputeFTS(ctx, db, d)
	if err != nil {
		return nil, fmt.Errorf("compute fts signature: %w", err)
	}
	stored, err := Load(ctx, db, d, FTSSignatureID)
	if err != nil {
		return nil, fmt.Errorf("load fts signature: %w", err)
	}
	res := &VerifyResult{
		SignatureID:   FTSSignatureID,
		NewHash:       newHash,
		InputsSummary: summary,
	}
	if stored == nil {
		res.FirstBoot = true
		res.Match = false
		if err := Upsert(ctx, db, d, FTSSignatureID, newHash, summary); err != nil {
			return nil, err
		}
		return res, nil
	}
	res.OldHash = stored.SignatureHash
	if stored.SignatureHash == newHash {
		res.Match = true
		return res, nil
	}
	if err := Upsert(ctx, db, d, FTSSignatureID, newHash, summary); err != nil {
		return nil, err
	}
	return res, nil
}
