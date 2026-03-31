package builtins

func init() {
	MustRegister("url.generic", Def{
		Description: "Generic URL fetch and extraction pipeline",
		Steps:       []string{"url_fetcher", "content_type_router", "html_cleaner", "typedetector", "pdf_extractor", "textcleaner", "sectioner", "tagger", "embedding"},
	})
}
