package builtins_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

// frames and summarization are capabilities like the other roles: present
// for a real backend, absent for a stub and for a failed provider.
func TestFramesAndSummarizationCapabilities(t *testing.T) {
	toolsOnPath(t, "ffmpeg", "ffprobe")
	cfg := mediaCfg(t)
	cfg.Frames = be("auto")
	cfg.Summarization = be("auto") // local Ollama: a real backend
	caps := builtins.CapabilitiesFromFactory(providers.NewFactory(cfg, nil))
	assert.True(t, caps["frames"], "frames with ffmpeg on PATH")
	assert.True(t, caps["summarization"], "summarization on auto")

	cfg.Frames = be("stub")
	cfg.Summarization = be("stub")
	caps = builtins.CapabilitiesFromFactory(providers.NewFactory(cfg, nil))
	assert.False(t, caps["frames"], "a stub is no frames capability")
	assert.False(t, caps["summarization"], "a stub is no summarization capability")

	cfg.Frames = be("magic")
	cfg.Summarization = config.ProviderBackendConfig{Backend: "auto", PromptTemplate: "{{.Text"}
	caps = builtins.CapabilitiesFromFactory(providers.NewFactory(cfg, nil))
	assert.False(t, caps["frames"], "a failed frames provider is no capability")
	assert.False(t, caps["summarization"], "a failed summarizer is no capability")
}

// frame_sampler declares frames, audio_extractor keeps video, and video.full
// needs both roles.
func TestFrameSamplerDeclaresFramesCapability(t *testing.T) {
	assert.Equal(t, []string{"frames"}, steps.NewFrameSampler().Contract().Capabilities)
	assert.Equal(t, []string{"video"}, steps.NewAudioExtractor().Contract().Capabilities)
	assert.Contains(t, builtins.Defs()["video.full"].Providers, "frames")
	assert.Contains(t, builtins.Defs()["video.full"].Providers, "video")
	assert.NotContains(t, builtins.Defs()["video.audio_only"].Providers, "frames")
}

// Without a frames backend video.full is unavailable with the install
// hint, the frame step is never pruned, and video.audio_only still
// registers.
func TestVideoFullNeedsFrames(t *testing.T) {
	toolsOnPath(t, "ffmpeg", "ffprobe", "whisper-cli", "tesseract")
	cfg := mediaCfg(t)
	cfg.Frames = be("stub")
	reg, err := builtins.ConfiguredRegistryWithOpts(builtins.BuildOpts{Factory: providers.NewFactory(cfg, nil)})
	require.NoError(t, err)

	_, err = reg.Get("video.full")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "needs frames")
	assert.Contains(t, err.Error(), "ffmpeg")
	_, err = reg.Get("video.audio_only")
	assert.NoError(t, err)

	cfg.Frames = be("auto")
	reg, err = builtins.ConfiguredRegistryWithOpts(builtins.BuildOpts{Factory: providers.NewFactory(cfg, nil)})
	require.NoError(t, err)
	p, err := reg.Get("video.full")
	require.NoError(t, err)
	var sampler bool
	for _, s := range p.Steps {
		sampler = sampler || s.Name() == "frame_sampler"
	}
	assert.True(t, sampler, "video.full keeps its frame_sampler")
}

// A per-pipeline frames override uses the shared overlay: it can enable a
// pipeline the base lacks, and cannot break one the base has.
func TestFramesOverrideEnablesAndGuardsVideoFull(t *testing.T) {
	toolsOnPath(t, "ffmpeg", "ffprobe", "whisper-cli", "tesseract")
	cfg := mediaCfg(t)
	cfg.Frames = be("stub")
	opts := builtins.BuildOpts{Factory: providers.NewFactory(cfg, nil)}

	reg, err := builtins.ConfiguredRegistryWithPipelineOverrides(opts, cfg, config.PipelinesConfig{
		Overrides: map[string]config.PipelineOverride{
			"video.full": {Providers: map[string]config.ProviderBackendConfig{"frames": {Backend: "auto", MaxFrames: 10}}},
		},
	})
	require.NoError(t, err)
	_, err = reg.Get("video.full")
	assert.NoError(t, err, "the frames override names a working backend")

	cfg.Frames = be("auto")
	opts = builtins.BuildOpts{Factory: providers.NewFactory(cfg, nil)}
	_, err = builtins.ConfiguredRegistryWithPipelineOverrides(opts, cfg, config.PipelinesConfig{
		Overrides: map[string]config.PipelineOverride{
			"video.full": {Providers: map[string]config.ProviderBackendConfig{"frames": be("stub")}},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "frames")
}

// A summarization override is merged over the base and validated like the
// base: a hosted backend with no key is a config error naming the pipeline.
func TestSummarizationOverrideIsValidated(t *testing.T) {
	for _, k := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY", "OPENROUTER_API_KEY"} {
		t.Setenv(k, "")
	}
	cfg := mediaCfg(t)
	opts := builtins.BuildOpts{Factory: providers.NewFactory(cfg, nil)}
	_, err := builtins.ConfiguredRegistryWithPipelineOverrides(opts, cfg, config.PipelinesConfig{
		Overrides: map[string]config.PipelineOverride{
			"text.short": {Providers: map[string]config.ProviderBackendConfig{"summarization": {Backend: "openai", Model: "m"}}},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "text.short")
	assert.Contains(t, err.Error(), "OPENAI_API_KEY")

	_, err = builtins.ConfiguredRegistryWithPipelineOverrides(opts, cfg, config.PipelinesConfig{
		Overrides: map[string]config.PipelineOverride{
			"text.short": {Providers: map[string]config.ProviderBackendConfig{"summarization": {PromptTemplate: "{{.Text"}}},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt_template")
}
