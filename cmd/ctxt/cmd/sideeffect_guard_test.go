package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
)

// Every runnable command declares its side effect. A command that opens
// the store directly picks the read-only or the locked path from it
// (newService): an undeclared one would silently take the write path.
func TestEveryRunnableCommandDeclaresSideEffect(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Runnable() && !c.Hidden && c.Name() != "help" && c.Name() != "completion" {
			if _, ok := kitcli.GetSideEffect(c); !ok {
				t.Errorf("%s declares no side effect (cliconv.WithSideEffect)", c.CommandPath())
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
}
