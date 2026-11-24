package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func providerErrs(c *Config) []ValidationError {
	var out []ValidationError
	for _, e := range Validate(c) {
		if strings.HasPrefix(e.Field, "providers.") || strings.HasPrefix(e.Field, "pipelines.") {
			out = append(out, e)
		}
	}
	return out
}

func TestValidate_ProviderBackendNames(t *testing.T) {
	t.Run("defaults are valid", func(t *testing.T) {
		assert.Empty(t, providerErrs(&Config{}))
	})
	t.Run("every role accepts its backends", func(t *testing.T) {
		c := &Config{}
		c.Providers.Video.Backend = "ffmpeg"
		c.Providers.Document.Backend = "pdftotext"
		c.Providers.OCR.Backend = "tesseract"
		c.Providers.Transcription.Backend = "whisper-cli"
		c.Providers.Vision.Backend = "openrouter"
		c.Providers.Diarization.Backend = "pyannote"
		c.Providers.LLM.Backend = "anthropic"
		assert.Empty(t, providerErrs(c))
	})
	bad := map[string]func(*Config, string){
		"providers.video.backend":         func(c *Config, v string) { c.Providers.Video.Backend = v },
		"providers.document.backend":      func(c *Config, v string) { c.Providers.Document.Backend = v },
		"providers.ocr.backend":           func(c *Config, v string) { c.Providers.OCR.Backend = v },
		"providers.transcription.backend": func(c *Config, v string) { c.Providers.Transcription.Backend = v },
		"providers.vision.backend":        func(c *Config, v string) { c.Providers.Vision.Backend = v },
		"providers.diarization.backend":   func(c *Config, v string) { c.Providers.Diarization.Backend = v },
		"providers.llm.backend":           func(c *Config, v string) { c.Providers.LLM.Backend = v },
	}
	for field, set := range bad {
		for _, v := range []string{"Anthropic", "claude"} {
			t.Run(field+"/"+v, func(t *testing.T) {
				c := &Config{}
				set(c, v)
				errs := providerErrs(c)
				if assert.Len(t, errs, 1) {
					assert.Equal(t, field, errs[0].Field)
					assert.Contains(t, errs[0].Message, v)
					assert.Contains(t, errs[0].Message, "auto")
				}
			})
		}
	}
	t.Run("pipeline override", func(t *testing.T) {
		c := &Config{}
		c.Pipelines.Overrides = map[string]PipelineOverride{
			"text.short": {Providers: map[string]ProviderBackendConfig{"llm": {Backend: "OpenAI"}}},
		}
		errs := providerErrs(c)
		if assert.Len(t, errs, 1) {
			assert.Equal(t, "pipelines.overrides.text.short.providers.llm.backend", errs[0].Field)
		}
	})
}

func TestProviderBackendConfig_Overlay(t *testing.T) {
	base := ProviderBackendConfig{Backend: "ollama", Model: "m1", Endpoint: "http://base", Language: "eng"}
	cases := []struct {
		name string
		over ProviderBackendConfig
		want ProviderBackendConfig
	}{
		{"empty override inherits everything", ProviderBackendConfig{}, base},
		{"endpoint only", ProviderBackendConfig{Endpoint: "http://lan:11434"},
			ProviderBackendConfig{Backend: "ollama", Model: "m1", Endpoint: "http://lan:11434", Language: "eng"}},
		{"backend auto keeps model and endpoint", ProviderBackendConfig{Backend: "auto"},
			ProviderBackendConfig{Backend: "auto", Model: "m1", Endpoint: "http://base", Language: "eng"}},
		{"every field", ProviderBackendConfig{Backend: "openai", Model: "m2", Endpoint: "e2", Language: "fra"},
			ProviderBackendConfig{Backend: "openai", Model: "m2", Endpoint: "e2", Language: "fra"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, base.Overlay(tc.over))
		})
	}
}

// An override that moves the role across the hosted/local line takes its
// model and endpoint from the override alone: a local model name means
// nothing to a hosted API and the other way round.
func TestProviderBackendConfig_OverlayBackendChangeDropsInherited(t *testing.T) {
	t.Run("local to hosted", func(t *testing.T) {
		base := ProviderBackendConfig{Backend: "ollama", Model: "qwen2.5:14b", Endpoint: "http://lan:11434", Language: "eng"}
		got := base.Overlay(ProviderBackendConfig{Backend: "anthropic"})
		assert.Equal(t, ProviderBackendConfig{Backend: "anthropic"}, got)
	})
	t.Run("hosted to local", func(t *testing.T) {
		base := ProviderBackendConfig{Backend: "anthropic", Model: "claude-haiku-4-5-20251001"}
		got := base.Overlay(ProviderBackendConfig{Backend: "ollama", Model: "llama3.2"})
		assert.Equal(t, ProviderBackendConfig{Backend: "ollama", Model: "llama3.2"}, got)
	})
	t.Run("hosted to other hosted", func(t *testing.T) {
		base := ProviderBackendConfig{Backend: "openai", Model: "gpt-4.1"}
		got := base.Overlay(ProviderBackendConfig{Backend: "anthropic", Endpoint: "e"})
		assert.Equal(t, ProviderBackendConfig{Backend: "anthropic", Endpoint: "e"}, got)
	})
	t.Run("auto to hosted", func(t *testing.T) {
		base := ProviderBackendConfig{Backend: "auto", Model: "llama3", Endpoint: "http://localhost:11434"}
		got := base.Overlay(ProviderBackendConfig{Backend: "openai"})
		assert.Equal(t, ProviderBackendConfig{Backend: "openai"}, got)
	})
	t.Run("hosted to auto", func(t *testing.T) {
		base := ProviderBackendConfig{Backend: "anthropic", Model: "claude-haiku-4-5-20251001"}
		got := base.Overlay(ProviderBackendConfig{Backend: "auto"})
		assert.Equal(t, ProviderBackendConfig{Backend: "auto"}, got)
	})
	t.Run("same backend inherits", func(t *testing.T) {
		base := ProviderBackendConfig{Backend: "anthropic", Model: "m"}
		got := base.Overlay(ProviderBackendConfig{Backend: "anthropic"})
		assert.Equal(t, base, got)
	})
}

// setProbe fills field i of v with a non-zero value that differs between
// the "override-" and "base-" prefixes, and reports whether it knew the
// field's kind. A kind it does not know fails the test, so a field of a
// new kind is taught to Overlay and here together.
func setProbe(t *testing.T, v reflect.Value, i int, prefix string) bool {
	t.Helper()
	f, sf := v.Field(i), v.Type().Field(i)
	switch {
	case f.Kind() == reflect.String:
		f.SetString(prefix + sf.Name)
	case f.Kind() == reflect.Float64:
		f.SetFloat(float64(len(prefix)) + 0.5)
	case f.Kind() == reflect.Int:
		f.SetInt(int64(len(prefix)) + 1)
	case f.Kind() == reflect.Pointer && f.Type().Elem().Kind() == reflect.Bool:
		b := len(prefix) == len("override-")
		f.Set(reflect.ValueOf(&b))
	default:
		assert.Failf(t, "unknown field kind", "field %s (%s): teach this test (and Overlay) the new field type", sf.Name, f.Type())
		return false
	}
	return true
}

// Every field of ProviderBackendConfig must survive Overlay, so a field
// added later without Overlay support fails here.
func TestProviderBackendConfig_OverlayCoversEveryField(t *testing.T) {
	typ := reflect.TypeOf(ProviderBackendConfig{})
	over := reflect.New(typ).Elem()
	for i := 0; i < typ.NumField(); i++ {
		if !setProbe(t, over, i, "override-") {
			return
		}
	}
	o := over.Interface().(ProviderBackendConfig)

	t.Run("from zero base", func(t *testing.T) {
		assert.Equal(t, o, ProviderBackendConfig{}.Overlay(o))
	})
	t.Run("same backend, base set", func(t *testing.T) {
		base := reflect.New(typ).Elem()
		for i := 0; i < typ.NumField(); i++ {
			setProbe(t, base, i, "base-")
		}
		b := base.Interface().(ProviderBackendConfig)
		o2 := o
		o2.Backend = b.Backend // not a backend change: every other field must still win
		assert.Equal(t, o2, b.Overlay(o2))
	})
	t.Run("each field alone", func(t *testing.T) {
		for i := 0; i < typ.NumField(); i++ {
			name := typ.Field(i).Name
			single := reflect.New(typ).Elem()
			single.Field(i).Set(over.Field(i))
			base := ProviderBackendConfig{Backend: "ollama"}
			got := reflect.ValueOf(base.Overlay(single.Interface().(ProviderBackendConfig)))
			assert.Equal(t, over.Field(i).Interface(), got.Field(i).Interface(), "field %s dropped by Overlay", name)
		}
	})
	t.Run("an empty override keeps every base field", func(t *testing.T) {
		base := reflect.New(typ).Elem()
		for i := 0; i < typ.NumField(); i++ {
			setProbe(t, base, i, "base-")
		}
		b := base.Interface().(ProviderBackendConfig)
		assert.Equal(t, b, b.Overlay(ProviderBackendConfig{}))
	})
}

// A backend change across the hosted/local line drops the local-only
// settings but keeps the backend-independent ones.
func TestProviderBackendConfig_OverlayBackendChangeKeepsIndependentFields(t *testing.T) {
	scene := true
	base := ProviderBackendConfig{
		Backend: "ollama", Model: "m", Endpoint: "e", Language: "l", NumCtx: 8192,
		PromptTemplate: "{{.Text}}", MinFPS: 0.5, SceneDetection: &scene, MaxFrames: 10,
	}
	got := base.Overlay(ProviderBackendConfig{Backend: "anthropic"})
	assert.Equal(t, ProviderBackendConfig{
		Backend: "anthropic", PromptTemplate: "{{.Text}}", MinFPS: 0.5, SceneDetection: &scene, MaxFrames: 10,
	}, got)
}

func TestValidateProviders_Exported(t *testing.T) {
	p := ProvidersConfig{}
	p.LLM.Backend = "Anthropic"
	assert.Len(t, ValidateProviders(p), 1)
}
