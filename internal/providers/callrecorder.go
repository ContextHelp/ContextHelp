package providers

import (
	"context"
	"errors"
	"sync"

	"github.com/ideacrafterslabs/ctxt/internal/providers/vision"
)

// A CallRecorder collects the failed calls of the LLM and vision
// providers a context's work made. Steps swallow a provider error (an
// outage must not fail an ingest), so the error never reaches the caller
// of the pipeline; a caller that must not act on a silently degraded
// result installs a recorder and asks it afterwards.
type CallRecorder struct {
	mu   sync.Mutex
	errs []error
}

type callRecorderKey struct{}

// WithCallRecorder returns ctx carrying a new recorder, and the recorder.
func WithCallRecorder(ctx context.Context) (context.Context, *CallRecorder) {
	r := &CallRecorder{}
	return context.WithValue(ctx, callRecorderKey{}, r), r
}

// ReportCallFailure records err on ctx's recorder; with none it does
// nothing, so a provider call outside a recording context behaves as
// before.
func ReportCallFailure(ctx context.Context, provider string, err error) {
	if err == nil {
		return
	}
	if r, ok := ctx.Value(callRecorderKey{}).(*CallRecorder); ok {
		r.add(errors.New(provider + ": " + err.Error()))
	}
}

func (r *CallRecorder) add(err error) {
	r.mu.Lock()
	r.errs = append(r.errs, err)
	r.mu.Unlock()
}

// Err is the failed calls joined, nil when every call succeeded.
func (r *CallRecorder) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return errors.Join(r.errs...)
}

// recordLLM wraps p so a failed Generate is reported to the context's
// recorder. The stub and an unavailable provider make no call and stay as
// they are: capability checks tell them apart by type.
func recordLLM(p LLMProvider) LLMProvider {
	if _, ok := p.(*StubLLMProvider); ok || IsFailed(p) {
		return p
	}
	return recordingLLM{p}
}

type recordingLLM struct{ LLMProvider }

func (r recordingLLM) Generate(ctx context.Context, prompt string) (string, error) {
	out, err := r.LLMProvider.Generate(ctx, prompt)
	ReportCallFailure(ctx, "llm "+r.Name(), err)
	return out, err
}

// Model forwards the wrapped provider's model, which provenance records.
func (r recordingLLM) Model() string { return modelOf(r.LLMProvider) }

// Endpoint forwards the wrapped provider's endpoint (see EndpointOf).
func (r recordingLLM) Endpoint() string { return EndpointOf(r.LLMProvider) }

// recordVision is recordLLM for a vision provider.
func recordVision(p vision.Provider) vision.Provider {
	if _, ok := p.(*vision.StubProvider); ok {
		return p
	}
	return recordingVision{p}
}

type recordingVision struct{ vision.Provider }

func (r recordingVision) Analyze(ctx context.Context, img []byte, contentType string) (*vision.Result, error) {
	out, err := r.Provider.Analyze(ctx, img, contentType)
	ReportCallFailure(ctx, "vision "+r.Name(), err)
	return out, err
}

func (r recordingVision) Model() string { return modelOf(r.Provider) }

// Endpoint forwards the wrapped provider's endpoint (see EndpointOf).
func (r recordingVision) Endpoint() string { return EndpointOf(r.Provider) }

// EndpointOf is the URL a provider calls, when it has one to report: the
// local backends do. "" for a hosted backend, which has a fixed API host.
func EndpointOf(p any) string {
	if e, ok := p.(interface{ Endpoint() string }); ok {
		return e.Endpoint()
	}
	return ""
}

func modelOf(p any) string {
	if m, ok := p.(interface{ Model() string }); ok {
		return m.Model()
	}
	return ""
}

// ReportUnusableReply records on ctx's recorder that a step could not use
// the reply of an LLM call that succeeded: empty, or not the structure the
// step requires. A well-formed reply that holds nothing is not unusable.
// With no recorder it does nothing.
func ReportUnusableReply(ctx context.Context, step, why string) {
	if r, ok := ctx.Value(callRecorderKey{}).(*CallRecorder); ok {
		r.add(errors.New(step + ": unusable reply: " + why))
	}
}
