package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// embeddingsByIDChunk bounds the IN-list size per query, well under
// SQLite's host-parameter limit.
const embeddingsByIDChunk = 500

var _ storage.EmbeddingReader = (*EmbeddingStore)(nil)

// EmbeddingsByID implements storage.EmbeddingReader: one query per chunk of
// ids against the canonical embeddings rows of modelID (the rows its vec0
// index is built from). Ids without a row under modelID are absent from
// the result.
func (s *EmbeddingStore) EmbeddingsByID(ctx context.Context, modelID string, ids []string) (map[string][]storage.ObjectVector, error) {
	out := make(map[string][]storage.ObjectVector, len(ids))
	// Dedupe up front: a repeated id in two chunks would append its rows
	// twice.
	ids = slices.Compact(slices.Sorted(slices.Values(ids)))
	for start := 0; start < len(ids); start += embeddingsByIDChunk {
		chunk := ids[start:min(start+embeddingsByIDChunk, len(ids))]
		if err := s.embeddingsChunk(ctx, modelID, chunk, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *EmbeddingStore) embeddingsChunk(ctx context.Context, modelID string, ids []string, out map[string][]storage.ObjectVector) error {
	args := make([]any, 0, len(ids)+1)
	args = append(args, modelID)
	for _, id := range ids {
		args = append(args, id)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	// #nosec G202 -- placeholders are literal "?" markers; model and ids are bound.
	rows, err := s.db.QueryContext(ctx, `
		SELECT object_id, chunk_idx, vector, text FROM embeddings
		 WHERE model_id = ? AND object_id IN (`+placeholders+`)
		 ORDER BY object_id, chunk_idx`, args...)
	if err != nil {
		return fmt.Errorf("sqlite embeddings by id %s: %w", modelID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id   string
			v    storage.ObjectVector
			blob []byte
			text sql.NullString
		)
		if err := rows.Scan(&id, &v.ChunkIdx, &blob, &text); err != nil {
			return fmt.Errorf("sqlite embeddings by id %s: %w", modelID, err)
		}
		if len(blob)%4 != 0 {
			return fmt.Errorf("sqlite embeddings by id %s/%s chunk %d: vector blob of %d bytes is not float32",
				id, modelID, v.ChunkIdx, len(blob))
		}
		v.ModelID = modelID
		v.Vector = decodeFloat32(blob)
		v.Text = text.String
		out[id] = append(out[id], v)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("sqlite embeddings by id %s: %w", modelID, err)
	}
	return nil
}
