package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestFormatDetectorImage(t *testing.T) {
	step := NewFormatDetector()
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".webp", ".tiff", ".bmp", ".gif"} {
		draft := &storage.KnowledgeObject{Source: "/tmp/test" + ext}
		got, err := step.Run(context.Background(), draft)
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		if got.Type != "image" {
			t.Errorf("%s: Type got %q, want image", ext, got.Type)
		}
	}
}

func TestFormatDetectorAudio(t *testing.T) {
	step := NewFormatDetector()
	for _, ext := range []string{".mp3", ".wav", ".ogg", ".flac", ".m4a"} {
		draft := &storage.KnowledgeObject{Source: "/tmp/test" + ext}
		got, err := step.Run(context.Background(), draft)
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		if got.Type != "audio" {
			t.Errorf("%s: Type got %q, want audio", ext, got.Type)
		}
	}
}

func TestFormatDetectorVideo(t *testing.T) {
	step := NewFormatDetector()
	for _, ext := range []string{".mp4", ".mov", ".avi", ".mkv"} {
		draft := &storage.KnowledgeObject{Source: "/tmp/test" + ext}
		got, err := step.Run(context.Background(), draft)
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		if got.Type != "video" {
			t.Errorf("%s: Type got %q, want video", ext, got.Type)
		}
	}
}

func TestFormatDetectorDocument(t *testing.T) {
	step := NewFormatDetector()
	tests := map[string]string{".pdf": "pdf", ".md": "markdown", ".go": "code", ".docx": "office"}
	for ext, subtype := range tests {
		draft := &storage.KnowledgeObject{Source: "/tmp/test" + ext}
		got, err := step.Run(context.Background(), draft)
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		if got.Type != "document" {
			t.Errorf("%s: Type got %q, want document", ext, got.Type)
		}
		if got.Subtype != subtype {
			t.Errorf("%s: Subtype got %q, want %q", ext, got.Subtype, subtype)
		}
	}
}

func TestFormatDetectorUnsupported(t *testing.T) {
	step := NewFormatDetector()
	draft := &storage.KnowledgeObject{Source: "/tmp/test.xyz"}
	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
}
