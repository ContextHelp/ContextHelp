package pinboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

const (
	defaultBaseURL     = "https://api.pinboard.in"
	pinboardTimeLayout = "2006-01-02T15:04:05Z"
	minHTTPTimeoutSecs = 20
)

// Bookmark is a normalized Pinboard bookmark record.
type Bookmark struct {
	URL    string
	Title  string
	Notes  string
	Tags   []string
	Time   time.Time
	Hash   string
	Shared bool
	ToRead bool
}

// FetchOptions controls Pinboard API result filtering.
type FetchOptions struct {
	Since    *time.Time
	Tags     []string
	MaxItems int
}

// Client is a minimal Pinboard API client used by importer flows.
type Client struct {
	httpClient *http.Client
	baseURL    string
	token      string
}

// NewClient creates a Pinboard API client.
func NewClient(httpClient *http.Client, baseURL, token string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: minHTTPTimeoutSecs * time.Second}
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBaseURL
	}

	return &Client{
		httpClient: httpClient,
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      strings.TrimSpace(token),
	}
}

// FetchPosts fetches bookmarks from Pinboard's posts/all endpoint.
func (c *Client) FetchPosts(ctx context.Context, opts FetchOptions) ([]Bookmark, error) {
	if strings.TrimSpace(c.token) == "" {
		return nil, fmt.Errorf("pinboard token is required")
	}

	endpoint, err := url.Parse(c.baseURL + "/v1/posts/all")
	if err != nil {
		return nil, fmt.Errorf("parse pinboard endpoint: %w", err)
	}

	query := endpoint.Query()
	query.Set("auth_token", c.token)
	query.Set("format", "json")
	if opts.Since != nil {
		query.Set("fromdt", opts.Since.UTC().Format(pinboardTimeLayout))
	}
	tagFilter := normalizeTagFilter(opts.Tags)
	if len(tagFilter) > 0 {
		query.Set("tag", strings.Join(tagFilter, " "))
	}
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create pinboard request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pinboard request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read pinboard response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pinboard API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	bookmarks, err := parseExport(body)
	if err != nil {
		return nil, err
	}

	return FilterBookmarks(bookmarks, opts.Since, tagFilter, opts.MaxItems), nil
}

// ParseExportFile parses a Pinboard JSON export file.
func ParseExportFile(path string) ([]Bookmark, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read pinboard file: %w", err)
	}
	return parseExport(data)
}

// FilterBookmarks applies since/tag/max filters.
func FilterBookmarks(bookmarks []Bookmark, since *time.Time, tags []string, maxItems int) []Bookmark {
	out := make([]Bookmark, 0, len(bookmarks))
	requiredTags := normalizeTagFilter(tags)

	for _, bookmark := range bookmarks {
		if since != nil && !bookmark.Time.IsZero() && bookmark.Time.Before(*since) {
			continue
		}
		if len(requiredTags) > 0 && !containsAllTags(bookmark.Tags, requiredTags) {
			continue
		}

		out = append(out, bookmark)
		if maxItems > 0 && len(out) >= maxItems {
			break
		}
	}

	return out
}

// RenderContent builds a text payload preserving bookmark metadata.
func RenderContent(bookmark Bookmark) string {
	title := strings.TrimSpace(bookmark.Title)
	if title == "" {
		title = strings.TrimSpace(bookmark.URL)
	}
	if title == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")

	if strings.TrimSpace(bookmark.URL) != "" {
		b.WriteString("URL: ")
		b.WriteString(strings.TrimSpace(bookmark.URL))
		b.WriteString("\n")
	}
	b.WriteString("Source: pinboard\n")
	if strings.TrimSpace(bookmark.Hash) != "" {
		b.WriteString("External ID: ")
		b.WriteString(strings.TrimSpace(bookmark.Hash))
		b.WriteString("\n")
	}
	if !bookmark.Time.IsZero() {
		b.WriteString("Saved: ")
		b.WriteString(bookmark.Time.UTC().Format(time.RFC3339))
		b.WriteString("\n")
	}
	if len(bookmark.Tags) > 0 {
		b.WriteString("Tags: ")
		b.WriteString(strings.Join(bookmark.Tags, ", "))
		b.WriteString("\n")
	}
	b.WriteString("Shared: ")
	if bookmark.Shared {
		b.WriteString("yes")
	} else {
		b.WriteString("no")
	}
	b.WriteString("\n")
	b.WriteString("Read Later: ")
	if bookmark.ToRead {
		b.WriteString("yes")
	} else {
		b.WriteString("no")
	}
	b.WriteString("\n")

	if strings.TrimSpace(bookmark.Notes) != "" {
		b.WriteString("\nNotes:\n")
		b.WriteString(strings.TrimSpace(bookmark.Notes))
		b.WriteString("\n")
	}

	return b.String()
}

type pinboardPost struct {
	URL         string `json:"href"`
	Description string `json:"description"`
	Extended    string `json:"extended"`
	Hash        string `json:"hash"`
	Time        string `json:"time"`
	Shared      string `json:"shared"`
	ToRead      string `json:"toread"`
	Tags        string `json:"tags"`
}

func parseExport(data []byte) ([]Bookmark, error) {
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		return nil, fmt.Errorf("pinboard input is empty")
	}

	var posts []pinboardPost
	if err := json.Unmarshal(data, &posts); err != nil {
		var payload struct {
			Posts []pinboardPost `json:"posts"`
		}
		if errObj := json.Unmarshal(data, &payload); errObj != nil {
			return nil, fmt.Errorf("decode pinboard export: %w", err)
		}
		posts = payload.Posts
	}

	out := make([]Bookmark, 0, len(posts))
	for _, post := range posts {
		out = append(out, toBookmark(post))
	}
	return out, nil
}

func toBookmark(post pinboardPost) Bookmark {
	savedAt := parseBookmarkTime(post.Time)
	title := strings.TrimSpace(post.Description)
	urlValue := strings.TrimSpace(post.URL)
	if title == "" {
		title = urlValue
	}

	return Bookmark{
		URL:    urlValue,
		Title:  title,
		Notes:  strings.TrimSpace(post.Extended),
		Tags:   normalizeTagFilter(strings.Fields(post.Tags)),
		Time:   savedAt,
		Hash:   strings.TrimSpace(post.Hash),
		Shared: parsePinboardBool(post.Shared),
		ToRead: parsePinboardBool(post.ToRead),
	}
}

func parseBookmarkTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}

	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed
	}
	if parsed, err := time.Parse(pinboardTimeLayout, raw); err == nil {
		return parsed
	}
	return time.Time{}
}

func parsePinboardBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "yes", "true", "1":
		return true
	default:
		return false
	}
}

func normalizeTagFilter(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		if !slices.Contains(out, tag) {
			out = append(out, tag)
		}
	}
	return out
}

func containsAllTags(bookmarkTags []string, required []string) bool {
	if len(required) == 0 {
		return true
	}
	current := make(map[string]struct{}, len(bookmarkTags))
	for _, tag := range bookmarkTags {
		current[strings.ToLower(strings.TrimSpace(tag))] = struct{}{}
	}
	for _, req := range required {
		if _, ok := current[req]; !ok {
			return false
		}
	}
	return true
}
