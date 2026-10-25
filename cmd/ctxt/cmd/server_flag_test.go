package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestServerFlagIsGlobal: --server is one persistent flag on the root;
// no command registers its own copy.
func TestServerFlagIsGlobal(t *testing.T) {
	if rootCmd.PersistentFlags().Lookup("server") == nil {
		t.Fatal("--server is not a root persistent flag")
	}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c != rootCmd {
			if f := c.LocalNonPersistentFlags().Lookup("server"); f != nil {
				t.Errorf("%s registers its own --server", c.CommandPath())
			}
			if f := c.PersistentFlags().Lookup("server"); f != nil {
				t.Errorf("%s registers a persistent --server", c.CommandPath())
			}
		}
		for _, child := range c.Commands() {
			walk(child)
		}
	}
	walk(rootCmd)
}

// TestServerFlagAnyPosition: the global reaches the endpoint lookup
// before or after the subcommand.
func TestServerFlagAnyPosition(t *testing.T) {
	srv := startMockFeedServer(t)
	defer srv.Close()
	for _, args := range [][]string{
		{"--server", srv.URL, "feed", "list"},
		{"feed", "list", "--server", srv.URL},
	} {
		out, err := executeCommand(args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !strings.Contains(out, "feed_001") {
			t.Errorf("%v did not reach --server:\n%s", args, out)
		}
	}
}
