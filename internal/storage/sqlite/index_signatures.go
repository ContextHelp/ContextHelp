package sqlite

import (
	"context"
	"database/sql"

	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
)

// Index-signature computation lives in the driver-neutral
// internal/storage/indexsig package (ADR-070 provenance works on both
// backends). This file keeps the sqlite-named surface delegating there so
// existing callers and tests stay stable. The hash formula is byte-stable
// across the move — indexsig pins it with a golden test.

// FTSSignatureID is the canonical signature key for the objects_fts virtual table.
const FTSSignatureID = indexsig.FTSSignatureID

// IndexSignatureRow is the on-disk representation of a row in index_signatures.
type IndexSignatureRow = indexsig.Row

// VerifyResult describes the outcome of VerifyFTSSignature.
type VerifyResult = indexsig.VerifyResult

// ComputeFTSSignature returns the canonical signature hash for the
// objects_fts virtual table. Inputs hashed: tokenizer name, projection
// version, and the FTS schema DDL from sqlite_master.
func ComputeFTSSignature(ctx context.Context, db *sql.DB) (string, string, error) {
	return indexsig.ComputeFTS(ctx, db, indexsig.DialectSQLite)
}

// hashFTSInputs is kept for white-box determinism tests; the formula is the
// shared one in indexsig.
func hashFTSInputs(tokenizer, projection, ddl string) string {
	return indexsig.HashFTSInputs(tokenizer, projection, ddl)
}

// LoadIndexSignature returns the stored signature row for signatureID.
// Returns (nil, nil) when no row exists yet (first-boot case).
func LoadIndexSignature(ctx context.Context, db *sql.DB, signatureID string) (*IndexSignatureRow, error) {
	return indexsig.Load(ctx, db, indexsig.DialectSQLite, signatureID)
}

// VerifyFTSSignature computes the current FTS signature, compares it against
// the stored row, persists the freshly computed signature on mismatch, and
// returns a VerifyResult describing what happened. Detection-only per
// ADR-070 §3.
func VerifyFTSSignature(ctx context.Context, db *sql.DB) (*VerifyResult, error) {
	return indexsig.VerifyFTS(ctx, db, indexsig.DialectSQLite)
}

// UpsertIndexSignature writes (or replaces) the stored signature for signatureID.
func UpsertIndexSignature(ctx context.Context, db *sql.DB, signatureID, hash, inputsSummary string) error {
	return indexsig.Upsert(ctx, db, indexsig.DialectSQLite, signatureID, hash, inputsSummary)
}
