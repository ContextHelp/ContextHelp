package sqlite

import (
	"context"
	"strings"
	"testing"
)

func TestComputeFTSSignature_Deterministic(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	hash1, summary1, err := ComputeFTSSignature(ctx, d.db)
	if err != nil {
		t.Fatalf("first compute: %v", err)
	}
	if hash1 == "" {
		t.Fatalf("hash must be non-empty")
	}
	if !strings.Contains(summary1, "tokenizer=fts5-default") {
		t.Errorf("summary missing tokenizer: %q", summary1)
	}
	if !strings.Contains(summary1, "projection=v1") {
		t.Errorf("summary missing projection: %q", summary1)
	}

	hash2, summary2, err := ComputeFTSSignature(ctx, d.db)
	if err != nil {
		t.Fatalf("second compute: %v", err)
	}
	if hash1 != hash2 {
		t.Errorf("hash not deterministic: %q vs %q", hash1, hash2)
	}
	if summary1 != summary2 {
		t.Errorf("summary not deterministic: %q vs %q", summary1, summary2)
	}
}

func TestHashFTSInputs_ChangesWhenInputsChange(t *testing.T) {
	base := hashFTSInputs("fts5-default", "v1", "CREATE VIRTUAL TABLE objects_fts ...")

	// Tokenizer change.
	if h := hashFTSInputs("icu", "v1", "CREATE VIRTUAL TABLE objects_fts ..."); h == base {
		t.Errorf("tokenizer change must change hash")
	}
	// Projection-version change.
	if h := hashFTSInputs("fts5-default", "v2", "CREATE VIRTUAL TABLE objects_fts ..."); h == base {
		t.Errorf("projection change must change hash")
	}
	// DDL change.
	if h := hashFTSInputs("fts5-default", "v1", "CREATE VIRTUAL TABLE objects_fts (id, body)"); h == base {
		t.Errorf("ddl change must change hash")
	}
}

func TestVerifyFTSSignature_FirstBootWritesRow(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	res, err := VerifyFTSSignature(ctx, d.db)
	if err != nil {
		t.Fatalf("first boot verify: %v", err)
	}
	if !res.FirstBoot {
		t.Errorf("expected FirstBoot=true on empty index_signatures")
	}
	if res.Match {
		t.Errorf("expected Match=false on first boot")
	}
	if res.OldHash != "" {
		t.Errorf("expected empty OldHash on first boot, got %q", res.OldHash)
	}
	if res.NewHash == "" {
		t.Errorf("expected non-empty NewHash")
	}

	stored, err := LoadIndexSignature(ctx, d.db, FTSSignatureID)
	if err != nil {
		t.Fatalf("load after first boot: %v", err)
	}
	if stored == nil {
		t.Fatalf("expected stored signature row after first boot")
	}
	if stored.SignatureHash != res.NewHash {
		t.Errorf("stored hash = %q, want %q", stored.SignatureHash, res.NewHash)
	}
}

func TestVerifyFTSSignature_SecondBootMatches(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	// First boot.
	if _, err := VerifyFTSSignature(ctx, d.db); err != nil {
		t.Fatalf("first verify: %v", err)
	}
	// Second boot — same code, no DDL change.
	res, err := VerifyFTSSignature(ctx, d.db)
	if err != nil {
		t.Fatalf("second verify: %v", err)
	}
	if !res.Match {
		t.Errorf("expected Match=true on second boot")
	}
	if res.FirstBoot {
		t.Errorf("expected FirstBoot=false on second boot")
	}
	if res.OldHash != res.NewHash {
		t.Errorf("Match=true but OldHash != NewHash: %q vs %q", res.OldHash, res.NewHash)
	}
}

func TestVerifyFTSSignature_MutationTriggersMismatch(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	// First boot stamps the signature.
	if _, err := VerifyFTSSignature(ctx, d.db); err != nil {
		t.Fatalf("first verify: %v", err)
	}
	stored1, err := LoadIndexSignature(ctx, d.db, FTSSignatureID)
	if err != nil || stored1 == nil {
		t.Fatalf("missing stored row after first verify: %v", err)
	}

	// Simulate a projection-version bump by directly perturbing the stored
	// hash (a real bump would be a code change; this is the test-side proxy
	// for the same drift signal the daemon would see).
	if err := UpsertIndexSignature(ctx, d.db, FTSSignatureID, "deadbeef", "tokenizer=fts5-default;projection=v0;ddl-len=0"); err != nil {
		t.Fatalf("perturb stored: %v", err)
	}

	res, err := VerifyFTSSignature(ctx, d.db)
	if err != nil {
		t.Fatalf("third verify: %v", err)
	}
	if res.Match {
		t.Errorf("expected Match=false after perturbing stored hash")
	}
	if res.FirstBoot {
		t.Errorf("expected FirstBoot=false (row exists, just stale)")
	}
	if res.OldHash != "deadbeef" {
		t.Errorf("OldHash = %q, want deadbeef", res.OldHash)
	}
	if res.NewHash == res.OldHash {
		t.Errorf("NewHash should be the freshly computed value, not the perturbed one")
	}

	// Third call now matches — the new hash was persisted.
	res2, err := VerifyFTSSignature(ctx, d.db)
	if err != nil {
		t.Fatalf("fourth verify: %v", err)
	}
	if !res2.Match {
		t.Errorf("expected Match=true after mismatch upsert healed the row")
	}
}

func TestIndexSignaturesTableSurvivesMigrations(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	var n int
	if err := d.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='index_signatures'`,
	).Scan(&n); err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if n != 1 {
		t.Errorf("index_signatures table not present after Migrate: count=%d", n)
	}
}
