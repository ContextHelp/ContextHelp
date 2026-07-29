package ica_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	ica "github.com/ideacrafterslabs/ctxt/plugins/ica"
	"github.com/ideacrafterslabs/ctxt/plugins/ica/steps"
)

// mockItems returns two NormalizedItem fixtures sharing one
// distribution channel URL for overlap testing.
func mockItems() []ica.NormalizedItem {
	return []ica.NormalizedItem{
		{
			CanonicalURL: "https://example.com/article-1",
			Title:        "Test Article",
			ArticleBody:  "Article content here",
			ContentHash:  "sha256:abc123",
			Language:     "en",
			MediaTypesPresent: []ica.MediaType{
				ica.MediaText,
			},
			PrimarySource: ica.Source{
				Domain:    "example.com",
				Publisher: "Example",
			},
			DistChannels: []ica.DistChannel{{
				Domain: "mirror.com",
				URL:    "https://mirror.com/article-1",
			}},
			ExtractedEntities: []ica.Entity{{
				Type:  "person",
				Value: "John Doe",
			}},
		},
		{
			CanonicalURL: "https://example.com/article-2",
			Title:        "Second Article",
			ArticleBody:  "More content",
			ContentHash:  "sha256:def456",
			MediaTypesPresent: []ica.MediaType{
				ica.MediaText, ica.MediaVideo,
			},
			PrimarySource: ica.Source{
				Domain:    "example.com",
				Publisher: "Example",
			},
			DistChannels: []ica.DistChannel{{
				Domain: "mirror.com",
				URL:    "https://mirror.com/article-1",
			}},
		},
	}
}

// newMockAPI returns an httptest.Server simulating the ica API.
func newMockAPI(t *testing.T, items []ica.NormalizedItem) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/api/v1/feeds", func(w http.ResponseWriter, r *http.Request) {
		feed := []map[string]string{{
			"id":     "f1",
			"url":    r.URL.Query().Get("url"),
			"status": "active",
		}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(feed) //nolint:errcheck
	})

	mux.HandleFunc(
		"/api/v1/feeds/sync",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(items) //nolint:errcheck
		},
	)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// newMockProcessor returns an httptest.Server simulating /process.
func newMockProcessor(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/process", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		var item ica.NormalizedItem
		if err := json.Unmarshal(body, &item); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		item.Embedding = []float32{0.1, 0.2, 0.3}

		resp := map[string]any{
			"item":         item,
			"is_new_story": true,
			"status":       "ok",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// newIntegrationPlugin wires up a Plugin with the given URLs.
func newIntegrationPlugin(
	t *testing.T,
	apiURL, processorURL string,
) *ica.Plugin {
	t.Helper()
	p := ica.NewWithSteps(steps.Factory())
	p.InitForTest(ica.Config{
		APIURL:                 apiURL,
		ProcessorURL:           processorURL,
		DistChannelsAsMentions: true,
	}, &http.Client{})
	return p
}

func TestICAFeedSyncEndToEnd(t *testing.T) {
	items := mockItems()
	apiSrv := newMockAPI(t, items)
	procSrv := newMockProcessor(t)

	p := newIntegrationPlugin(t, apiSrv.URL, procSrv.URL)
	pipeSteps := p.PipelineSteps()
	if len(pipeSteps) != 4 {
		t.Fatalf("want 4 steps, got %d", len(pipeSteps))
	}

	ctx := context.Background()
	draft := &pluginapi.KnowledgeObject{
		Source: apiSrv.URL + "/feed.xml",
	}

	// Step 0: ica_feed_manager
	var err error
	draft, err = pipeSteps[0].Run(ctx, draft)
	if err != nil {
		t.Fatalf("feed_manager: %v", err)
	}
	if draft.Metadata["feed_id"] != "f1" {
		t.Errorf(
			"feed_id: want %q, got %v",
			"f1", draft.Metadata["feed_id"],
		)
	}

	// Step 1: ica_fetcher
	draft, err = pipeSteps[1].Run(ctx, draft)
	if err != nil {
		t.Fatalf("fetcher: %v", err)
	}
	if draft.RawContent == "" {
		t.Error("fetcher: RawContent empty")
	}
	if draft.Metadata["feed_items"] == nil {
		t.Error("fetcher: feed_items not set")
	}

	// Step 2: ica_normalizer
	draft, err = pipeSteps[2].Run(ctx, draft)
	if err != nil {
		t.Fatalf("normalizer: %v", err)
	}

	if len(draft.Tags) == 0 {
		t.Error("normalizer: no tags")
	}
	hasMediaTag := false
	for _, tag := range draft.Tags {
		if tag.Source == "media" {
			hasMediaTag = true
			break
		}
	}
	if !hasMediaTag {
		t.Error("normalizer: no media type tag")
	}

	if len(draft.Mentions) == 0 {
		t.Error("normalizer: no mentions")
	}

	processed, _ := draft.Metadata["ica_items_processed"].(int)
	if processed < 1 {
		t.Errorf(
			"normalizer: ica_items_processed=%d, want >=1",
			processed,
		)
	}

	// Step 3: ica_processor
	draft, err = pipeSteps[3].Run(ctx, draft)
	if err != nil {
		t.Fatalf("processor: %v", err)
	}
	if len(draft.Embeddings) == 0 {
		t.Error("processor: embeddings empty")
	}
	isNew, _ := draft.Metadata["ica_is_new_story"].(bool)
	if !isNew {
		t.Error("processor: ica_is_new_story not true")
	}
}

func TestICAFeedSyncDegradedNoProcessor(t *testing.T) {
	items := mockItems()
	apiSrv := newMockAPI(t, items)

	p := newIntegrationPlugin(t, apiSrv.URL, "")
	pipeSteps := p.PipelineSteps()
	if len(pipeSteps) != 3 {
		t.Fatalf("want 3 steps, got %d", len(pipeSteps))
	}

	ctx := context.Background()
	draft := &pluginapi.KnowledgeObject{
		Source: apiSrv.URL + "/feed.xml",
	}

	var err error
	for i, s := range pipeSteps {
		draft, err = s.Run(ctx, draft)
		if err != nil {
			t.Fatalf("step[%d] %s: %v", i, s.Name(), err)
		}
	}

	// All Go-native enrichment should work.
	if len(draft.Tags) == 0 {
		t.Error("no tags after normalizer")
	}
	if len(draft.Mentions) == 0 {
		t.Error("no mentions after normalizer")
	}

	processed, _ := draft.Metadata["ica_items_processed"].(int)
	if processed < 1 {
		t.Errorf("ica_items_processed=%d, want >=1", processed)
	}

	// Without processor, embeddings stay nil.
	if draft.Embeddings != nil {
		t.Errorf(
			"embeddings should be nil without processor, got %v",
			draft.Embeddings,
		)
	}
}

func TestICASharedDistChannelsMentions(t *testing.T) {
	items := mockItems()
	apiSrv := newMockAPI(t, items)

	p := newIntegrationPlugin(t, apiSrv.URL, "")
	pipeSteps := p.PipelineSteps()

	ctx := context.Background()

	// Build two drafts through the full Go-native pipeline.
	drafts := make([]*pluginapi.KnowledgeObject, 2)
	for idx := range drafts {
		d := &pluginapi.KnowledgeObject{
			Source: apiSrv.URL + "/feed.xml",
		}
		var err error
		for i, s := range pipeSteps {
			d, err = s.Run(ctx, d)
			if err != nil {
				t.Fatalf(
					"draft[%d] step[%d] %s: %v",
					idx, i, s.Name(), err,
				)
			}
		}
		drafts[idx] = d
	}

	// Both drafts processed the same items, so their Mentions
	// slices should overlap (shared dist channel URL).
	setA := make(map[string]bool, len(drafts[0].Mentions))
	for _, m := range drafts[0].Mentions {
		setA[m.String()] = true
	}

	overlap := 0
	for _, m := range drafts[1].Mentions {
		if setA[m.String()] {
			overlap++
		}
	}

	if overlap == 0 {
		t.Error(
			"no shared mentions between drafts; " +
				"AlternativeDetector jaccardMentions " +
				"would produce zero overlap",
		)
	}
}
