package builtins

func init() {
	MustRegister("text.long", Def{
		Description: "Long text pipeline (>= 500 chars)",
		ContentTest: func(content string) bool { return len(content) >= 500 },
		Steps:       []string{"typedetector", "sectioner", "tagger", "entity_extractor", "entity_resolver", "graph_extractor", "structured_metadata", "c12n_classify", "embedding"},
	})
}
