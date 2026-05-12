package sqlite

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
)

// FTSSignatureID is the canonical signature key for the objects_fts virtual table.
const FTSSignatureID = "objects_fts"

// projectionVersion is the build-time-known version of internal/projection
// logic that feeds objects_fts.projected_fts_body. Bump when projection.ProjectIndex
// behaviour diverges in a way that should trigger reindex_auto on existing
// installations. ADR-070 §3 calls this the "projection logic version" hash input.
//
// v1 = behaviour after T-0565 (graph-derived FTSBody with flat-text fallback when
//	    the graph carries Tag/EntityMention nodes only).
const projectionVersion = "v1"

// ftsTokenizerName is the tokenizer in use for objects_fts. The DDL in
// migrations 001/007/023 does not specify `tokenize=`, so FTS5 falls back to
// its default ("unicode61" with English stop-word handling). We record the
// canonical name here so the signature changes when we explicitly swap
// tokenizers in a future migration.
const ftsTokenizerName = "fts5-default"

// ComputeFTSSignature returns the canonical signature hash for the objects_fts
// virtual table. Inputs hashed:
//   - tokenizer name (build-time constant; see ftsTokenizerName).
//   - projection version constant (see projectionVersion).
//   - FTS schema DDL (read from sqlite_master where name='objects_fts').
//
// Returns (hex-sha256, human-readable inputs summary, error).
//
// inputs_summary is plain text for ops debuggability:
//
//	"tokenizer=fts5-default;projection=v1;ddl-len=234"
func ComputeFTSSignature(ctx context.Context, db *sql.DB) (string, string, error) {
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
	return hashFTSInputs(ftsTokenizerName, projectionVersion, ddlText), summariseFTSInputs(ftsTokenizerName, projectionVersion, ddlText), nil
}

// hashFTSInputs is split out so unit tests can assert deterministic hashing
// without standing up a live DB.
func hashFTSInputs(tokenizer, projection, ddl string) string {
	// Inputs are joined with newline-separated key=value lines, sorted for
	// determinism. Adding new inputs in the future is a deliberate signature
	// bump — that is the whole point.
	parts := []string{
		"tokenizer=" + tokenizer,
		"projection=" + projection,
		"ddl=" + ddl,
	}
	sort.Strings(parts)
	h := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(h[:])
}

func summariseFTSInputs(tokenizer, projection, ddl string) string {
	return fmt.Sprintf("tokenizer=%s;projection=%s;ddl-len=%d", tokenizer, projection, len(ddl))
}

// IndexSignatureRow is the on-disk representation of a row in index_signatures.
type IndexSignatureRow struct {
	SignatureID    string
	SignatureHash  string
	ComputedAt     time.Time
	InputsSummary  string
}

// LoadIndexSignature returns the stored signature row for signatureID.
// Returns (nil, nil) when no row exists yet (first-boot case).
func LoadIndexSignature(ctx context.Context, db *sql.DB, signatureID string) (*IndexSignatureRow, error) {
	var (
		row IndexSignatureRow
		ts  string
	)
	err := db.QueryRowContext(ctx, `
		SELECT signature_id, signature_hash, computed_at, inputs_summary
		  FROM index_signatures
		 WHERE signature_id = ?`, signatureID).Scan(
		&row.SignatureID, &row.SignatureHash, &ts, &row.InputsSummary,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load index signature %q: %w", signatureID, err)
	}
	t, perr := time.Parse(time.RFC3339, ts)
	if perr != nil {
		// Not fatal: keep the row but zero the time. Callers don't depend on
		// the exact moment for correctness, only for observability.
		row.ComputedAt = time.Time{}
	} else {
		row.ComputedAt = t
	}
	return &row, nil
}

// VerifyResult describes the outcome of VerifyFTSSignature.
type VerifyResult struct {
	// Match is true when the stored signature equals the freshly computed one.
	// First-boot installs (no row yet) report Match=false and OldHash="".
	Match bool
	// SignatureID is the row key (always FTSSignatureID for the FTS path).
	SignatureID string
	// OldHash is the previously stored hash, or "" on first boot.
	OldHash string
	// NewHash is the freshly computed hash.
	NewHash string
	// InputsSummary is the human-readable hash-input summary for OldHash if
	// present, else for NewHash. Used in startup logs / bus payloads.
	InputsSummary string
	// FirstBoot is true when no prior signature row existed (so the mismatch is
	// expected and no reindex_auto bus event is conceptually warranted — but
	// see ADR-070: T-0579 still publishes for symmetry, T-0581 will gate on
	// FirstBoot to skip the rebuild path).
	FirstBoot bool
}

// VerifyFTSSignature computes the current FTS signature, compares it against
// the stored row, persists the freshly computed signature on mismatch, and
// returns a VerifyResult describing what happened.
//
// This is detection-only per ADR-070 §3 / T-0579. Callers may log and / or
// publish a bus event on Match=false; the actual reindex worker is T-0581.
func VerifyFTSSignature(ctx context.Context, db *sql.DB) (*VerifyResult, error) {
	newHash, summary, err := ComputeFTSSignature(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("compute fts signature: %w", err)
	}
	stored, err := LoadIndexSignature(ctx, db, FTSSignatureID)
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
		if err := UpsertIndexSignature(ctx, db, FTSSignatureID, newHash, summary); err != nil {
			return nil, err
		}
		return res, nil
	}
	res.OldHash = stored.SignatureHash
	if stored.SignatureHash == newHash {
		res.Match = true
		return res, nil
	}
	if err := UpsertIndexSignature(ctx, db, FTSSignatureID, newHash, summary); err != nil {
		return nil, err
	}
	return res, nil
}

// UpsertIndexSignature writes (or replaces) the stored signature for signatureID.
func UpsertIndexSignature(ctx context.Context, db *sql.DB, signatureID, hash, inputsSummary string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO index_signatures (signature_id, signature_hash, computed_at, inputs_summary)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(signature_id) DO UPDATE SET
		    signature_hash = excluded.signature_hash,
		    computed_at    = excluded.computed_at,
		    inputs_summary = excluded.inputs_summary`,
		signatureID, hash, time.Now().UTC().Format(time.RFC3339), inputsSummary,
	)
	if err != nil {
		return fmt.Errorf("upsert index signature %q: %w", signatureID, err)
	}
	return nil
}
