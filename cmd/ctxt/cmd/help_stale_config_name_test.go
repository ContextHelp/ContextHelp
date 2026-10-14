package cmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// staleConfigNames are config file names the CLI no longer reads. The
// real files are ctxt.yaml / dpkms.yaml under the contexthelp config
// dirs (see `ctxt config paths`); help text naming anything else sends
// users to edit a file nothing loads.
var staleConfigNames = []string{"config.yaml", "config.yml"}

// helpTexts returns every user-visible help string on c: Use, Short,
// Long, Example, annotation values (kit/examples etc.) and flag usages.
func helpTexts(c *cobra.Command) map[string]string {
	out := map[string]string{
		"Use": c.Use, "Short": c.Short, "Long": c.Long, "Example": c.Example,
	}
	for k, v := range c.Annotations {
		out["annotation "+k] = v
	}
	addFlag := func(f *pflag.Flag) { out["flag --"+f.Name] = f.Usage }
	c.LocalFlags().VisitAll(addFlag)
	c.PersistentFlags().VisitAll(addFlag)
	return out
}

func staleConfigNameHits(root *cobra.Command) []string {
	var hits []string
	walkTree(root, func(c *cobra.Command) {
		for field, text := range helpTexts(c) {
			for _, name := range staleConfigNames {
				if strings.Contains(text, name) {
					hits = append(hits, fmt.Sprintf("%q %s mentions %s", c.CommandPath(), field, name))
				}
			}
		}
	})
	return hits
}

func TestHelpText_NoStaleConfigFileNames(t *testing.T) {
	for _, hit := range staleConfigNameHits(rootCmd) {
		t.Error(hit)
	}
}
