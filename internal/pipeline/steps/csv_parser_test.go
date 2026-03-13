package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestCSVParserBasic(t *testing.T) {
	content := "content,type,source\nhello world,note,test\nsecond entry,doc,test"
	step := NewCSVParser()
	draft := &storage.KnowledgeObject{RawContent: content}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	rows, ok := got.Metadata["csv_rows"].([]map[string]string)
	if !ok {
		t.Fatalf("csv_rows wrong type: %T", got.Metadata["csv_rows"])
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(rows))
	}
	if rows[0]["content"] != "hello world" {
		t.Errorf("row[0].content: got %q", rows[0]["content"])
	}
	if rows[0]["type"] != "note" {
		t.Errorf("row[0].type: got %q", rows[0]["type"])
	}
}

func TestCSVParserCustomDelimiter(t *testing.T) {
	content := "content\ttype\nhello\tnote"
	step := NewCSVParser(WithDelimiter('\t'))
	draft := &storage.KnowledgeObject{RawContent: content}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	rows := got.Metadata["csv_rows"].([]map[string]string)
	if len(rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(rows))
	}
	if rows[0]["content"] != "hello" {
		t.Errorf("content: got %q", rows[0]["content"])
	}
}

func TestCSVParserEmpty(t *testing.T) {
	step := NewCSVParser()
	draft := &storage.KnowledgeObject{RawContent: ""}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	rows := got.Metadata["csv_rows"].([]map[string]string)
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

func TestCSVParserHeaderOnly(t *testing.T) {
	step := NewCSVParser()
	draft := &storage.KnowledgeObject{RawContent: "content,type"}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	rows := got.Metadata["csv_rows"].([]map[string]string)
	if len(rows) != 0 {
		t.Errorf("expected 0 data rows, got %d", len(rows))
	}
}
