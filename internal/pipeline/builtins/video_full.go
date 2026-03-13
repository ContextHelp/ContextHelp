package builtins

func init() {
	MustRegister("video.full", Def{
		Description: "Video analysis pipeline (frames + transcript + OCR)",
		Extensions:  []string{".mp4", ".mov", ".avi", ".mkv", ".webm"},
		Steps:       []string{"filereader", "formatdetector", "audio_extractor", "audio_transcriber", "frame_sampler", "scene_detector", "frame_ocr", "timeline_assembler", "tagger", "embedding"},
		Providers:   []string{"video", "transcription", "ocr"},
	})
}
