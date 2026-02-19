package builtins

import (
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

// CapabilitiesFromFactory builds a CapabilitySet by checking which providers
// resolve to real (non-stub) implementations.
func CapabilitiesFromFactory(f *providers.Factory) pipeline.CapabilitySet {
	caps := make(pipeline.CapabilitySet)
	if f == nil {
		return caps
	}

	// io is always available (filesystem access).
	caps["io"] = true

	if _, isStub := f.OCR().(*providers.StubOCRProvider); !isStub {
		caps["ocr"] = true
	}
	if _, isStub := f.Vision().(*providers.StubVisionProvider); !isStub {
		caps["vision"] = true
	}
	if _, isStub := f.Transcription().(*providers.StubTranscriptionProvider); !isStub {
		caps["transcription"] = true
	}
	if _, isStub := f.Diarization().(*providers.StubDiarizationProvider); !isStub {
		caps["diarization"] = true
	}

	return caps
}
