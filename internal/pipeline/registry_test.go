package pipeline

import (
	"strings"
	"testing"
)

func TestRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	p := &Pipeline{PipelineName: "test", Description: "test pipeline"}
	if err := r.Register("test", p); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, err := r.Get("test")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.PipelineName != "test" {
		t.Errorf("name: got %q", got.PipelineName)
	}
}

func TestGetNotFound(t *testing.T) {
	r := NewRegistry()
	_, err := r.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()
	r.Register("b", &Pipeline{PipelineName: "b"})
	r.Register("a", &Pipeline{PipelineName: "a"})

	names := r.List()
	if len(names) != 2 {
		t.Fatalf("count: got %d", len(names))
	}
	if names[0] != "a" || names[1] != "b" {
		t.Errorf("order: got %v", names)
	}
}

func TestSelectPipelineShort(t *testing.T) {
	r := NewRegistry()
	name := r.SelectPipeline("", "short text")
	if name != "text.short" {
		t.Errorf("got %q, want text.short", name)
	}
}

func TestSelectPipelineLong(t *testing.T) {
	r := NewRegistry()
	name := r.SelectPipeline("", strings.Repeat("word ", 200))
	if name != "text.long" {
		t.Errorf("got %q, want text.long", name)
	}
}

func TestSelectPipelineMedia(t *testing.T) {
	r := NewRegistry()
	tests := []struct {
		input string
		want  string
	}{
		{"/tmp/photo.png", "image.ocr"},
		{"/tmp/photo.JPG", "image.ocr"},
		{"/tmp/photo.jpeg", "image.ocr"},
		{"/tmp/photo.webp", "image.ocr"},
		{"/tmp/photo.tiff", "image.ocr"},
		{"/tmp/photo.bmp", "image.ocr"},
		{"/tmp/photo.gif", "image.ocr"},
		{"/tmp/song.mp3", "audio.transcribe"},
		{"/tmp/song.wav", "audio.transcribe"},
		{"/tmp/song.ogg", "audio.transcribe"},
		{"/tmp/song.flac", "audio.transcribe"},
		{"/tmp/song.m4a", "audio.transcribe"},
		{"/tmp/clip.mp4", "video.full"},
		{"/tmp/clip.mov", "video.full"},
		{"/tmp/clip.avi", "video.full"},
		{"/tmp/clip.mkv", "video.full"},
		{"/tmp/clip.webm", "video.full"},
		{"/tmp/doc.pdf", "doc.pdf"},
		{"/tmp/doc.md", "doc.markdown"},
		{"/tmp/doc.markdown", "doc.markdown"},
		{"/tmp/main.go", "doc.code"},
		{"/tmp/script.py", "doc.code"},
		{"/tmp/app.js", "doc.code"},
		{"/tmp/report.docx", "doc.office"},
	}
	for _, tt := range tests {
		got := r.SelectPipeline(tt.input, "")
		if got != tt.want {
			t.Errorf("SelectPipeline(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// doc.office extracts .docx only; other office formats must not reach it.
func TestSelectPipelineUnextractableOfficeFormats(t *testing.T) {
	r := NewRegistry()
	for _, ext := range []string{".doc", ".odt", ".rtf", ".epub"} {
		if got := r.SelectPipeline("/tmp/report"+ext, ""); got == "doc.office" {
			t.Errorf("SelectPipeline(%s) = doc.office, which cannot extract it", ext)
		}
	}
}

// Extension rules read only the source; length rules read only the content.
func TestSelectPipelineSourceVsContent(t *testing.T) {
	r := NewRegistry()
	long := strings.Repeat("word ", 200)
	tests := []struct {
		source, content, want string
	}{
		{"argument", "fixed bug in main.go", "text.short"},
		{"", "grabbed shot.png", "text.short"},
		{"stdin", long + "paper.pdf", "text.long"},
		{"/tmp/shot.png", "tiny", "image.ocr"},
		{"/tmp/notes.md", long, "doc.markdown"},
		{"/tmp/notes.txt", "tiny", "text.short"},
		{"/tmp/notes.txt", long, "text.long"},
		{"/var/log/app.log", long, "text.long"},
	}
	for _, tt := range tests {
		if got := r.SelectPipeline(tt.source, tt.content); got != tt.want {
			t.Errorf("SelectPipeline(%q, %d bytes ending %q) = %q, want %q",
				tt.source, len(tt.content), tt.content[max(0, len(tt.content)-12):], got, tt.want)
		}
	}
}

// Detectors see the source and the full content, each in its own field.
func TestDetectPassesSourceAndContentApart(t *testing.T) {
	r := NewRegistry()
	var seen DetectInput
	r.RegisterDetector(DetectorFunc(func(in DetectInput) (string, error) {
		seen = in
		return "", ErrDelegate
	}))
	long := strings.Repeat("word ", 200)
	r.Detect(DetectInput{Source: "/tmp/notes.txt", Content: long})
	if seen.Source != "/tmp/notes.txt" || seen.Content != long {
		t.Errorf("detector saw source %q and %d content bytes; want the source and all %d bytes",
			seen.Source, len(seen.Content), len(long))
	}
}
