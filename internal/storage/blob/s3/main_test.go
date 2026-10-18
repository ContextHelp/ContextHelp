package s3

import (
	"os"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// TestMain runs the package isolated from the developer's machine: HOME
// and XDG dirs point at a throwaway tree, so the AWS SDK reads no real
// shared config or credentials. Requests go to an in-process fake S3
// only. See internal/testguard.
func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}
