package builtins

func init() {
	MustRegister("audio.transcribe", Def{
		Description: "Audio transcription pipeline",
		Extensions:  []string{".mp3", ".wav", ".ogg", ".flac", ".m4a"},
		Steps:       []string{"filereader", "formatdetector", "audio_transcriber", "speaker_diarizer", "timestamp_aligner", "sectioner", "tagger", "embedding"},
		Providers:   []string{"transcription", "diarization"},
	})
}
