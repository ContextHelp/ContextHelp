package builtins

func init() {
	MustRegister("video.full", Def{
		Description: "Video analysis pipeline (audio extraction + transcription)",
		Extensions:  []string{".mp4", ".mov", ".avi", ".mkv", ".webm"},
		Steps:       []string{"filereader", "formatdetector", "audio_transcriber", "speaker_diarizer", "timestamp_aligner", "sectioner", "tagger", "embedding"},
		Providers:   []string{"transcription", "diarization"},
	})
}
