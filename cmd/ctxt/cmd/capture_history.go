package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ambient/browserhistory"
	"github.com/ideacrafterslabs/ctxt/internal/ambient/position"
	"github.com/ideacrafterslabs/ctxt/internal/browser/chromium"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/idxbridge"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/timeframe"
	"github.com/ideacrafterslabs/ctxt/internal/urlfilter"
	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
	"hop.top/kit/go/console/output"
)

// historySampleSize caps how many allowed and how many denied visits a
// dry run lists.
const historySampleSize = 10

// Run modes, as reported in the structured output.
const (
	historyModeIncremental = "incremental"
	historyModeSince       = "since"
	historyModeBackfill    = "backfill"
)

// Where an incremental run started, as reported in position.start.
const (
	historyStartSaved    = "saved"
	historyStartLookback = "initial_lookback"
	historyStartSince    = "since"
)

const historyTimeLayout = "2006-01-02 15:04:05 MST"

var captureHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Capture the browsing history of one browser profile",
	Long: `Send the pages visited in one profile of a Chromium-family browser
(chrome, brave, edge, arc, chromium, vivaldi) to the configured ctxt
server. History is read from a copy of the profile's History database;
the browser can stay open.

Three modes, picked by the time flags:

  no time flag      incremental: send visits newer than the saved
                    position for this browser profile, then move the
                    position to the last one handed off. The first run
                    reads back capture.history.initial_lookback (24h).
  --since X alone   send visits from X to now, then move the position
                    forward to the last one handed off (never back).
  --until, --range  bounded backfill: send that window; the saved
                    position is neither read nor changed.

When a send fails, the position stops just before that visit, so the
next run retries it and skips nothing.

Every URL goes through the capture.url_filter rules for that browser and
profile first. Identical URLs are sent once per run.

--dry-run prints the count per range and a sample of allowed and denied
visits with the reason, sends nothing and neither reads nor writes the
saved position; an incremental dry run previews the first-run window.

--reset-position forgets this browser profile's saved position and
exits; the next incremental run starts from the initial lookback.

Exit status: 0 every allowed visit was accepted, 1 any send failed, 2 bad
invocation (unknown browser or profile, bad time value, bad
capture.history.initial_lookback), 3 the browser, profile folder or
History database is not on disk, or the position file is corrupt.`,
	Args: cobra.NoArgs,
	RunE: runCaptureHistory,
}

func init() {
	captureCmd.AddCommand(captureHistoryCmd)
	cliconv.WithSideEffect(captureHistoryCmd, cliconv.SideEffectWrite)
	// An incremental run sends only what is new since the saved position;
	// a backfill re-sends its window and the server's content fingerprint
	// dedupes the objects.
	cliconv.WithIdempotency(captureHistoryCmd, cliconv.IdempotencyConditional)
	cliconv.WithExamples(captureHistoryCmd, []cliconv.Example{
		{Title: "Preview the last seven days", Command: "ctxt capture history --browser brave --browser-profile Work --since 7d --dry-run"},
		{Title: "Send what is new since the last run", Command: "ctxt capture history --browser brave --browser-profile Work"},
		{Title: "Backfill one day", Command: "ctxt capture history --browser brave --browser-profile Work --range 2026-09-10"},
		{Title: "Start over from the initial lookback", Command: "ctxt capture history --browser brave --browser-profile Work --reset-position"},
	})
	cliconv.WithNextSteps(captureHistoryCmd, []cliconv.NextStep{
		{When: "after a dry run", Suggest: "ctxt capture history --browser <b> --browser-profile <p> --since <time>", Reason: "send the allowed visits"},
		{When: "to keep it running", Suggest: "ctxt capture schedule install --browser <b> --browser-profile <p>", Reason: "run incremental captures on a timer"},
		{When: "on success", Suggest: "ctxt find <query>", Reason: "search the captured pages"},
	})

	f := captureHistoryCmd.Flags()
	f.String("browser", "", "browser to read (default: capture.browser, then auto-select): "+browserList())
	f.String("browser-profile", "", `browser profile: display name ("Work") or folder name ("Profile 1")`)
	f.String("since", "", "send visits from this time (RFC 3339, YYYY-MM-DD, or 30m/12h/7d/2w ago)")
	f.String("until", "", "bounded backfill: send visits before this time (a bare date includes that day)")
	f.StringArray("range", nil, "bounded backfill window FROM..TO, or one YYYY-MM-DD; repeatable")
	f.String("tz", "", "IANA time zone for dates and display (default: local)")
	f.Bool("reset-position", false, "forget this browser profile's saved position, then exit")
}

// historyReport is the structured result of one capture history run.
type historyReport struct {
	Command  string           `json:"command"`
	Browser  string           `json:"browser"`
	Profile  tabsProfile      `json:"profile"`
	Mode     string           `json:"mode"`
	Ranges   []historyRange   `json:"ranges"`
	Position *historyPosition `json:"position"`
	// Visits lists every visit sent or failed; a dry run lists a sample
	// of allowed and denied visits instead (Sampled).
	Visits   []visitOutcome `json:"visits"`
	Sampled  bool           `json:"sampled"`
	Warnings []string       `json:"warnings"`
	Summary  historySummary `json:"summary"`
	DryRun   bool           `json:"dry_run"`
}

// historyRange is one half-open window read; nil ends are open.
type historyRange struct {
	From    *time.Time `json:"from"`
	To      *time.Time `json:"to"`
	Visits  int        `json:"visits"`
	Allowed int        `json:"allowed"`
	Denied  int        `json:"denied"`
	Deduped int        `json:"deduped"`
}

// historyPosition reports the saved position of a run that uses one.
type historyPosition struct {
	Key    string     `json:"key"`
	Path   string     `json:"path"`
	Start  string     `json:"start"`
	Before *time.Time `json:"before"`
	After  *time.Time `json:"after"`
}

type visitOutcome struct {
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	VisitedAt time.Time `json:"visited_at"`
	Decision  string    `json:"decision"`
	Reason    string    `json:"reason"`
	Status    string    `json:"status"`
	JobID     string    `json:"job_id,omitempty"`
	Error     string    `json:"error,omitempty"`
	Allowed   bool      `json:"allowed"`
	// QueuedLocally marks a URL enqueued on the local queue because no
	// configured server answered.
	QueuedLocally bool `json:"queued_locally,omitempty"`
}

type historySummary struct {
	Total     int `json:"total"`
	WouldSend int `json:"would_send"`
	Sent      int `json:"sent"`
	Denied    int `json:"denied"`
	Deduped   int `json:"deduped"`
	Failed    int `json:"failed"`
}

// historyOptions are the validated flags of one run.
type historyOptions struct {
	tf       timeframe.Flags
	loc      *time.Location
	reset    bool
	dryRun   bool
	lookback time.Duration
}

func runCaptureHistory(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	stderr := cmd.ErrOrStderr()
	verbose, _ := cmd.Root().PersistentFlags().GetCount("verbose")

	opts, err := historyFlags(cmd)
	if err != nil {
		return err
	}
	now := time.Now()
	ranges, err := timeframe.Resolve(opts.tf, timeframe.Options{Now: func() time.Time { return now }, Location: opts.loc})
	if err != nil {
		return output.UsageError(err.Error())
	}

	profile, err := resolveHistoryTarget(cmd)
	if err != nil {
		return err
	}
	client, err := browserhistory.NewChromiumClient(profile.Browser, profile.DirName)
	if err != nil {
		return fmt.Errorf("open %s profile %q history: %w", profile.Browser, profile.Name, err)
	}
	key := client.Name()

	statePath, err := position.DefaultPath()
	if err != nil {
		return err
	}
	store := position.New(statePath)
	if verbose > 0 {
		fmt.Fprintf(stderr, "position file: %s\n", statePath)
	}

	if opts.reset {
		return resetHistoryPosition(cmd, store, key, opts)
	}

	histPath := filepath.Join(profile.Dir, chromium.HistoryFile)
	if _, err := os.Stat(histPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			e := output.NotFoundError(fmt.Sprintf("%s profile %q has no History database", profile.Browser, profile.Name))
			e.SuggestedFix = "open the profile in the browser once, then re-run"
			return e
		}
		return fmt.Errorf("read %s profile %q history: %w", profile.Browser, profile.Name, err)
	}
	if verbose > 0 {
		fmt.Fprintf(stderr, "history file: %s\n", histPath)
	}

	report := historyReport{
		Command:  cmd.CommandPath(),
		Browser:  string(profile.Browser),
		Profile:  tabsProfile{Name: profile.Name, Dir: profile.DirName},
		DryRun:   opts.dryRun,
		Ranges:   []historyRange{},
		Visits:   []visitOutcome{},
		Sampled:  opts.dryRun,
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
	for _, k := range unmatchedProfileKeys(urlCfg, profile.Browser) {
		warn(fmt.Sprintf("capture.url_filter.browsers.%s.profiles.%s matches no %s profile; its rules apply to nothing",
			profile.Browser, k, profile.Browser))
	}
	filter, err := urlCfg.For(string(profile.Browser), profile.DirName, profile.Name)
	if err != nil {
		return fmt.Errorf("capture.url_filter: %w", err)
	}

	// Pick the windows to read and, for the modes that keep a position,
	// where it stands now. A dry run never touches the store.
	type window struct{ from, to time.Time }
	var windows []window
	var pos *historyPosition
	switch {
	case !opts.tf.IsSet():
		report.Mode = historyModeIncremental
		start := now.Add(-opts.lookback)
		startKind := historyStartLookback
		var before *time.Time
		if !opts.dryRun {
			saved, ok, err := store.Get(key)
			if err != nil {
				return positionError(err)
			}
			if ok {
				start, startKind, before = saved, historyStartSaved, &saved
			}
		}
		windows = []window{{from: start}}
		if !opts.dryRun {
			pos = &historyPosition{Key: key, Path: statePath, Start: startKind, Before: before}
		}
	case opts.tf.Since != "" && opts.tf.Until == "" && len(opts.tf.Ranges) == 0:
		report.Mode = historyModeSince
		windows = []window{{from: ranges[0].From}}
		if !opts.dryRun {
			// Fail on an unusable store before anything is sent.
			saved, ok, err := store.Get(key)
			if err != nil {
				return positionError(err)
			}
			pos = &historyPosition{Key: key, Path: statePath, Start: historyStartSince}
			if ok {
				pos.Before = &saved
			}
		}
	default:
		report.Mode = historyModeBackfill
		for _, r := range ranges {
			windows = append(windows, window{from: r.From, to: r.To})
		}
	}
	report.Position = pos

	seen := map[string]bool{}
	var all []visitOutcome
	for _, w := range windows {
		var visits []browserhistory.Visit
		if report.Mode == historyModeIncremental {
			visits, err = visitsAfter(ctx, client, w.from)
		} else {
			visits, err = client.VisitsBetween(ctx, w.from, w.to)
		}
		if err != nil {
			return fmt.Errorf("read %s profile %q history: %w", profile.Browser, profile.Name, err)
		}
		hr := historyRange{From: timePtr(w.from), To: timePtr(w.to), Visits: len(visits)}
		for _, v := range visits {
			o := evaluateVisit(v, filter, seen)
			switch o.Status {
			case tabStatusDenied:
				hr.Denied++
				report.Summary.Denied++
			case tabStatusDuplicate:
				hr.Deduped++
				report.Summary.Deduped++
			default:
				hr.Allowed++
				report.Summary.WouldSend++
			}
			all = append(all, o)
		}
		report.Summary.Total += len(visits)
		report.Ranges = append(report.Ranges, hr)
	}

	if opts.dryRun {
		report.Visits = historySample(all)
	} else {
		sendVisits(ctx, cmd, all, &report.Summary)
		for _, o := range all {
			if o.Status == tabStatusSent || o.Status == tabStatusFailed {
				report.Visits = append(report.Visits, o)
			}
		}
		if pos != nil {
			if t, ok := safeAdvancePoint(all); ok {
				stored, err := store.Advance(key, t)
				if err != nil {
					// The visits are already handed off; report them, then
					// fail on the position.
					_ = renderHistoryReport(cmd.OutOrStdout(), report, opts)
					return positionError(err)
				}
				pos.After = &stored
			} else {
				pos.After = pos.Before
			}
		}
	}

	if err := renderHistoryReport(cmd.OutOrStdout(), report, opts); err != nil {
		return err
	}
	if report.Summary.Failed > 0 {
		attempted := report.Summary.Sent + report.Summary.Failed
		return fmt.Errorf("capture history: %d of %d sends failed", report.Summary.Failed, attempted)
	}
	return nil
}

// resolveHistoryTarget maps --browser / --browser-profile (either may be
// omitted) to a profile on disk via the shared capture selection rules.
func resolveHistoryTarget(cmd *cobra.Command) (chromium.Profile, error) {
	browserFlag, _ := cmd.Flags().GetString("browser")
	profileFlag, _ := cmd.Flags().GetString("browser-profile")
	return resolveCaptureTarget(cmd, browserFlag, profileFlag)
}

// historyFlags reads and validates every flag that needs no disk access.
func historyFlags(cmd *cobra.Command) (historyOptions, error) {
	var o historyOptions
	fl := cmd.Flags()
	for _, name := range []string{"since", "until", "tz"} {
		if fl.Changed(name) {
			if v, _ := fl.GetString(name); strings.TrimSpace(v) == "" {
				return o, output.UsageError(fmt.Sprintf("--%s needs a value", name))
			}
		}
	}
	o.tf.Since, _ = fl.GetString("since")
	o.tf.Until, _ = fl.GetString("until")
	o.tf.Ranges, _ = fl.GetStringArray("range")
	emptyRange := fl.Changed("range") && len(o.tf.Ranges) == 0
	for _, r := range o.tf.Ranges {
		emptyRange = emptyRange || strings.TrimSpace(r) == ""
	}
	if emptyRange {
		return o, output.UsageError("--range needs a value (FROM..TO, or YYYY-MM-DD)")
	}
	o.tf.Since, o.tf.Until = strings.TrimSpace(o.tf.Since), strings.TrimSpace(o.tf.Until)

	o.loc = time.Local
	if tz, _ := fl.GetString("tz"); tz != "" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return o, output.UsageError(fmt.Sprintf("--tz %q: unknown time zone (use an IANA name such as Europe/Paris)", tz))
		}
		o.loc = loc
	}

	o.reset, _ = fl.GetBool("reset-position")
	if o.reset && o.tf.IsSet() {
		return o, output.UsageError("--reset-position cannot be combined with --since, --until or --range")
	}
	o.dryRun = kitcli.IsDryRun(cmd)

	if !o.tf.IsSet() && !o.reset {
		o.lookback = config.DefaultCaptureHistoryInitialLookback
		if cfg != nil {
			hc := cfg.Capture.History
			if err := hc.Validate(); err != nil {
				return o, output.UsageError(err.Error())
			}
			o.lookback = hc.InitialLookback
		}
	}
	return o, nil
}

// resetHistoryPosition implements --reset-position.
func resetHistoryPosition(cmd *cobra.Command, store *position.Store, key string, o historyOptions) error {
	w := cmd.OutOrStdout()
	if o.dryRun {
		_, err := fmt.Fprintf(w, "dry run: would reset the saved position %q in %s; nothing changed\n", key, store.Path())
		return err
	}
	if err := store.Reset(key); err != nil {
		return positionError(err)
	}
	if isJSONOutput() {
		return outputJSON(w, map[string]any{
			"command": cmd.CommandPath(),
			"reset":   key,
			"path":    store.Path(),
		})
	}
	_, err := fmt.Fprintf(w, "reset the saved position %q in %s; the next incremental run starts from capture.history.initial_lookback\n",
		key, store.Path())
	return err
}

// positionError maps a position store failure: a corrupt file is exit 3
// and is never reset or rewritten.
func positionError(err error) error {
	if errors.Is(err, position.ErrCorrupt) {
		e := output.WrapError(err, output.CodeNotFound, 3)
		e.SuggestedFix = "fix the file by hand, or delete it (that resets every browser profile)"
		return e
	}
	return err
}

// visitsAfter pages through VisitsSince until it comes back empty.
func visitsAfter(ctx context.Context, c browserhistory.BrowserClient, since time.Time) ([]browserhistory.Visit, error) {
	var all []browserhistory.Visit
	cursor := since
	for {
		batch, err := c.VisitsSince(ctx, cursor)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			return all, nil
		}
		all = append(all, batch...)
		last := batch[len(batch)-1].VisitedAt
		if !last.After(cursor) {
			return all, nil // defensive: a reader that does not move on
		}
		cursor = last
	}
}

// evaluateVisit runs one visit through the filter; a repeat of an
// allowed URL already seen this run is a duplicate. Decisions are logged
// without the URL.
func evaluateVisit(v browserhistory.Visit, filter *urlfilter.Filter, seen map[string]bool) visitOutcome {
	d := filter.Evaluate(v.URL)
	o := visitOutcome{
		URL:       v.URL,
		Title:     v.Title,
		VisitedAt: v.VisitedAt,
		Allowed:   d.Allowed,
		Decision:  string(d.Reason),
		Reason:    d.String(),
	}
	switch {
	case !d.Allowed:
		o.Status = tabStatusDenied
	case seen[v.URL]:
		o.Status = tabStatusDuplicate
	default:
		seen[v.URL] = true
		o.Status = tabStatusWouldSend
	}
	slog.Debug("capture history: filter decision", "status", o.Status, "decision", d)
	return o
}

// sendVisits enqueues every would_send visit through the configured
// endpoints with the request `ctxt capture <url>` builds, like capture
// tabs. A failure is recorded on its visit and the loop moves on.
func sendVisits(ctx context.Context, cmd *cobra.Command, visits []visitOutcome, s *historySummary) {
	bridge := idxbridge.New(idxbridge.Config{
		Endpoints:       clientEndpoints(),
		AnalyzeFallback: idxbridge.AnalyzeFunc(localDirectAnalyze),
		WarnWriter:      cmd.ErrOrStderr(),
	})
	focusProfile, _ := cmd.Flags().GetString("profile")

	for i := range visits {
		o := &visits[i]
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
			s.Failed++
			slog.Debug("capture history: send failed")
			continue
		}
		o.Status = tabStatusSent
		o.JobID = jobID
		o.QueuedLocally = servedBy == ""
		s.Sent++
	}
	s.WouldSend = 0
}

// safeAdvancePoint returns the newest visit time the position may move
// to: every visit up to it was handed off, denied or deduped. visits are
// oldest first. With a failed send, only visits strictly older than the
// first failure count, so a visit sharing its time with the failure is
// not skipped either (the next run reads strictly after the position).
func safeAdvancePoint(visits []visitOutcome) (time.Time, bool) {
	var limit time.Time
	for _, o := range visits {
		if o.Status == tabStatusFailed {
			limit = o.VisitedAt
			break
		}
	}
	var best time.Time
	for _, o := range visits {
		if !limit.IsZero() && !o.VisitedAt.Before(limit) {
			break
		}
		if o.VisitedAt.After(best) {
			best = o.VisitedAt
		}
	}
	return best, !best.IsZero()
}

// historySample picks the first allowed and the first denied visits, up
// to historySampleSize each, in time order.
func historySample(all []visitOutcome) []visitOutcome {
	out := []visitOutcome{}
	var allowed, denied int
	for _, o := range all {
		switch {
		case o.Status == tabStatusWouldSend && allowed < historySampleSize:
			allowed++
		case o.Status == tabStatusDenied && denied < historySampleSize:
			denied++
		default:
			continue
		}
		out = append(out, o)
	}
	return out
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func renderHistoryReport(w io.Writer, r historyReport, o historyOptions) error {
	if isJSONOutput() {
		return outputJSON(w, r)
	}
	at := func(t *time.Time, open string) string {
		if t == nil {
			return open
		}
		return t.In(o.loc).Format(historyTimeLayout)
	}

	head := fmt.Sprintf("%s profile %q (%s): ", r.Browser, r.Profile.Name, r.Profile.Dir)
	switch {
	case r.Mode == historyModeIncremental && r.DryRun:
		head += fmt.Sprintf("dry run of an incremental run over the first-run window (capture.history.initial_lookback %s); saved position not read", o.lookback)
	case r.Mode == historyModeIncremental && r.Position.Start == historyStartSaved:
		head += "incremental, new visits since the saved position"
	case r.Mode == historyModeIncremental:
		head += fmt.Sprintf("incremental, first run: visits from the last %s (capture.history.initial_lookback)", o.lookback)
	case r.Mode == historyModeSince && r.DryRun:
		head += "visits since --since; saved position not read"
	case r.Mode == historyModeSince:
		head += "visits since --since; the saved position moves forward to the last one handed off"
	default:
		head += "backfill; saved position untouched"
	}
	if _, err := fmt.Fprintln(w, head); err != nil {
		return err
	}
	for _, hr := range r.Ranges {
		fmt.Fprintf(w, "  %s .. %s: %s: %d allowed, %d denied, %d deduped\n",
			at(hr.From, "start of history"), at(hr.To, "now"), visitCount(hr.Visits), hr.Allowed, hr.Denied, hr.Deduped)
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, v := range r.Visits {
		switch {
		case r.DryRun:
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", statusLabel(v.Status),
				v.VisitedAt.In(o.loc).Format(time.DateTime), v.URL, displayTitle(v.Title), v.Reason)
		case v.Status == tabStatusSent:
			detail := "job " + v.JobID
			if v.QueuedLocally {
				detail += " (queued locally)"
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", statusLabel(v.Status), v.URL, detail)
		case v.Status == tabStatusFailed:
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", statusLabel(v.Status), v.URL, v.Error)
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	s := r.Summary
	if r.DryRun {
		if s.WouldSend > historySampleSize || s.Denied > historySampleSize {
			fmt.Fprintf(w, "(sample: first %d allowed and first %d denied visits)\n", historySampleSize, historySampleSize)
		}
		_, err := fmt.Fprintf(w, "dry run: %d would be sent, %d denied, %d deduped (%s); nothing sent\n",
			s.WouldSend, s.Denied, s.Deduped, visitCount(s.Total))
		return err
	}
	if _, err := fmt.Fprintf(w, "sent %d, denied %d, deduped %d, failed %d (%s)\n",
		s.Sent, s.Denied, s.Deduped, s.Failed, visitCount(s.Total)); err != nil {
		return err
	}
	if p := r.Position; p != nil {
		var err error
		switch {
		case p.After == nil:
			_, err = fmt.Fprintf(w, "position %q: none saved yet (nothing handed off)\n", p.Key)
		case p.Before != nil && p.After.Equal(*p.Before):
			_, err = fmt.Fprintf(w, "position %q: unchanged at %s\n", p.Key, at(p.After, ""))
		default:
			_, err = fmt.Fprintf(w, "position %q: saved at %s\n", p.Key, at(p.After, ""))
		}
		return err
	}
	return nil
}

func visitCount(n int) string {
	if n == 1 {
		return "1 visit"
	}
	return fmt.Sprintf("%d visits", n)
}
