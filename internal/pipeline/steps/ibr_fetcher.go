package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/browser"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// IBRFetcher fetches web content using the IBR browser automation daemon.
// Unlike url_fetcher (plain HTTP), this handles JavaScript-rendered pages,
// login walls, cookie consent, pagination, and structured data extraction.
type IBRFetcher struct {
	pipeline.BaseContract
	client *browser.Client
}

// IBRFetcherOption configures an IBRFetcher.
type IBRFetcherOption func(*IBRFetcher)

// NewIBRFetcher creates an IBRFetcher with the given browser client.
func NewIBRFetcher(client *browser.Client, opts ...IBRFetcherOption) *IBRFetcher {
	f := &IBRFetcher{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires:     []string{"Source"},
			Produces:     []string{"RawContent", "Metadata"},
			Capabilities: []string{"browser"},
		}),
		client: client,
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

func (s *IBRFetcher) Name() string { return "ibr_fetcher" }

func (s *IBRFetcher) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if s.client == nil {
		return nil, fmt.Errorf("ibr_fetcher: browser not available (is browser.enabled=true in config?)")
	}

	rawURL := strings.TrimSpace(draft.Source)
	if rawURL == "" {
		if strings.HasPrefix(strings.TrimSpace(draft.RawContent), "http") {
			rawURL = strings.TrimSpace(draft.RawContent)
		}
	}
	if rawURL == "" {
		return nil, fmt.Errorf("ibr_fetcher: no URL to fetch")
	}

	log.Printf("ibr_fetcher: fetching %s via browser", rawURL)

	instructions := []string{"extract the full page content as HTML"}
	if draft.Metadata != nil {
		if instr, ok := draft.Metadata["ibr_instructions"]; ok {
			if instrSlice, ok := instr.([]any); ok {
				instructions = make([]string, 0, len(instrSlice))
				for _, v := range instrSlice {
					if s, ok := v.(string); ok {
						instructions = append(instructions, s)
					}
				}
			}
		}
	}

	var sb strings.Builder
	sb.WriteString("url: " + rawURL + "\n")
	sb.WriteString("instructions:\n")
	for _, instr := range instructions {
		sb.WriteString("  - " + instr + "\n")
	}

	result, err := s.client.Execute(ctx, sb.String())
	if err != nil {
		return nil, fmt.Errorf("ibr_fetcher: execute: %w", err)
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["source_url"] = rawURL
	draft.Metadata["ibr_token_usage"] = result.TokenUsage

	if len(result.Extracts) > 0 {
		draft.RawContent = extractContent(result.Extracts[0])
		raw, _ := json.Marshal(result.Extracts)
		draft.Metadata["ibr_extracts"] = json.RawMessage(raw)
	}

	log.Printf("ibr_fetcher: fetched %d bytes, %d extracts", len(draft.RawContent), len(result.Extracts))
	return draft, nil
}

// extractContent picks the best text field from an extract map.
func extractContent(extract map[string]any) string {
	for _, key := range []string{"html", "content", "text", "body"} {
		if v, ok := extract[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	b, _ := json.Marshal(extract)
	return string(b)
}
