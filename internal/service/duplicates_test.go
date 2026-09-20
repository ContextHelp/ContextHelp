package service

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubObjectsForDup is a minimal ObjectStore stub for duplicate tests.
type stubObjectsForDup struct {
	storage.ObjectStore
	byHash      map[string]*storage.KnowledgeObject
	bySourceKey map[string]*storage.KnowledgeObject
	similar     []*storage.KnowledgeObject // returned by VectorSearch
}

func (s *stubObjectsForDup) GetByContentHash(_ context.Context, hash string) (*storage.KnowledgeObject, error) {
	if hash == "" {
		return nil, nil
	}
	return s.byHash[hash], nil
}

func (s *stubObjectsForDup) GetBySourceKey(_ context.Context, key string) (*storage.KnowledgeObject, error) {
	if key == "" || s.bySourceKey == nil {
		return nil, nil
	}
	return s.bySourceKey[key], nil
}

func (s *stubObjectsForDup) VectorSearch(_ context.Context, _ []float32, _ storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	out := make([]*storage.KnowledgeObject, 0, len(s.similar))
	for _, obj := range s.similar {
		if score, ok := obj.Metadata["score"].(float64); ok && score >= 0 {
			out = append(out, obj)
		}
	}
	return out, nil
}

// stubDriverForDup implements StorageDriver minimally, delegating Objects() to the stub.
type stubDriverForDup struct {
	storage.StorageDriver
	objs *stubObjectsForDup
}

func (d *stubDriverForDup) Objects() storage.ObjectStore { return d.objs }

func TestCheckDuplicates(t *testing.T) {
	existingObj := &storage.KnowledgeObject{ID: "existing-1", ContentHash: "sha256:abc"}
	sourceKeyObj := &storage.KnowledgeObject{ID: "sk-1", SourceKey: "slack:C01/1234.5678"}
	similarObj := &storage.KnowledgeObject{
		ID:       "similar-1",
		Metadata: map[string]any{"score": float64(0.97)},
	}

	tests := []struct {
		name        string
		hash        string
		sourceKey   string
		embeddings  []float32
		cfg         config.DuplicatesConfig
		byHash      map[string]*storage.KnowledgeObject
		bySourceKey map[string]*storage.KnowledgeObject
		similar     []*storage.KnowledgeObject
		wantKind    DuplicateKind
		wantNil     bool
	}{
		{
			name:     "exact match found",
			hash:     "sha256:abc",
			cfg:      config.DuplicatesConfig{CheckExact: true, SimilarityThreshold: 0.95},
			byHash:   map[string]*storage.KnowledgeObject{"sha256:abc": existingObj},
			wantKind: DuplicateExact,
		},
		{
			name:        "source key match found",
			sourceKey:   "slack:C01/1234.5678",
			cfg:         config.DuplicatesConfig{SimilarityThreshold: 0.95},
			bySourceKey: map[string]*storage.KnowledgeObject{"slack:C01/1234.5678": sourceKeyObj},
			wantKind:    DuplicateSourceKey,
		},
		{
			name:       "similar match found",
			embeddings: []float32{1, 0, 0},
			cfg:        config.DuplicatesConfig{CheckSimilar: true, SimilarityThreshold: 0.95},
			similar:    []*storage.KnowledgeObject{similarObj},
			wantKind:   DuplicateSimilar,
		},
		{
			name:    "no match",
			hash:    "sha256:xyz",
			cfg:     config.DuplicatesConfig{CheckExact: true, SimilarityThreshold: 0.95},
			byHash:  map[string]*storage.KnowledgeObject{},
			wantNil: true,
		},
		{
			name:    "empty store returns no duplicate",
			hash:    "sha256:fresh",
			cfg:     config.DuplicatesConfig{CheckExact: true, SimilarityThreshold: 0.95},
			byHash:  map[string]*storage.KnowledgeObject{},
			wantNil: true,
		},
		{
			name:    "exact check disabled",
			hash:    "sha256:abc",
			cfg:     config.DuplicatesConfig{CheckExact: false},
			byHash:  map[string]*storage.KnowledgeObject{"sha256:abc": existingObj},
			wantNil: true,
		},
		{
			name:       "similar check disabled",
			embeddings: []float32{1, 0, 0},
			cfg:        config.DuplicatesConfig{CheckSimilar: false, SimilarityThreshold: 0.95},
			similar:    []*storage.KnowledgeObject{similarObj},
			wantNil:    true,
		},
		{
			name:        "source key empty string returns no match",
			sourceKey:   "",
			cfg:         config.DuplicatesConfig{SimilarityThreshold: 0.95},
			bySourceKey: map[string]*storage.KnowledgeObject{"slack:C01/1234.5678": sourceKeyObj},
			wantNil:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{}
			stub := &stubDriverForDup{
				objs: &stubObjectsForDup{
					byHash:      tt.byHash,
					bySourceKey: tt.bySourceKey,
					similar:     tt.similar,
				},
			}
			svc.Store = stub

			result, err := svc.checkDuplicates(context.Background(), tt.hash, tt.sourceKey, tt.embeddings, tt.cfg)
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.wantKind, result.Kind)
			}
		})
	}
}
