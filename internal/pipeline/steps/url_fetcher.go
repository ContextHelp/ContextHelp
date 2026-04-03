package steps

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// URLFetcher fetches the raw content of a URL.
type URLFetcher struct {
	pipeline.BaseContract
	bridge *ClientBridge
}

// URLFetcherOption configures a URLFetcher.
type URLFetcherOption func(*URLFetcher)

// WithURLHTTPClient sets a custom HTTP client for the fetcher.
// Deprecated: prefer WithURLClientBridge for full bridge configuration.
// Kept for backward compatibility.
func WithURLHTTPClient(c *http.Client) URLFetcherOption {
	return func(f *URLFetcher) {
		f.bridge = NewClientBridge(WithBridgeHTTPClient(c))
	}
}

// WithURLClientBridge replaces the ClientBridge used by the fetcher.
func WithURLClientBridge(b *ClientBridge) URLFetcherOption {
	return func(f *URLFetcher) { f.bridge = b }
}

// NewURLFetcher creates a URLFetcher with optional configuration.
func NewURLFetcher(opts ...URLFetcherOption) *URLFetcher {
	f := &URLFetcher{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"Source"},
			Produces: []string{"RawContent", "Metadata"},
		}),
		bridge: NewClientBridge(),
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

func (s *URLFetcher) Name() string { return "url_fetcher" }

// isHTTPURL returns true only for http/https URLs.
func isHTTPURL(s string) bool {
	s = strings.TrimSpace(s)
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func (s *URLFetcher) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	rawURL := ""
	// Only use Source if it is a valid http/https URL; non-URL origin strings
	// such as "cli", "argument", "stdin", "clipboard", "import:pinboard" must
	// not be passed to the HTTP client.
	if isHTTPURL(draft.Source) {
		rawURL = strings.TrimSpace(draft.Source)
	}
	if rawURL == "" {
		// Fallback to RawContent if Source is missing/non-URL but content looks like a URL.
		if strings.HasPrefix(strings.TrimSpace(draft.RawContent), "http") {
			rawURL = strings.TrimSpace(draft.RawContent)
		}
	}
	if rawURL == "" {
		log.Printf("url_fetcher: no URL to fetch (Source: %q, RawContent length: %d)", draft.Source, len(draft.RawContent))
		return nil, fmt.Errorf("url_fetcher: no URL to fetch")
	}

	log.Printf("url_fetcher: fetching %s", rawURL)

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("url_fetcher: build request: %w", err)
	}

	resp, err := s.bridge.Do(req)
	if err != nil {
		log.Printf("url_fetcher: fetch error: %v", err)
		wrapped := fmt.Errorf("url_fetcher: fetch %s: %w", rawURL, err)
		if isPermanentNetError(err) {
			return nil, pipeline.Permanent(wrapped)
		}
		return nil, wrapped
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("url_fetcher: unexpected status %d", resp.StatusCode)
		return nil, fmt.Errorf("url_fetcher: unexpected status %d for %s", resp.StatusCode, rawURL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("url_fetcher: read body: %w", err)
	}

	log.Printf("url_fetcher: fetched %d bytes", len(body))

	draft.RawContent = string(body)
	draft.Metadata["final_url"] = resp.Request.URL.String()
	draft.Metadata["content_type"] = resp.Header.Get("Content-Type")

	return draft, nil
}

// permanentPatterns lists substrings that indicate a non-retryable network error.
var permanentPatterns = []string{
	"no such host",
	"tls:",
	"certificate",
	"x509",
}

// isPermanentNetError returns true if the error message (via string matching)
// contains a known non-retryable network failure such as DNS resolution or TLS errors.
func isPermanentNetError(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, pat := range permanentPatterns {
		if strings.Contains(msg, pat) {
			return true
		}
	}
	return false
}
