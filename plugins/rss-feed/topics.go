package rssfeed

// Event topics this plugin publishes, in kit's source.category.object.action
// form. The category is the plugin name in snake_case; hyphens are not
// valid in a topic segment.
const (
	// TopicItemDetected fires for each feed item not seen before this session.
	TopicItemDetected = "ctxt.rss_feed.item.detected"
	// TopicFeedFailed fires when fetching or parsing a feed failed.
	TopicFeedFailed = "ctxt.rss_feed.feed.failed"
)
