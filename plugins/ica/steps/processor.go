package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/plugins/ica"
)

const maxRetries = 3

// ICAProcessor wraps ica's Python processor service.
type ICAProcessor struct {
	pipeline.BaseContract
	processorURL string
	client       *http.Client
}

// processorResponse is the JSON envelope returned by /process.
type processorResponse struct {
	Item       ica.NormalizedItem `json:"item"`
	IsNewStory bool               `json:"is_new_story"`
	Status     string             `json:"status"`
}

// NewICAProcessor creates an ICAProcessor targeting the given
// processor service URL.
func NewICAProcessor(
	processorURL string,
	client *http.Client,
) *ICAProcessor {
	if client == nil {
		client = http.DefaultClient
	}
	return &ICAProcessor{
		BaseContract: pipeline.NewBaseContract(
			pipeline.StepContract{
				Requires: []string{"RawContent"},
				Produces: []string{
					"Embeddings", "Metadata", "Sections",
				},
				Capabilities: []string{"ica_processor"},
			},
		),
		processorURL: processorURL,
		client:       client,
	}
}

func (s *ICAProcessor) Name() string { return "ica_processor" }

func (s *ICAProcessor) Run(
	ctx context.Context,
	draft *storage.KnowledgeObject,
) (*storage.KnowledgeObject, error) {
	item := ica.DraftToNormalizedItem(draft)

	body, err := json.Marshal(item)
	if err != nil {
		return nil, pipeline.Permanent(
			fmt.Errorf("ica_processor: marshal: %w", err),
		)
	}

	respBody, err := s.postWithRetry(ctx, body)
	if err != nil {
		return nil, err
	}

	var resp processorResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, pipeline.Permanent(
			fmt.Errorf(
				"ica_processor: decode response: %w", err,
			),
		)
	}

	enriched := ica.NormalizedItemToDraft(&resp.Item)

	// merge enriched fields into original draft
	if len(enriched.Embeddings) > 0 {
		draft.Embeddings = enriched.Embeddings
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	for k, v := range enriched.Metadata {
		draft.Metadata[k] = v
	}
	draft.Metadata["ica_is_new_story"] = resp.IsNewStory

	draft.Sections = append(draft.Sections, enriched.Sections...)

	return draft, nil
}

// postWithRetry POSTs body to /process with exponential backoff
// on network errors and 5xx responses. 4xx returns permanent error.
func (s *ICAProcessor) postWithRetry(
	ctx context.Context,
	body []byte,
) ([]byte, error) {
	backoff := time.Second
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf(
					"ica_processor: %w", ctx.Err(),
				)
			case <-time.After(backoff):
				backoff *= 2
			}
		}

		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			s.processorURL+"/process",
			bytes.NewReader(body),
		)
		if err != nil {
			return nil, pipeline.Permanent(
				fmt.Errorf(
					"ica_processor: build request: %w", err,
				),
			)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := s.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf(
				"ica_processor: post: %w", err,
			)
			continue
		}

		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return nil, pipeline.Permanent(
				fmt.Errorf(
					"ica_processor: %d: %s",
					resp.StatusCode, data,
				),
			)
		}

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf(
				"ica_processor: %d: %s",
				resp.StatusCode, data,
			)
			continue
		}

		if readErr != nil {
			lastErr = fmt.Errorf(
				"ica_processor: read body: %w", readErr,
			)
			continue
		}

		return data, nil
	}

	return nil, fmt.Errorf(
		"ica_processor: retries exhausted: %w", lastErr,
	)
}
