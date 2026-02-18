package steps

import (
	"context"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type Sectioner struct{}

func NewSectioner() *Sectioner { return &Sectioner{} }

func (s *Sectioner) Name() string { return "sectioner" }

func (s *Sectioner) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	content := draft.RawContent

	// Split on markdown headings (## ...).
	lines := strings.Split(content, "\n")
	var sections []storage.Section
	var currentTitle string
	var currentContent strings.Builder
	order := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			// Flush previous section.
			if currentContent.Len() > 0 || currentTitle != "" {
				sections = append(sections, storage.Section{
					Title:   currentTitle,
					Content: strings.TrimSpace(currentContent.String()),
					Order:   order,
				})
				order++
			}
			currentTitle = strings.TrimPrefix(trimmed, "## ")
			currentContent.Reset()
		} else {
			currentContent.WriteString(line)
			currentContent.WriteString("\n")
		}
	}

	// Flush last section.
	if currentContent.Len() > 0 || currentTitle != "" {
		sections = append(sections, storage.Section{
			Title:   currentTitle,
			Content: strings.TrimSpace(currentContent.String()),
			Order:   order,
		})
	}

	// If no headings found, treat entire content as one section.
	if len(sections) == 0 {
		sections = []storage.Section{{
			Title:   "",
			Content: strings.TrimSpace(content),
			Order:   0,
		}}
	}

	draft.Sections = sections
	return draft, nil
}
