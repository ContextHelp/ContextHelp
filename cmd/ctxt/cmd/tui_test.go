package cmd

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/tui"
)

// The TUI runs under the same active profile as every other command:
// --profile over profile.default, an explicit "" selecting none. The
// resolved name reaches the model and its status bar.
func TestTUIStartOptsActiveProfile(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "flag over default", args: []string{"--profile", "growth"}, want: "growth"},
		{name: "default, no flag", want: "ops"},
		{name: "empty flag selects none", args: []string{"--profile", ""}, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prev := cfg
			t.Cleanup(func() { cfg = prev })
			cfg = &config.Config{Profile: config.ProfileConfig{Default: "ops"}}

			c := &cobra.Command{Use: "tui"}
			c.Flags().String("profile", "", "")
			if err := c.ParseFlags(tc.args); err != nil {
				t.Fatal(err)
			}

			opts := tuiStartOpts(c, tui.StartOpts{InitialQuery: "q"})
			if opts.Profile != tc.want {
				t.Errorf("StartOpts.Profile = %q, want %q", opts.Profile, tc.want)
			}
			if opts.InitialQuery != "q" {
				t.Errorf("StartOpts.InitialQuery = %q, want %q (caller opts must survive)", opts.InitialQuery, "q")
			}

			m, _ := tui.New(tui.NewRealAdapter(nil), opts).Update(tea.WindowSizeMsg{Width: 200, Height: 30})
			view := m.(tui.Model).View().Content
			if !strings.Contains(view, "profile: "+tc.want+" ") {
				t.Errorf("status bar missing %q:\n%s", "profile: "+tc.want, view)
			}
			if tc.want != "ops" && strings.Contains(view, "profile: ops") {
				t.Errorf("status bar shows config default %q despite --profile:\n%s", "ops", view)
			}
		})
	}
}
