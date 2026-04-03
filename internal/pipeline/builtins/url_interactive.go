package builtins

func init() {
	MustRegister("url.interactive", Def{
		Description: "Browser-based interactive URL fetch and extraction",
		Steps:       []string{"ibr_fetcher", "html_cleaner", "typedetector", "textcleaner", "sectioner", "tagger", "entity_extractor", "entity_resolver", "embedding"},
		Providers:   []string{"browser"},
	})
}
