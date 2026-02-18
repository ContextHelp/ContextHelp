package onedrive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// Item is a normalized Microsoft Graph drive item used by the importer.
type Item struct {
	DriveID          string
	ID               string
	Name             string
	Path             string
	WebURL           string
	DownloadURL      string
	MimeType         string
	Size             int64
	LastModifiedTime time.Time
	IsFile           bool
}

// Client is a minimal Microsoft Graph client for OneDrive/SharePoint import.
type Client struct {
	httpClient *http.Client
	baseURL    string
	token      string
}

// NewClient creates a new OneDrive Graph API client.
func NewClient(httpClient *http.Client, baseURL, token string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://graph.microsoft.com"
	}

	return &Client{
		httpClient: httpClient,
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      strings.TrimSpace(token),
	}
}

// ListDriveFolderItems lists children of a drive folder path.
// folderPath "/" targets drive root.
func (c *Client) ListDriveFolderItems(ctx context.Context, driveID, folderPath string, maxItems int) ([]Item, error) {
	if strings.TrimSpace(driveID) == "" {
		return nil, fmt.Errorf("drive ID is required")
	}
	if strings.TrimSpace(c.token) == "" {
		return nil, fmt.Errorf("microsoft graph token is required")
	}

	next := buildDriveChildrenEndpoint(driveID, folderPath)
	items := make([]Item, 0)

	for next != "" {
		if maxItems > 0 && len(items) >= maxItems {
			break
		}

		var resp driveListResponse
		if err := c.getJSON(ctx, next, &resp); err != nil {
			return nil, err
		}

		for _, raw := range resp.Value {
			item, ok := toItem(raw, driveID)
			if !ok {
				continue
			}
			items = append(items, item)
			if maxItems > 0 && len(items) >= maxItems {
				break
			}
		}

		if maxItems > 0 && len(items) >= maxItems {
			break
		}

		next = strings.TrimSpace(resp.NextLink)
	}

	return items, nil
}

// GetItem fetches a specific drive item.
func (c *Client) GetItem(ctx context.Context, driveID, itemID string) (Item, error) {
	if strings.TrimSpace(driveID) == "" {
		return Item{}, fmt.Errorf("drive ID is required")
	}
	if strings.TrimSpace(itemID) == "" {
		return Item{}, fmt.Errorf("item ID is required")
	}
	if strings.TrimSpace(c.token) == "" {
		return Item{}, fmt.Errorf("microsoft graph token is required")
	}

	endpoint := fmt.Sprintf("/v1.0/drives/%s/items/%s", url.PathEscape(driveID), url.PathEscape(itemID))

	var raw graphItem
	if err := c.getJSON(ctx, endpoint, &raw); err != nil {
		return Item{}, err
	}

	item, ok := toItem(raw, driveID)
	if !ok {
		return Item{}, fmt.Errorf("drive item %s/%s is invalid", driveID, itemID)
	}
	return item, nil
}

// ListSharedWithMe lists files shared with the current token user.
func (c *Client) ListSharedWithMe(ctx context.Context, maxItems int) ([]Item, error) {
	if strings.TrimSpace(c.token) == "" {
		return nil, fmt.Errorf("microsoft graph token is required")
	}

	next := "/v1.0/me/drive/sharedWithMe?$top=200"
	items := make([]Item, 0)

	for next != "" {
		if maxItems > 0 && len(items) >= maxItems {
			break
		}

		var resp sharedWithMeResponse
		if err := c.getJSON(ctx, next, &resp); err != nil {
			return nil, err
		}

		for _, shared := range resp.Value {
			item, ok := toItem(shared.RemoteItem, "")
			if !ok {
				continue
			}
			items = append(items, item)
			if maxItems > 0 && len(items) >= maxItems {
				break
			}
		}

		if maxItems > 0 && len(items) >= maxItems {
			break
		}

		next = strings.TrimSpace(resp.NextLink)
	}

	return items, nil
}

// RenderContent creates text content for ingestion from a drive item.
func RenderContent(item Item) string {
	var b strings.Builder

	title := strings.TrimSpace(item.Name)
	if title == "" {
		title = item.ID
	}
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")

	if item.WebURL != "" {
		b.WriteString("Source: ")
		b.WriteString(item.WebURL)
		b.WriteString("\n\n")
	}

	b.WriteString("Provider: Microsoft OneDrive/SharePoint\n")
	if item.DriveID != "" {
		b.WriteString("Drive ID: ")
		b.WriteString(item.DriveID)
		b.WriteString("\n")
	}
	if item.ID != "" {
		b.WriteString("Item ID: ")
		b.WriteString(item.ID)
		b.WriteString("\n")
	}
	if item.Path != "" {
		b.WriteString("Path: ")
		b.WriteString(item.Path)
		b.WriteString("\n")
	}
	if item.MimeType != "" {
		b.WriteString("MIME Type: ")
		b.WriteString(item.MimeType)
		b.WriteString("\n")
	}
	if item.Size > 0 {
		b.WriteString("Size: ")
		b.WriteString(fmt.Sprintf("%d", item.Size))
		b.WriteString(" bytes\n")
	}
	if !item.LastModifiedTime.IsZero() {
		b.WriteString("Last Modified: ")
		b.WriteString(item.LastModifiedTime.UTC().Format(time.RFC3339))
		b.WriteString("\n")
	}
	if item.DownloadURL != "" {
		b.WriteString("\nTemporary Download URL: ")
		b.WriteString(item.DownloadURL)
		b.WriteString("\n")
	}

	return b.String()
}

func buildDriveChildrenEndpoint(driveID, folderPath string) string {
	driveID = strings.TrimSpace(driveID)
	fp := strings.TrimSpace(folderPath)

	if fp == "" || fp == "/" {
		return fmt.Sprintf("/v1.0/drives/%s/root/children?$top=200", url.PathEscape(driveID))
	}

	fp = strings.TrimPrefix(fp, "/")
	segments := strings.Split(fp, "/")
	encoded := make([]string, 0, len(segments))
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		encoded = append(encoded, url.PathEscape(segment))
	}
	if len(encoded) == 0 {
		return fmt.Sprintf("/v1.0/drives/%s/root/children?$top=200", url.PathEscape(driveID))
	}

	return fmt.Sprintf(
		"/v1.0/drives/%s/root:/%s:/children?$top=200",
		url.PathEscape(driveID),
		strings.Join(encoded, "/"),
	)
}

func (c *Client) graphRequest(ctx context.Context, method, endpointOrURL string) (*http.Response, error) {
	fullURL := strings.TrimSpace(endpointOrURL)
	if fullURL == "" {
		return nil, fmt.Errorf("graph endpoint is required")
	}
	if !strings.HasPrefix(fullURL, "http://") && !strings.HasPrefix(fullURL, "https://") {
		fullURL = c.baseURL + fullURL
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create graph request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("graph request failed: %w", err)
	}
	return resp, nil
}

func (c *Client) getJSON(ctx context.Context, endpointOrURL string, out any) error {
	resp, err := c.graphRequest(ctx, http.MethodGet, endpointOrURL)
	if err != nil {
		return err
	}
	return decodeGraphResponse(resp, out)
}

func decodeGraphResponse(resp *http.Response, out any) error {
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read graph response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("microsoft graph returned %d: %s", resp.StatusCode, msg)
	}

	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode graph response: %w", err)
	}
	return nil
}

func toItem(raw graphItem, fallbackDriveID string) (Item, bool) {
	driveID := strings.TrimSpace(raw.ParentReference.DriveID)
	if driveID == "" {
		driveID = strings.TrimSpace(fallbackDriveID)
	}
	if driveID == "" || strings.TrimSpace(raw.ID) == "" {
		return Item{}, false
	}

	lastModified, _ := time.Parse(time.RFC3339, strings.TrimSpace(raw.LastModifiedDateTime))
	mime := ""
	isFile := raw.File != nil
	if raw.File != nil {
		mime = strings.TrimSpace(raw.File.MimeType)
	}

	itemPath := normalizeItemPath(raw.ParentReference.Path, raw.Name)

	return Item{
		DriveID:          driveID,
		ID:               strings.TrimSpace(raw.ID),
		Name:             strings.TrimSpace(raw.Name),
		Path:             itemPath,
		WebURL:           strings.TrimSpace(raw.WebURL),
		DownloadURL:      strings.TrimSpace(raw.DownloadURL),
		MimeType:         mime,
		Size:             raw.Size,
		LastModifiedTime: lastModified,
		IsFile:           isFile,
	}, true
}

func normalizeItemPath(parentPath, name string) string {
	base := strings.TrimSpace(parentPath)
	if base != "" {
		if idx := strings.Index(base, ":"); idx >= 0 {
			base = base[idx+1:]
		}
	}
	base = strings.TrimSpace(base)
	if base == "" {
		base = "/"
	}
	if !strings.HasPrefix(base, "/") {
		base = "/" + base
	}

	itemName := strings.TrimSpace(name)
	if itemName == "" {
		return path.Clean(base)
	}

	if base == "/" {
		return "/" + itemName
	}
	return path.Clean(base + "/" + itemName)
}

type driveListResponse struct {
	Value    []graphItem `json:"value"`
	NextLink string      `json:"@odata.nextLink"`
}

type sharedWithMeResponse struct {
	Value    []sharedWithMeItem `json:"value"`
	NextLink string             `json:"@odata.nextLink"`
}

type sharedWithMeItem struct {
	RemoteItem graphItem `json:"remoteItem"`
}

type graphItem struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	WebURL               string `json:"webUrl"`
	LastModifiedDateTime string `json:"lastModifiedDateTime"`
	Size                 int64  `json:"size"`
	DownloadURL          string `json:"@microsoft.graph.downloadUrl"`
	File                 *struct {
		MimeType string `json:"mimeType"`
	} `json:"file"`
	ParentReference struct {
		DriveID string `json:"driveId"`
		Path    string `json:"path"`
	} `json:"parentReference"`
}
