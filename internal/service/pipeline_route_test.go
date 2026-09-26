package service

import (
	"context"
	"strings"
	"testing"
)

// routeCase is one routing expectation: content captured from source, with
// an optional explicit pipeline, lands in want.
type routeCase struct {
	name, source, content, pipeline, want string
}

// sourceVsContentCases pin the split between the two routing signals:
// URL and extension rules read only the source; length and structure rules
// read only the content.
var sourceVsContentCases = []routeCase{
	// A filename at the end of a note is words, not a locator.
	{name: "note ending in .go", source: "argument", content: "fixed bug in main.go", want: "text.short"},
	{name: "note ending in .png", source: "argument", content: "grabbed shot.png", want: "text.short"},
	{name: "note ending in .pdf", source: "stdin", content: "read the paper.pdf", want: "text.short"},
	{name: "unlabelled note ending in .md", source: "", content: "see notes/bay.md", want: "text.short"},
	{name: "long text ending in a filename", source: "argument", content: routeLong + "see main.go", want: "text.long"},
	{name: "long clipboard text ending in .png", source: "clipboard", content: routeLong + "shot.png", want: "text.long"},

	// A URL source routes by its URL patterns, whatever the content.
	{name: "url source", source: "https://example.com/article", content: routeLong, want: "url.generic"},
	{name: "github url source", source: "https://github.com/ideacrafterslabs/ctxt", content: routeShort, want: "url.github.repo"},

	// A known extension routes by the path, whatever the content length.
	{name: "md path short", source: "/vault/notes/bay.md", content: routeShort, want: "doc.markdown"},
	{name: "md watcher path long", source: "watch:generic:notes/bay.md", content: routeLong, want: "doc.markdown"},
	{name: "pdf path short", source: "/vault/papers/bay.pdf", content: routeShort, want: "doc.pdf"},

	// An unknown or text extension says nothing; the content decides.
	{name: "txt short", source: "/vault/notes/bay.txt", content: routeShort, want: "text.short"},
	{name: "txt long", source: "/vault/notes/bay.txt", content: routeLong, want: "text.long"},
	{name: "txt structured", source: "/vault/notes/bay.txt", content: routeStructured, want: "text.long"},
	{name: "log short", source: "/var/log/bay.log", content: routeShort, want: "text.short"},
	{name: "log long", source: "/var/log/bay.log", content: routeLong, want: "text.long"},

	// An explicit pipeline beats both signals.
	{name: "explicit over md path", source: "/vault/notes/bay.md", content: routeShort, pipeline: "text.short", want: "text.short"},
	{name: "explicit over long content", source: "/vault/notes/bay.txt", content: routeLong, pipeline: "text.short", want: "text.short"},
	{name: "explicit over short note", source: "argument", content: "fixed bug in main.go", pipeline: "text.long", want: "text.long"},
}

func TestAnalyzeRoutesSourceVsContent(t *testing.T) {
	svc := newRoutingService(t)
	for _, tc := range sourceVsContentCases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := svc.Analyze(context.Background(), AnalyzeRequest{
				Content: tc.content, Type: "text", Source: tc.source, Pipeline: tc.pipeline, Force: true,
			})
			if err != nil {
				t.Fatalf("analyze: %v", err)
			}
			if got := jobPipeline(t, svc, id); got != tc.want {
				t.Errorf("routed to %s, want %s", got, tc.want)
			}
		})
	}
}

func TestEnqueueRoutesSourceVsContent(t *testing.T) {
	svc := newRoutingService(t)
	for _, tc := range sourceVsContentCases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := svc.Enqueue(context.Background(), AnalyzeRequest{
				Content: tc.content, Type: "text", Source: tc.source, Pipeline: tc.pipeline,
			})
			if err != nil {
				t.Fatalf("enqueue: %v", err)
			}
			if got := jobPipeline(t, svc, id); got != tc.want {
				t.Errorf("routed to %s, want %s", got, tc.want)
			}
		})
	}
}

func TestTriageInboxRoutesSourceVsContent(t *testing.T) {
	svc := newRoutingService(t)
	ctx := context.Background()
	for i, tc := range sourceVsContentCases {
		t.Run(tc.name, func(t *testing.T) {
			// Inbox objects are unique by content hash; leading blank
			// lines keep each distinct without touching how it ends.
			content := strings.Repeat("\n", i) + tc.content
			obj, err := svc.CaptureToInbox(ctx, InboxCaptureRequest{Content: content, Type: "text", Source: tc.source})
			if err != nil {
				t.Fatalf("capture: %v", err)
			}
			id, err := svc.TriageInbox(ctx, obj.ID, TriageRequest{Pipeline: tc.pipeline})
			if err != nil {
				t.Fatalf("triage: %v", err)
			}
			if got := jobPipeline(t, svc, id); got != tc.want {
				t.Errorf("routed to %s, want %s", got, tc.want)
			}
		})
	}
}
