package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// Bundle entry names never write outside the config dir, even when
// they reach writeRestoredFiles without bundle's own checks.
func TestWriteRestoredFilesStaysInConfigDir(t *testing.T) {
	base := t.TempDir()
	cfgDir := filepath.Join(base, "cfg")
	t.Setenv(config.EnvConfigPath, filepath.Join(cfgDir, "config.yaml"))
	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)

	for _, name := range []string{"../evil", "keys/../../evil", filepath.Join(base, "evil")} {
		if err := writeRestoredFiles(cmd, map[string][]byte{name: []byte("x")}); err == nil {
			t.Errorf("writeRestoredFiles(%q): want error", name)
		}
	}
	if _, err := os.Stat(filepath.Join(base, "evil")); !os.IsNotExist(err) {
		t.Fatalf("restore wrote outside the config dir (stat err %v)", err)
	}

	if err := writeRestoredFiles(cmd, map[string][]byte{"keys/a.pub": []byte("k")}); err != nil {
		t.Fatalf("writeRestoredFiles: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(cfgDir, "keys", "a.pub")); err != nil || string(got) != "k" {
		t.Errorf("keys/a.pub = %q, %v", got, err)
	}
}
