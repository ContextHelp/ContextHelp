package providers

import "context"

// DocumentPage holds extracted content for a single page.
type DocumentPage struct {
	Number  int
	Content string
	Images  []string // paths to extracted images, if any
}

// DocumentResult holds the output of document extraction.
type DocumentResult struct {
	FullText  string
	Pages     []DocumentPage
	PageCount int
	Title     string
	Author    string
	Metadata  map[string]string
}

// DocumentProvider extracts text from structured documents.
type DocumentProvider interface {
	Name() string
	ExtractPDF(ctx context.Context, pdfPath string) (*DocumentResult, error)
	ExtractOffice(ctx context.Context, docPath string) (*DocumentResult, error)
	ParseMarkdown(ctx context.Context, content string) (*DocumentResult, error)
}

// StubDocumentProvider returns placeholder extraction results.
type StubDocumentProvider struct{}

func NewStubDocumentProvider() *StubDocumentProvider { return &StubDocumentProvider{} }

func (p *StubDocumentProvider) Name() string { return "stub" }

func (p *StubDocumentProvider) ExtractPDF(_ context.Context, pdfPath string) (*DocumentResult, error) {
	return &DocumentResult{
		FullText:  "[Extracted PDF text content]",
		PageCount: 3,
		Pages: []DocumentPage{
			{Number: 1, Content: "[Page 1 content]"},
			{Number: 2, Content: "[Page 2 content]"},
			{Number: 3, Content: "[Page 3 content]"},
		},
		Title:  "Sample PDF",
		Author: "Unknown",
		Metadata: map[string]string{
			"creator": "stub",
		},
	}, nil
}

func (p *StubDocumentProvider) ExtractOffice(_ context.Context, docPath string) (*DocumentResult, error) {
	return &DocumentResult{
		FullText:  "[Extracted office document text]",
		PageCount: 1,
		Pages: []DocumentPage{
			{Number: 1, Content: "[Office document content]"},
		},
		Title: "Sample Document",
		Metadata: map[string]string{
			"creator": "stub",
		},
	}, nil
}

func (p *StubDocumentProvider) ParseMarkdown(_ context.Context, content string) (*DocumentResult, error) {
	return &DocumentResult{
		FullText:  content,
		PageCount: 1,
		Pages: []DocumentPage{
			{Number: 1, Content: content},
		},
		Metadata: map[string]string{
			"format": "markdown",
		},
	}, nil
}
