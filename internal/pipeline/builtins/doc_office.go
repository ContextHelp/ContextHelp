package builtins

func init() {
	MustRegister("doc.office", Def{
		Description: "Office document pipeline (docx)",
		Extensions:  []string{".docx"},
		Steps:       []string{"filereader", "formatdetector", "office_extractor", "textcleaner", "sectioner", "tagger", "embedding"},
	})
}
