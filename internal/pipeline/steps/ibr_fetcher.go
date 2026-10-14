package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ideacrafterslabs/ctxt/internal/browser"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/safefetch"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// IBRFetcher fetches web content using the IBR browser automation daemon.
// Unlike url_fetcher (plain HTTP), this handles JavaScript-rendered pages,
// login walls, cookie consent, pagination, and structured data extraction.
//
// The browser does its own DNS lookups and follows its own redirects, so
// the fetch guard cannot sit in its dialer: Run vets the URL with
// safefetch.Guard.CheckURL before handing it over, refusing a host that
// resolves to a private-network address.
type IBRFetcher struct {
	pipeline.BaseContract
	client *browser.Client
	guard  *safefetch.Guard
}

// IBRFetcherOption configures an IBRFetcher.
type IBRFetcherOption func(*IBRFetcher)

// WithIBRFetchGuard sets the guard that vets each URL, built from the
// fetch.* config. Without it the default guard applies.
func WithIBRFetchGuard(g *safefetch.Guard) IBRFetcherOption {
	return func(f *IBRFetcher) {
		if g != nil {
			f.guard = g
		}
	}
}

// NewIBRFetcher creates an IBRFetcher with the given browser client.
func NewIBRFetcher(client *browser.Client, opts ...IBRFetcherOption) *IBRFetcher {
	f := &IBRFetcher{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires:     []string{"Source"},
			Produces:     []string{"RawContent", "Metadata"},
			Capabilities: []string{"browser"},
		}),
		client: client,
		guard:  safefetch.Default(),
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

	if err := s.guard.CheckURL(ctx, rawURL); err != nil {
		wrapped := fmt.Errorf("ibr_fetcher: %w", err)
		if safefetch.IsBlocked(err) {
			return nil, pipeline.Permanent(wrapped)
		}
		return nil, wrapped
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

	prompt := struct {
		URL          string   `yaml:"url"`
		Instructions []string `yaml:"instructions"`
	}{
		URL:          rawURL,
		Instructions: instructions,
	}
	promptBytes, err := yaml.Marshal(prompt)
	if err != nil {
		return nil, fmt.Errorf("ibr_fetcher: marshal prompt: %w", err)
	}

	result, err := s.client.Execute(ctx, string(promptBytes))
	if err != nil {
		return nil, fmt.Errorf("ibr_fetcher: execute: %w", err)
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["source_url"] = rawURL
	draft.Metadata["ibr_token_usage"] = result.TokenUsage

	if len(result.Extracts) > 0 {
		content, contentType := extractContent(result.Extracts[0])
		draft.RawContent = content
		draft.ContentType = contentType
		raw, _ := json.Marshal(result.Extracts)
		draft.Metadata["ibr_extracts"] = json.RawMessage(raw)
	}

	log.Printf("ibr_fetcher: fetched %d bytes, %d extracts", len(draft.RawContent), len(result.Extracts))
	return draft, nil
}

// extractContent picks the best text field from an extract map and returns
// the content along with a MIME content type.
func extractContent(extract map[string]any) (string, string) {
	// html/content fields are treated as HTML; text/body as plain text.
	htmlKeys := map[string]bool{"html": true, "content": true}
	for _, key := range []string{"html", "content", "text", "body"} {
		if v, ok := extract[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				if htmlKeys[key] {
					return s, "text/html"
				}
				return s, "text/plain"
			}
		}
	}
	b, _ := json.Marshal(extract)
	return string(b), "application/json"
}
