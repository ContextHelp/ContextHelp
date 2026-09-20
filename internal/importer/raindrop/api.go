package raindrop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.raindrop.io"

// Item is a normalized Raindrop bookmark item.
type Item struct {
	ID           int64
	Title        string
	Link         string
	Type         string
	Domain       string
	Excerpt      string
	Note         string
	Tags         []string
	Highlights   []string
	CollectionID int64
	Created      time.Time
	LastUpdate   time.Time
}

// Collection is a normalized Raindrop collection.
type Collection struct {
	ID       int64
	Title    string
	ParentID int64
}

// ListOptions controls Raindrop item selection.
type ListOptions struct {
	CollectionIDs []int64
	Query         string
	Tag           string
	Since         *time.Time
	MaxItems      int
}

// Client is a minimal Raindrop API client used by import flows.
type Client struct {
	httpClient *http.Client
	baseURL    string
	token      string
}

// NewClient creates a Raindrop API client.
func NewClient(httpClient *http.Client, baseURL, token string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
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

// ListCollections fetches Raindrop collections available to the token.
func (c *Client) ListCollections(ctx context.Context) ([]Collection, error) {
	if c.token == "" {
		return nil, fmt.Errorf("raindrop token is required")
	}

	var resp raindropCollectionsResponse
	if err := c.getJSON(ctx, "/rest/v1/collections", &resp); err != nil {
		return nil, err
	}

	out := make([]Collection, 0, len(resp.Items))
	for _, raw := range resp.Items {
		collection, ok := toCollection(raw)
		if !ok {
			continue
		}
		out = append(out, collection)
	}

	return out, nil
}

// ListItems fetches bookmark items from the selected collections and applies client-side filters.
func (c *Client) ListItems(ctx context.Context, opts ListOptions) ([]Item, error) {
	if c.token == "" {
		return nil, fmt.Errorf("raindrop token is required")
	}
	if len(opts.CollectionIDs) == 0 {
		return nil, fmt.Errorf("at least one collection ID is required")
	}

	uniqueCollections := dedupeInt64(opts.CollectionIDs)
	query := strings.ToLower(strings.TrimSpace(opts.Query))
	tag := strings.ToLower(strings.TrimSpace(opts.Tag))

	out := make([]Item, 0, 128)
	seenByID := make(map[int64]struct{})

	for _, collectionID := range uniqueCollections {
		if opts.MaxItems > 0 && len(out) >= opts.MaxItems {
			break
		}

		remaining := 0
		if opts.MaxItems > 0 && query == "" && tag == "" && opts.Since == nil {
			remaining = opts.MaxItems - len(out)
		}

		items, err := c.listCollectionItems(ctx, collectionID, remaining)
		if err != nil {
			return nil, fmt.Errorf("list raindrop collection %d: %w", collectionID, err)
		}

		for _, item := range items {
			if item.ID != 0 {
				if _, exists := seenByID[item.ID]; exists {
					continue
				}
				seenByID[item.ID] = struct{}{}
			}

			if opts.Since != nil && !item.LastUpdate.IsZero() && item.LastUpdate.Before(*opts.Since) {
				continue
			}
			if tag != "" && !itemHasTag(item, tag) {
				continue
			}
			if query != "" && !itemMatchesQuery(item, query) {
				continue
			}

			out = append(out, item)
			if opts.MaxItems > 0 && len(out) >= opts.MaxItems {
				break
			}
		}
	}

	if opts.MaxItems > 0 && len(out) > opts.MaxItems {
		out = out[:opts.MaxItems]
	}

	return out, nil
}

// BuildCollectionPathMap resolves collection IDs to display paths (for example "Engineering/Research").
func BuildCollectionPathMap(collections []Collection) map[int64]string {
	byID := make(map[int64]Collection, len(collections))
	for _, c := range collections {
		if c.ID != 0 {
			byID[c.ID] = c
		}
	}

	out := make(map[int64]string, len(byID))
	visiting := make(map[int64]bool)

	var resolve func(id int64) string
	resolve = func(id int64) string {
		if id == 0 {
			return ""
		}
		if path, ok := out[id]; ok {
			return path
		}

		c, ok := byID[id]
		if !ok {
			return strconv.FormatInt(id, 10)
		}

		name := strings.TrimSpace(c.Title)
		if name == "" {
			name = strconv.FormatInt(id, 10)
		}

		if c.ParentID == 0 || c.ParentID == id {
			out[id] = name
			return name
		}

		if visiting[id] {
			out[id] = name
			return name
		}
		visiting[id] = true
		parentPath := resolve(c.ParentID)
		visiting[id] = false

		if parentPath == "" {
			out[id] = name
			return name
		}

		full := parentPath + "/" + name
		out[id] = full
		return full
	}

	for id := range byID {
		resolve(id)
	}

	return out
}

// RenderContent creates a deterministic text payload for ingestion.
func RenderContent(item Item, collectionPath string) string {
	var b strings.Builder

	title := strings.TrimSpace(item.Title)
	if title == "" {
		if item.Link != "" {
			title = item.Link
		} else {
			title = fmt.Sprintf("Raindrop %d", item.ID)
		}
	}
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")

	if item.Link != "" {
		b.WriteString("Source: ")
		b.WriteString(item.Link)
		b.WriteString("\n\n")
	}

	b.WriteString("Provider: Raindrop.io\n")
	if item.ID != 0 {
		b.WriteString("Item ID: ")
		b.WriteString(strconv.FormatInt(item.ID, 10))
		b.WriteString("\n")
	}
	if item.Type != "" {
		b.WriteString("Type: ")
		b.WriteString(item.Type)
		b.WriteString("\n")
	}
	if collectionPath != "" {
		b.WriteString("Collection: ")
		b.WriteString(collectionPath)
		b.WriteString("\n")
	} else if item.CollectionID != 0 {
		b.WriteString("Collection ID: ")
		b.WriteString(strconv.FormatInt(item.CollectionID, 10))
		b.WriteString("\n")
	}
	if item.Domain != "" {
		b.WriteString("Domain: ")
		b.WriteString(item.Domain)
		b.WriteString("\n")
	}
	if len(item.Tags) > 0 {
		b.WriteString("Tags: ")
		b.WriteString(strings.Join(item.Tags, ", "))
		b.WriteString("\n")
	}
	if !item.Created.IsZero() {
		b.WriteString("Created: ")
		b.WriteString(item.Created.UTC().Format(time.RFC3339))
		b.WriteString("\n")
	}
	if !item.LastUpdate.IsZero() {
		b.WriteString("Last Updated: ")
		b.WriteString(item.LastUpdate.UTC().Format(time.RFC3339))
		b.WriteString("\n")
	}

	if text := strings.TrimSpace(item.Excerpt); text != "" {
		b.WriteString("\nExcerpt:\n")
		b.WriteString(text)
		b.WriteString("\n")
	}
	if text := strings.TrimSpace(item.Note); text != "" {
		b.WriteString("\nNote:\n")
		b.WriteString(text)
		b.WriteString("\n")
	}
	if len(item.Highlights) > 0 {
		b.WriteString("\nHighlights:\n")
		for _, highlight := range item.Highlights {
			highlight = strings.TrimSpace(highlight)
			if highlight == "" {
				continue
			}
			b.WriteString("- ")
			b.WriteString(highlight)
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (c *Client) listCollectionItems(ctx context.Context, collectionID int64, maxItems int) ([]Item, error) {
	page := 0
	out := make([]Item, 0, 128)

	for maxItems <= 0 || len(out) < maxItems {
		perPage := 50
		if maxItems > 0 && maxItems-len(out) < perPage {
			perPage = maxItems - len(out)
		}
		if perPage <= 0 {
			break
		}

		params := url.Values{}
		params.Set("page", strconv.Itoa(page))
		params.Set("perpage", strconv.Itoa(perPage))

		endpoint := fmt.Sprintf("/rest/v1/raindrops/%d?%s", collectionID, params.Encode())

		var resp raindropItemsResponse
		if err := c.getJSON(ctx, endpoint, &resp); err != nil {
			return nil, err
		}

		for _, raw := range resp.Items {
			item, ok := toItem(raw)
			if !ok {
				continue
			}
			out = append(out, item)
			if maxItems > 0 && len(out) >= maxItems {
				break
			}
		}

		if len(resp.Items) == 0 {
			break
		}
		if resp.Count > 0 && (page+1)*perPage >= resp.Count {
			break
		}
		if len(resp.Items) < perPage {
			break
		}

		page++
	}

	return out, nil
}

func itemMatchesQuery(item Item, query string) bool {
	fields := []string{
		item.Title,
		item.Link,
		item.Excerpt,
		item.Note,
		item.Domain,
		strings.Join(item.Tags, " "),
		strings.Join(item.Highlights, " "),
	}

	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

func itemHasTag(item Item, tag string) bool {
	for _, existing := range item.Tags {
		if strings.EqualFold(strings.TrimSpace(existing), tag) {
			return true
		}
	}
	return false
}

func dedupeInt64(values []int64) []int64 {
	out := make([]int64, 0, len(values))
	seen := make(map[int64]struct{}, len(values))
	for _, v := range values {
		if _, exists := seen[v]; exists {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func (c *Client) raindropRequest(ctx context.Context, endpoint string) (*http.Response, error) {
	fullURL := strings.TrimSpace(endpoint)
	if fullURL == "" {
		return nil, fmt.Errorf("raindrop endpoint is required")
	}
	if !strings.HasPrefix(fullURL, "http://") && !strings.HasPrefix(fullURL, "https://") {
		fullURL = c.baseURL + fullURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create raindrop request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("raindrop request failed: %w", err)
	}
	return resp, nil
}

func (c *Client) getJSON(ctx context.Context, endpoint string, out any) error {
	resp, err := c.raindropRequest(ctx, endpoint)
	if err != nil {
		return err
	}
	return decodeRaindropResponse(resp, out)
}

func decodeRaindropResponse(resp *http.Response, out any) error {
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read raindrop response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("raindrop API returned %d: %s", resp.StatusCode, msg)
	}

	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode raindrop response: %w", err)
	}
	return nil
}

func toCollection(raw map[string]any) (Collection, bool) {
	id := asInt64(raw["_id"])
	if id == 0 {
		return Collection{}, false
	}

	parentID := int64(0)
	if parentRaw, ok := raw["parent"].(map[string]any); ok {
		parentID = asInt64(parentRaw["$id"])
	}

	title := strings.TrimSpace(asString(raw["title"]))
	if title == "" {
		title = strconv.FormatInt(id, 10)
	}

	return Collection{
		ID:       id,
		Title:    title,
		ParentID: parentID,
	}, true
}

func toItem(raw map[string]any) (Item, bool) {
	id := asInt64(raw["_id"])
	title := strings.TrimSpace(asString(raw["title"]))
	link := strings.TrimSpace(asString(raw["link"]))
	if id == 0 && title == "" && link == "" {
		return Item{}, false
	}

	if title == "" {
		if link != "" {
			title = link
		} else {
			title = fmt.Sprintf("Raindrop %d", id)
		}
	}

	collectionID := int64(0)
	if collectionRaw, ok := raw["collection"].(map[string]any); ok {
		collectionID = asInt64(collectionRaw["$id"])
	}

	return Item{
		ID:           id,
		Title:        title,
		Link:         link,
		Type:         strings.TrimSpace(asString(raw["type"])),
		Domain:       strings.TrimSpace(asString(raw["domain"])),
		Excerpt:      strings.TrimSpace(asString(raw["excerpt"])),
		Note:         strings.TrimSpace(asString(raw["note"])),
		Tags:         asStringSlice(raw["tags"]),
		Highlights:   asHighlights(raw["highlights"]),
		CollectionID: collectionID,
		Created:      parseTime(asString(raw["created"])),
		LastUpdate:   parseTime(asString(raw["lastUpdate"])),
	}, true
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}

func asInt64(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case json.Number:
		n, _ := t.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		return n
	default:
		return 0
	}
}

func asStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, raw := range arr {
		str := strings.TrimSpace(asString(raw))
		if str == "" {
			continue
		}
		out = append(out, str)
	}
	return out
}

func asHighlights(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}

	out := make([]string, 0, len(arr))
	for _, raw := range arr {
		switch h := raw.(type) {
		case string:
			h = strings.TrimSpace(h)
			if h != "" {
				out = append(out, h)
			}
		case map[string]any:
			for _, key := range []string{"text", "note", "title", "quote"} {
				if text := strings.TrimSpace(asString(h[key])); text != "" {
					out = append(out, text)
					break
				}
			}
		}
	}
	return out
}

func parseTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
	}
	for _, format := range formats {
		if ts, err := time.Parse(format, raw); err == nil {
			return ts
		}
	}
	return time.Time{}
}

type raindropItemsResponse struct {
	Result bool             `json:"result"`
	Items  []map[string]any `json:"items"`
	Count  int              `json:"count"`
}

type raindropCollectionsResponse struct {
	Result bool             `json:"result"`
	Items  []map[string]any `json:"items"`
}
