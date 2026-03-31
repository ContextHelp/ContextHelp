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
	home, _ := os.UserHomeDir()
	extraPaths := []string{
		"/opt/homebrew/bin",
		"/usr/local/bin",
		filepath.Join(home, ".local", "bin"),
	}

	path := os.Getenv("PATH")
	for _, p := range extraPaths {
		if !contains(path, p) {
			path = p + string(os.PathListSeparator) + path
		}
	}
	os.Setenv("PATH", path)
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
// resolve to real (non-stub) implementations.
func CapabilitiesFromFactory(f *providers.Factory) pipeline.CapabilitySet {
	ensureToolPath()
	caps := make(pipeline.CapabilitySet)

	// io is always available (filesystem access is not provider-dependent).
	caps["io"] = true

	if f == nil {
		return caps
	}

	if _, isStub := f.LLM().(*providers.StubLLMProvider); !isStub {
		caps["llm"] = true
	}
	if _, isStub := f.OCR().(*providers.StubOCRProvider); !isStub {
		caps["ocr"] = true
	}
	if _, isStub := f.Vision().(*vision.StubProvider); !isStub {
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

// CapabilitiesFromOpts builds a CapabilitySet from BuildOpts, including
// provider capabilities and blob-externalize if a BlobStore is configured.
func CapabilitiesFromOpts(opts BuildOpts) pipeline.CapabilitySet {
	caps := CapabilitiesFromFactory(opts.Factory)
	if opts.Factory == nil {
		probeToolCapabilities(caps)
	}
	if opts.BlobStore != nil {
		caps["blob-externalize"] = true
	}
	return caps
}

// probeToolCapabilities sets capability flags by checking whether the
// required CLI tools or API keys are present. Used when no factory is available.
func probeToolCapabilities(caps pipeline.CapabilitySet) {
	// ocr: tesseract
	if _, err := providers.LookupTool("tesseract"); err == nil {
		caps["ocr"] = true
	}
	// transcription: whisper or whisper-cpp
	for _, name := range []string{"whisper-cpp", "whisper"} {
		if _, err := providers.LookupTool(name); err == nil {
			caps["transcription"] = true
			break
		}
	}
	// diarization: pyannote-audio binary or python module
	if _, err := providers.LookupTool("pyannote-audio"); err == nil {
		caps["diarization"] = true
	} else if _, err := providers.LookupTool("pyannote"); err == nil {
		caps["diarization"] = true
	}
	// vision: any API key or local ollama binary = capable
	for _, env := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY", "OPENROUTER_API_KEY"} {
		if os.Getenv(env) != "" {
			caps["vision"] = true
			break
		}
	}
	if _, err := providers.LookupTool("ollama"); err == nil {
		caps["vision"] = true
	}
}
