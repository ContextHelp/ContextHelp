package providers

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// TesseractOCRProvider shells out to the tesseract CLI.
type TesseractOCRProvider struct {
	language string
}

func NewTesseractOCRProvider(language string) *TesseractOCRProvider {
	if language == "" {
		language = "eng"
	}
	return &TesseractOCRProvider{language: language}
}

func (p *TesseractOCRProvider) Name() string { return "tesseract" }

func (p *TesseractOCRProvider) Extract(ctx context.Context, imageData []byte, contentType string) (*OCRResult, error) {
	// Write image data to a temp file.
	ext := extensionForContentType(contentType)
	tmpFile, err := os.CreateTemp("", "ctxt-ocr-*"+ext)
	if err != nil {
		return nil, fmt.Errorf("tesseract: create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(imageData); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("tesseract: write temp file: %w", err)
	}
	tmpFile.Close()

	// Run tesseract to get plain text.
	result, err := RunCommand(ctx, "tesseract", tmpFile.Name(), "stdout", "-l", p.language)
	if err != nil {
		return nil, fmt.Errorf("tesseract: %w", err)
	}

	text := strings.TrimSpace(result.Stdout)

	// Run tesseract with HOCR to get confidence.
	confidence := estimateConfidence(ctx, tmpFile.Name(), p.language)

	return &OCRResult{
		Text:       text,
		Confidence: confidence,
	}, nil
}

// estimateConfidence runs tesseract with tsv output to extract word confidences.
func estimateConfidence(ctx context.Context, imagePath, language string) float64 {
	result, err := RunCommand(ctx, "tesseract", imagePath, "stdout", "-l", language, "tsv")
	if err != nil {
		return 0.0
	}

	// TSV output has columns: level, page_num, block_num, par_num, line_num, word_num, left, top, width, height, conf, text
	lines := strings.Split(result.Stdout, "\n")
	var total, count float64
	confPattern := regexp.MustCompile(`\t(\d+)\t[^\t]*$`)

	for _, line := range lines[1:] { // skip header
		if m := confPattern.FindStringSubmatch(line); len(m) > 1 {
			if c, err := strconv.ParseFloat(m[1], 64); err == nil && c >= 0 {
				total += c
				count++
			}
		}
	}

	if count == 0 {
		return 0.0
	}
	return total / count / 100.0 // tesseract reports 0-100, we want 0.0-1.0
}

func extensionForContentType(ct string) string {
	switch {
	case strings.Contains(ct, "png"):
		return ".png"
	case strings.Contains(ct, "jpeg"), strings.Contains(ct, "jpg"):
		return ".jpg"
	case strings.Contains(ct, "tiff"), strings.Contains(ct, "tif"):
		return ".tiff"
	case strings.Contains(ct, "bmp"):
		return ".bmp"
	case strings.Contains(ct, "webp"):
		return ".webp"
	case strings.Contains(ct, "gif"):
		return ".gif"
	default:
		return ".png"
	}
}
