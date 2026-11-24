package postgres

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/keys"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ErrTDERequired is returned when storage.postgres.require_tde is enabled but
// the Postgres server lacks TLS or TDE attestation (pg_tde or volume attestation).
var ErrTDERequired = errors.New("postgres: TDE/TLS posture requirement not satisfied")

const (
	encTextPrefix = "ctxt-enc:v1:"
	encJSONMarker = "__ctxt_enc"
)

// EncryptionConfig configures Postgres encryption-at-rest posture.
type EncryptionConfig struct {
	RequireTDE       bool
	TDEAttested      bool // operator attestation for managed encrypted volumes (RDS/Neon/LUKS)
	ColumnEncryption string
	FTSOnEncrypted   string
	KeyProvider      keys.Provider
}

// Option configures a Postgres Driver at New.
type Option func(*Driver)

// WithEncryption attaches encryption-at-rest configuration to the Postgres driver.
func WithEncryption(cfg EncryptionConfig) Option {
	return func(d *Driver) { d.enc = cfg }
}

func (c EncryptionConfig) columnMode() string {
	if c.ColumnEncryption == "" {
		return config.ColumnEncryptionOff
	}
	return c.ColumnEncryption
}

func (c EncryptionConfig) ftsMode() string {
	if c.FTSOnEncrypted == "" {
		return config.FTSOnEncryptedOmit
	}
	return c.FTSOnEncrypted
}

// Validate checks internal consistency of EncryptionConfig.
func (c EncryptionConfig) Validate() error {
	mode := c.columnMode()
	switch mode {
	case config.ColumnEncryptionOff, config.ColumnEncryptionSensitive, config.ColumnEncryptionAll:
	default:
		return fmt.Errorf("postgres: unknown column_encryption mode %q", c.ColumnEncryption)
	}
	if mode != config.ColumnEncryptionOff && c.KeyProvider == nil {
		return fmt.Errorf("postgres: column_encryption=%s requires a key provider: %w", mode, keys.ErrUnavailable)
	}
	fts := c.ftsMode()
	switch fts {
	case config.FTSOnEncryptedOmit, config.FTSOnEncryptedRequireTDE:
	default:
		return fmt.Errorf("postgres: unknown fts_on_encrypted mode %q", c.FTSOnEncrypted)
	}
	if fts == config.FTSOnEncryptedRequireTDE && !c.RequireTDE {
		return fmt.Errorf("postgres: fts_on_encrypted=require_tde requires require_tde=true: %w", ErrTDERequired)
	}
	return nil
}

// VerifyTDE checks that the Postgres server satisfies require_tde:
// TLS enabled (SHOW ssl = on) AND either pg_tde extension installed or
// explicit operator volume attestation (TDEAttested or CTXT_PG_TDE_ATTESTED=true).
func (c EncryptionConfig) VerifyTDE(ctx context.Context, db *sql.DB) error {
	if !c.RequireTDE {
		return nil
	}
	var sslStatus string
	if err := db.QueryRowContext(ctx, `SHOW ssl`).Scan(&sslStatus); err != nil {
		return fmt.Errorf("%w: query ssl status: %w", ErrTDERequired, err)
	}
	if !strings.EqualFold(strings.TrimSpace(sslStatus), "on") {
		return fmt.Errorf("%w: server ssl is %q (must be on)", ErrTDERequired, sslStatus)
	}
	if c.TDEAttested || strings.EqualFold(os.Getenv("CTXT_PG_TDE_ATTESTED"), "true") {
		return nil
	}
	var extCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM pg_extension WHERE extname = 'pg_tde'`).Scan(&extCount); err != nil {
		return fmt.Errorf("%w: check pg_tde extension: %w", ErrTDERequired, err)
	}
	if extCount == 0 {
		return fmt.Errorf("%w: pg_tde extension not installed and volume TDE not attested", ErrTDERequired)
	}
	return nil
}

// ShouldEncryptObject reports whether obj's content columns must be encrypted
// under c.ColumnEncryption.
func (c EncryptionConfig) ShouldEncryptObject(obj *storage.KnowledgeObject) bool {
	switch c.columnMode() {
	case config.ColumnEncryptionAll:
		return true
	case config.ColumnEncryptionSensitive:
		return isSensitiveObject(obj)
	default:
		return false
	}
}

func isSensitiveObject(obj *storage.KnowledgeObject) bool {
	if obj == nil {
		return false
	}
	for _, cls := range obj.Classes {
		switch strings.ToLower(strings.TrimSpace(cls)) {
		case "personal", "restricted", "confidential", "private", "pii", "secret", "sensitive":
			return true
		}
	}
	if obj.Metadata != nil {
		if sens, ok := obj.Metadata["bos_sensitivity"].(string); ok {
			s := strings.ToLower(strings.TrimSpace(sens))
			if s != "" && s != "public" {
				return true
			}
		}
	}
	return false
}

func pgAAD(table, rowID, col string) []byte {
	return []byte("pg:" + table + ":" + rowID + ":" + col)
}

func sealTextColumn(ctx context.Context, p keys.Provider, table, rowID, col, plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	if p == nil {
		return "", fmt.Errorf("encrypt %s.%s: %w", table, col, keys.ErrUnavailable)
	}
	env, err := keys.SealEnvelope(ctx, p, []byte(plain), pgAAD(table, rowID, col))
	if err != nil {
		return "", fmt.Errorf("encrypt %s.%s: %w", table, col, err)
	}
	return encTextPrefix + base64.StdEncoding.EncodeToString(env), nil
}

func openTextColumn(ctx context.Context, p keys.Provider, table, rowID, col, stored string) (string, error) {
	if !strings.HasPrefix(stored, encTextPrefix) {
		return stored, nil
	}
	if p == nil {
		return "", fmt.Errorf("decrypt %s.%s: %w", table, col, keys.ErrUnavailable)
	}
	env, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, encTextPrefix))
	if err != nil {
		return "", fmt.Errorf("decrypt %s.%s: %w", table, col, keys.ErrUnwrap)
	}
	plain, err := keys.OpenEnvelope(ctx, p, env, pgAAD(table, rowID, col))
	if err != nil {
		return "", fmt.Errorf("decrypt %s.%s: %w", table, col, err)
	}
	return string(plain), nil
}

type encJSONBox struct {
	Version string `json:"__ctxt_enc"`
	CT      string `json:"ct"`
}

func sealJSONColumn(ctx context.Context, p keys.Provider, table, rowID, col string, plainJSON []byte) ([]byte, error) {
	if len(plainJSON) == 0 || string(plainJSON) == "null" || string(plainJSON) == "{}" || string(plainJSON) == "[]" {
		return plainJSON, nil
	}
	if p == nil {
		return nil, fmt.Errorf("encrypt %s.%s: %w", table, col, keys.ErrUnavailable)
	}
	env, err := keys.SealEnvelope(ctx, p, plainJSON, pgAAD(table, rowID, col))
	if err != nil {
		return nil, fmt.Errorf("encrypt %s.%s: %w", table, col, err)
	}
	return json.Marshal(encJSONBox{Version: "v1", CT: base64.StdEncoding.EncodeToString(env)})
}

func parseEncJSONBox(stored []byte) (encJSONBox, bool) {
	if len(stored) == 0 || !strings.Contains(string(stored), encJSONMarker) {
		return encJSONBox{}, false
	}
	var box encJSONBox
	if json.Unmarshal(stored, &box) != nil || box.Version != "v1" {
		return encJSONBox{}, false
	}
	return box, true
}

func openObjectJSONColumn(ctx context.Context, p keys.Provider, rowID, col string, stored []byte) ([]byte, error) {
	box, ok := parseEncJSONBox(stored)
	if !ok {
		return stored, nil
	}
	if p == nil {
		return nil, fmt.Errorf("decrypt objects.%s: %w", col, keys.ErrUnavailable)
	}
	env, err := base64.StdEncoding.DecodeString(box.CT)
	if err != nil {
		return nil, fmt.Errorf("decrypt objects.%s: %w", col, keys.ErrUnwrap)
	}
	plain, err := keys.OpenEnvelope(ctx, p, env, pgAAD("objects", rowID, col))
	if err != nil {
		return nil, fmt.Errorf("decrypt objects.%s: %w", col, err)
	}
	return plain, nil
}

// encryptedObjectWrite holds the wire values for an object INSERT/UPDATE after
// applying column encryption and FTS policy.
type encryptedObjectWrite struct {
	rawContent  string
	textContent string
	metadata    string
	summaries   string
	graphJSON   []byte
	ftsBody     string
	maskedFTS   string
	ftsIndexed  bool
}

func (c EncryptionConfig) prepareObjectWrite(
	ctx context.Context,
	obj *storage.KnowledgeObject,
	f *objectFields,
	graphJSON []byte,
	ftsBody, maskedFTS string,
) (encryptedObjectWrite, error) {
	out := encryptedObjectWrite{
		rawContent:  obj.RawContent,
		textContent: obj.TextContent,
		metadata:    f.metadata,
		summaries:   f.summaries,
		graphJSON:   graphJSON,
		ftsBody:     ftsBody,
		maskedFTS:   maskedFTS,
		ftsIndexed:  ftsBody != "",
	}
	if !c.ShouldEncryptObject(obj) {
		return out, nil
	}
	var err error
	if out.rawContent, err = sealTextColumn(ctx, c.KeyProvider, "objects", obj.ID, "raw_content", obj.RawContent); err != nil {
		return out, err
	}
	if out.textContent, err = sealTextColumn(ctx, c.KeyProvider, "objects", obj.ID, "text_content", obj.TextContent); err != nil {
		return out, err
	}
	metaBytes, err := sealJSONColumn(ctx, c.KeyProvider, "objects", obj.ID, "metadata", []byte(f.metadata))
	if err != nil {
		return out, err
	}
	out.metadata = string(metaBytes)
	sumBytes, err := sealJSONColumn(ctx, c.KeyProvider, "objects", obj.ID, "summaries", []byte(f.summaries))
	if err != nil {
		return out, err
	}
	out.summaries = string(sumBytes)
	out.graphJSON, err = sealJSONColumn(ctx, c.KeyProvider, "objects", obj.ID, "graph_json", graphJSON)
	if err != nil {
		return out, err
	}
	// FTS policy on encrypted rows: omit plaintext FTS unless require_tde is explicitly enabled.
	if c.ftsMode() != config.FTSOnEncryptedRequireTDE {
		out.ftsBody = ""
		out.maskedFTS = ""
		out.ftsIndexed = false
	}
	return out, nil
}

// decryptObjectRaw decrypts any encrypted text/JSON columns on a scanned object
// row before JSON unmarshaling.
func (c EncryptionConfig) decryptRawColumns(
	ctx context.Context,
	id string,
	rawContent, textContent *string,
	metadataJSON, summariesJSON, graphJSON *[]byte,
) error {
	var err error
	if *rawContent, err = openTextColumn(ctx, c.KeyProvider, "objects", id, "raw_content", *rawContent); err != nil {
		return err
	}
	if *textContent, err = openTextColumn(ctx, c.KeyProvider, "objects", id, "text_content", *textContent); err != nil {
		return err
	}
	if *metadataJSON, err = openObjectJSONColumn(ctx, c.KeyProvider, id, "metadata", *metadataJSON); err != nil {
		return err
	}
	if *summariesJSON, err = openObjectJSONColumn(ctx, c.KeyProvider, id, "summaries", *summariesJSON); err != nil {
		return err
	}
	if *graphJSON, err = openObjectJSONColumn(ctx, c.KeyProvider, id, "graph_json", *graphJSON); err != nil {
		return err
	}
	return nil
}

// rewrapTextColumn rewraps an encrypted text column to toKeyID without decrypting.
func rewrapTextColumn(ctx context.Context, p keys.Provider, table, rowID, col, stored, toKeyID string) (string, bool, error) {
	if !strings.HasPrefix(stored, encTextPrefix) {
		return stored, false, nil
	}
	env, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, encTextPrefix))
	if err != nil {
		return "", false, fmt.Errorf("rewrap %s.%s: %w", table, col, keys.ErrUnwrap)
	}
	next, _, err := keys.RewrapEnvelope(ctx, p, env, pgAAD(table, rowID, col), toKeyID)
	if err != nil {
		return "", false, fmt.Errorf("rewrap %s.%s: %w", table, col, err)
	}
	return encTextPrefix + base64.StdEncoding.EncodeToString(next), true, nil
}

func rewrapObjectJSONColumn(ctx context.Context, p keys.Provider, rowID, col string, stored []byte, toKeyID string) ([]byte, bool, error) {
	box, ok := parseEncJSONBox(stored)
	if !ok {
		return stored, false, nil
	}
	env, err := base64.StdEncoding.DecodeString(box.CT)
	if err != nil {
		return nil, false, fmt.Errorf("rewrap objects.%s: %w", col, keys.ErrUnwrap)
	}
	next, _, err := keys.RewrapEnvelope(ctx, p, env, pgAAD("objects", rowID, col), toKeyID)
	if err != nil {
		return nil, false, fmt.Errorf("rewrap objects.%s: %w", col, err)
	}
	b, err := json.Marshal(encJSONBox{Version: "v1", CT: base64.StdEncoding.EncodeToString(next)})
	return b, true, err
}

// RewrapEncryptedObjects rotates KEKs across all column-encrypted object rows
// without decrypting row payloads. Returns the number of rewrapped rows.
func (d *Driver) RewrapEncryptedObjects(ctx context.Context, toKeyID string) (int, error) {
	if d.enc.KeyProvider == nil {
		return 0, fmt.Errorf("rewrap postgres objects: %w", keys.ErrUnavailable)
	}
	type item struct {
		id, raw, text          string
		meta, summaries, graph []byte
	}
	loadItems := func() ([]item, error) {
		rows, err := d.db.QueryContext(ctx, `SELECT id, raw_content, text_content, metadata, summaries, graph_json FROM objects`)
		if err != nil {
			return nil, fmt.Errorf("rewrap postgres objects: query: %w", err)
		}
		defer rows.Close()
		var out []item
		for rows.Next() {
			var it item
			if err := rows.Scan(&it.id, &it.raw, &it.text, &it.meta, &it.summaries, &it.graph); err != nil {
				return nil, err
			}
			out = append(out, it)
		}
		return out, rows.Err()
	}
	items, err := loadItems()
	if err != nil {
		return 0, err
	}

	rewrapped := 0
	for _, it := range items {
		var c1, c2, c3, c4, c5 bool
		var err error
		if it.raw, c1, err = rewrapTextColumn(ctx, d.enc.KeyProvider, "objects", it.id, "raw_content", it.raw, toKeyID); err != nil {
			return rewrapped, err
		}
		if it.text, c2, err = rewrapTextColumn(ctx, d.enc.KeyProvider, "objects", it.id, "text_content", it.text, toKeyID); err != nil {
			return rewrapped, err
		}
		if it.meta, c3, err = rewrapObjectJSONColumn(ctx, d.enc.KeyProvider, it.id, "metadata", it.meta, toKeyID); err != nil {
			return rewrapped, err
		}
		if it.summaries, c4, err = rewrapObjectJSONColumn(ctx, d.enc.KeyProvider, it.id, "summaries", it.summaries, toKeyID); err != nil {
			return rewrapped, err
		}
		if it.graph, c5, err = rewrapObjectJSONColumn(ctx, d.enc.KeyProvider, it.id, "graph_json", it.graph, toKeyID); err != nil {
			return rewrapped, err
		}
		if !c1 && !c2 && !c3 && !c4 && !c5 {
			continue
		}
		if _, err := d.db.ExecContext(ctx,
			`UPDATE objects SET raw_content=$1, text_content=$2, metadata=$3, summaries=$4, graph_json=$5 WHERE id=$6`,
			it.raw, it.text, it.meta, it.summaries, it.graph, it.id,
		); err != nil {
			return rewrapped, fmt.Errorf("rewrap postgres object %s: %w", it.id, err)
		}
		rewrapped++
	}
	return rewrapped, nil
}
