package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// postAnalyzeWithOpts POSTs to /api/v1/analyze with arbitrary fields.
func postAnalyzeWithOpts(t *testing.T, url string, body map[string]any) (int, map[string]string) {
	t.Helper()
	data, _ := json.Marshal(body)
	resp, err := gohttp.Post(url+"/api/v1/analyze", "application/json", bytes.NewReader(data))
	require.NoError(t, err)
	defer resp.Body.Close()

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

// TestUS0404_ExactDuplicateBlocked verifies that ingesting the same text
// twice with drop policy returns the existing object ID on the second call.
func TestUS0404_ExactDuplicateBlocked(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Cfg.Duplicates = config.DuplicatesConfig{
		Policy:              "drop",
		CheckExact:          true,
		SimilarityThreshold: 0.95,
	}

	const content = "US-0404 exact dedup test content"
	hash := storageutil.ContentHash(content, "")

	// Pre-seed an object with the expected content hash.
	obj := &storage.KnowledgeObject{
		ID:          "us0404-existing-1",
		Type:        "text",
		ContentHash: hash,
		RawContent:  content,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	require.NoError(t, env.svc.Store.Objects().Create(t.Context(), obj))

	// POST same content — should be silently dropped and return existing ID.
	status, result := postAnalyzeWithOpts(t, env.URL, map[string]any{
		"content": content,
		"type":    "text",
	})
	assert.Equal(t, gohttp.StatusAccepted, status)
	assert.Equal(t, "us0404-existing-1", result["job_id"])
}

// TestUS0404_SourceKeyBlocked verifies that ingesting content with a
// source_key that already exists returns the existing object ID.
func TestUS0404_SourceKeyBlocked(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Cfg.Duplicates = config.DuplicatesConfig{
		Policy:              "drop",
		CheckExact:          false,
		SimilarityThreshold: 0.95,
	}

	obj := &storage.KnowledgeObject{
		ID:        "us0404-sk-1",
		Type:      "text",
		SourceKey: "slack:C01/9876.5432",
		RawContent: "original slack message",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, env.svc.Store.Objects().Create(t.Context(), obj))

	// POST with same source_key — should match.
	status, result := postAnalyzeWithOpts(t, env.URL, map[string]any{
		"content":    "different content but same key",
		"type":       "text",
		"source_key": "slack:C01/9876.5432",
	})
	assert.Equal(t, gohttp.StatusAccepted, status)
	assert.Equal(t, "us0404-sk-1", result["job_id"])
}

// TestUS0404_SourceKeyNewProceeds verifies that a new source_key
// does not block ingestion.
func TestUS0404_SourceKeyNewProceeds(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Cfg.Duplicates = config.DuplicatesConfig{
		Policy:              "drop",
		CheckExact:          false,
		SimilarityThreshold: 0.95,
	}

	status, result := postAnalyzeWithOpts(t, env.URL, map[string]any{
		"content":    "brand new message",
		"type":       "text",
		"source_key": "slack:C02/new-message",
	})
	assert.Equal(t, gohttp.StatusAccepted, status)
	// A new job ID means dedup didn't block; it's a UUID, not a pre-existing object ID.
	assert.NotEmpty(t, result["job_id"])
	assert.NotEqual(t, "us0404-sk-1", result["job_id"])
}

// TestUS0404_ForceBypasses verifies that --force skips dedup.
func TestUS0404_ForceBypasses(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Cfg.Duplicates = config.DuplicatesConfig{
		Policy:              "drop",
		CheckExact:          true,
		SimilarityThreshold: 0.95,
	}

	const content = "US-0404 force bypass test"
	hash := storageutil.ContentHash(content, "")

	obj := &storage.KnowledgeObject{
		ID:          "us0404-force-1",
		Type:        "text",
		ContentHash: hash,
		RawContent:  content,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	require.NoError(t, env.svc.Store.Objects().Create(t.Context(), obj))

	// POST with force=true — should create new job despite duplicate.
	status, result := postAnalyzeWithOpts(t, env.URL, map[string]any{
		"content": content,
		"type":    "text",
		"force":   true,
	})
	assert.Equal(t, gohttp.StatusAccepted, status)
	// With force, a new job is created — ID should differ from the existing object.
	assert.NotEqual(t, "us0404-force-1", result["job_id"])
}

// TestUS0404_DedupAuditLog verifies that dedup decisions are logged.
func TestUS0404_DedupAuditLog(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Cfg.Duplicates = config.DuplicatesConfig{
		Policy:              "drop",
		CheckExact:          true,
		SimilarityThreshold: 0.95,
	}

	const content = "US-0404 audit log test"
	hash := storageutil.ContentHash(content, "")

	obj := &storage.KnowledgeObject{
		ID:          "us0404-audit-1",
		Type:        "text",
		ContentHash: hash,
		RawContent:  content,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	require.NoError(t, env.svc.Store.Objects().Create(t.Context(), obj))

	// Trigger dedup.
	postAnalyzeWithOpts(t, env.URL, map[string]any{
		"content": content,
		"type":    "text",
	})

	// Check audit log.
	entries, err := env.svc.Store.AuditLog().GetObjectHistory(t.Context(), "us0404-audit-1")
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	found := false
	for _, e := range entries {
		if e.EventType == "dedup.exact" {
			found = true
			assert.Equal(t, "system", e.Actor)
			assert.Equal(t, "drop", e.Payload["policy"])
			break
		}
	}
	assert.True(t, found,
		fmt.Sprintf("expected dedup.exact audit entry; got %d entries", len(entries)))
}
