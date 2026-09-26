package builtins

func init() {
	MustRegister("import.discord", Def{
		Description: "Discord message export import pipeline: one item per message",
		Steps:       []string{"discord_parser", "item_deduplicator", "item_enqueuer"},
	})
}
