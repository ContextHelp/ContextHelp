package builtins

import (
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

func TestAllDefsRegistered(t *testing.T) {
	d := Defs()
	// Verify that the core set of pipelines is always present.
	// New pipelines may be added over time; this list represents the stable baseline.
	required := []string{
		"text.short", "text.long",
		"image.ocr", "image.analysis",
		"audio.transcribe", "video.full", "video.audio_only",
		"doc.pdf", "doc.markdown", "doc.code", "doc.office",
		"url.generic", "url.repo", "url.interactive", "url.authenticated",
		"feed.sync",
		"batch.jsonl", "batch.csv", "batch.tsv",
		"import.twitter", "import.linkedin.posts", "import.linkedin.articles",
	}
	for _, name := range required {
		if _, ok := d[name]; !ok {
			t.Errorf("missing pipeline def %q", name)
		}
	}
}

func TestAllStepsResolve(t *testing.T) {
	for name, d := range Defs() {
		for _, step := range d.Steps {
			s, err := resolveStep(step, BuildOpts{})
			if err != nil {
				t.Errorf("pipeline %q: step %q failed to resolve: %v", name, step, err)
				continue
			}
			if s == nil {
				t.Errorf("pipeline %q: step %q resolved to nil", name, step)
			}
		}
	}
}

func TestRegistryBuildsAllPipelines(t *testing.T) {
	r := Registry()
	names := r.List()

	// Verify that the core set of pipelines is present in the registry.
	// The registry may contain additional pipelines; we check membership, not an exact list.
	required := []string{
		"audio.transcribe", "batch.csv", "batch.jsonl", "batch.tsv",
		"doc.code", "doc.markdown", "doc.office", "doc.pdf",
		"feed.sync", "image.analysis", "image.ocr", "text.long",
		"text.short", "url.generic", "url.repo", "video.audio_only", "video.full",
		"import.twitter", "import.linkedin.posts", "import.linkedin.articles",
	}

	nameSet := make(map[string]struct{}, len(names))
	for _, n := range names {
		nameSet[n] = struct{}{}
	}
	for _, name := range required {
		if _, ok := nameSet[name]; !ok {
			t.Errorf("pipeline %q missing from registry; registered: %v", name, names)
		}
	}
}

func TestSelectPipelineByExtension(t *testing.T) {
	r := Registry()

	tests := []struct {
		input string
		want  string
	}{
		{"/tmp/photo.png", "image.ocr"},
		{"/tmp/photo.JPG", "image.ocr"},
		{"/tmp/photo.jpeg", "image.ocr"},
		{"/tmp/photo.webp", "image.ocr"},
		{"/tmp/song.mp3", "audio.transcribe"},
		{"/tmp/song.wav", "audio.transcribe"},
		{"/tmp/song.flac", "audio.transcribe"},
		{"/tmp/clip.mp4", "video.full"},
		{"/tmp/clip.mov", "video.full"},
		{"/tmp/clip.mkv", "video.full"},
		{"/tmp/doc.pdf", "doc.pdf"},
		{"/tmp/doc.md", "doc.markdown"},
		{"/tmp/doc.markdown", "doc.markdown"},
		{"/tmp/main.go", "doc.code"},
		{"/tmp/script.py", "doc.code"},
		{"/tmp/app.js", "doc.code"},
		{"/tmp/report.docx", "doc.office"},
		{"/tmp/book.epub", "doc.office"},
	}
	for _, tt := range tests {
		got := r.SelectPipeline(tt.input)
		if got != tt.want {
			t.Errorf("SelectPipeline(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSelectPipelineContentFallback(t *testing.T) {
	r := Registry()

	// Short text → text.short
	got := r.SelectPipeline("short text")
	if got != "text.short" {
		t.Errorf("short text: got %q, want text.short", got)
	}

	// Long text → text.long
	got = r.SelectPipeline(strings.Repeat("word ", 200))
	if got != "text.long" {
		t.Errorf("long text: got %q, want text.long", got)
	}
}

func TestPipelineStepsMatchDef(t *testing.T) {
	r := Registry()
	for name, d := range Defs() {
		p, err := r.Get(name)
		if err != nil {
			t.Errorf("Get(%q): %v", name, err)
			continue
		}
		// Step count may be less than def due to capability pruning (no factory = no providers).
		if len(p.Steps) > len(d.Steps) {
			t.Errorf("%q: step count: got %d, want <= %d", name, len(p.Steps), len(d.Steps))
			continue
		}
		if p.Description != d.Description {
			t.Errorf("%q: description: got %q, want %q", name, p.Description, d.Description)
		}
	}
}

func TestDefsHaveDescriptions(t *testing.T) {
	for name, d := range Defs() {
		if d.Description == "" {
			t.Errorf("pipeline %q has empty description", name)
		}
	}
}

func TestAllPipelinesValidateComposability(t *testing.T) {
	r := Registry()
	for _, name := range r.List() {
		p, err := r.Get(name)
		if err != nil {
			t.Errorf("Get(%q): %v", name, err)
			continue
		}
		if err := pipeline.ValidateComposability(p.Steps); err != nil {
			t.Errorf("pipeline %q composability: %v", name, err)
		}
	}
}

func TestStrictModeRejectsIncapable(t *testing.T) {
	// With a factory containing only stubs and strict mode, pipelines requiring capabilities should fail.
	f := providers.NewFactory(config.ProvidersConfig{
		OCR: config.ProviderBackendConfig{Backend: "stub"},
	}, nil)
	d := Defs()["image.ocr"]
	_, err := buildPipeline("image.ocr", d, BuildOpts{Factory: f}, true)
	if err == nil {
		t.Error("expected error in strict mode with stubbed opts for image.ocr")
	}
}

func TestMustRegisterPanicsOnDuplicate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()
	MustRegister("text.short", Def{Description: "duplicate"})
}

func TestInjectDedupStep(t *testing.T) {
	stepNames := []string{"typedetector", "embedding", "tagger"}

	// CheckSimilar disabled: no injection.
	out := InjectDedupStep(stepNames, config.DuplicatesConfig{CheckSimilar: false})
	if len(out) != len(stepNames) {
		t.Errorf("expected %d steps (no injection), got %d", len(stepNames), len(out))
	}

	// CheckSimilar enabled: dedup inserted after embedding.
	out = InjectDedupStep(stepNames, config.DuplicatesConfig{CheckSimilar: true})
	want := []string{"typedetector", "embedding", "dedup", "tagger"}
	if len(out) != len(want) {
		t.Fatalf("expected %d steps after injection, got %d: %v", len(want), len(out), out)
	}
	for i, s := range want {
		if out[i] != s {
			t.Errorf("step[%d]: got %q, want %q", i, out[i], s)
		}
	}
}

func TestSelectPipelineGitHub(t *testing.T) {
	r := Registry()

	tests := []struct {
		desc    string
		content string
		want    string
	}{
		// Specific sub-paths resolve before the repo catch-all.
		{
			desc:    "pull request",
			content: "https://github.com/torvalds/linux/pull/1234",
			want:    "url.github.pr",
		},
		{
			desc:    "issue",
			content: "https://github.com/torvalds/linux/issues/42",
			want:    "url.github.issue",
		},
		{
			desc:    "release",
			content: "https://github.com/cli/cli/releases/tag/v2.0.0",
			want:    "url.github.release",
		},
		// Repo root URL (two segments).
		{
			desc:    "repo root",
			content: "https://github.com/torvalds/linux",
			want:    "url.github.repo",
		},
		{
			desc:    "repo root trailing slash",
			content: "https://github.com/torvalds/linux/",
			want:    "url.github.repo",
		},
		// Profile: single segment, no repo path.
		{
			desc:    "user profile",
			content: "https://github.com/torvalds",
			want:    "url.github.profile",
		},
		{
			desc:    "org profile",
			content: "https://github.com/nomic-ai",
			want:    "url.github.profile",
		},
		{
			desc:    "profile with tab param",
			content: "https://github.com/monk1337?tab=repositories",
			want:    "url.github.profile",
		},
		// Starred: payload-based via ContentTest.
		{
			desc: "starred repo payload",
			content: "Repository: torvalds/linux\nURL: https://github.com/torvalds/linux\n" +
				"Source: starred\nLanguage: C\nStars: 180000\n\nDescription:\nLinux kernel source tree",
			want: "url.github.starred",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := r.SelectPipeline(tt.content)
			if got != tt.want {
				t.Errorf("SelectPipeline(%q) = %q, want %q", tt.content, got, tt.want)
			}
		})
	}
}

func TestDedupStepRegistered(t *testing.T) {
	s, err := resolveStep("dedup", BuildOpts{})
	if err != nil {
		t.Fatalf("dedup step should be registered: %v", err)
	}
	if s == nil {
		t.Fatal("dedup step resolved to nil")
	}
	if s.Name() != "dedup" {
		t.Errorf("name: got %q, want dedup", s.Name())
	}
}
