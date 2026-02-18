package steps

import (
	"context"
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type FileReader struct {
	maxFileSize int64 // bytes; 0 means no limit
}

type FileReaderOption func(*FileReader)

func WithMaxFileSize(max int64) FileReaderOption {
	return func(fr *FileReader) { fr.maxFileSize = max }
}

func NewFileReader(opts ...FileReaderOption) *FileReader {
	fr := &FileReader{maxFileSize: 50 * 1024 * 1024} // default 50MB
	for _, opt := range opts {
		opt(fr)
	}
	return fr
}

func (s *FileReader) Name() string { return "file_reader" }

func (s *FileReader) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	path := draft.Source
	if path == "" {
		return nil, fmt.Errorf("file_reader: no source path")
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("file_reader: %w", err)
	}

	if s.maxFileSize > 0 && info.Size() > s.maxFileSize {
		return nil, fmt.Errorf("file_reader: file size %d exceeds limit %d", info.Size(), s.maxFileSize)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("file_reader: %w", err)
	}

	draft.RawContent = string(data)
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["file_size_bytes"] = info.Size()
	draft.Metadata["file_name"] = info.Name()

	return draft, nil
}
