package integration

// US-0210: Cross-Platform Entity Resolution
//
// Verifies: ingest entities from two mock platforms with same real-world entity,
// dedup/merge produces single canonical object.
// Each platform fetch runs through its own xrr cassette
// (testdata/cassettes/us0210_*). In the default replay mode the recorded
// round-trip answers from disk and the fixture server is never contacted.
// See xrr_capture_helpers_test.go for what the cassettes do and do not
// prove.
//
// The entity-matching step itself is pure in-process logic and is
// deliberately NOT recorded — a cassette can only capture the HTTP seam.
//
// Re-record: XRR_MODE=record go test -count=1 ./test/integration/ \
//   -run TestUS0210_RecordCassettes
//
// Gate: INTEGRATION=1 env var required.

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock platform payloads
// ---------------------------------------------------------------------------

type mockPlatformEntity struct {
	PlatformSlug string   `json:"platform_slug"` // e.g. "@github.janedoe"
	PlatformName string   `json:"platform_name"` // "Jane Doe"
	Platform     string   `json:"platform"`      // "github"
	Bio          string   `json:"bio"`
	LinkedURLs   []string `json:"linked_urls"`
	Email        string   `json:"email,omitempty"`
	Organization string   `json:"organization,omitempty"`
}

// ---------------------------------------------------------------------------
// Pipeline steps
// ---------------------------------------------------------------------------

// platformEntityFetchStep fetches entity data from a mock platform.
type platformEntityFetchStep struct {
	pipeline.BaseContract
	platformURL string
	entityPath  string
	// client carries the xrr record/replay transport. Nil falls back to
	// the default client.
	client *gohttp.Client
}

func (s *platformEntityFetchStep) Name() string { return "test-platform-entity-fetch" }
func (s *platformEntityFetchStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	resp, err := captureGet(s.client, fmt.Sprintf("%s%s", s.platformURL, s.entityPath))
	if err != nil {
		return draft, err
	}
	defer resp.Body.Close()

	var ent mockPlatformEntity
	if err := json.NewDecoder(resp.Body).Decode(&ent); err != nil {
		return draft, err
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Type = "entity.person"
	draft.Metadata["platform_slug"] = ent.PlatformSlug
	draft.Metadata["platform_name"] = ent.PlatformName
	draft.Metadata["platform"] = ent.Platform
	draft.Metadata["bio"] = ent.Bio
	draft.Metadata["linked_urls"] = ent.LinkedURLs
	draft.Metadata["organization"] = ent.Organization
	return draft, nil
}

// toStringSlice normalises a metadata value that may be []string or []any into []string.
func toStringSlice(v any) []string {
	switch typed := v.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// entityMatchStep simulates the matching heuristic: compares name + org + linked URLs.
type entityMatchStep struct {
	pipeline.BaseContract
	existingObjects []storage.KnowledgeObject
}

func (s *entityMatchStep) Name() string { return "test-entity-match" }
func (s *entityMatchStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	draftName, _ := draft.Metadata["platform_name"].(string)
	draftOrg, _ := draft.Metadata["organization"].(string)
	draftLinks := toStringSlice(draft.Metadata["linked_urls"])

	var matchedSlug string
	var confidence float64

	for _, existing := range s.existingObjects {
		existingName, _ := existing.Metadata["platform_name"].(string)
		existingOrg, _ := existing.Metadata["organization"].(string)
		existingLinks := toStringSlice(existing.Metadata["linked_urls"])

		nameMatch := draftName == existingName
		orgMatch := draftOrg != "" && draftOrg == existingOrg

		// Check for linked URL overlap.
		urlMatch := false
		for _, dl := range draftLinks {
			for _, el := range existingLinks {
				if dl != "" && dl == el {
					urlMatch = true
					break
				}
			}
			if urlMatch {
				break
			}
		}

		var score float64
		if nameMatch {
			score += 0.3
		}
		if orgMatch {
			score += 0.3
		}
		if urlMatch {
			score += 0.4
		}

		if score > confidence {
			confidence = score
			if score >= 0.75 {
				matchedSlug, _ = existing.Metadata["canonical_slug"].(string)
				if matchedSlug == "" {
					matchedSlug, _ = existing.Metadata["platform_slug"].(string)
				}
			}
		}
	}

	draft.Metadata["match_confidence"] = confidence
	draft.Metadata["matched_slug"] = matchedSlug
	draft.Metadata["is_duplicate"] = matchedSlug != ""

	// If matched, record same_as relationship.
	if matchedSlug != "" {
		draft.Metadata["same_as"] = matchedSlug
		draft.Metadata["canonical_slug"] = matchedSlug
	} else {
		// First entity — becomes canonical.
		draft.Metadata["canonical_slug"] = draft.Metadata["platform_slug"]
	}

	return draft, nil
}

// ---------------------------------------------------------------------------
// Fixture handlers
//
// Named so the replaying test and the recorder share one definition of
// each payload. See xrr_capture_helpers_test.go.
// ---------------------------------------------------------------------------

const (
	entityCrossRefURL  = "https://shared-profile.example.com/janedoe"
	entityGitHubPath   = "/user/janedoe"
	entityLinkedInPath = "/profile/jane-doe"
	entityNameOnlyPath = "/user/janedoe99"
)

// platformEntityHandler serves one platform entity for any path, matching
// the previous inline handlers, which also ignored the request path.
func platformEntityHandler(ent *mockPlatformEntity) gohttp.HandlerFunc {
	return func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ent)
	}
}

// entityGitHubFixture and entityLinkedInFixture share entityCrossRefURL,
// which is what drives the cross-reference match.
func entityGitHubFixture() *mockPlatformEntity {
	return &mockPlatformEntity{
		PlatformSlug: "@github.janedoe",
		PlatformName: "Jane Doe",
		Platform:     "github",
		Bio:          "Open source contributor at Acme",
		LinkedURLs:   []string{entityCrossRefURL},
		Organization: "Acme Corp",
	}
}

func entityLinkedInFixture() *mockPlatformEntity {
	return &mockPlatformEntity{
		PlatformSlug: "@linkedin.jane-doe",
		PlatformName: "Jane Doe",
		Platform:     "linkedin",
		Bio:          "Principal Engineer at Acme Corp",
		LinkedURLs:   []string{entityCrossRefURL},
		Organization: "Acme Corp",
	}
}

// entityNameOnlyFixture is a different person who happens to share a
// name: no org, no links, so only the name can match.
func entityNameOnlyFixture() *mockPlatformEntity {
	return &mockPlatformEntity{
		PlatformSlug: "@x.janedoe99",
		PlatformName: "Jane Doe",
		Platform:     "x",
		Bio:          "Random person with same name",
		LinkedURLs:   []string{},
		Organization: "",
	}
}

func entityResolutionCassettes() []captureFixture {
	return []captureFixture{
		{
			Cassette: "us0210_merge_github",
			Handler:  platformEntityHandler(entityGitHubFixture()),
			Path:     entityGitHubPath,
		},
		{
			Cassette: "us0210_merge_linkedin",
			Handler:  platformEntityHandler(entityLinkedInFixture()),
			Path:     entityLinkedInPath,
		},
		{
			Cassette: "us0210_name_only_match",
			Handler:  platformEntityHandler(entityNameOnlyFixture()),
			Path:     entityNameOnlyPath,
		},
	}
}

// TestUS0210_RecordCassettes re-records the US-0210 cassettes. No-op
// unless XRR_MODE=record; needs no Postgres/Redis.
func TestUS0210_RecordCassettes(t *testing.T) {
	recordCaptureFixtures(t, entityResolutionCassettes())
}

// ---------------------------------------------------------------------------
// US-0210 Tests
// ---------------------------------------------------------------------------

// TestUS0210_SameEntityFromTwoPlatformsMerged verifies that two captures of the
// same real-world person from different platforms produce a same_as link.
func TestUS0210_SameEntityFromTwoPlatformsMerged(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	// Both fixtures link to entityCrossRefURL, which is what makes them
	// resolve to one entity.
	//
	// Replay answers from the cassettes; the fixture servers only matter
	// when re-recording.
	srvGH := httptest.NewServer(platformEntityHandler(entityGitHubFixture()))
	defer srvGH.Close()

	srvLI := httptest.NewServer(platformEntityHandler(entityLinkedInFixture()))
	defer srvLI.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	// First: ingest GitHub entity (no existing objects — becomes canonical).
	env.svc.Pipes.Upsert("entity.github.person", &pipeline.Pipeline{
		PipelineName: "entity.github.person",
		Steps: []pipeline.PipelineStep{
			&platformEntityFetchStep{
				platformURL: srvGH.URL,
				entityPath:  entityGitHubPath,
				client:      captureClient(t, "us0210_merge_github"),
			},
			&entityMatchStep{existingObjects: nil},
		},
	})

	jobID1, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "https://github.com/janedoe",
		Type:     "url",
		Pipeline: "entity.github.person",
		Source:   "https://github.com/janedoe",
	})
	require.NoError(t, err)

	job1 := waitForJob(t, env.URL, jobID1, storage.JobCompleted)
	require.NotEmpty(t, job1.ResultID)

	resp1, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job1.ResultID))
	require.NoError(t, err)
	defer resp1.Body.Close()

	var ghObj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp1.Body).Decode(&ghObj))

	assert.Equal(t, "entity.person", ghObj.Type)
	assert.Equal(t, "@github.janedoe", ghObj.Metadata["canonical_slug"],
		"first entity becomes canonical")
	assert.Equal(t, false, ghObj.Metadata["is_duplicate"],
		"first entity is not a duplicate")

	// Second: ingest LinkedIn entity — should match GitHub entity.
	env.svc.Pipes.Upsert("entity.linkedin.person", &pipeline.Pipeline{
		PipelineName: "entity.linkedin.person",
		Steps: []pipeline.PipelineStep{
			&platformEntityFetchStep{
				platformURL: srvLI.URL,
				entityPath:  entityLinkedInPath,
				client:      captureClient(t, "us0210_merge_linkedin"),
			},
			&entityMatchStep{existingObjects: []storage.KnowledgeObject{ghObj}},
		},
	})

	jobID2, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "https://linkedin.com/in/jane-doe",
		Type:     "url",
		Pipeline: "entity.linkedin.person",
		Source:   "https://linkedin.com/in/jane-doe",
	})
	require.NoError(t, err)

	job2 := waitForJob(t, env.URL, jobID2, storage.JobCompleted)
	require.NotEmpty(t, job2.ResultID)

	resp2, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job2.ResultID))
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp2.StatusCode)

	var liObj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&liObj))

	assert.Equal(t, "entity.person", liObj.Type)
	assert.Equal(t, true, liObj.Metadata["is_duplicate"],
		"LinkedIn entity must be detected as duplicate")
	assert.Equal(t, "@github.janedoe", liObj.Metadata["same_as"],
		"same_as must point to canonical GitHub entity")

	confidence, _ := liObj.Metadata["match_confidence"].(float64)
	assert.GreaterOrEqual(t, confidence, 0.75,
		"confidence must be >= 0.75 for auto-link")
}

// TestUS0210_LowConfidenceNotAutoLinked verifies that low confidence matches
// are not auto-linked.
func TestUS0210_LowConfidenceNotAutoLinked(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	// Existing entity: Jane Doe at AcmeCorp.
	existingEntity := storage.KnowledgeObject{
		Type: "entity.person",
		Metadata: map[string]any{
			"platform_slug":  "@github.janedoe",
			"platform_name":  "Jane Doe",
			"platform":       "github",
			"organization":   "Acme Corp",
			"linked_urls":    []any{},
			"canonical_slug": "@github.janedoe",
		},
	}

	// New entity: Jane Doe, no org overlap, no URL links — name-only match.
	srvX := httptest.NewServer(platformEntityHandler(entityNameOnlyFixture()))
	defer srvX.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("entity.x.person", &pipeline.Pipeline{
		PipelineName: "entity.x.person",
		Steps: []pipeline.PipelineStep{
			&platformEntityFetchStep{
				platformURL: srvX.URL,
				entityPath:  entityNameOnlyPath,
				client:      captureClient(t, "us0210_name_only_match"),
			},
			&entityMatchStep{existingObjects: []storage.KnowledgeObject{existingEntity}},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "https://x.com/janedoe99",
		Type:     "url",
		Pipeline: "entity.x.person",
		Source:   "https://x.com/janedoe99",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	confidence, _ := obj.Metadata["match_confidence"].(float64)
	assert.Less(t, confidence, 0.75, "name-only match must score < 0.75")
	assert.Equal(t, false, obj.Metadata["is_duplicate"],
		"low confidence must not auto-link")
	assert.Empty(t, obj.Metadata["same_as"], "same_as must not be set for low confidence")
}
