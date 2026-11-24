package providers

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// roleBackends is the backend allowlist of each role; config owns it.
// Validate rejects any other name, and the getters of the tool-backed
// roles return a failed provider for it.
var roleBackends = config.ProviderBackends

// backendError is the configuration error for role's named backend:
// it names the config key and wraps the cause.
func backendError(role, backend string, cause error) error {
	return fmt.Errorf("providers.%s.backend %q: %w", role, backend, cause)
}

// unknownBackendError is the error for a backend name role does not know,
// worded as config validation words it.
func unknownBackendError(role, backend string) error {
	return config.ValidationError{Field: "providers." + role + ".backend", Message: config.UnknownBackendMessage(role, backend)}
}

// IsFailed reports whether p stands in for a backend that cannot run: a
// provider whose every call returns the cause. Capability checks treat
// it as unavailable.
func IsFailed(p any) bool {
	_, ok := p.(interface{ Unavailable() error })
	return ok
}

// failed carries why a named backend cannot run.
type failed struct{ err error }

// Name is "unavailable"; the cause is in Unavailable and every call.
func (f failed) Name() string { return "unavailable" }

// Unavailable returns the cause.
func (f failed) Unavailable() error { return f.err }

type failedOCR struct{ failed }

func (p failedOCR) Extract(context.Context, []byte, string) (*OCRResult, error) {
	return nil, p.err
}

type failedTranscription struct{ failed }

func (p failedTranscription) Transcribe(context.Context, string, TranscribeOptions) (*TranscriptResult, error) {
	return nil, p.err
}

type failedDiarization struct{ failed }

func (p failedDiarization) Diarize(context.Context, string, []TranscriptSegment) (*DiarizedResult, error) {
	return nil, p.err
}

type failedVideo struct{ failed }

func (p failedVideo) ProbeMetadata(context.Context, string) (*VideoMetadata, error) {
	return nil, p.err
}

func (p failedVideo) ExtractAudio(context.Context, string, string) (*ExtractedAudio, error) {
	return nil, p.err
}

type failedDocument struct{ failed }

func (p failedDocument) ExtractPDF(context.Context, string) (*DocumentResult, error) {
	return nil, p.err
}

func (p failedDocument) ExtractOffice(context.Context, string) (*DocumentResult, error) {
	return nil, p.err
}

func (p failedDocument) ParseMarkdown(context.Context, string) (*DocumentResult, error) {
	return nil, p.err
}

type failedFrames struct{ failed }

func (p failedFrames) ExtractFrames(context.Context, MediaSource, FrameOptions, FrameYield) error {
	return p.err
}

type failedSummarizer struct{ failed }

func (p failedSummarizer) Summarize(context.Context, SummarizeRequest) (*SummaryResult, error) {
	return nil, p.err
}
