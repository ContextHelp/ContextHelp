package builtins

func init() {
	MustRegister("image.analysis", Def{
		Description: "Image vision analysis pipeline",
		Steps:       []string{"filereader", "formatdetector", "ocr_extractor", "vision_analyzer", "textcleaner", "tagger", "embedding"},
		Providers:   []string{"ocr", "vision"},
	})
}
