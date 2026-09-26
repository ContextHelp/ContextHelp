package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lib/pq"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

var _ storage.EmbeddingReader = (*EmbeddingStore)(nil)

// EmbeddingsByID implements storage.EmbeddingReader: a single
// `model_id = $1 AND object_id = ANY($2)` query over the canonical
// embeddings rows modelID's partial index covers. Ids without a row under
// modelID are absent from the result.
func (s *EmbeddingStore) EmbeddingsByID(ctx context.Context, modelID string, ids []string) (map[string][]storage.ObjectVector, error) {
	out := make(map[string][]storage.ObjectVector, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT object_id, chunk_idx, vector::text, text FROM embeddings
		 WHERE model_id = $1 AND object_id = ANY($2)
		 ORDER BY object_id, chunk_idx`, modelID, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("postgres embeddings by id %s: %w", modelID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id   string
			v    storage.ObjectVector
			lit  string
			text sql.NullString
		)
		if err := rows.Scan(&id, &v.ChunkIdx, &lit, &text); err != nil {
			return nil, fmt.Errorf("postgres embeddings by id %s: %w", modelID, err)
		}
		if v.Vector, err = parsePgVectorLiteral(lit); err != nil {
			return nil, fmt.Errorf("postgres embeddings by id %s/%s chunk %d: %w", id, modelID, v.ChunkIdx, err)
		}
		v.ModelID = modelID
		v.Text = text.String
		out[id] = append(out[id], v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres embeddings by id %s: %w", modelID, err)
	}
	return out, nil
}
