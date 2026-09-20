// Package dropbox provides a minimal Dropbox API client for import flows.
// It supports cursor-based incremental sync via list_folder/continue.
package dropbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultAPIBase     = "https://api.dropboxapi.com"
	defaultContentBase = "https://content.dropboxapi.com"
)

// File represents a Dropbox file metadata record.
type File struct {
	ID           string
	Name         string
	Path         string
	Size         int64
	ModifiedTime time.Time
	ContentHash  string
	// IsFolder is true when this entry is a folder (not downloadable).
	IsFolder bool
}

// ListOptions controls which files are returned.
type ListOptions struct {
	// Path is the folder path to list (empty string means root "").
	Path string
	// Recursive lists all sub-folders recursively.
	Recursive bool
	// Since filters files modified on/after this time. Requires scanning all
	// results because the Dropbox API does not support server-side time filtering.
	Since *time.Time
	// MaxItems caps the total number of files returned (0 = all).
	MaxItems int
	// Cursor is a previously saved cursor for incremental sync.
	// When non-empty the client resumes from this cursor via list_folder/continue.
	Cursor string
}

// SyncResult is returned by ListFiles and includes a cursor for the next
// incremental sync run.
type SyncResult struct {
	Files  []File
	Cursor string
}

// Client is a minimal Dropbox API client for import flows.
type Client struct {
	httpClient  *http.Client
	apiBase     string
	contentBase string
	token       string
}

// NewClient creates a Dropbox API client.
// httpClient may be nil (uses a default with 30 s timeout).
// apiBase/contentBase may be empty (use Dropbox production endpoints).
func NewClient(httpClient *http.Client, apiBase, contentBase, token string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if strings.TrimSpace(apiBase) == "" {
		apiBase = defaultAPIBase
	}
	if strings.TrimSpace(contentBase) == "" {
		contentBase = defaultContentBase
	}
	return &Client{
		httpClient:  httpClient,
		apiBase:     strings.TrimRight(apiBase, "/"),
		contentBase: strings.TrimRight(contentBase, "/"),
		token:       strings.TrimSpace(token),
	}
}

// ListFiles fetches the contents of a Dropbox folder using cursor-based
// pagination. It returns a SyncResult that includes an updated cursor for the
// next incremental run.
//
// When opts.Cursor is non-empty the call resumes from that cursor via
// list_folder/continue, ignoring opts.Path and opts.Recursive (the cursor
// already encodes those settings).
func (c *Client) ListFiles(ctx context.Context, opts ListOptions) (SyncResult, error) {
	if c.token == "" {
		return SyncResult{}, fmt.Errorf("dropbox access token is required")
	}

	var (
		entries []listFolderEntry
		cursor  string
		err     error
	)

	if opts.Cursor != "" {
		entries, cursor, err = c.continueListing(ctx, opts.Cursor, opts.MaxItems)
	} else {
		entries, cursor, err = c.startListing(ctx, opts)
	}
	if err != nil {
		return SyncResult{}, err
	}

	files := make([]File, 0, len(entries))
	for _, e := range entries {
		if e.Tag == "deleted" {
			continue
		}
		f := toFile(e)
		if opts.Since != nil && !f.IsFolder && !f.ModifiedTime.IsZero() {
			if f.ModifiedTime.Before(*opts.Since) {
				continue
			}
		}
		files = append(files, f)
	}

	return SyncResult{Files: files, Cursor: cursor}, nil
}

// DownloadFile downloads the content of a Dropbox file by path.
func (c *Client) DownloadFile(ctx context.Context, path string) ([]byte, error) {
	if c.token == "" {
		return nil, fmt.Errorf("dropbox access token is required")
	}
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("dropbox file path is required")
	}

	endpoint := c.contentBase + "/2/files/download"

	argJSON, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		return nil, fmt.Errorf("dropbox download: encode arg: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("dropbox download: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Dropbox-API-Arg", string(argJSON))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dropbox download %q: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("dropbox download %q: read body: %w", path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, classifyError(resp.StatusCode, body, "download "+path)
	}

	return body, nil
}

// --- internal helpers ---

func (c *Client) startListing(ctx context.Context, opts ListOptions) ([]listFolderEntry, string, error) {
	path := strings.TrimSpace(opts.Path)
	// Dropbox requires "" for root, not "/".
	if path == "/" {
		path = ""
	}

	payload := map[string]any{
		"path":      path,
		"recursive": opts.Recursive,
		"limit":     2000,
	}

	var entries []listFolderEntry
	cursor, err := c.paginateListFolder(ctx, c.apiBase+"/2/files/list_folder", payload, &entries, opts.MaxItems)
	return entries, cursor, err
}

func (c *Client) continueListing(ctx context.Context, cursor string, maxItems int) ([]listFolderEntry, string, error) {
	payload := map[string]string{"cursor": cursor}

	var entries []listFolderEntry
	newCursor, err := c.paginateListFolder(ctx, c.apiBase+"/2/files/list_folder/continue", payload, &entries, maxItems)
	return entries, newCursor, err
}

// paginateListFolder handles pagination for both list_folder and
// list_folder/continue. The first call uses startURL; subsequent continuation
// calls always use list_folder/continue.
func (c *Client) paginateListFolder(
	ctx context.Context,
	startURL string,
	firstPayload any,
	out *[]listFolderEntry,
	maxItems int,
) (string, error) {
	url := startURL
	payload := firstPayload
	first := true

	for maxItems <= 0 || len(*out) < maxItems {
		body, err := c.doJSONPost(ctx, url, payload)
		if err != nil {
			return "", err
		}

		var result listFolderResult
		if err := json.Unmarshal(body, &result); err != nil {
			return "", fmt.Errorf("dropbox list_folder: decode response: %w", err)
		}

		for _, e := range result.Entries {
			*out = append(*out, e)
			if maxItems > 0 && len(*out) >= maxItems {
				break
			}
		}

		if !result.HasMore {
			return result.Cursor, nil
		}

		// Subsequent pages always go through list_folder/continue.
		if first {
			url = c.apiBase + "/2/files/list_folder/continue"
			first = false
		}
		payload = map[string]string{"cursor": result.Cursor}
	}

	// Return the latest cursor even if we stopped early due to maxItems.
	// Caller can resume from here.
	body, err := c.doJSONPost(ctx, c.apiBase+"/2/files/list_folder/get_latest_cursor", firstPayload)
	if err == nil {
		var lc latestCursorResult
		if err2 := json.Unmarshal(body, &lc); err2 == nil && lc.Cursor != "" {
			return lc.Cursor, nil
		}
	}
	// Best-effort: return an empty cursor so the next run does a fresh list.
	return "", nil
}

func (c *Client) doJSONPost(ctx context.Context, endpoint string, payload any) ([]byte, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("dropbox: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("dropbox: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dropbox request to %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("dropbox: read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, classifyError(resp.StatusCode, data, endpoint)
	}

	return data, nil
}

// classifyError maps Dropbox HTTP status codes to actionable error messages.
func classifyError(status int, body []byte, context string) error {
	msg := strings.TrimSpace(string(body))
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("dropbox auth error (401) for %s: token may be expired or revoked", context)
	case http.StatusForbidden:
		return fmt.Errorf("dropbox permission denied (403) for %s: check token scopes", context)
	case http.StatusTooManyRequests:
		return fmt.Errorf("dropbox rate limit (429) for %s: back off and retry", context)
	default:
		if msg == "" {
			msg = "(empty)"
		}
		return fmt.Errorf("dropbox API returned %d for %s: %s", status, context, msg)
	}
}

// --- Dropbox API response types ---

type listFolderResult struct {
	Entries []listFolderEntry `json:"entries"`
	Cursor  string            `json:"cursor"`
	HasMore bool              `json:"has_more"`
}

type listFolderEntry struct {
	// ".tag" is one of "file", "folder", "deleted".
	Tag            string `json:".tag"`
	Name           string `json:"name"`
	PathLower      string `json:"path_lower"`
	PathDisplay    string `json:"path_display"`
	ID             string `json:"id"`
	Size           int64  `json:"size"`
	ServerModified string `json:"server_modified"`
	ContentHash    string `json:"content_hash"`
}

type latestCursorResult struct {
	Cursor string `json:"cursor"`
}

func toFile(e listFolderEntry) File {
	f := File{
		ID:          e.ID,
		Name:        e.Name,
		Path:        e.PathDisplay,
		Size:        e.Size,
		ContentHash: e.ContentHash,
		IsFolder:    e.Tag == "folder",
	}
	if e.PathDisplay == "" {
		f.Path = e.PathLower
	}
	if ts, err := time.Parse("2006-01-02T15:04:05Z", e.ServerModified); err == nil {
		f.ModifiedTime = ts
	}
	return f
}

// IsSupportedExtension reports whether a file path has a text-extractable extension.
func IsSupportedExtension(path string) bool {
	lower := strings.ToLower(path)
	for _, ext := range supportedExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

var supportedExtensions = []string{
	".md", ".markdown", ".txt", ".text", ".rst",
	".pdf",
	".docx", ".xlsx", ".pptx",
	".csv", ".tsv",
	".json", ".jsonl",
	".html", ".htm",
}
