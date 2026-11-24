package contracttest

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/providers/vision"
)

// Vision is the vision role's suite: Analyze returns a description and a
// confidence in 0..1 for an image, and honours context cancellation.
func Vision(t *testing.T, cases []Case[vision.Provider], image []byte) {
	Run(t, cases, func(t *testing.T, c Case[vision.Provider], p vision.Provider) {
		requireName(t, p.Name())
		res, err := p.Analyze(context.Background(), image, "image/png")
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if res.Description == "" {
			t.Error("empty description")
		}
		if res.Confidence < 0 || res.Confidence > 1 {
			t.Errorf("confidence %g outside 0..1", res.Confidence)
		}
		if !c.Stub {
			if _, err := p.Analyze(canceled(), image, "image/png"); err == nil {
				t.Error("canceled context: want error")
			}
		}
	})
}
