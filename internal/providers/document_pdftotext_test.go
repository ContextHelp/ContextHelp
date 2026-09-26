package providers

import (
	"context"
	"testing"
)

func TestParsePdfinfo(t *testing.T) {
	sample := `Title:          My Document
Author:         John Doe
Pages:          5
Page size:      612 x 792 pts (letter)
`
	meta := parsePdfinfo(sample)
	if meta["Title"] != "My Document" {
		t.Errorf("Title: got %q", meta["Title"])
	}
	if meta["Author"] != "John Doe" {
		t.Errorf("Author: got %q", meta["Author"])
	}
	if meta["Pages"] != "5" {
		t.Errorf("Pages: got %q", meta["Pages"])
	}
}

func TestPdftotextProviderName(t *testing.T) {
	p := NewPdftotextDocumentProvider()
	if p.Name() != "pdftotext" {
		t.Errorf("Name: got %q", p.Name())
	}
}

func TestPdftotextExtractOfficeIsUnsupported(t *testing.T) {
	got, err := NewPdftotextDocumentProvider().ExtractOffice(context.Background(), "survey.docx")
	if err == nil {
		t.Errorf("extracted %q, want an unsupported-format error", got.FullText)
	}
}
