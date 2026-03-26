package integration

// US-0206: OSINT Entity Aggregation
//
// Verifies: mock multiple source APIs, run entity aggregation for a slug,
// verify merged profile stored.
// All external HTTP calls use httptest.NewServer — no real network.
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
// Mock multi-source payloads
// ---------------------------------------------------------------------------

type mockOSINTSource struct {
	Platform  string            `json:"platform"`
	Slug      string            `json:"slug"`
	Name      string            `json:"name"`
	Bio       string            `json:"bio"`
	Links     []string          `json:"links"`
	Metadata  map[string]string `json:"metadata"`
}

// ---------------------------------------------------------------------------
// Pipeline steps
// ---------------------------------------------------------------------------

// osintMultiSourceFetchStep fetches from multiple mock sources and merges.
type osintMultiSourceFetchStep struct {
	pipeline.BaseContract
	sources []string // URLs of mock source endpoints
	slug    string
}

func (s *osintMultiSourceFetchStep) Name() string { return "test-osint-multi-source-fetch" }
func (s *osintMultiSourceFetchStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	var platforms []string
	var names []string
	var bios []string
	var links []string

	for _, srcURL := range s.sources {
		resp, err := gohttp.Get(fmt.Sprintf("%s/entity/%s", srcURL, s.slug))
		if err != nil {
			continue
		}
		var src mockOSINTSource
		if err := json.NewDecoder(resp.Body).Decode(&src); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		platforms = append(platforms, src.Platform)
		names = append(names, src.Name)
		bios = append(bios, src.Bio)
		links = append(links, src.Links...)
	}

	draft.Type = "entity.profile"
	draft.Metadata["entity_slug"] = s.slug
	draft.Metadata["platforms"] = platforms
	draft.Metadata["names"] = names
	draft.Metadata["bios"] = bios
	draft.Metadata["links"] = links
	draft.Metadata["source_count"] = len(s.sources)

	// Use first name as canonical name.
	if len(names) > 0 {
		draft.Metadata["canonical_name"] = names[0]
	}

	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0206 Tests
// ---------------------------------------------------------------------------

// TestUS0206_MergedProfileFromMultipleSources verifies that data from multiple
// mock platforms is aggregated into a single merged profile.
func TestUS0206_MergedProfileFromMultipleSources(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	const slug = "person.jane-doe"

	// Mock X/Twitter source.
	srvX := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockOSINTSource{
			Platform: "x",
			Slug:     slug,
			Name:     "Jane Doe",
			Bio:      "Engineer @acme. Building distributed systems.",
			Links:    []string{"https://github.com/janedoe"},
		})
	}))
	defer srvX.Close()

	// Mock LinkedIn source.
	srvLI := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockOSINTSource{
			Platform: "linkedin",
			Slug:     slug,
			Name:     "Jane Doe",
			Bio:      "Principal Engineer at Acme Corp",
			Links:    []string{"https://x.com/janedoe"},
		})
	}))
	defer srvLI.Close()

	// Mock GitHub source.
	srvGH := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockOSINTSource{
			Platform: "github",
			Slug:     slug,
			Name:     "janedoe",
			Bio:      "Open source contributor",
			Links:    []string{"https://www.linkedin.com/in/jane-doe"},
		})
	}))
	defer srvGH.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("osint.aggregate", &pipeline.Pipeline{
		PipelineName: "osint.aggregate",
		Steps: []pipeline.PipelineStep{
			&osintMultiSourceFetchStep{
				sources: []string{srvX.URL, srvLI.URL, srvGH.URL},
				slug:    slug,
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  slug,
		Type:     "entity",
		Pipeline: "osint.aggregate",
		Source:   "osint",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, "entity.profile", obj.Type)
	assert.Equal(t, slug, obj.Metadata["entity_slug"])
	assert.EqualValues(t, 3, obj.Metadata["source_count"], "must aggregate from all 3 sources")

	platforms, _ := obj.Metadata["platforms"].([]any)
	assert.Len(t, platforms, 3, "must have 3 platform entries")
	assert.Equal(t, "Jane Doe", obj.Metadata["canonical_name"])
}

// TestUS0206_SingleSourceAggregation verifies single-source aggregation works.
func TestUS0206_SingleSourceAggregation(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	const slug = "person.john-smith"

	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockOSINTSource{
			Platform: "x",
			Slug:     slug,
			Name:     "John Smith",
			Bio:      "Researcher",
			Links:    []string{},
		})
	}))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("osint.aggregate", &pipeline.Pipeline{
		PipelineName: "osint.aggregate",
		Steps: []pipeline.PipelineStep{
			&osintMultiSourceFetchStep{
				sources: []string{srv.URL},
				slug:    slug,
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  slug,
		Type:     "entity",
		Pipeline: "osint.aggregate",
		Source:   "osint",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, "entity.profile", obj.Type)
	assert.EqualValues(t, 1, obj.Metadata["source_count"])
}
