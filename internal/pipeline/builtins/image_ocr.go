package builtins

func init() {
	MustRegister("image.ocr", Def{
		Description: "Image OCR extraction pipeline",
		Extensions:  []string{".png", ".jpg", ".jpeg", ".webp", ".tiff", ".tif", ".bmp", ".gif"},
		Steps:       []string{"filereader", "formatdetector", "ocr_extractor", "textcleaner", "tagger", "embedding"},
		Providers:   []string{"ocr"},
	})
}
