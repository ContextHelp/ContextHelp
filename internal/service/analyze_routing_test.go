package service

import (
	"context"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

var (
	routeShort      = "dugongs graze seagrass meadows in shark bay."
	routeLong       = strings.Repeat("dugongs graze seagrass meadows in shark bay. ", 15)
	routeStructured = "# Shark Bay\n\ndugongs graze seagrass meadows."
)

func newRoutingService(t *testing.T) *Service {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	return New(driver, jobs.NewQueue(driver.Jobs()), builtins.Registry(), search.NewEngine(driver), "", nil)
}

func jobPipeline(t *testing.T, svc *Service, id string) string {
	t.Helper()
	job, err := svc.GetJob(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return job.Pipeline
}

// The registry's content rules the routing tests rely on: under 500 bytes is
// text.short; 500+ bytes or markdown structure is text.long.
func TestRoutingFixturesMatchRegistryContentRules(t *testing.T) {
	r := builtins.Registry()
	for content, want := range map[string]string{
		routeShort:      "text.short",
		routeLong:       "text.long",
		routeStructured: "text.long",
	} {
		if got := r.SelectPipeline(content); got != want {
			t.Fatalf("SelectPipeline(%q) = %s, want %s", content, got, want)
		}
	}
}

// Text captured under a label (argument, stdin, clipboard, ...) routes by
// its content, not by the label.
func TestAnalyzeRoutesTextByContent(t *testing.T) {
	svc := newRoutingService(t)
	cases := []struct {
		name, content, want string
	}{
		{"short", routeShort, "text.short"},
		{"long", routeLong, "text.long"},
		{"structured", routeStructured, "text.long"},
	}
	sources := []string{"", "argument", "stdin", "clipboard", "file", "watcher/screen"}
	for _, tc := range cases {
		for _, source := range sources {
			id, err := svc.Analyze(context.Background(), AnalyzeRequest{
				Content: tc.content, Type: "text", Source: source, Force: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := jobPipeline(t, svc, id); got != tc.want {
				t.Errorf("%s text from source %q routed to %s, want %s", tc.name, source, got, tc.want)
			}
		}
	}
}

func TestAnalyzeExplicitPipelineWins(t *testing.T) {
	svc := newRoutingService(t)
	for _, tc := range []struct{ content, pipeline string }{
		{routeLong, "text.short"},
		{routeShort, "text.long"},
		{"https://example.com/article", "text.short"},
	} {
		id, err := svc.Analyze(context.Background(), AnalyzeRequest{
			Content: tc.content, Type: "text", Source: "argument", Pipeline: tc.pipeline, Force: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := jobPipeline(t, svc, id); got != tc.pipeline {
			t.Errorf("--pipeline %s: routed to %s", tc.pipeline, got)
		}
	}
}

// URLs and file paths are locators: they route by the locator, whatever the
// content length.
func TestAnalyzeRoutesLocatorsBySource(t *testing.T) {
	svc := newRoutingService(t)
	cases := []struct {
		name                 string
		content, typ, source string
		want                 string
	}{
		{"url content", "https://example.com/article", "text", "argument", "url.generic"},
		{"github url content", "https://github.com/ideacrafterslabs/ctxt", "text", "argument", "url.github.repo"},
		{"markdown file", routeShort, "text", "watch:generic:notes/bay.md", "doc.markdown"},
		{"pdf path", routeLong, "text", "/vault/papers/bay.pdf", "doc.pdf"},
	}
	for _, tc := range cases {
		id, err := svc.Analyze(context.Background(), AnalyzeRequest{
			Content: tc.content, Type: tc.typ, Source: tc.source, Force: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := jobPipeline(t, svc, id); got != tc.want {
			t.Errorf("%s: routed to %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestEnqueueRoutesTextByContent(t *testing.T) {
	svc := newRoutingService(t)
	for content, want := range map[string]string{routeShort: "text.short", routeLong: "text.long"} {
		for _, source := range []string{"", "argument", "stdin"} {
			id, err := svc.Enqueue(context.Background(), AnalyzeRequest{Content: content, Type: "text", Source: source})
			if err != nil {
				t.Fatal(err)
			}
			if got := jobPipeline(t, svc, id); got != want {
				t.Errorf("enqueue from %q: %d-byte text routed to %s, want %s", source, len(content), got, want)
			}
		}
	}
}

func TestTriageInboxRoutesTextByContent(t *testing.T) {
	svc := newRoutingService(t)
	ctx := context.Background()
	for content, want := range map[string]string{routeShort: "text.short", routeLong: "text.long"} {
		for _, source := range []string{"", "argument", "stdin"} {
			// Inbox objects are unique by content hash; vary it per source.
			obj, err := svc.CaptureToInbox(ctx, InboxCaptureRequest{Content: content + " " + source, Type: "text", Source: source})
			if err != nil {
				t.Fatal(err)
			}
			id, err := svc.TriageInbox(ctx, obj.ID, TriageRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if got := jobPipeline(t, svc, id); got != want {
				t.Errorf("triage from %q: %d-byte text routed to %s, want %s", source, len(content), got, want)
			}
		}
	}
}
