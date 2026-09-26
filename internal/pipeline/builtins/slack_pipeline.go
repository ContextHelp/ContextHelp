package builtins

func init() {
	MustRegister("import.slack", Def{
		Description: "Slack workspace export import pipeline: one item per message",
		Steps:       []string{"slack_parser", "item_deduplicator", "item_enqueuer"},
	})
}
