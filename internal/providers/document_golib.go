package providers

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/text"
)

// GolibDocumentProvider uses pure Go libraries for document extraction.
type GolibDocumentProvider struct {
	md goldmark.Markdown
	// pdfProvider is used for PDF extraction; if nil, falls back to returning an error.
	pdfProvider DocumentProvider
}

func NewGolibDocumentProvider(opts ...func(*GolibDocumentProvider)) *GolibDocumentProvider {
	p := &GolibDocumentProvider{
		md: goldmark.New(),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// WithPDFFallback sets a fallback provider for PDF extraction (e.g. pdftotext).
func WithPDFFallback(fallback DocumentProvider) func(*GolibDocumentProvider) {
	return func(p *GolibDocumentProvider) { p.pdfProvider = fallback }
}

func (p *GolibDocumentProvider) Name() string { return "golib" }

func (p *GolibDocumentProvider) ExtractPDF(ctx context.Context, pdfPath string) (*DocumentResult, error) {
	if p.pdfProvider != nil {
		return p.pdfProvider.ExtractPDF(ctx, pdfPath)
	}
	// No pure-Go PDF extraction available yet; try pdftotext if available.
	if _, err := LookupTool("pdftotext"); err == nil {
		fallback := NewPdftotextDocumentProvider()
		return fallback.ExtractPDF(ctx, pdfPath)
	}
	return &DocumentResult{
		FullText:  "[PDF extraction requires pdftotext CLI tool]",
		PageCount: 0,
		Metadata:  map[string]string{"error": "no PDF backend available"},
	}, nil
}

// ExtractOffice extracts the text of a .docx; any other format is an
// error, so callers never mistake a file's bytes for its text.
func (p *GolibDocumentProvider) ExtractOffice(_ context.Context, docPath string) (*DocumentResult, error) {
	if ext := strings.ToLower(filepath.Ext(docPath)); ext != ".docx" {
		return nil, fmt.Errorf("golib: office format %q not supported (only .docx)", ext)
	}
	text, err := extractDocxText(docPath)
	if err != nil {
		return nil, fmt.Errorf("golib: %w", err)
	}
	return &DocumentResult{
		FullText:  text,
		PageCount: 1,
		Pages:     []DocumentPage{{Number: 1, Content: text}},
		Metadata:  map[string]string{"format": "docx"},
	}, nil
}

func (p *GolibDocumentProvider) ParseMarkdown(_ context.Context, content string) (*DocumentResult, error) {
	source := []byte(content)
	reader := text.NewReader(source)
	doc := p.md.Parser().Parse(reader)

	// Render to HTML for structured output, keep raw content as FullText.
	var buf bytes.Buffer
	if err := p.md.Renderer().Render(&buf, source, doc); err != nil {
		return nil, err
	}

	// Split into sections by headings.
	pages := splitMarkdownSections(content)

	return &DocumentResult{
		FullText:  content,
		PageCount: len(pages),
		Pages:     pages,
		Metadata: map[string]string{
			"format":      "markdown",
			"html_length": strings.Repeat("0", len(buf.String())),
		},
	}, nil
}

// splitMarkdownSections splits markdown content by top-level headings.
func splitMarkdownSections(content string) []DocumentPage {
	lines := strings.Split(content, "\n")
	var pages []DocumentPage
	var current strings.Builder
	pageNum := 1

	for _, line := range lines {
		if strings.HasPrefix(line, "# ") && current.Len() > 0 {
			pages = append(pages, DocumentPage{
				Number:  pageNum,
				Content: strings.TrimSpace(current.String()),
			})
			pageNum++
			current.Reset()
		}
		current.WriteString(line)
		current.WriteString("\n")
	}

	if current.Len() > 0 {
		pages = append(pages, DocumentPage{
			Number:  pageNum,
			Content: strings.TrimSpace(current.String()),
		})
	}

	if len(pages) == 0 {
		pages = []DocumentPage{{Number: 1, Content: content}}
	}

	return pages
}
