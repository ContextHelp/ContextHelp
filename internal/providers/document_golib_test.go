package providers

import (
	"context"
	"os"
	"path/filepath"
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

// testdata/survey.docx was written by macOS textutil from HTML: three
// paragraphs, the second split across bold and plain runs.
func TestGolibExtractOfficeDocx(t *testing.T) {
	p := NewGolibDocumentProvider()
	result, err := p.ExtractOffice(context.Background(), filepath.Join("testdata", "survey.docx"))
	if err != nil {
		t.Fatalf("ExtractOffice: %v", err)
	}
	want := "Quokka survey\nMore animals near the salt lakes than last season.\nNight transects start next year."
	if result.FullText != want {
		t.Errorf("FullText:\n got %q\nwant %q", result.FullText, want)
	}
	if result.PageCount != 1 || len(result.Pages) != 1 || result.Pages[0].Content != want {
		t.Errorf("pages: count=%d pages=%+v, want one page of the full text", result.PageCount, result.Pages)
	}
}

func TestGolibExtractOfficeRejectsUnsupportedFormats(t *testing.T) {
	p := NewGolibDocumentProvider()
	for _, ext := range []string{".doc", ".odt", ".rtf", ".epub", ".pptx", ".xlsx"} {
		path := filepath.Join(t.TempDir(), "survey"+ext)
		if err := os.WriteFile(path, []byte("quokka"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got, err := p.ExtractOffice(context.Background(), path); err == nil {
			t.Errorf("%s: extracted %q, want an unsupported-format error", ext, got.FullText)
		}
	}
}

func TestGolibExtractOfficeRejectsCorruptDocx(t *testing.T) {
	path := filepath.Join(t.TempDir(), "survey.docx")
	if err := os.WriteFile(path, []byte("Quokka survey report, not a zip archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := NewGolibDocumentProvider().ExtractOffice(context.Background(), path); err == nil {
		t.Errorf("extracted %q from a non-zip .docx, want an error", got.FullText)
	}
}
