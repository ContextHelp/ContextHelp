package service

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeSearchObject(id string, summaries []string, rawContent string) *storage.KnowledgeObject {
	now := time.Now().Truncate(time.Second)
	return &storage.KnowledgeObject{
		ID:         id,
		Type:       "text",
		Summaries:  summaries,
		RawContent: rawContent,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func rebuildFTS(t *testing.T, svc *Service) {
	t.Helper()
	d, ok := svc.Store.(*sqlite.Driver)
	require.True(t, ok, "store must be *sqlite.Driver for FTS rebuild")
	_, err := d.DB().ExecContext(context.Background(), "INSERT INTO objects_fts(objects_fts) VALUES('rebuild')")
	require.NoError(t, err)
}

func TestHybridSearch_FTSOnly_WhenNoEmbeddingProvider(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	obj := makeSearchObject("hs-1", []string{"distributed systems fault tolerance"}, "")
	require.NoError(t, svc.Store.Objects().Create(ctx, obj))
	rebuildFTS(t, svc)

	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
		FallbackToFTS: true,
	}

	results, err := svc.HybridSearch(ctx, "distributed", 10, nil, cfg)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "hs-1", results[0].ID)
}

func TestHybridSearch_ErrorWhenNoProvider_FallbackDisabled(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		FallbackToFTS: false,
	}
	_, err := svc.HybridSearch(ctx, "anything", 10, nil, cfg)
	require.Error(t, err)
}
