package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// Capturing an image with no OCR backend answers 422 PIPELINE_NOT_FOUND
// naming the pipeline, the missing role and how to install it.
func TestAnalyze_MissingProviderNamesPipelineRoleAndHint(t *testing.T) {
	stub := config.ProviderBackendConfig{Backend: "stub"}
	f := providers.NewFactory(config.ProvidersConfig{
		OCR: stub, Vision: stub, LLM: stub, Video: stub, Transcription: stub, Diarization: stub, Document: stub,
	}, nil)
	driver := storageutil.NewTestDriver(t)
	pipes, err := builtins.ConfiguredRegistryWithOpts(builtins.BuildOpts{Factory: f})
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(driver, jobs.NewQueue(driver.Jobs()), pipes, search.NewEngine(driver), "", nil)
	srv := httptest.NewServer(NewRouter(svc, false, nil))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/analyze", "application/json",
		strings.NewReader(`{"content":"x","type":"image","source":"shot.png"}`))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	var env ErrorEnvelope
	_ = json.NewDecoder(resp.Body).Decode(&env)

	if resp.StatusCode != http.StatusUnprocessableEntity || env.Error.Code != "PIPELINE_NOT_FOUND" {
		t.Fatalf("got %d %q, want 422 PIPELINE_NOT_FOUND", resp.StatusCode, env.Error.Code)
	}
	for _, want := range []string{"image.ocr", "needs ocr", "tesseract"} {
		if !strings.Contains(env.Error.Message, want) {
			t.Errorf("message %q lacks %q", env.Error.Message, want)
		}
	}
}
