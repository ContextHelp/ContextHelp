package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"charm.land/fang/v2"
	"github.com/ideacrafterslabs/ctxt/internal/cli/banner"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/logger"
	"github.com/ideacrafterslabs/ctxt/internal/telemetry"
	"github.com/ideacrafterslabs/ctxt/internal/tui"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	kitcli "hop.top/kit/go/console/cli"
)

// binName selects which file in the shared `contexthelp/` config
// namespace this binary reads. ctxt reads contexthelp/ctxt.yaml.
const binName = "ctxt"

const longDescription = `ctxt is the user-facing interface for ContextHelp.

ContextHelp provides universal capture, semantic search, and intelligent
composition of your knowledge. It's local-first, offline-capable, and
designed to augment both human and agent workflows.

If called without a subcommand, it defaults to 'analyze', capturing content
from arguments, stdin, or the clipboard.`

var (
	cfgFile string
	cfg     *config.Config
	// tel holds the process-wide Telemetry sink. telemetry.New currently
	// returns NoopTelemetry on every path, so nothing reads it yet; it is the
	// documented wiring point for a real backend (see internal/telemetry).
	//nolint:unused // assigned in initConfig; consumed once a real backend lands
	tel telemetry.Telemetry

	version   string
	buildTime string
	gitCommit string

	// root is initialised at package load — BEFORE any init() in this package
	// runs — so subcommands' init() functions can call rootCmd.AddCommand.
	root = kitcli.New(kitcli.Config{
		Name:    "ctxt",
		Version: "dev", // overwritten by SetVersionInfo
		Short:   "ContextHelp - Your agentic context brain",
		// MaxTopLevelVerbs raises kit's default of 10 because ctxt is a
		// meta-tool (capture + curate + compose + manage) with a wider
		// surface than the average single-purpose CLI. Silent grouping
		// would be a UX-breaking change to documented verbs like
		// `ctxt analyze`, `ctxt find`, `ctxt show`, so we accept the
		// expanded depth-1 fan-out. Re-evaluate at the next major.
		MaxTopLevelVerbs: 30,
		// ValidationFailureError surfaces Validate() failures as a
		// returned error from Execute() (cmd/ctxt/main.go prints +
		// exits 1) instead of kit's default ValidationFailureExit
		// (os.Exit(2) inside kit). Same exit code class, but the
		// surface is testable from `go test` callers that drive
		// Execute() directly and friendlier to mid-sprint fan-out
		// agents who are still closing 12fcc bucket items.
		ValidationFailureMode: kitcli.ValidationFailureError,
		// T-0595 enables full strict-gate enforcement at boot. The
		// 12fcc-conformance track verified every depth-1 and depth-2+
		// leaf carries kit/side-effect, kit/idempotent, kit/examples
		// (and kit/next-steps + kit/destructive-token where applicable),
		// so the live Root now enforces what the build-tagged probe
		// previewed in T-0592. Removing any of these flags is a
		// regression: ctxt will refuse to start if a newly added
		// command leaves an annotation off. See
		// docs/sprints/12fcc-conformance-baseline.md and the
		// regression guard TestRootValidate_StrictGatesPass in
		// cmd/ctxt/cmd/strict_validation_test.go.
		EnforceGuidance:         true,
		EnforceDryRunRationale:  true,
		EnforceDestructiveToken: true,
		SignatureStrictness:     kitcli.SignatureStrictnessReject,
		PassthroughStrictness:   "reject",
		Help: kitcli.HelpConfig{
			Disclaimer: longDescription,
			Groups: []kitcli.GroupConfig{
				{ID: "capture", Title: "CAPTURE"},
				{ID: "knowledge", Title: "KNOWLEDGE"},
				{ID: "compose", Title: "COMPOSE"},
				{ID: "curate", Title: "CURATE"},
				{ID: "organize", Title: "ORGANIZE"},
				{ID: "interact", Title: "INTERACT"},
				{ID: "instance", Title: "INSTANCE"},
				{ID: "deprecated", Title: "DEPRECATED", Hidden: true},
			},
		},
		Globals: []kitcli.Flag{
			{Name: "profile", Usage: "focus profile to use"},
			{Name: "instance", Usage: "target dpkms instance by name or port (overrides current-instance state and config)"},
		},
		// Hook runs after kit's built-in chain (chdir → identity → peer →
		// progress); we use it to reject an unknown --format and to init
		// the verbose logger.
		Hooks: kitcli.Hooks{
			PrePersistentRunE: func(cmd *cobra.Command, _ []string) error {
				// Fatal -c/--config failure stashed by initConfig
				// (cobra.OnInitialize cannot return). Abort before any
				// command body can run against the default config.
				if initConfigErr != nil {
					return initConfigErr
				}
				// Publish the executing command so --format resolves
				// from the real flag, then reject an unknown value
				// before any command body runs — a bogus value must
				// never reach a renderer that would fall back to the
				// human table under exit 0.
				cliformat.Bind(cmd)
				if err := cliformat.ValidateActive(); err != nil {
					return err
				}
				count, _ := cmd.Root().PersistentFlags().GetCount("verbose")
				logger.Init(count > 0)
				// Upgrade banner (ADR-070 §5, T-0580). Reads the
				// shadow file under config.RunDir(); no-op when no
				// upgrade is in flight. Failure to render the banner
				// MUST NOT block the underlying command.
				_ = banner.Inject(cmd.ErrOrStderr())
				return nil
			},
		},
	})
	rootCmd = root.Cmd
)

func init() {
	rootCmd.Use = "ctxt [content]"
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
	rootCmd.SuggestionsMinimumDistance = 2

	// Take ownership of --version from cobra/fang. We render the legacy
	// "ctxt version <semver> (<date>)" format. rootCmd.Version stays empty
	// so cobra doesn't auto-handle --version. Update checks moved to
	// `ctxt upgrade check`.
	rootCmd.Flags().BoolP("version", "v", false, "print version and exit")

	// Wrap RunE later so we can short-circuit on --version.

	// kit/cli registers --output (-o) as the output-path flag. We do NOT
	// re-register it here: the previous code defined --output as a hidden
	// alias for --format, which collides with kit's path semantics and
	// panics at init time on every invocation.

	rootCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if v, _ := cmd.Flags().GetBool("version"); v {
			printVersion(cmd)
			return nil
		}
		if len(args) == 1 && strings.HasPrefix(args[0], "ctxt://") {
			return dispatchURI(cmd, args[0])
		}
		return RunAnalyze(cmd, args)
	}

	v := root.Viper

	// Mirror kit/cli's bindings into the GLOBAL viper used throughout this
	// codebase (helpers.go, stats.go, etc. read from viper.Get*). Kit owns
	// its private viper for parsing; we re-bind every persistent flag here.
	pf := rootCmd.PersistentFlags()
	for _, name := range []string{"format", "quiet", "no-color", "verbose", "config", "profile", "offline", "instance", "output", "no-hints", "chdir"} {
		if f := pf.Lookup(name); f != nil {
			_ = viper.BindPFlag(name, f)
		}
	}
	// Aliases the rest of the codebase reads.
	viper.RegisterAlias("output.format", "format")
	viper.RegisterAlias("profile.default", "profile")
	viper.RegisterAlias("offline.enabled", "offline")
	viper.RegisterAlias("cli.verbose", "verbose")
	_ = viper.BindEnv("instance", "CTXT_INSTANCE")
	_ = v.BindEnv("instance", "CTXT_INSTANCE")

	// PersistentPreRunE chain is wired via kitcli.Config.Hooks.PrePersistentRunE
	// at package load (see root var block above). Kit composes:
	//   chdir → identity → peer → progress → our hook (format gate + logger init)

	cobra.OnInitialize(initConfig)
}

// commandGroups maps each top-level subcommand name to its help-output
// group. Subcommands not listed fall through to the default "COMMANDS"
// group. Edit here to move a command between groups; no need to touch
// individual <name>.go files.
var commandGroups = map[string]string{
	// CAPTURE — get content into ctxt
	"analyze": "capture", "capture": "capture", "import": "capture",
	"ingest": "capture", "inbox": "capture", "feed": "capture",
	"watch": "capture",

	// KNOWLEDGE — read & navigate the graph
	"find": "knowledge", "list": "knowledge", "show": "knowledge",
	"link": "knowledge",
	"log":  "knowledge", "stats": "knowledge",

	// COMPOSE — synthesize knowledge into outputs
	"compose": "compose", "export": "compose", "page": "compose",
	"remind": "compose", "resurface": "compose",

	// CURATE — modify the graph
	"edit": "curate", "delete": "curate", "classify": "curate",
	"reprocess": "curate",

	// ORGANIZE — taxonomy + scoping
	"entity": "organize", "index": "organize", "profile": "organize",
	"registry": "organize", "doctor": "organize",

	// INTERACT — interactive surfaces
	"shell": "interact", "tui": "interact", "setup": "interact",

	// INSTANCE — talk to a specific dpkms
	"instance": "instance", "audit": "instance",

	// MANAGEMENT — hidden by default; --help-all to show
	"config": "management", "uri": "management", "version": "management",
	"upgrade": "management",

	// DEPRECATED — moved to dpkms; hidden by default
	"detector": "deprecated", "dev": "deprecated", "job": "deprecated",
	"key": "deprecated", "secret": "deprecated",
}

// applyCommandGroups assigns GroupID to every top-level subcommand based
// on the commandGroups map. Run after all init() functions have called
// rootCmd.AddCommand, before help is rendered.
func applyCommandGroups() {
	for _, c := range rootCmd.Commands() {
		if g, ok := commandGroups[c.Name()]; ok {
			c.GroupID = g
		}
	}
}

// Execute runs the root command via fang (styled help + errors). We bypass
// fang's --version handling because ctxt has its own --version flag with
// build-date and --check support; the fang default would override our RunE.
//
// Unlike kit's root.Execute() we cannot delegate wholesale: kit calls
// fang.WithVersion which would intercept --version. We replicate the
// minimum kit pre-flight steps (completion registration, group
// visibility, shape annotations, Validate()) and then call fang
// directly with WithoutVersion().
func Execute() error {
	ctx := context.Background()
	if err := prepareTree(); err != nil {
		return err
	}

	// Resolve the invocation before dispatch. A word naming no child of
	// a non-runnable command never reaches an Args validator or kit's
	// RunE middleware — cobra renders the group's help and exits 0 — so
	// the refusal has to be raised here, ahead of fang.
	if err := checkUnknownSubcommand(rootCmd, os.Args[1:]); err != nil {
		return refuseUnknownSubcommand(rootCmd, err)
	}

	return fang.Execute(ctx, rootCmd,
		fang.WithoutVersion(),
		// Single stderr writer for a failed run: suppresses errors
		// WrapRunE already rendered so one failure is not printed
		// twice, once as an envelope and once as fang prose.
		fang.WithErrorHandler(envelopeErrorHandler),
	)
}

// prepareTree applies every pre-flight step Execute performs before
// handing the tree to fang: kit's setup, boot-time validation, and the
// error-envelope middleware.
//
// Split out of Execute so tests can assert on the prepared tree.
// Execute's remaining statement runs the CLI for real, which a test
// cannot drive, and the one step most easily lost in a merge —
// root.WrapRunE — is invisible until a command fails under --format
// json. Every step here is idempotent, so calling this twice is safe.
func prepareTree() error {
	// kit/cli.Execute would call fang.WithVersion(); we need WithoutVersion
	// so fang doesn't intercept --version. Replicate kit's other setup.
	rootCmd.InitDefaultCompletionCmd()
	applyCommandGroups()
	root.ApplyGroupVisibility()
	for _, c := range rootCmd.Commands() {
		if c.Name() == "completion" {
			c.GroupID = "management"
			break
		}
	}

	// Stamp kit/top-level-verb on every depth-1 runnable leaf and
	// kit/hierarchical on the depth-2 intermediates whose subtrees
	// reach depth >= 3. Done after applyCommandGroups so the walk
	// sees the same shape the validator does.
	applyShapeAnnotations()

	// Boot-time strict validation (T-0593). Kit's Root.Execute() runs
	// this implicitly; ctxt has to call it explicitly because we use
	// fang.WithoutVersion(). Per Config.ValidationFailureMode=Error,
	// failures surface as a returned error rather than os.Exit(2) so
	// main.go's "Error: %v" path handles them uniformly.
	if root.Config.EnforceValidate {
		if err := root.Validate(); err != nil {
			return err
		}
	}

	// Promote bare command errors to kit envelopes naming their class
	// (NOT_FOUND, CONFLICT, PREREQUISITE, USAGE). Must run BEFORE
	// WrapRunE: kit's middleware generalizes anything still bare to
	// GENERIC/1 at the outermost layer, so a classifier installed
	// after it would only ever see that.
	installErrorClassification(rootCmd)

	// Structured-error envelope (12fcc Factor 4) AND kit's RunE
	// middleware chain, of which the confirmation gate is the
	// outermost link — one call installs both. A returned error is
	// rendered through output.RenderError, which honors --format, so
	// JSON/YAML callers get kit's envelope (code, message, exit_code,
	// transience, suggested fix) instead of prose to regex. Without
	// it the kit/side-effect and kit/destructive-token annotations
	// every destructive leaf carries are inert: --confirm-token is
	// never registered as a flag and `ctxt inbox clear` runs
	// unchallenged. It also installs the usage-classification seams
	// (FlagErrorFunc + Args wrappers) that give a flag-parse or arity
	// failure Code=USAGE / ExitCode=2 for ExitCodeFor to read.
	//
	// kit's own Root.Execute calls this; ctxt replicates that method
	// rather than delegating (see the comment above) and had dropped
	// this step. Idempotent — leaves already wrapped are skipped — so
	// a second Execute in the same process is safe. See errenvelope.go.
	root.WrapRunE()

	return nil
}

// applyShapeAnnotations stamps the structural annotations the kit
// shape validator demands:
//
//   - kit/top-level-verb on every depth-1 runnable leaf
//   - kit/hierarchical on every intermediate ancestor (depth >= 1,
//     depth <= 2 in practice) of a depth-3+ leaf
//
// Both keys describe the tree's *structure* — not the leaves'
// semantics — so they live in one centralised walk next to root
// wiring instead of per-leaf init(). The signature validator's
// depth-hierarchical check requires kit/hierarchical on EVERY
// intermediate up to (but excluding) the root, so this helper walks
// each depth-3+ leaf's ancestor chain and stamps each one.
//
// See kit/go/console/cli/shape.go and validate_signature.go.
func applyShapeAnnotations() {
	if rootCmd == nil {
		return
	}
	for _, c := range rootCmd.Commands() {
		// Skip built-ins; kit exempts them from shape validation.
		switch c.Name() {
		case "completion", helpWord:
			continue
		}
		// Depth-1 runnable nodes: stamp top-level-verb. Kit's shape
		// validator treats a depth-1 cmd as a "leaf" purely on
		// Runnable(), independent of whether it also carries
		// subcommands (e.g. `ctxt lateral` runs AND has `lateral
		// config`, `lateral eval` underneath). Pure groups (not
		// runnable) don't need the annotation.
		if c.Runnable() {
			cliconv.MarkTopLevelVerb(c)
		}
	}
	// Walk the tree once to find depth-3+ leaves, then stamp every
	// intermediate ancestor up to the root.
	markHierarchicalAncestors(rootCmd, 0)
}

// markHierarchicalAncestors recursively walks cmd's subtree. For
// every runnable leaf at depth >= 3, it stamps kit/hierarchical on
// every ancestor between the root (exclusive) and the leaf
// (exclusive). The annotation is idempotent so multiple sibling
// leaves under the same intermediate cost nothing extra.
func markHierarchicalAncestors(cmd *cobra.Command, depth int) {
	if cmd == nil {
		return
	}
	switch cmd.Name() {
	case "completion", helpWord:
		return
	}
	if cmd.Runnable() && !hasSubcommands(cmd) && depth >= 3 {
		for p := cmd.Parent(); p != nil && p != rootCmd; p = p.Parent() {
			cliconv.MarkHierarchical(p)
		}
		return
	}
	for _, child := range cmd.Commands() {
		markHierarchicalAncestors(child, depth+1)
	}
}

// hasSubcommands reports whether cmd carries non-built-in children.
// Mirrors kit's "non-leaf" check without importing the internal
// helper.
func hasSubcommands(cmd *cobra.Command) bool {
	for _, c := range cmd.Commands() {
		switch c.Name() {
		case "completion", helpWord:
			continue
		}
		return true
	}
	return false
}

func printVersion(cmd *cobra.Command) {
	date := strings.SplitN(buildTime, "_", 2)[0]

	if viper.GetString("output.format") == "json" {
		payload := map[string]string{
			"name":       "ctxt",
			"version":    version,
			"date":       date,
			"git_commit": gitCommit,
		}
		out, _ := json.Marshal(payload)
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return
	}

	fmt.Fprintf(cmd.OutOrStdout(), "ctxt version %s (%s)\n", version, date)
}

// initConfigErr carries a fatal config-bootstrap failure out of
// initConfig. cobra.OnInitialize hooks cannot return an error, and
// calling os.Exit from one would bypass fang's error rendering and
// break in-process tests. Instead initConfig stashes the error here and
// the PersistentPreRunE hook (which cobra runs immediately after the
// OnInitialize chain) returns it, so the failure travels the normal
// Execute() → main() path and yields a clean message plus exit 1.
var initConfigErr error

func initConfig() {
	initConfigErr = nil
	// kit/cli's -c/--config global supports both bare paths and key=value
	// overrides. ConfigArgs splits the two halves so we can layer them
	// through kit/core/config.Load. A non-nil parse error means the user
	// explicitly passed -c and it is invalid — fatal, never a fallback to
	// the default config (see config.Bootstrap).
	paths, overrides, parseErr := root.ConfigArgs()
	// Legacy global: callers (config doctor, validate, etc.) read this
	// to find the user-supplied config path. With kit's repeatable
	// -c/--config, later paths layer on top of earlier ones, so the
	// LAST path is the highest-precedence (effective) file — that's
	// what subcommands operating on "the" config should target.
	// Empty when no -c <path> was supplied; callers fall back to
	// config.GetConfigPath in that case.
	var err error
	cfg, cfgFile, err = config.Bootstrap(binName, paths, overrides, parseErr)
	if err != nil {
		initConfigErr = err
		cfg, cfgFile = nil, ""
		return
	}

	if err := config.EnsureConfigDir(binName); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to ensure config directory: %v\n", err)
	}
	if err := config.EnsureDataDir(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to ensure data directory: %v\n", err)
	}

	tel = telemetry.New(cfg)
}

func SetVersionInfo(v, bt, gc string) {
	version = v
	buildTime = bt
	gitCommit = gc
	if root != nil {
		root.Config.Version = strings.TrimPrefix(v, "v")
	}
}

// dispatchURI handles ctxt:// URIs passed directly as an argument (e.g. from
// OS URL handler after `ctxt uri register`).
//
// Routing:
//   - ctxt://<objectID>         → open <objectID>
//   - ctxt://search/<query>     → find <query>
//
// The ui.handler config key (default "cli") controls the presentation layer:
//   - "cli"  → prints to stdout via runShow / runFind
//   - "tui"  → opens the interactive terminal interface focused on the result
func dispatchURI(cmd *cobra.Command, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid ctxt:// URI %q: %w", raw, err)
	}

	handler := "cli"
	if cfg != nil && cfg.URI.Handler != "" {
		handler = cfg.URI.Handler
	}

	switch {
	case u.Host == "search":
		query := strings.TrimPrefix(u.Path, "/")
		if handler == "tui" {
			return dispatchURIViaTUI(tui.StartOpts{InitialQuery: query})
		}
		return runFind(cmd, []string{query})
	default:
		objectID := u.Host
		if objectID == "" {
			return fmt.Errorf("empty object ID in URI %q", raw)
		}
		if handler == "tui" {
			return dispatchURIViaTUI(tui.StartOpts{InitialObjectID: objectID})
		}
		return runShow(cmd, []string{objectID})
	}
}

func dispatchURIViaTUI(opts tui.StartOpts) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()
	return tui.RunWithOpts(tui.NewRealAdapter(svc), cfg, opts)
}
