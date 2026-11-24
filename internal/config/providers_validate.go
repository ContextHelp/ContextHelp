package config

import (
	"fmt"
	"slices"
	"strings"
)

// Provider backend names with a meaning shared across roles.
const (
	// BackendAuto picks the best local backend; unset means the same.
	BackendAuto = "auto"
	// BackendStub is the placeholder backend: the role is unavailable.
	BackendStub = "stub"
)

// ProviderBackends lists the backend names each provider role accepts,
// besides unset. It is the one allowlist: config validation, the provider
// factory and its failed providers all read it. "auto" (local-only
// resolution) is valid for every role. The embedding role has its own
// resolver and validation.
var ProviderBackends = map[string][]string{
	"video":         {BackendAuto, "ffmpeg", BackendStub},
	"frames":        {BackendAuto, "ffmpeg", BackendStub},
	"document":      {BackendAuto, "golib", "pdftotext", BackendStub},
	"ocr":           {BackendAuto, "tesseract", BackendStub},
	"transcription": {BackendAuto, "whisper-cli", BackendStub},
	"vision":        {BackendAuto, "ollama", "openai", "anthropic", "gemini", "openrouter", BackendStub},
	"diarization":   {BackendAuto, "pyannote", BackendStub},
	"llm":           {BackendAuto, "ollama", "openai", "anthropic", BackendStub},
	"summarization": {BackendAuto, "ollama", "openai", "anthropic", BackendStub},
}

// UnknownBackendMessage is the message for a backend name role does not
// know. Names are exact: a miscased one is unknown.
func UnknownBackendMessage(role, backend string) string {
	return fmt.Sprintf("unknown backend %q for %s; valid values: %s", backend, role, strings.Join(ProviderBackends[role], ", "))
}

// validateProviderBackend reports a backend name the role does not know.
// Empty means unset.
func validateProviderBackend(field, role, backend string) []ValidationError {
	valid, ok := ProviderBackends[role]
	if !ok || backend == "" || slices.Contains(valid, backend) {
		return nil
	}
	return []ValidationError{{Field: field, Message: UnknownBackendMessage(role, backend)}}
}

// ValidateProviders checks every providers.<role> block: the backend
// names, and the frames and summarization settings.
func ValidateProviders(p ProvidersConfig) []ValidationError {
	errs := make([]ValidationError, 0, len(ProviderBackends))
	for role, bc := range map[string]ProviderBackendConfig{
		"video": p.Video, "document": p.Document, "ocr": p.OCR,
		"transcription": p.Transcription, "vision": p.Vision,
		"diarization": p.Diarization, "llm": p.LLM,
	} {
		errs = append(errs, validateProviderBackend("providers."+role+".backend", role, bc.Backend)...)
	}
	errs = append(errs, validateFramesRole(p.Frames, "providers.frames")...)
	errs = append(errs, validateSummarizationRole(p.Summarization, "providers.summarization")...)
	slices.SortStableFunc(errs, func(a, b ValidationError) int { return strings.Compare(a.Field, b.Field) })
	return errs
}
