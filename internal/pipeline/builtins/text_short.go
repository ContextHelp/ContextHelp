package builtins

func init() {
	MustRegister("text.short", Def{
		Description: "Short text pipeline (< 500 chars)",
		ContentTest: func(content string) bool { return len(content) < 500 },
		Steps:       []string{"url_fetcher", "html_cleaner", "typedetector", "tagger"},
	})
}
