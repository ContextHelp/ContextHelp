package steps

import (
	"context"
	"fmt"
	"strings"

	dropboximporter "github.com/ideacrafterslabs/ctxt/internal/importer/dropbox"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// DropboxFetcher lists files from a Dropbox folder using cursor-based sync.
// It reads configuration from draft.Metadata and writes results back.
//
// Input metadata keys:
//   - "dropbox_token"   (required) OAuth access token
//   - "dropbox_path"    folder path to list (default: "")
//   - "dropbox_cursor"  saved cursor for incremental sync (optional)
//   - "dropbox_recursive" bool: recurse into sub-folders (optional)
//   - "dropbox_max_items" int: max files to return (optional)
//
// Output metadata keys:
//   - "dropbox_files"    []map[string]any — file metadata records
//   - "dropbox_cursor"   updated cursor for the next run
type DropboxFetcher struct {
	pipeline.BaseContract
	client *dropboximporter.Client
}

// DropboxFetcherOption configures a DropboxFetcher.
type DropboxFetcherOption func(*DropboxFetcher)

// WithDropboxClient injects a pre-configured Dropbox API client (primarily
// for tests that supply a mock HTTP server).
func WithDropboxClient(cl *dropboximporter.Client) DropboxFetcherOption {
	return func(f *DropboxFetcher) { f.client = cl }
}

// NewDropboxFetcher creates a DropboxFetcher.
func NewDropboxFetcher(opts ...DropboxFetcherOption) *DropboxFetcher {
	f := &DropboxFetcher{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			// Source carries the folder path (or is empty for root); Metadata is
			// initialised by Run if nil, so we only depend on the guaranteed seed.
			Requires: []string{"Source"},
			Produces: []string{"Metadata"},
		}),
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

func (s *DropboxFetcher) Name() string { return "dropbox_fetcher" }

func (s *DropboxFetcher) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	token, _ := draft.Metadata["dropbox_token"].(string)
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("dropbox_fetcher: dropbox_token is required in metadata")
	}

	folderPath, _ := draft.Metadata["dropbox_path"].(string)
	cursor, _ := draft.Metadata["dropbox_cursor"].(string)
	recursive, _ := draft.Metadata["dropbox_recursive"].(bool)
	maxItems, _ := draft.Metadata["dropbox_max_items"].(int)

	client := s.client
	if client == nil {
		client = dropboximporter.NewClient(nil, "", "", token)
	}

	opts := dropboximporter.ListOptions{
		Path:      folderPath,
		Cursor:    cursor,
		Recursive: recursive,
		MaxItems:  maxItems,
	}

	result, err := client.ListFiles(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("dropbox_fetcher: list files: %w", err)
	}

	fileRecords := make([]map[string]any, 0, len(result.Files))
	for _, f := range result.Files {
		rec := map[string]any{
			"id":           f.ID,
			"name":         f.Name,
			"path":         f.Path,
			"size":         f.Size,
			"is_folder":    f.IsFolder,
			"content_hash": f.ContentHash,
		}
		if !f.ModifiedTime.IsZero() {
			rec["modified_time"] = f.ModifiedTime.UTC().Format("2006-01-02T15:04:05Z")
		}
		fileRecords = append(fileRecords, rec)
	}

	draft.Metadata["dropbox_files"] = fileRecords
	draft.Metadata["dropbox_cursor"] = result.Cursor

	return draft, nil
}
