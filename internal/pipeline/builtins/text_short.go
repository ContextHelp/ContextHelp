package builtins

func init() {
	MustRegister("text.short", Def{
		Description: "Short text pipeline (< 500 chars)",
		ContentTest: func(content string) bool { return len(content) < 500 },
		// Priority 1 (lower priority than text.long) ensures text.long wins
		// the tie when a short markdown doc with structure satisfies both
		// ContentTests. See text_long.go for the routing-fix rationale (T-0575).
		Priority: 1,
		Steps:    []string{"typedetector", "tagger", "entity_extractor", "entity_resolver", "structured_metadata", "c12n_classify", "embedding"},
	})
}
