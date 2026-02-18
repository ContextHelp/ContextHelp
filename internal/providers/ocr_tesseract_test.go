package providers

import (
	"testing"
)

func TestExtensionForContentType(t *testing.T) {
	tests := []struct {
		ct   string
		want string
	}{
		{"image/png", ".png"},
		{"image/jpeg", ".jpg"},
		{"image/tiff", ".tiff"},
		{"image/bmp", ".bmp"},
		{"image/webp", ".webp"},
		{"image/gif", ".gif"},
		{"application/octet-stream", ".png"},
	}
	for _, tt := range tests {
		got := extensionForContentType(tt.ct)
		if got != tt.want {
			t.Errorf("extensionForContentType(%q) = %q, want %q", tt.ct, got, tt.want)
		}
	}
}

func TestTesseractProviderName(t *testing.T) {
	p := NewTesseractOCRProvider("eng")
	if p.Name() != "tesseract" {
		t.Errorf("Name: got %q", p.Name())
	}
}

func TestTesseractDefaultLanguage(t *testing.T) {
	p := NewTesseractOCRProvider("")
	if p.language != "eng" {
		t.Errorf("default language: got %q", p.language)
	}
}
