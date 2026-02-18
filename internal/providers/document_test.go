package providers

import (
	"context"
	"testing"
)

func TestStubDocumentPDF(t *testing.T) {
	p := NewStubDocumentProvider()
	if p.Name() != "stub" {
		t.Errorf("Name: got %q", p.Name())
	}
	result, err := p.ExtractPDF(context.Background(), "/tmp/test.pdf")
	if err != nil {
		t.Fatalf("ExtractPDF: %v", err)
	}
	if result.FullText == "" {
		t.Error("FullText is empty")
	}
	if result.PageCount != 3 {
		t.Errorf("PageCount: got %d, want 3", result.PageCount)
	}
	if len(result.Pages) != 3 {
		t.Errorf("Pages: got %d, want 3", len(result.Pages))
	}
	if result.Title != "Sample PDF" {
		t.Errorf("Title: got %q", result.Title)
	}
}

func TestStubDocumentOffice(t *testing.T) {
	p := NewStubDocumentProvider()
	result, err := p.ExtractOffice(context.Background(), "/tmp/test.docx")
	if err != nil {
		t.Fatalf("ExtractOffice: %v", err)
	}
	if result.FullText == "" {
		t.Error("FullText is empty")
	}
	if result.PageCount != 1 {
		t.Errorf("PageCount: got %d, want 1", result.PageCount)
	}
}

func TestStubDocumentMarkdown(t *testing.T) {
	p := NewStubDocumentProvider()
	result, err := p.ParseMarkdown(context.Background(), "# Hello\n\nWorld")
	if err != nil {
		t.Fatalf("ParseMarkdown: %v", err)
	}
	if result.FullText != "# Hello\n\nWorld" {
		t.Errorf("FullText: got %q", result.FullText)
	}
	if result.Metadata["format"] != "markdown" {
		t.Errorf("format metadata: got %q", result.Metadata["format"])
	}
}
