package builtins

func init() {
	MustRegister("feed.sync", Def{
		Description: "RSS/Atom/JSON Feed subscription sync pipeline",
		Steps:       []string{"feed_fetcher", "feed_parser", "item_deduplicator", "item_enqueuer"},
	})
}
