package builtins

import (
	"strings"
	"testing"
)

// URL patterns and extensions read only the source; content tests read only
// the content.
func TestSelectPipelineSourceVsContent(t *testing.T) {
	r := Registry()
	short := "dugongs graze seagrass meadows in shark bay."
	long := strings.Repeat("dugongs graze seagrass meadows in shark bay. ", 15)
	structured := "# Shark Bay\n\ndugongs graze seagrass meadows."
	starred := "Repository: torvalds/linux\nURL: https://github.com/torvalds/linux\nSource: starred\n"

	tests := []struct {
		desc, source, content, want string
	}{
		{"note ending in .go", "argument", "fixed bug in main.go", "text.short"},
		{"note ending in .png", "", "grabbed shot.png", "text.short"},
		{"note ending in .pdf", "stdin", "read the paper.pdf", "text.short"},
		{"long text ending in a filename", "clipboard", long + "see main.go", "text.long"},
		// Routing never lifts a URL out of the content; a caller that
		// captured a URL passes it as the source.
		{"url as content", "argument", "https://github.com/foo/bar", "text.short"},
		{"url source", "https://example.com/article", long, "url.generic"},
		{"github url source", "https://github.com/foo/bar", short, "url.github.repo"},
		{"md path", "/vault/notes/bay.md", long, "doc.markdown"},
		{"md watcher path", "watch:generic:notes/bay.md", short, "doc.markdown"},
		{"png path", "/tmp/shot.PNG", short, "image.ocr"},
		{"txt short", "/vault/notes/bay.txt", short, "text.short"},
		{"txt long", "/vault/notes/bay.txt", long, "text.long"},
		{"txt structured", "/vault/notes/bay.txt", structured, "text.long"},
		{"log short", "/var/log/bay.log", short, "text.short"},
		{"log long", "/var/log/bay.log", long, "text.long"},
		{"payload under an import label", "import:github-starred", starred, "url.github.starred"},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := r.SelectPipeline(tt.source, tt.content); got != tt.want {
				t.Errorf("SelectPipeline(%q, ...) = %q, want %q", tt.source, got, tt.want)
			}
		})
	}
}
