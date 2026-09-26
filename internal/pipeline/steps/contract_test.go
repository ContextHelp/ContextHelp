package steps

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
)

func TestNoopContract(t *testing.T) {
	s := NewNoop()
	c := s.Contract()
	if c.Requires != nil || c.Produces != nil || c.Capabilities != nil {
		t.Errorf("noop: expected empty contract, got %+v", c)
	}
}

func TestTypeDetectorContract(t *testing.T) {
	s := NewTypeDetector()
	c := s.Contract()
	assertRequires(t, "typedetect", c, []string{"RawContent"})
	assertProduces(t, "typedetect", c, []string{"Type", "Subtype"})
	assertCapabilities(t, "typedetect", c, nil)
}

func TestTextCleanerContract(t *testing.T) {
	s := NewTextCleaner()
	c := s.Contract()
	assertRequires(t, "text_cleaner", c, []string{"RawContent"})
	assertProduces(t, "text_cleaner", c, []string{"RawContent"})
	assertCapabilities(t, "text_cleaner", c, nil)
}

func TestSectionerContract(t *testing.T) {
	s := NewSectioner()
	c := s.Contract()
	assertRequires(t, "sectioner", c, []string{"RawContent"})
	assertProduces(t, "sectioner", c, []string{"Sections"})
	assertCapabilities(t, "sectioner", c, nil)
}

func TestTaggerContract(t *testing.T) {
	s := NewTagger()
	c := s.Contract()
	assertRequires(t, "tagger", c, []string{"RawContent"})
	assertProduces(t, "tagger", c, []string{"Tags"})
	assertCapabilities(t, "tagger", c, nil)
}

func TestFileReaderContract(t *testing.T) {
	s := NewFileReader()
	c := s.Contract()
	assertRequires(t, "file_reader", c, []string{"Source"})
	assertProduces(t, "file_reader", c, []string{"RawContent", "Metadata"})
	assertCapabilities(t, "file_reader", c, []string{"io"})
}

func TestFormatDetectorContract(t *testing.T) {
	s := NewFormatDetector()
	c := s.Contract()
	assertRequires(t, "format_detector", c, []string{"RawContent"})
	assertProduces(t, "format_detector", c, []string{"ContentType", "Type", "Subtype", "Metadata"})
	assertCapabilities(t, "format_detector", c, nil)
}

func TestOCRExtractorContract(t *testing.T) {
	s := NewOCRExtractor()
	c := s.Contract()
	assertRequires(t, "ocr_extractor", c, []string{"RawContent", "ContentType"})
	assertProduces(t, "ocr_extractor", c, []string{"RawContent", "Sections", "Metadata"})
	assertCapabilities(t, "ocr_extractor", c, []string{"ocr"})
}

func TestVisionAnalyzerContract(t *testing.T) {
	s := NewVisionAnalyzer()
	c := s.Contract()
	assertRequires(t, "vision_analyzer", c, []string{"RawContent", "ContentType"})
	assertProduces(t, "vision_analyzer", c, []string{"Sections", "Metadata"})
	assertCapabilities(t, "vision_analyzer", c, []string{"vision"})
}

func TestAudioTranscriberContract(t *testing.T) {
	s := NewAudioTranscriber()
	c := s.Contract()
	assertRequires(t, "audio_transcriber", c, []string{"Source", "ContentType"})
	assertProduces(t, "audio_transcriber", c, []string{"RawContent", "Metadata"})
	assertCapabilities(t, "audio_transcriber", c, []string{"transcription"})
}

func TestSpeakerDiarizerContract(t *testing.T) {
	s := NewSpeakerDiarizer(false)
	c := s.Contract()
	assertRequires(t, "speaker_diarizer", c, []string{"RawContent", "Metadata"})
	assertProduces(t, "speaker_diarizer", c, []string{"Metadata", "Sections"})
	assertCapabilities(t, "speaker_diarizer", c, []string{"diarization"})
}

func TestTimestampAlignerContract(t *testing.T) {
	s := NewTimestampAligner()
	c := s.Contract()
	assertRequires(t, "timestamp_aligner", c, []string{"Metadata"})
	assertProduces(t, "timestamp_aligner", c, []string{"Sections"})
	assertCapabilities(t, "timestamp_aligner", c, nil)
}

func TestEmbeddingGeneratorContract(t *testing.T) {
	s := NewEmbeddingGenerator(nil, nil)
	c := s.Contract()
	assertRequires(t, "embedding_generator", c, []string{"RawContent"})
	assertProduces(t, "embedding_generator", c, []string{"Vectors"})
	assertCapabilities(t, "embedding_generator", c, nil)
}

// --- helpers ---

func assertRequires(t *testing.T, name string, c pipeline.StepContract, want []string) {
	t.Helper()
	if !equalSorted(c.Requires, want) {
		t.Errorf("%s Requires: got %v, want %v", name, c.Requires, want)
	}
}

func assertProduces(t *testing.T, name string, c pipeline.StepContract, want []string) {
	t.Helper()
	if !equalSorted(c.Produces, want) {
		t.Errorf("%s Produces: got %v, want %v", name, c.Produces, want)
	}
}

func assertCapabilities(t *testing.T, name string, c pipeline.StepContract, want []string) {
	t.Helper()
	if !equalSorted(c.Capabilities, want) {
		t.Errorf("%s Capabilities: got %v, want %v", name, c.Capabilities, want)
	}
}

func equalSorted(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	am := make(map[string]bool, len(a))
	for _, s := range a {
		am[s] = true
	}
	for _, s := range b {
		if !am[s] {
			return false
		}
	}
	return true
}
