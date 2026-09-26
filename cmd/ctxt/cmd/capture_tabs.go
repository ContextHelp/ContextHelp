package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/ideacrafterslabs/ctxt/internal/browser/chromium"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/idxbridge"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/urlfilter"
	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
	"hop.top/kit/go/console/output"
)

// staleSessionAfter is how old a session file may be before capture tabs
// says the browser may be closed. A running Chromium rewrites the file
// within seconds of every tab open, close or navigation, so a quarter
// hour without a write means the browser was quit (or left untouched):
// the tabs listed are the ones open as of the file's time.
const staleSessionAfter = 15 * time.Minute

// Per-tab outcomes, as reported in the structured output.
const (
	tabStatusWouldSend = "would_send"
	tabStatusSent      = "sent"
	tabStatusFailed    = "failed"
	tabStatusDenied    = "denied"
	tabStatusDuplicate = "duplicate"
)

var captureTabsCmd = &cobra.Command{
	Use:   "tabs",
	Short: "Capture the open tabs of one browser profile",
	Long: `Capture the tabs currently open in one profile of a Chromium-family
browser (chrome, brave, edge, arc, chromium, vivaldi).

The tabs are read from the profile's session file on disk; the browser
does not need to be running and no extension is involved. When the
browser is closed, the tabs are the ones open when it last saved its
session, and the command says how old that snapshot is.

Every URL goes through the capture.url_filter rules for that browser and
profile before anything leaves the machine. Identical URLs are sent
once. Each remaining URL is enqueued on the configured ctxt server the
same way "ctxt capture <url>" enqueues one, so the server picks the
pipeline.

--browser-profile takes the name shown in the browser's profile picker
("Work") or the profile folder name ("Profile 1"); a folder name
disambiguates two profiles with the same name.

Both flags are optional. Without --browser the browser is
capture.browser from the config; failing that, the one installed
browser holding --browser-profile; failing that, the OS default
browser. Without --browser-profile the browser's last-used profile is
read. Whatever is picked automatically is named on stderr; run
"ctxt capture browsers" to see the candidates.

Run with --dry-run first: it lists every tab with the filter's decision
and the reason, and sends nothing.

Exit status: 0 when every allowed URL was accepted, 1 when any send
failed (the rest are still sent), 2 for a bad invocation (unknown
browser, unknown or ambiguous profile, nothing to auto-select), 3 when
the browser, profile folder or session file is not on disk.`,
	Args: cobra.NoArgs,
	RunE: runCaptureTabs,
}

func init() {
	captureCmd.AddCommand(captureTabsCmd)
	cliconv.WithSideEffect(captureTabsCmd, cliconv.SideEffectWrite)
	// Each run enqueues one job per allowed URL; re-running enqueues
	// again and the server's content fingerprint dedupes the objects.
	cliconv.WithIdempotency(captureTabsCmd, cliconv.IdempotencyConditional)
	cliconv.WithExamples(captureTabsCmd, []cliconv.Example{
		{Title: "Preview what would be sent", Command: "ctxt capture tabs --browser brave --browser-profile Work --dry-run"},
		{Title: "Auto-select the browser holding a profile", Command: "ctxt capture tabs --browser-profile Work --dry-run"},
		{Title: "Capture the open tabs", Command: "ctxt capture tabs --browser brave --browser-profile Work"},
		{Title: "Pick a profile by folder name", Command: `ctxt capture tabs --browser chrome --browser-profile "Profile 1"`},
	})
	cliconv.WithNextSteps(captureTabsCmd, []cliconv.NextStep{
		{When: "after a dry run", Suggest: "ctxt capture tabs --browser <b> --browser-profile <p>", Reason: "send the allowed tabs"},
		{When: "on success", Suggest: "ctxt find <query>", Reason: "search the captured pages"},
	})

	captureTabsCmd.Flags().String("browser", "", "browser to read: "+browserList()+" (default: capture.browser, else auto-select)")
	captureTabsCmd.Flags().String("browser-profile", "", `browser profile: display name ("Work") or folder name ("Profile 1") (default: last used)`)
}

func browserList() string {
	names := make([]string, 0, len(chromium.Browsers()))
	for _, b := range chromium.Browsers() {
		names = append(names, string(b))
	}
	return strings.Join(names, ", ")
}

// tabsReport is the structured result of one capture tabs run. The same
// document answers --dry-run (dry_run: true, statuses would_send /
// denied / duplicate) and a real run (sent / failed / denied /
// duplicate).
type tabsReport struct {
	Command  string       `json:"command"`
	Browser  string       `json:"browser"`
	Profile  tabsProfile  `json:"profile"`
	Session  tabsSession  `json:"session"`
	Tabs     []tabOutcome `json:"tabs"`
	Warnings []string     `json:"warnings"`
	Summary  tabsSummary  `json:"summary"`
	DryRun   bool         `json:"dry_run"`
}

type tabsProfile struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

type tabsSession struct {
	AsOf  time.Time `json:"as_of"`
	Path  string    `json:"path"`
	Stale bool      `json:"stale"`
}

type tabOutcome struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	// Decision is the filter's reason code (urlfilter.Reason); Reason
	// is its human rendering.
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	Status   string `json:"status"`
	JobID    string `json:"job_id,omitempty"`
	Error    string `json:"error,omitempty"`
	Index    int    `json:"index"`
	Window   int32  `json:"window"`
	Allowed  bool   `json:"allowed"`
	// QueuedLocally marks a URL enqueued on the local queue because no
	// configured server answered.
	QueuedLocally bool `json:"queued_locally,omitempty"`
}

type tabsSummary struct {
	Total     int `json:"total"`
	WouldSend int `json:"would_send"`
	Sent      int `json:"sent"`
	Denied    int `json:"denied"`
	Deduped   int `json:"deduped"`
	Failed    int `json:"failed"`
}

func runCaptureTabs(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	stderr := cmd.ErrOrStderr()
	verbose, _ := cmd.Root().PersistentFlags().GetCount("verbose")

	browserFlag, _ := cmd.Flags().GetString("browser")
	profileFlag, _ := cmd.Flags().GetString("browser-profile")
	profile, err := resolveCaptureTarget(cmd, browserFlag, profileFlag)
	if err != nil {
		return err
	}
	browser := profile.Browser

	report := tabsReport{
		Command:  cmd.CommandPath(),
		Browser:  string(browser),
		Profile:  tabsProfile{Name: profile.Name, Dir: profile.DirName},
		DryRun:   kitcli.IsDryRun(cmd),
		Tabs:     []tabOutcome{},
		Warnings: []string{},
	}
	warn := func(msg string) {
		report.Warnings = append(report.Warnings, msg)
		fmt.Fprintln(stderr, "warning: "+msg)
	}

	var urlCfg urlfilter.Config
	if cfg != nil {
		urlCfg = cfg.Capture.URLFilter
	}
	for _, key := range unmatchedProfileKeys(urlCfg, browser) {
		warn(fmt.Sprintf("capture.url_filter.browsers.%s.profiles.%s matches no %s profile; its rules apply to nothing",
			browser, key, browser))
	}
	filter, err := urlCfg.For(string(browser), profile.DirName, profile.Name)
	if err != nil {
		return fmt.Errorf("capture.url_filter: %w", err)
	}

	sess, err := chromium.OpenSession(ctx, profile.Dir)
	if err != nil {
		if errors.Is(err, chromium.ErrNoSessionFile) {
			e := output.NotFoundError(fmt.Sprintf("%s profile %q has no session file", browser, profile.Name))
			e.SuggestedFix = "open the profile in the browser once, then re-run"
			return e
		}
		return fmt.Errorf("read %s profile %q session: %w", browser, profile.Name, err)
	}
	age := time.Since(sess.ModTime)
	report.Session = tabsSession{AsOf: sess.ModTime, Path: sess.Path, Stale: age > staleSessionAfter}
	if verbose > 0 {
		fmt.Fprintf(stderr, "session file: %s\n", sess.Path)
	}
	if report.Session.Stale {
		warn(fmt.Sprintf("session last written %s ago; the browser may be closed, so these are the tabs open as of %s",
			roundAge(age), sess.ModTime.Format(time.DateTime)))
	}

	evaluateTabs(&report, sess.Tabs, filter)

	if !report.DryRun {
		sendTabs(ctx, cmd, &report)
	}

	if err := renderTabsReport(cmd.OutOrStdout(), report); err != nil {
		return err
	}
	if report.Summary.Failed > 0 {
		attempted := report.Summary.Sent + report.Summary.Failed
		return fmt.Errorf("capture tabs: %d of %d sends failed", report.Summary.Failed, attempted)
	}
	return nil
}

// unmatchedProfileKeys returns the capture.url_filter.browsers.<b>.profiles
// keys that name no existing profile of browser, sorted. A typo there
// would silently leave a profile unfiltered.
func unmatchedProfileKeys(c urlfilter.Config, browser chromium.Browser) []string {
	var keys []string
	for bk, bc := range c.Browsers {
		if !strings.EqualFold(bk, string(browser)) {
			continue
		}
		keys = append(keys, profileKeys(bc)...)
	}
	if len(keys) == 0 {
		return nil
	}
	profiles, err := chromium.Profiles(browser)
	if err != nil {
		return nil
	}
	var out []string
	for _, k := range keys {
		matched := false
		for _, p := range profiles {
			if strings.EqualFold(k, p.DirName) || strings.EqualFold(k, p.Name) {
				matched = true
				break
			}
		}
		if !matched {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func profileKeys(bc urlfilter.BrowserConfig) []string {
	keys := make([]string, 0, len(bc.Profiles))
	for k := range bc.Profiles {
		keys = append(keys, k)
	}
	return keys
}

// evaluateTabs runs every tab through the filter and marks repeats of
// an allowed URL as duplicates. Every allowed, first-seen URL starts as
// would_send. Decisions are logged without the URL.
func evaluateTabs(report *tabsReport, tabs []chromium.Tab, filter urlfilter.Evaluator) {
	seen := make(map[string]bool, len(tabs))
	for _, t := range tabs {
		d := filter.Evaluate(t.URL)
		o := tabOutcome{
			Window:   t.WindowID,
			Index:    t.Index,
			URL:      t.URL,
			Title:    t.Title,
			Allowed:  d.Allowed,
			Decision: string(d.Reason),
			Reason:   d.String(),
		}
		switch {
		case !d.Allowed:
			o.Status = tabStatusDenied
			report.Summary.Denied++
		case seen[t.URL]:
			o.Status = tabStatusDuplicate
			report.Summary.Deduped++
		default:
			seen[t.URL] = true
			o.Status = tabStatusWouldSend
			report.Summary.WouldSend++
		}
		slog.Debug("capture tabs: filter decision",
			"window", t.WindowID, "index", t.Index, "status", o.Status, "decision", d)
		report.Tabs = append(report.Tabs, o)
	}
	report.Summary.Total = len(tabs)
}

// sendTabs enqueues every would_send URL through the configured
// endpoints — the routing, tokens and local fallback `ctxt analyze`
// uses — with the request `ctxt capture <url>` builds. A failure is
// recorded on its tab and the loop moves on.
func sendTabs(ctx context.Context, cmd *cobra.Command, report *tabsReport) {
	bridge := idxbridge.New(idxbridge.Config{
		Endpoints:       clientEndpoints(),
		AnalyzeFallback: idxbridge.AnalyzeFunc(localDirectAnalyze),
		WarnWriter:      cmd.ErrOrStderr(),
	})
	focusProfile, _ := cmd.Flags().GetString("profile")

	for i := range report.Tabs {
		o := &report.Tabs[i]
		if o.Status != tabStatusWouldSend {
			continue
		}
		jobID, servedBy, err := bridge.Analyze(ctx, service.AnalyzeRequest{
			Content: o.URL,
			Source:  o.URL,
			Type:    "text",
			Profile: focusProfile,
		})
		if err != nil {
			o.Status = tabStatusFailed
			o.Error = sendError(err)
			report.Summary.Failed++
			slog.Debug("capture tabs: send failed", "window", o.Window, "index", o.Index)
			continue
		}
		o.Status = tabStatusSent
		o.JobID = jobID
		o.QueuedLocally = servedBy == ""
		report.Summary.Sent++
	}
	report.Summary.WouldSend = 0
}

func sendError(err error) string {
	var rerr *idxbridge.RemoteError
	if errors.As(err, &rerr) {
		return fmt.Sprintf("server returned %d: %s", rerr.StatusCode, strings.TrimSpace(rerr.Body))
	}
	return err.Error()
}

func renderTabsReport(w io.Writer, r tabsReport) error {
	if isJSONOutput() {
		return outputJSON(w, r)
	}

	fmt.Fprintf(w, "%s profile %q (%s): session as of %s (%s ago)\n",
		r.Browser, r.Profile.Name, r.Profile.Dir,
		r.Session.AsOf.Format(time.DateTime), roundAge(time.Since(r.Session.AsOf)))

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, o := range r.Tabs {
		switch {
		case r.DryRun:
			reason := o.Reason
			if o.Status == tabStatusDuplicate {
				reason = "duplicate of an earlier tab"
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", statusLabel(o.Status), o.URL, displayTitle(o.Title), reason)
		case o.Status == tabStatusSent:
			detail := "job " + o.JobID
			if o.QueuedLocally {
				detail += " (queued locally)"
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", statusLabel(o.Status), o.URL, detail)
		case o.Status == tabStatusFailed:
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", statusLabel(o.Status), o.URL, o.Error)
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	s := r.Summary
	if r.DryRun {
		_, err := fmt.Fprintf(w, "dry run: %d would be sent, %d denied, %d deduped (%s); nothing sent\n",
			s.WouldSend, s.Denied, s.Deduped, tabCount(s.Total))
		return err
	}
	_, err := fmt.Fprintf(w, "sent %d, denied %d, deduped %d, failed %d (%s)\n",
		s.Sent, s.Denied, s.Deduped, s.Failed, tabCount(s.Total))
	return err
}

func tabCount(n int) string {
	if n == 1 {
		return "1 tab"
	}
	return fmt.Sprintf("%d tabs", n)
}

func statusLabel(status string) string {
	return strings.ReplaceAll(status, "_", " ")
}

// displayTitle quotes a tab title for a one-line listing: control
// characters flattened, long titles shortened.
func displayTitle(title string) string {
	const maxRunes = 60
	r := []rune(strings.Map(func(c rune) rune {
		if unicode.IsControl(c) {
			return ' '
		}
		return c
	}, title))
	if len(r) > maxRunes {
		r = append(r[:maxRunes-1], '…')
	}
	return fmt.Sprintf("%q", string(r))
}

// roundAge renders an age in its largest whole unit: 42s, 7m, 3h, 2d.
func roundAge(d time.Duration) string {
	switch {
	case d < 0:
		return "0s"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	}
}
