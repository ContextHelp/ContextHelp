package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDevTreeHasNoShadowSchema pins the embedded migrations
// (internal/storage/{sqlite,postgres}) as the only schema source. Dev
// tooling must not point at a data/migrations tree or drive golang-migrate,
// whose file format those migrations do not use.
func TestDevTreeHasNoShadowSchema(t *testing.T) {
	for _, rel := range []string{"Taskfile.yml", "docker-compose.dev.yml", "scripts/dev-setup.sh"} {
		b, err := os.ReadFile(filepath.Join("..", rel))
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			for _, bad := range []string{"data/migrations", "golang-migrate", "migrate -path", "MIGRATION_PATH"} {
				if strings.Contains(line, bad) {
					t.Errorf("%s:%d references %q: %s", rel, i+1, bad, strings.TrimSpace(line))
				}
			}
		}
	}
}
