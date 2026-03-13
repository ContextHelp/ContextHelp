package builtins

func init() {
	MustRegister("url.generic", Def{
		Description: "Generic URL fetch and extraction pipeline",
		Steps:       []string{"url_fetcher", "html_cleaner", "typedetector", "pdf_extractor", "textcleaner", "sectioner", "tagger", "embedding"},
	})
}
