package providers

import (
	"context"
	"testing"
)

func TestGolibParseMarkdown(t *testing.T) {
	p := NewGolibDocumentProvider()
	if p.Name() != "golib" {
		t.Errorf("Name: got %q", p.Name())
	}

	result, err := p.ParseMarkdown(context.Background(), "# Hello\n\nParagraph text.\n\n## Sub\n\nMore text.")
	if err != nil {
		t.Fatalf("ParseMarkdown: %v", err)
	}
	if result.FullText == "" {
		t.Error("FullText is empty")
	}
	if result.Metadata["format"] != "markdown" {
		t.Errorf("format: got %q", result.Metadata["format"])
	}
}

func TestGolibParseMarkdownSections(t *testing.T) {
	content := "# Section One\n\nContent 1.\n\n# Section Two\n\nContent 2."
	pages := splitMarkdownSections(content)
	if len(pages) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(pages))
	}
	if pages[0].Number != 1 {
		t.Errorf("first section number: %d", pages[0].Number)
	}
	if pages[1].Number != 2 {
		t.Errorf("second section number: %d", pages[1].Number)
	}
}

func TestGolibParseMarkdownNoHeadings(t *testing.T) {
	content := "Just plain text."
	pages := splitMarkdownSections(content)
	if len(pages) != 1 {
		t.Fatalf("expected 1 section, got %d", len(pages))
	}
}

func TestGolibExtractPDFWithoutTool(t *testing.T) {
	p := NewGolibDocumentProvider()
	result, err := p.ExtractPDF(context.Background(), "/nonexistent.pdf")
	if err != nil {
		// If pdftotext is installed, it will fail on a non-existent file.
		// That's fine — we're testing the provider doesn't panic.
		return
	}
	// If pdftotext is not installed, we get an informative result.
	if result.FullText == "" {
		t.Error("expected non-empty FullText")
	}
}
