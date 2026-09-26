package contentmonitor

// Event topics this plugin publishes, in kit's source.category.object.action
// form. The category is the plugin name in snake_case; hyphens are not
// valid in a topic segment.
const (
	// TopicBaselineRecorded fires on the first successful fetch of a target.
	TopicBaselineRecorded = "ctxt.content_monitor.baseline.recorded"
	// TopicPageChanged fires when a fetch differs from the previous one.
	TopicPageChanged = "ctxt.content_monitor.page.changed"
	// TopicCheckFailed fires on a fetch error or a non-200 status.
	TopicCheckFailed = "ctxt.content_monitor.check.failed"
)
