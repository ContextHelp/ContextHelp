package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// staleConfigNames are config file names dpkms no longer reads. The
// real file is dpkms.yaml under the contexthelp config dirs; help text
// naming anything else sends users to edit a file nothing loads.
var staleConfigNames = []string{"config.yaml", "config.yml"}

func TestHelpText_NoStaleConfigFileNames(t *testing.T) {
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		texts := map[string]string{
			"Use": c.Use, "Short": c.Short, "Long": c.Long, "Example": c.Example,
		}
		for k, v := range c.Annotations {
			texts["annotation "+k] = v
		}
		addFlag := func(f *pflag.Flag) { texts["flag --"+f.Name] = f.Usage }
		c.LocalFlags().VisitAll(addFlag)
		c.PersistentFlags().VisitAll(addFlag)
		for field, text := range texts {
			for _, name := range staleConfigNames {
				if strings.Contains(text, name) {
					t.Errorf("%q %s mentions %s", c.CommandPath(), field, name)
				}
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
}
