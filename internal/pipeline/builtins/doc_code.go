package builtins

func init() {
	MustRegister("doc.code", Def{
		Description: "Source code file pipeline",
		Extensions:  []string{".go", ".py", ".js", ".ts", ".rs", ".java", ".rb", ".cpp", ".c", ".cs"},
		Steps:       []string{"filereader", "formatdetector", "sectioner", "tagger", "embedding"},
	})
}
