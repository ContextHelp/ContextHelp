package dirwatcher

// Event topics this plugin publishes, in kit's source.category.object.action
// form. The category is the plugin name in snake_case; hyphens are not
// valid in a topic segment.
const (
	// TopicFileDetected fires for each new file found in the watched directory.
	TopicFileDetected = "ctxt.dir_watcher.file.detected"
	// TopicScanFailed fires when reading the directory or a file failed.
	TopicScanFailed = "ctxt.dir_watcher.scan.failed"
)
