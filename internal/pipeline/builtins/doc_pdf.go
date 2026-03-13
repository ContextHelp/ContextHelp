package builtins

func init() {
	MustRegister("doc.pdf", Def{
		Description: "PDF document extraction pipeline",
		Extensions:  []string{".pdf"},
		Steps:       []string{"filereader", "formatdetector", "pdf_extractor", "textcleaner", "sectioner", "tagger", "embedding"},
	})
}
