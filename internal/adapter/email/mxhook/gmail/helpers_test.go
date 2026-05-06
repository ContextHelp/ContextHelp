package gmail

import "os"

// writeFile is the test-side wrapper around os.WriteFile so the body
// of gmail_test.go can stay focused on the adapter assertions.
func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
