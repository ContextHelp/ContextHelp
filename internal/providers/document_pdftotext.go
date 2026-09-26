package providers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// PdftotextDocumentProvider shells out to pdftotext and pdfinfo.
type PdftotextDocumentProvider struct{}

func NewPdftotextDocumentProvider() *PdftotextDocumentProvider {
	return &PdftotextDocumentProvider{}
}

func (p *PdftotextDocumentProvider) Name() string { return "pdftotext" }

func (p *PdftotextDocumentProvider) ExtractPDF(ctx context.Context, pdfPath string) (*DocumentResult, error) {
	// Extract full text.
	textResult, err := RunCommand(ctx, "pdftotext", "-layout", pdfPath, "-")
	if err != nil {
		return nil, fmt.Errorf("pdftotext: %w", err)
	}

	result := &DocumentResult{
		FullText: textResult.Stdout,
		Metadata: make(map[string]string),
	}

	// Get metadata via pdfinfo.
	infoResult, err := RunCommand(ctx, "pdfinfo", pdfPath)
	if err == nil {
		result.Metadata = parsePdfinfo(infoResult.Stdout)
		if pages, ok := result.Metadata["Pages"]; ok {
			if n, err := strconv.Atoi(strings.TrimSpace(pages)); err == nil {
				result.PageCount = n
			}
		}
		result.Title = result.Metadata["Title"]
		result.Author = result.Metadata["Author"]
	}

	// Extract per-page text.
	if result.PageCount > 0 {
		for i := 1; i <= result.PageCount; i++ {
			pageResult, err := RunCommand(ctx, "pdftotext",
				"-f", strconv.Itoa(i),
				"-l", strconv.Itoa(i),
				"-layout",
				pdfPath, "-",
			)
			if err != nil {
				continue
			}
			result.Pages = append(result.Pages, DocumentPage{
				Number:  i,
				Content: pageResult.Stdout,
			})
		}
	}

	return result, nil
}

func (p *PdftotextDocumentProvider) ExtractOffice(_ context.Context, docPath string) (*DocumentResult, error) {
	return nil, fmt.Errorf("pdftotext: office documents not supported: %s", docPath)
}

func (p *PdftotextDocumentProvider) ParseMarkdown(_ context.Context, content string) (*DocumentResult, error) {
	return &DocumentResult{
		FullText:  content,
		PageCount: 1,
		Pages:     []DocumentPage{{Number: 1, Content: content}},
		Metadata:  map[string]string{"format": "markdown"},
	}, nil
}

// parsePdfinfo parses the key: value output of pdfinfo.
func parsePdfinfo(output string) map[string]string {
	meta := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		meta[key] = val
	}
	return meta
}
