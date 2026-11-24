package contracttest

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

// OCR is the ocr role's suite: Extract returns text and a confidence in
// 0..1 for an image, and honours context cancellation.
func OCR(t *testing.T, cases []Case[providers.OCRProvider], image []byte) {
	Run(t, cases, func(t *testing.T, c Case[providers.OCRProvider], p providers.OCRProvider) {
		requireName(t, p.Name())
		res, err := p.Extract(context.Background(), image, "image/png")
		if err != nil {
			t.Fatalf("Extract: %v", err)
		}
		if res.Text == "" {
			t.Error("empty text")
		}
		if res.Confidence < 0 || res.Confidence > 1 {
			t.Errorf("confidence %g outside 0..1", res.Confidence)
		}
		if !c.Stub {
			if _, err := p.Extract(canceled(), image, "image/png"); err == nil {
				t.Error("canceled context: want error")
			}
		}
	})
}
