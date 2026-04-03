package builtins

func init() {
	MustRegister("url.authenticated", Def{
		Description: "Browser-based authenticated URL fetch using imported cookies",
		Steps:       []string{"ibr_fetcher", "html_cleaner", "typedetector", "textcleaner", "sectioner", "tagger", "entity_extractor", "entity_resolver", "embedding"},
		Providers:   []string{"browser"},
	})
}
