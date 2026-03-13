package builtins

func init() {
	MustRegister("import.twitter", Def{
		Description: "Twitter/X archive import pipeline (tweets.js)",
		// No extension-based matching to avoid conflicting with doc.code (.js files).
		// Detection is done via content-based test.
		ContentTest: func(content string) bool {
			// tweets.js files start with the window.YTD.tweets assignment prefix.
			return len(content) > 22 && content[:22] == "window.YTD.tweets.part"
		},
		Steps: []string{"twitter_archive_parser", "item_deduplicator", "item_enqueuer"},
	})

	MustRegister("import.linkedin.posts", Def{
		Description: "LinkedIn Posts.csv archive import pipeline",
		Steps:       []string{"linkedin_posts_parser", "item_deduplicator", "item_enqueuer"},
	})

	MustRegister("import.linkedin.articles", Def{
		Description: "LinkedIn Articles.csv archive import pipeline",
		Steps:       []string{"linkedin_articles_parser", "item_deduplicator", "item_enqueuer"},
	})
}
