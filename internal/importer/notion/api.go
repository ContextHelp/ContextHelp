package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultNotionVersion = "2022-06-28"

// Page represents a Notion page returned by the search endpoint.
type Page struct {
	ID             string
	Title          string
	URL            string
	LastEditedTime time.Time
}

// Client is a minimal Notion API client for page import.
type Client struct {
	httpClient    *http.Client
	baseURL       string
	token         string
	notionVersion string
}

// NewClient creates a Notion API client.
func NewClient(httpClient *http.Client, baseURL, token string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.notion.com"
	}

	return &Client{
		httpClient:    httpClient,
		baseURL:       strings.TrimRight(baseURL, "/"),
		token:         token,
		notionVersion: defaultNotionVersion,
	}
}

// SearchPages finds pages shared with the integration token.
func (c *Client) SearchPages(ctx context.Context, query string, since *time.Time, maxItems int) ([]Page, error) {
	if strings.TrimSpace(c.token) == "" {
		return nil, fmt.Errorf("notion token is required")
	}

	var (
		cursor string
		pages  []Page
	)

	for maxItems <= 0 || len(pages) < maxItems {
		pageSize := 100
		if maxItems > 0 && maxItems-len(pages) < pageSize {
			pageSize = maxItems - len(pages)
		}
		if pageSize <= 0 {
			break
		}

		reqBody := map[string]any{
			"query": query,
			"filter": map[string]any{
				"property": "object",
				"value":    "page",
			},
			"sort": map[string]any{
				"direction": "descending",
				"timestamp": "last_edited_time",
			},
			"page_size": pageSize,
		}
		if cursor != "" {
			reqBody["start_cursor"] = cursor
		}

		var resp notionSearchResponse
		if err := c.postJSON(ctx, "/v1/search", reqBody, &resp); err != nil {
			return nil, err
		}

		for _, result := range resp.Results {
			p, ok := toPage(result)
			if !ok {
				continue
			}
			if since != nil && !p.LastEditedTime.IsZero() && p.LastEditedTime.Before(*since) {
				continue
			}
			pages = append(pages, p)

			if maxItems > 0 && len(pages) >= maxItems {
				break
			}
		}

		if !resp.HasMore || resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}

	return pages, nil
}

// GetPage fetches page metadata by page ID.
func (c *Client) GetPage(ctx context.Context, pageID string) (Page, error) {
	if strings.TrimSpace(pageID) == "" {
		return Page{}, fmt.Errorf("page ID is required")
	}

	var resp notionSearchPageResult
	if err := c.getJSON(ctx, "/v1/pages/"+url.PathEscape(pageID), &resp); err != nil {
		return Page{}, err
	}

	p, ok := toPage(resp)
	if !ok {
		return Page{}, fmt.Errorf("notion object %q is not a page", pageID)
	}
	return p, nil
}

// QueryDatabasePages fetches pages from a Notion database.
func (c *Client) QueryDatabasePages(ctx context.Context, databaseID string, since *time.Time, maxItems int) ([]Page, error) {
	if strings.TrimSpace(databaseID) == "" {
		return nil, fmt.Errorf("database ID is required")
	}

	var (
		cursor string
		pages  []Page
	)

	for maxItems <= 0 || len(pages) < maxItems {
		pageSize := 100
		if maxItems > 0 && maxItems-len(pages) < pageSize {
			pageSize = maxItems - len(pages)
		}
		if pageSize <= 0 {
			break
		}

		reqBody := map[string]any{
			"sorts": []map[string]any{
				{
					"timestamp": "last_edited_time",
					"direction": "descending",
				},
			},
			"page_size": pageSize,
		}
		if cursor != "" {
			reqBody["start_cursor"] = cursor
		}

		var resp notionDatabaseQueryResponse
		endpoint := fmt.Sprintf("/v1/databases/%s/query", url.PathEscape(databaseID))
		if err := c.postJSON(ctx, endpoint, reqBody, &resp); err != nil {
			return nil, err
		}

		for _, result := range resp.Results {
			p, ok := toPage(result)
			if !ok {
				continue
			}
			if since != nil && !p.LastEditedTime.IsZero() && p.LastEditedTime.Before(*since) {
				continue
			}
			pages = append(pages, p)

			if maxItems > 0 && len(pages) >= maxItems {
				break
			}
		}

		if !resp.HasMore || resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}

	return pages, nil
}

// PageContent fetches markdown-like plain text content for a page.
func (c *Client) PageContent(ctx context.Context, pageID string) (string, error) {
	if strings.TrimSpace(pageID) == "" {
		return "", fmt.Errorf("page ID is required")
	}

	var (
		cursor string
		lines  []string
	)

	for {
		endpoint := fmt.Sprintf("/v1/blocks/%s/children?page_size=100", url.PathEscape(pageID))
		if cursor != "" {
			endpoint += "&start_cursor=" + url.QueryEscape(cursor)
		}

		var resp notionBlocksResponse
		if err := c.getJSON(ctx, endpoint, &resp); err != nil {
			return "", err
		}

		for _, block := range resp.Results {
			text := strings.TrimSpace(extractBlockText(block))
			if text != "" {
				lines = append(lines, text)
			}
		}

		if !resp.HasMore || resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}

	return strings.Join(lines, "\n\n"), nil
}

// RenderContent turns page metadata + text into an ingestion payload.
func RenderContent(page Page, body string) string {
	var b strings.Builder

	title := strings.TrimSpace(page.Title)
	if title == "" {
		title = page.ID
	}
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")

	if page.URL != "" {
		b.WriteString("Source: ")
		b.WriteString(page.URL)
		b.WriteString("\n\n")
	}

	if !page.LastEditedTime.IsZero() {
		b.WriteString("Last Edited: ")
		b.WriteString(page.LastEditedTime.UTC().Format(time.RFC3339))
		b.WriteString("\n\n")
	}

	body = strings.TrimSpace(body)
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	}

	return b.String()
}

func (c *Client) notionRequest(ctx context.Context, method, endpoint string, body io.Reader) (*http.Response, error) {
	fullURL := c.baseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, fmt.Errorf("create notion request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Notion-Version", c.notionVersion)
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("notion request failed: %w", err)
	}
	return resp, nil
}

func (c *Client) postJSON(ctx context.Context, endpoint string, reqBody any, out any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal notion request: %w", err)
	}
	resp, err := c.notionRequest(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	return decodeNotionResponse(resp, out)
}

func (c *Client) getJSON(ctx context.Context, endpoint string, out any) error {
	resp, err := c.notionRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	return decodeNotionResponse(resp, out)
}

func decodeNotionResponse(resp *http.Response, out any) error {
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read notion response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("notion API returned %d: %s", resp.StatusCode, msg)
	}

	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode notion response: %w", err)
	}
	return nil
}

type notionSearchResponse struct {
	Results    []notionSearchPageResult `json:"results"`
	HasMore    bool                     `json:"has_more"`
	NextCursor string                   `json:"next_cursor"`
}

type notionSearchPageResult struct {
	Object         string         `json:"object"`
	ID             string         `json:"id"`
	URL            string         `json:"url"`
	LastEditedTime string         `json:"last_edited_time"`
	Properties     map[string]any `json:"properties"`
}

type notionBlocksResponse struct {
	Results    []map[string]any `json:"results"`
	HasMore    bool             `json:"has_more"`
	NextCursor string           `json:"next_cursor"`
}

type notionDatabaseQueryResponse struct {
	Results    []notionSearchPageResult `json:"results"`
	HasMore    bool                     `json:"has_more"`
	NextCursor string                   `json:"next_cursor"`
}

func toPage(result notionSearchPageResult) (Page, bool) {
	if result.Object != "page" {
		return Page{}, false
	}

	edited, _ := time.Parse(time.RFC3339, result.LastEditedTime)
	p := Page{
		ID:             result.ID,
		Title:          extractPageTitle(result.Properties),
		URL:            result.URL,
		LastEditedTime: edited,
	}
	if p.Title == "" {
		p.Title = p.ID
	}
	return p, true
}

func extractPageTitle(properties map[string]any) string {
	for _, raw := range properties {
		prop, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		t, _ := prop["type"].(string)
		if t != "title" {
			continue
		}
		titleArr, ok := prop["title"].([]any)
		if !ok || len(titleArr) == 0 {
			return ""
		}
		var b strings.Builder
		for _, item := range titleArr {
			rt, ok := item.(map[string]any)
			if !ok {
				continue
			}
			plain, _ := rt["plain_text"].(string)
			b.WriteString(plain)
		}
		return strings.TrimSpace(b.String())
	}
	return ""
}

func extractBlockText(block map[string]any) string {
	blockType, _ := block["type"].(string)
	if blockType == "" {
		return ""
	}

	payloadRaw, ok := block[blockType]
	if !ok {
		return ""
	}
	payload, ok := payloadRaw.(map[string]any)
	if !ok {
		return ""
	}

	richTextRaw, ok := payload["rich_text"]
	if !ok {
		return ""
	}
	items, ok := richTextRaw.([]any)
	if !ok {
		return ""
	}

	var b strings.Builder
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		plain, _ := m["plain_text"].(string)
		if plain != "" {
			b.WriteString(plain)
		}
	}
	return strings.TrimSpace(b.String())
}
