package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every role defaults to "auto", which means local; the LLM and vision
// roles carry a local default model.
func TestLoad_ProviderDefaultsAreLocal(t *testing.T) {
	home := hermeticHome(t)
	withChdir(t, home)
	cfg, err := Load("ctxt", "")
	require.NoError(t, err)

	p := cfg.Providers
	for name, backend := range map[string]string{
		"video": p.Video.Backend, "document": p.Document.Backend, "ocr": p.OCR.Backend,
		"transcription": p.Transcription.Backend, "vision": p.Vision.Backend,
		"diarization": p.Diarization.Backend, "llm": p.LLM.Backend,
	} {
		assert.Equal(t, "auto", backend, "providers.%s.backend", name)
	}
	assert.Equal(t, "qwen2.5vl:7b", p.Vision.Model)
	assert.Equal(t, DefaultVisionModel, p.Vision.Model)
	assert.Equal(t, DefaultLLMModel, p.LLM.Model)
	assert.Equal(t, "http://localhost:11434", p.Vision.Endpoint)
	assert.Equal(t, "http://localhost:11434", p.LLM.Endpoint)
}
