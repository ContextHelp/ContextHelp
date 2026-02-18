package builtins

func init() {
	MustRegister("url.generic", Def{
		Description: "Generic URL fetch and extraction pipeline",
		Steps:       []string{"typedetector", "textcleaner", "sectioner", "tagger", "embedding"},
	})
}
