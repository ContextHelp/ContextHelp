package cmd

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/ideacrafterslabs/ctxt/internal/browser/chromium"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/spf13/cobra"
)

var captureBrowsersCmd = &cobra.Command{
	Use:   "browsers",
	Short: "List installed Chromium browsers and profiles, and what capture would pick",
	Long: `List the Chromium-family browsers found on this machine (chrome, brave,
edge, arc, chromium, vivaldi) with their profiles: display name and
folder. The OS default browser and each browser's last-used profile
are marked.

The last section shows what "ctxt capture tabs" (and schedule install)
would pick right now for the --browser / --browser-profile given here
and the capture.browser config, in this order:

  1. --browser
  2. capture.browser
  3. with only --browser-profile: the one installed browser holding it
  4. the OS default browser, when it is Chromium-family

An omitted --browser-profile means the browser's last-used profile.

Nothing is read beyond each browser's "Local State" file; no URLs are
shown. Exit status is 0 even when no browser is found.`,
	Args: cobra.NoArgs,
	RunE: runCaptureBrowsers,
}

func init() {
	captureCmd.AddCommand(captureBrowsersCmd)
	cliconv.WithSideEffect(captureBrowsersCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(captureBrowsersCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(captureBrowsersCmd, []cliconv.Example{
		{Title: "List browsers and the automatic pick", Command: "ctxt capture browsers"},
		{Title: "Which browser holds a profile", Command: "ctxt capture browsers --browser-profile Work"},
		{Title: "As JSON", Command: "ctxt capture browsers --format json"},
	})
	cliconv.WithNextSteps(captureBrowsersCmd, []cliconv.NextStep{
		{When: "to preview a capture", Suggest: "ctxt capture tabs --dry-run", Reason: "read the picked profile's open tabs"},
	})
	captureBrowsersCmd.Flags().String("browser", "", "preview the pick for this browser: "+browserList())
	captureBrowsersCmd.Flags().String("browser-profile", "", `preview the pick for this profile: display name ("Work") or folder name ("Profile 1")`)
}

// browsersReport is capture browsers' structured result.
type browsersReport struct {
	Browsers  []browserEntry    `json:"browsers"`
	OSDefault osDefaultEntry    `json:"os_default"`
	Selection browserSelectItem `json:"selection"`
}

type browserEntry struct {
	Browser     string              `json:"browser"`
	UserDataDir string              `json:"user_data_dir"`
	Error       string              `json:"error,omitempty"`
	Profiles    []browserProfileRow `json:"profiles"`
	OSDefault   bool                `json:"os_default"`
}

type browserProfileRow struct {
	Name     string `json:"name"`
	Dir      string `json:"dir"`
	LastUsed bool   `json:"last_used"`
	Missing  bool   `json:"missing,omitempty"`
}

type osDefaultEntry struct {
	// ID is the OS's handler id (bundle id, .desktop file, ProgId);
	// Browser is set when it is a supported Chromium-family browser.
	ID      string `json:"id"`
	Browser string `json:"browser,omitempty"`
	Error   string `json:"error,omitempty"`
}

type browserSelectItem struct {
	Profile       *tabsProfile `json:"profile,omitempty"`
	Browser       string       `json:"browser,omitempty"`
	BrowserSource string       `json:"browser_source,omitempty"`
	ProfileSource string       `json:"profile_source,omitempty"`
	Error         string       `json:"error,omitempty"`
}

func runCaptureBrowsers(cmd *cobra.Command, _ []string) error {
	// Detect once; the listing and the selection share the answer.
	sel := captureBrowserSelector()
	detected, defErr := sel.Default()
	sel.DefaultBrowser = func() (chromium.DefaultBrowser, error) { return detected, defErr }

	report := browsersReport{Browsers: []browserEntry{}}
	report.OSDefault = osDefaultEntry{ID: detected.ID, Browser: string(detected.Browser)}
	if defErr != nil {
		report.OSDefault.Error = defErr.Error()
	}
	for _, in := range sel.Installed() {
		e := browserEntry{
			Browser:     string(in.Browser),
			UserDataDir: in.UserDataDir,
			OSDefault:   defErr == nil && in.Browser == detected.Browser,
			Profiles:    []browserProfileRow{},
		}
		if in.Err != nil {
			e.Error = in.Err.Error()
		}
		for _, p := range in.Profiles {
			e.Profiles = append(e.Profiles, browserProfileRow{Name: p.Name, Dir: p.DirName, LastUsed: p.LastUsed, Missing: p.Missing})
		}
		report.Browsers = append(report.Browsers, e)
	}

	browserFlag, _ := cmd.Flags().GetString("browser")
	profileFlag, _ := cmd.Flags().GetString("browser-profile")
	picked, err := sel.Select(chromium.Request{Browser: browserFlag, ConfigBrowser: configuredCaptureBrowser(), Profile: profileFlag})
	if err != nil {
		report.Selection.Error = err.Error()
	} else {
		report.Selection = browserSelectItem{
			Browser:       string(picked.Profile.Browser),
			Profile:       &tabsProfile{Name: picked.Profile.Name, Dir: picked.Profile.DirName},
			BrowserSource: string(picked.BrowserSource),
			ProfileSource: string(picked.ProfileSource),
		}
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), report)
	}
	return renderBrowsersReport(cmd.OutOrStdout(), report)
}

func renderBrowsersReport(w io.Writer, r browsersReport) error {
	if len(r.Browsers) == 0 {
		fmt.Fprintf(w, "no Chromium-family browser found (looked for: %s)\n", browserList())
	}
	for _, b := range r.Browsers {
		head := b.Browser
		if b.OSDefault {
			head += " (OS default)"
		}
		fmt.Fprintf(w, "%s  %s\n", head, b.UserDataDir)
		if b.Error != "" {
			fmt.Fprintf(w, "  unreadable: %s\n", b.Error)
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, p := range b.Profiles {
			mark := " "
			if p.LastUsed {
				mark = "*"
			}
			if p.Missing {
				fmt.Fprintf(tw, "  %s %s\t(%s)\tmissing\n", mark, p.Name, p.Dir)
				continue
			}
			fmt.Fprintf(tw, "  %s %s\t(%s)\n", mark, p.Name, p.Dir)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	if len(r.Browsers) > 0 {
		fmt.Fprintln(w, "* last-used profile")
	}

	fmt.Fprintln(w)
	switch {
	case r.OSDefault.Error != "":
		fmt.Fprintf(w, "OS default browser: unknown (%s)\n", r.OSDefault.Error)
	case r.OSDefault.ID == "":
		fmt.Fprintln(w, "OS default browser: not set")
	case r.OSDefault.Browser == "":
		fmt.Fprintf(w, "OS default browser: %s (not Chromium-family)\n", r.OSDefault.ID)
	default:
		fmt.Fprintf(w, "OS default browser: %s (%s)\n", r.OSDefault.Browser, r.OSDefault.ID)
	}

	s := r.Selection
	if s.Error != "" {
		_, err := fmt.Fprintf(w, "would use: nothing: %s\n", s.Error)
		return err
	}
	_, err := fmt.Fprintf(w, "would use: %s profile %q (%s); browser: %s, profile: %s\n",
		s.Browser, s.Profile.Name, s.Profile.Dir, sourceLabel(s.BrowserSource, "--browser"), sourceLabel(s.ProfileSource, "--browser-profile"))
	return err
}

// sourceLabel renders a chromium.Source for people; flag names the
// flag a SourceFlag value came from.
func sourceLabel(src, flag string) string {
	switch chromium.Source(src) {
	case chromium.SourceFlag:
		return flag
	case chromium.SourceConfig:
		return "capture.browser"
	case chromium.SourceProfileMatch:
		return "only browser with that profile"
	case chromium.SourceOSDefault:
		return "OS default"
	case chromium.SourceLastUsed:
		return "last used"
	case chromium.SourceOnlyProfile:
		return "only profile"
	}
	return strings.ReplaceAll(src, "_", " ")
}
