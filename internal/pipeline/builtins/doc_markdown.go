package builtins

func init() {
	MustRegister("doc.markdown", Def{
		Description: "Markdown document pipeline",
		Extensions:  []string{".md", ".markdown"},
		Steps:       []string{"filereader", "sectioner", "tagger", "embedding"},
	})
}
