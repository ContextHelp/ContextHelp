package builtins

import (
	"os"
	"path/filepath"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/providers/vision"
)

// ensureToolPath augments the system PATH with common local tool directories.
func ensureToolPath() {
	path := os.Getenv("PATH")
	for _, p := range providers.ExtraToolDirs() {
		if !contains(path, p) {
			path = p + string(os.PathListSeparator) + path
		}
	}
	// Setenv fails only on a NUL byte, which neither the inherited PATH
	// nor these directories can hold.
	_ = os.Setenv("PATH", path)
}

func contains(path, sub string) bool {
	for _, p := range filepath.SplitList(path) {
		if p == sub {
			return true
		}
	}
	return false
}

// CapabilitiesFromFactory builds a CapabilitySet by checking which providers
// resolve to real implementations: neither a stub nor a failed provider
// (a named backend that cannot run) counts. It resolves each role once.
func CapabilitiesFromFactory(f *providers.Factory) pipeline.CapabilitySet {
	var r *providers.Resolved
	if f != nil {
		r = f.Resolve()
	}
	return capabilitiesFromResolved(r)
}

// capabilitiesFromResolved is CapabilitiesFromFactory over roles already
// resolved, so the check and the step constructors share one provider.
func capabilitiesFromResolved(r *providers.Resolved) pipeline.CapabilitySet {
	ensureToolPath()
	caps := make(pipeline.CapabilitySet)

	// io is always available (filesystem access is not provider-dependent).
	caps["io"] = true

	if r == nil {
		return caps
	}

	if !isStubLLM(r.LLM) {
		caps["llm"] = true
	}
	if !isStubOCR(r.OCR) && !providers.IsFailed(r.OCR) {
		caps["ocr"] = true
	}
	if !isStubVision(r.Vision) {
		caps["vision"] = true
	}
	if !isStubTranscription(r.Transcription) && !providers.IsFailed(r.Transcription) {
		caps["transcription"] = true
	}
	if !isStubVideo(r.Video) && !providers.IsFailed(r.Video) {
		caps["video"] = true
	}
	if !isStubFrames(r.Frames) && !providers.IsFailed(r.Frames) {
		caps["frames"] = true
	}
	if !isStubSummarization(r.Summarization) && !providers.IsFailed(r.Summarization) {
		caps["summarization"] = true
	}
	if !isStubDocument(r.Document) && !providers.IsFailed(r.Document) {
		caps["document"] = true
	}
	if !isStubDiarization(r.Diarization) && !providers.IsFailed(r.Diarization) {
		caps["diarization"] = true
	}

	return caps
}

func isStubLLM(p providers.LLMProvider) bool { _, ok := p.(*providers.StubLLMProvider); return ok }
func isStubOCR(p providers.OCRProvider) bool { _, ok := p.(*providers.StubOCRProvider); return ok }
func isStubVision(p vision.Provider) bool    { _, ok := p.(*vision.StubProvider); return ok }
func isStubTranscription(p providers.TranscriptionProvider) bool {
	_, ok := p.(*providers.StubTranscriptionProvider)
	return ok
}
func isStubVideo(p providers.VideoProvider) bool {
	_, ok := p.(*providers.StubVideoProvider)
	return ok
}
func isStubFrames(p providers.FrameProvider) bool {
	_, ok := p.(*providers.StubFrameProvider)
	return ok
}
func isStubSummarization(p providers.SummarizationProvider) bool {
	_, ok := p.(*providers.StubSummarizationProvider)
	return ok
}
func isStubDocument(p providers.DocumentProvider) bool {
	_, ok := p.(*providers.StubDocumentProvider)
	return ok
}
func isStubDiarization(p providers.DiarizationProvider) bool {
	_, ok := p.(*providers.StubDiarizationProvider)
	return ok
}

// CapabilitiesFromOpts builds a CapabilitySet from BuildOpts, including
// provider capabilities and blob-externalize if a BlobStore is configured.
func CapabilitiesFromOpts(opts BuildOpts) pipeline.CapabilitySet {
	r := opts.resolved
	if r == nil && opts.Factory != nil {
		r = opts.Factory.Resolve()
	}
	caps := capabilitiesFromResolved(r)
	if opts.Factory == nil {
		// Without a factory no provider runs, except document extraction:
		// its pure-Go provider needs no tool (see resolveStep).
		caps["document"] = true
	}
	if opts.OCR != nil {
		caps["ocr"] = true
	}
	if opts.BlobStore != nil {
		caps["blob-externalize"] = true
	}
	if opts.BrowserClient != nil {
		caps["browser"] = true
	}
	return caps
}
