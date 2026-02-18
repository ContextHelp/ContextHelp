package steps

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestFileReaderReadsFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0644)

	step := NewFileReader()
	draft := &storage.KnowledgeObject{Source: path}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.RawContent != "hello world" {
		t.Errorf("RawContent: got %q", got.RawContent)
	}
	if got.Metadata["file_size_bytes"] != int64(11) {
		t.Errorf("file_size_bytes: got %v", got.Metadata["file_size_bytes"])
	}
}

func TestFileReaderRejectsOversized(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "big.bin")
	os.WriteFile(path, make([]byte, 100), 0644)

	step := NewFileReader(WithMaxFileSize(50))
	draft := &storage.KnowledgeObject{Source: path}
	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
}

func TestFileReaderNoSource(t *testing.T) {
	step := NewFileReader()
	draft := &storage.KnowledgeObject{}
	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestFileReaderMissingFile(t *testing.T) {
	step := NewFileReader()
	draft := &storage.KnowledgeObject{Source: "/nonexistent/file.txt"}
	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
