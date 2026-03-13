package builtins

func init() {
	MustRegister("video.audio_only", Def{
		Description: "Video audio-only pipeline (extract audio + transcribe)",
		Extensions:  []string{},
		Steps:       []string{"filereader", "formatdetector", "audio_extractor", "audio_transcriber", "speaker_diarizer", "timestamp_aligner", "sectioner", "tagger", "embedding"},
		Providers:   []string{"video", "transcription", "diarization"},
	})
}
