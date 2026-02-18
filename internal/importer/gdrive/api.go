package gdrive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultBaseURL = "https://www.googleapis.com"

// File represents a Google Drive file metadata record.
type File struct {
	ID             string
	Name           string
	MimeType       string
	ModifiedTime   time.Time
	WebViewLink    string
	WebContentLink string
}

// ListOptions controls which files are returned.
type ListOptions struct {
	Query          string
	FolderIDs      []string
	MimeTypes      []string
	Since          *time.Time
	IncludeTrashed bool
	MaxItems       int
}

// Client is a minimal Google Drive API client for import flows.
type Client struct {
	httpClient *http.Client
	baseURL    string
	token      string
}

// NewClient creates a Google Drive client.
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

// ListFiles fetches Drive files using list pagination and optional scope filters.
func (c *Client) ListFiles(ctx context.Context, opts ListOptions) ([]File, error) {
	if c.token == "" {
		return nil, fmt.Errorf("google drive access token is required")
	}

	query := buildQuery(opts)
	pageToken := ""
	out := make([]File, 0, 128)

	for {
		if opts.MaxItems > 0 && len(out) >= opts.MaxItems {
			break
		}

		pageSize := 100
		if opts.MaxItems > 0 && opts.MaxItems-len(out) < pageSize {
			pageSize = opts.MaxItems - len(out)
		}
		if pageSize <= 0 {
			break
		}

		endpoint := c.baseURL + "/drive/v3/files"
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, fmt.Errorf("parse drive list url: %w", err)
		}

		params := u.Query()
		params.Set("fields", "nextPageToken,files(id,name,mimeType,modifiedTime,webViewLink,webContentLink)")
		params.Set("pageSize", fmt.Sprintf("%d", pageSize))
		params.Set("supportsAllDrives", "true")
		params.Set("includeItemsFromAllDrives", "true")
		if query != "" {
			params.Set("q", query)
		}
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}
		u.RawQuery = params.Encode()

		resp, err := c.doJSONRequest(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}

		var parsed listFilesResponse
		if err := json.Unmarshal(resp, &parsed); err != nil {
			return nil, fmt.Errorf("decode drive list response: %w", err)
		}

		for _, f := range parsed.Files {
			out = append(out, toFile(f))
			if opts.MaxItems > 0 && len(out) >= opts.MaxItems {
				break
			}
		}

		if parsed.NextPageToken == "" {
			break
		}
		pageToken = parsed.NextPageToken
	}

	return out, nil
}

// ExportText exports Google Workspace-native files to plain text.
func (c *Client) ExportText(ctx context.Context, fileID, mimeType string) (string, error) {
	if c.token == "" {
		return "", fmt.Errorf("google drive access token is required")
	}
	if strings.TrimSpace(fileID) == "" {
		return "", fmt.Errorf("file ID is required")
	}
	if strings.TrimSpace(mimeType) == "" {
		return "", fmt.Errorf("mime type is required")
	}

	endpoint := fmt.Sprintf("%s/drive/v3/files/%s/export?mimeType=%s", c.baseURL, url.PathEscape(fileID), url.QueryEscape(mimeType))
	resp, err := c.doRawRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	return string(resp), nil
}

func buildQuery(opts ListOptions) string {
	var parts []string

	if !opts.IncludeTrashed {
		parts = append(parts, "trashed = false")
	}
	if opts.Since != nil {
		parts = append(parts, fmt.Sprintf("modifiedTime >= '%s'", opts.Since.UTC().Format(time.RFC3339)))
	}

	if len(opts.FolderIDs) > 0 {
		folderExprs := make([]string, 0, len(opts.FolderIDs))
		for _, id := range opts.FolderIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			folderExprs = append(folderExprs, fmt.Sprintf("'%s' in parents", escapeDriveQueryLiteral(id)))
		}
		if len(folderExprs) == 1 {
			parts = append(parts, folderExprs[0])
		} else if len(folderExprs) > 1 {
			parts = append(parts, "("+strings.Join(folderExprs, " or ")+")")
		}
	}

	if len(opts.MimeTypes) > 0 {
		mimeExprs := make([]string, 0, len(opts.MimeTypes))
		for _, mt := range opts.MimeTypes {
			mt = strings.TrimSpace(mt)
			if mt == "" {
				continue
			}
			mimeExprs = append(mimeExprs, fmt.Sprintf("mimeType = '%s'", escapeDriveQueryLiteral(mt)))
		}
		if len(mimeExprs) == 1 {
			parts = append(parts, mimeExprs[0])
		} else if len(mimeExprs) > 1 {
			parts = append(parts, "("+strings.Join(mimeExprs, " or ")+")")
		}
	}

	if raw := strings.TrimSpace(opts.Query); raw != "" {
		parts = append(parts, "("+raw+")")
	}

	return strings.Join(parts, " and ")
}

func escapeDriveQueryLiteral(v string) string {
	return strings.ReplaceAll(v, "'", "\\'")
}

func (c *Client) doJSONRequest(ctx context.Context, method, endpoint string, body io.Reader) ([]byte, error) {
	return c.doRawRequest(ctx, method, endpoint, body)
}

func (c *Client) doRawRequest(ctx context.Context, method, endpoint string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("create drive request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("drive request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read drive response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("drive API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	return data, nil
}

type listFilesResponse struct {
	Files         []listFilesItem `json:"files"`
	NextPageToken string          `json:"nextPageToken"`
}

type listFilesItem struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	MimeType       string `json:"mimeType"`
	ModifiedTime   string `json:"modifiedTime"`
	WebViewLink    string `json:"webViewLink"`
	WebContentLink string `json:"webContentLink"`
}

func toFile(f listFilesItem) File {
	out := File{
		ID:             f.ID,
		Name:           f.Name,
		MimeType:       f.MimeType,
		WebViewLink:    f.WebViewLink,
		WebContentLink: f.WebContentLink,
	}
	if ts, err := time.Parse(time.RFC3339, f.ModifiedTime); err == nil {
		out.ModifiedTime = ts
	}
	return out
}
