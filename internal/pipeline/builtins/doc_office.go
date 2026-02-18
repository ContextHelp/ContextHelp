package builtins

func init() {
	MustRegister("doc.office", Def{
		Description: "Office document pipeline (docx, odt, rtf, epub)",
		Extensions:  []string{".docx", ".doc", ".odt", ".rtf", ".epub"},
		Steps:       []string{"filereader", "formatdetector", "textcleaner", "sectioner", "tagger", "embedding"},
	})
}
