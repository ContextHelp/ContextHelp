package integration

// US-0206: OSINT Entity Aggregation
//
// Verifies: mock multiple source APIs, run entity aggregation for a slug,
// verify merged profile stored.
// Each source fetch runs through its OWN xrr cassette
// (testdata/cassettes/us0206_*). Every source is queried at the same
// path (/entity/<slug>), so they cannot share a cassette directory: the
// http adapter fingerprints on method + path + body, and identical
// fingerprints would overwrite each other. One cassette per source keeps
// the three payloads distinct.
//
// See xrr_capture_helpers_test.go for what the cassettes do and do not
// prove.
//
// Re-record: XRR_MODE=record go test -count=1 ./test/integration/ \
//   -run TestUS0206_RecordCassettes
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
// Mock multi-source payloads
// ---------------------------------------------------------------------------

type mockOSINTSource struct {
	Platform string            `json:"platform"`
	Slug     string            `json:"slug"`
	Name     string            `json:"name"`
	Bio      string            `json:"bio"`
	Links    []string          `json:"links"`
	Metadata map[string]string `json:"metadata"`
}

// ---------------------------------------------------------------------------
// Pipeline steps
// ---------------------------------------------------------------------------

// osintMultiSourceFetchStep fetches from multiple mock sources and merges.
type osintMultiSourceFetchStep struct {
	pipeline.BaseContract
	sources []string // URLs of mock source endpoints
	slug    string
	// clients is indexed in parallel with sources: clients[i] carries the
	// xrr transport for sources[i]. A short or nil slice, or a nil entry,
	// falls back to the default client for that source.
	clients []*gohttp.Client
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

	for i, srcURL := range s.sources {
		var client *gohttp.Client
		if i < len(s.clients) {
			client = s.clients[i]
		}
		resp, err := captureGet(client, fmt.Sprintf("%s/entity/%s", srcURL, s.slug))
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
// Fixture handlers
//
// Named so the replaying test and the recorder share one definition of
// each payload. See xrr_capture_helpers_test.go.
// ---------------------------------------------------------------------------

const (
	osintJaneSlug = "person.jane-doe"
	osintJohnSlug = "person.john-smith"
)

// osintSourceHandler serves one source's profile for any path, matching
// the previous inline handlers, which also ignored the request path.
func osintSourceHandler(src mockOSINTSource) gohttp.HandlerFunc {
	return func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(src)
	}
}

func osintXFixture() mockOSINTSource {
	return mockOSINTSource{
		Platform: "x",
		Slug:     osintJaneSlug,
		Name:     "Jane Doe",
		Bio:      "Engineer @acme. Building distributed systems.",
		Links:    []string{"https://github.com/janedoe"},
	}
}

func osintLinkedInFixture() mockOSINTSource {
	return mockOSINTSource{
		Platform: "linkedin",
		Slug:     osintJaneSlug,
		Name:     "Jane Doe",
		Bio:      "Principal Engineer at Acme Corp",
		Links:    []string{"https://x.com/janedoe"},
	}
}

func osintGitHubFixture() mockOSINTSource {
	return mockOSINTSource{
		Platform: "github",
		Slug:     osintJaneSlug,
		Name:     "janedoe",
		Bio:      "Open source contributor",
		Links:    []string{"https://www.linkedin.com/in/jane-doe"},
	}
}

func osintSingleFixture() mockOSINTSource {
	return mockOSINTSource{
		Platform: "x",
		Slug:     osintJohnSlug,
		Name:     "John Smith",
		Bio:      "Researcher",
		Links:    []string{},
	}
}

func osintCassettes() []captureFixture {
	return []captureFixture{
		{
			Cassette: "us0206_merged_source_x",
			Handler:  osintSourceHandler(osintXFixture()),
			Path:     fmt.Sprintf("/entity/%s", osintJaneSlug),
		},
		{
			Cassette: "us0206_merged_source_linkedin",
			Handler:  osintSourceHandler(osintLinkedInFixture()),
			Path:     fmt.Sprintf("/entity/%s", osintJaneSlug),
		},
		{
			Cassette: "us0206_merged_source_github",
			Handler:  osintSourceHandler(osintGitHubFixture()),
			Path:     fmt.Sprintf("/entity/%s", osintJaneSlug),
		},
		{
			Cassette: "us0206_single_source",
			Handler:  osintSourceHandler(osintSingleFixture()),
			Path:     fmt.Sprintf("/entity/%s", osintJohnSlug),
		},
	}
}

// TestUS0206_RecordCassettes re-records the US-0206 cassettes. No-op
// unless XRR_MODE=record; needs no Postgres/Redis.
func TestUS0206_RecordCassettes(t *testing.T) {
	recordCaptureFixtures(t, osintCassettes())
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

	const slug = osintJaneSlug

	// Replay answers from the cassettes; the fixture servers only matter
	// when re-recording. One cassette per source — see the file header.
	srvX := httptest.NewServer(osintSourceHandler(osintXFixture()))
	defer srvX.Close()

	srvLI := httptest.NewServer(osintSourceHandler(osintLinkedInFixture()))
	defer srvLI.Close()

	srvGH := httptest.NewServer(osintSourceHandler(osintGitHubFixture()))
	defer srvGH.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("osint.aggregate", &pipeline.Pipeline{
		PipelineName: "osint.aggregate",
		Steps: []pipeline.PipelineStep{
			&osintMultiSourceFetchStep{
				sources: []string{srvX.URL, srvLI.URL, srvGH.URL},
				slug:    slug,
				clients: []*gohttp.Client{
					captureClient(t, "us0206_merged_source_x"),
					captureClient(t, "us0206_merged_source_linkedin"),
					captureClient(t, "us0206_merged_source_github"),
				},
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

	const slug = osintJohnSlug

	srv := httptest.NewServer(osintSourceHandler(osintSingleFixture()))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("osint.aggregate", &pipeline.Pipeline{
		PipelineName: "osint.aggregate",
		Steps: []pipeline.PipelineStep{
			&osintMultiSourceFetchStep{
				sources: []string{srv.URL},
				slug:    slug,
				clients: []*gohttp.Client{captureClient(t, "us0206_single_source")},
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
