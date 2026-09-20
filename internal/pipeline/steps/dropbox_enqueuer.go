package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// DropboxEnqueuer reads Dropbox file records from draft.Metadata["dropbox_files"]
// and stages them for ingestion by populating draft.Sections and
// draft.Metadata["items_to_enqueue"].
//
// It performs simple deduplication by skipping files whose content_hash was
// already seen in the current batch.
type DropboxEnqueuer struct {
	pipeline.BaseContract
}

// NewDropboxEnqueuer creates a DropboxEnqueuer.
func NewDropboxEnqueuer() *DropboxEnqueuer {
	return &DropboxEnqueuer{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"Metadata"},
			Produces: []string{"Sections", "Metadata"},
		}),
	}
}

func (s *DropboxEnqueuer) Name() string { return "dropbox_enqueuer" }

func (s *DropboxEnqueuer) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	raw, ok := draft.Metadata["dropbox_files"]
	if !ok {
		draft.Metadata["items_to_enqueue"] = []map[string]any{}
		return draft, nil
	}

	files, ok := raw.([]map[string]any)
	if !ok {
		return nil, fmt.Errorf("dropbox_enqueuer: dropbox_files has unexpected type %T", raw)
	}

	sections := make([]storage.Section, 0, len(files))
	pending := make([]map[string]any, 0, len(files))

	seenHashes := make(map[string]bool, len(files))

	for i, f := range files {
		// Skip folders — they have no downloadable content.
		if isFolder, _ := f["is_folder"].(bool); isFolder {
			continue
		}

		// Dedup within this batch using content_hash.
		hash, _ := f["content_hash"].(string)
		if hash != "" {
			if seenHashes[hash] {
				continue
			}
			seenHashes[hash] = true
		}

		name, _ := f["name"].(string)
		path, _ := f["path"].(string)

		content := path
		if content == "" {
			content = name
		}

		sections = append(sections, storage.Section{
			Title:   name,
			Content: content,
			Order:   i,
			Metadata: map[string]any{
				"dropbox_id":    f["id"],
				"dropbox_path":  path,
				"content_hash":  hash,
				"modified_time": f["modified_time"],
			},
		})
		pending = append(pending, f)
	}

	draft.Sections = sections
	draft.Metadata["items_to_enqueue"] = pending

	return draft, nil
}
