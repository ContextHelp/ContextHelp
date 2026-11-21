package cmd

import (
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// TestTaggedBoundToViper: list and compose bind --tagged to their viper
// keys like every other flag, so config and the key lookup see it.
func TestTaggedBoundToViper(t *testing.T) {
	for key, c := range map[string]*cobra.Command{"list.tagged": listCmd, "compose.tagged": composeCmd} {
		t.Run(key, func(t *testing.T) {
			fl := c.Flags().Lookup("tagged")
			if fl == nil {
				t.Fatal("no --tagged flag")
			}
			t.Cleanup(func() {
				_ = fl.Value.(pflag.SliceValue).Replace(nil)
				fl.Changed = false
			})
			if err := c.Flags().Set("tagged", "machine learning"); err != nil {
				t.Fatal(err)
			}
			if got := viper.GetStringSlice(key); !slices.Equal(got, []string{"machine learning"}) {
				t.Errorf("viper %s = %q, want the flag value", key, got)
			}
		})
	}
}

// A --tagged left off the command line reads from viper (config or
// environment) under the command's key; a flag on the command line wins.
func TestTaggedFromConfig(t *testing.T) {
	load := func(yaml string) {
		viper.SetConfigType("yaml")
		if err := viper.ReadConfig(strings.NewReader(yaml)); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { load("") })

	for name, c := range map[string]*cobra.Command{"list": listCmd, "compose": composeCmd} {
		t.Run(name, func(t *testing.T) {
			load(name + ":\n  tagged: [\"+a\", \"machine learning,b\"]\n")
			f, err := parseTaggedFlag(c)
			if err != nil {
				t.Fatal(err)
			}
			if got := f.String(); got != "+a,machine learning,b" {
				t.Errorf("config value = %q", got)
			}
			load(name + ":\n  tagged: \"+x,machine learning\"\n")
			if f, err = parseTaggedFlag(c); err != nil || f.String() != "+x,machine learning" {
				t.Errorf("config string = %v, %v", f.String(), err)
			}
			load(name + ":\n  tagged: \"a,,b\"\n")
			if _, err = parseTaggedFlag(c); err == nil {
				t.Error("a malformed config value must be refused")
			}

			load(name + ":\n  tagged: [\"+x\"]\n")
			fl := c.Flags().Lookup("tagged")
			t.Cleanup(func() { _ = fl.Value.(pflag.SliceValue).Replace(nil); fl.Changed = false })
			if err := c.Flags().Set("tagged", "flag"); err != nil {
				t.Fatal(err)
			}
			if f, err = parseTaggedFlag(c); err != nil || f.String() != "flag" {
				t.Errorf("flag over config = %v, %v", f.String(), err)
			}
		})
	}
}
