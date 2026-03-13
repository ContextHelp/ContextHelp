package providers

import "github.com/ideacrafterslabs/ctxt/internal/deps"

func init() {
	deps.Register(
		deps.Dep{
			Binary:      "pdftotext",
			BrewPkg:     "poppler",
			AptPkg:      "poppler-utils",
			DnfPkg:      "poppler-utils",
			PacmanPkg:   "poppler",
			Description: "PDF text extraction",
		},
		deps.Dep{
			Binary:      "whisper",
			PipPkg:      "openai-whisper",
			Description: "audio transcription",
		},
		deps.Dep{
			Binary:      "pyannote-audio",
			PipPkg:      "pyannote.audio",
			Description: "speaker diarization",
		},
	)
}
