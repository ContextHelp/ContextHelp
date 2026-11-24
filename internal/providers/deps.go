package providers

import "github.com/ideacrafterslabs/ctxt/internal/deps"

func init() {
	deps.Register(
		deps.Dep{
			Binary:      pdftotextTool,
			BrewPkg:     "poppler",
			AptPkg:      "poppler-utils",
			DnfPkg:      "poppler-utils",
			PacmanPkg:   "poppler",
			Description: "PDF text extraction",
		},
		deps.Dep{
			Binary:      backendWhisper,
			BrewPkg:     "whisper-cpp",
			Description: "audio transcription",
		},
		deps.Dep{
			Binary:      "pyannote-audio",
			PipPkg:      "pyannote.audio",
			Description: "speaker diarization",
		},
	)
}
