package watcher

import "github.com/fsnotify/fsnotify"

// WithFSWatcherHook hands fn each fsnotify watcher a watch loop opens,
// so a test can feed its Errors channel.
func WithFSWatcherHook(fn func(*fsnotify.Watcher)) Option {
	return func(m *Manager) { m.onFSWatcher = fn }
}
