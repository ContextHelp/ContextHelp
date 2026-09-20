package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"charm.land/fang/v2"
	"github.com/ideacrafterslabs/ctxt/internal/cli/banner"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/logger"
	internalversion "github.com/ideacrafterslabs/ctxt/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	kitcli "hop.top/kit/go/console/cli"
	"hop.top/kit/go/console/output"
)

// binName selects which file in the shared `contexthelp/` config
// namespace this binary reads. dpkms reads contexthelp/dpkms.yaml.
const binName = "dpkms"

const dpkmsLong = `dpkms is the infrastructure layer for ContextHelp.

dPKMS (Decentralized Personal Knowledge Management Substrate) provides
the storage, job queue, pipeline runtime, and API server that powers
ContextHelp's knowledge management capabilities.`

var (
	cfgFile string
	cfg     *config.Config

	version   string
	buildTime string
	gitCommit string

	// root is initialised at package load — BEFORE any init() in this package
	// runs — so subcommands' init() functions can call rootCmd.AddCommand.
	root = kitcli.New(kitcli.Config{
		Name:    "dpkms",
		Version: "dev",
		Short:   "dPKMS - Decentralized knowledge substrate",
		Help: kitcli.HelpConfig{
			Disclaimer: dpkmsLong,
			Groups: []kitcli.GroupConfig{
				{ID: "lifecycle", Title: "LIFECYCLE"},
				{ID: "data", Title: "DATA"},
				{ID: "pipelines", Title: "PIPELINES"},
				{ID: "security", Title: "SECURITY"},
				{ID: "dev", Title: "DEVELOPMENT"},
			},
		},
		Globals: []kitcli.Flag{
			{Name: "data-dir", Usage: "data directory override"},
			{Name: "server-url", Default: "http://localhost:8080", Usage: "dpkms server URL"},
			{Name: "instance", Usage: "name of dpkms instance to target (default: unnamed)"},
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
	rootCmd.Use = "dpkms"
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
	rootCmd.SuggestionsMinimumDistance = 2

	// Custom --version + --check (see ctxt root.go for rationale).
	rootCmd.Flags().BoolP("version", "v", false, "print version and exit")
	rootCmd.Flags().Bool("check", false, "check for a newer release (use with -v)")

	// kit/cli registers --output (-o) as the output-path flag. We do NOT
	// re-register it here: the previous code defined --output as a hidden
	// alias for --format, which collides with kit's path semantics and
	// panics at init time on every invocation.

	rootCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if v, _ := cmd.Flags().GetBool("version"); v {
			printVersion(cmd)
			return nil
		}
		return cmd.Help()
	}

	// Mirror kit/cli bindings into the global viper used throughout the codebase.
	pf := rootCmd.PersistentFlags()
	for _, name := range []string{"format", "quiet", "no-color", "verbose", "no-hints", "chdir", "config", "data-dir", "server-url", "offline", "instance", "output"} {
		if f := pf.Lookup(name); f != nil {
			_ = viper.BindPFlag(name, f)
		}
	}
	// Aliases used by the rest of the codebase.
	viper.RegisterAlias("storage.path", "data-dir")
	viper.RegisterAlias("server.url", "server-url")
	viper.RegisterAlias("output.format", "format")
	viper.RegisterAlias("offline.enabled", "offline")
	viper.RegisterAlias("cli.verbose", "verbose")

	// PersistentPreRunE chain is wired via kitcli.Config.Hooks.PrePersistentRunE
	// at package load (see root var block above). Kit composes:
	//   chdir → identity → peer → progress → our hook (format gate + logger init)

	cobra.OnInitialize(initConfig)
}

// commandGroups maps each top-level subcommand name to its help-output
// group. Edit here to move a command between groups.
var commandGroups = map[string]string{
	// LIFECYCLE — control running instances
	"serve": "lifecycle", "shutdown": "lifecycle", "reboot": "lifecycle",
	"ps": "lifecycle",

	// DATA — backup, restore, housekeeping
	"backup": "data", "restore": "data", "housekeeping": "data",

	// PIPELINES — ingestion machinery
	"pipeline": "pipelines", "detector": "pipelines", "job": "pipelines",

	// SECURITY — keys + secrets
	"key": "security", "secret": "security",

	// DEVELOPMENT — maintenance utilities
	"dev": "dev",

	// MANAGEMENT — hidden by default (kit auto-registers the group with
	// always-hidden semantics; opt-in via --help-all or --help-management).
	"version": "management", "install-deps": "management",
}

func applyCommandGroups() {
	for _, c := range rootCmd.Commands() {
		if g, ok := commandGroups[c.Name()]; ok {
			c.GroupID = g
		}
	}
}

// fallbackExitCode is the code dpkms reports for a failure it cannot
// place in kit's taxonomy, and for a class kit itself does not know.
//
// GENERIC (1) rather than 0 or a fresh number: 1 is the spec's
// catch-all failure slot, so an unclassified error still reads as a
// failure to every caller, and a class kit later adds surfaces as "not
// yet classified" rather than as a collision with USAGE or NOT_FOUND.
// [output.ExitCodeForClass] deliberately declines to pick a fallback
// (its second result is false for anything it does not define) so the
// choice is made here, once, in the open.
const fallbackExitCode = output.ExitGeneric

// ExitCodeFor maps an error returned from [Execute] to a process exit
// code, using kit's standard class table as the single source of truth.
// nil maps to 0.
//
// Resolution order, most specific first:
//
//  1. An error carrying a kit envelope (*output.Error, or anything
//     wrapping one) already names its class — kit's own seams build
//     these for flag-parse failures, Args-arity failures and the
//     unknown-subcommand refusal, and installErrorClassification builds
//     them for the shapes dpkms returns as bare errors. Its ExitCode is
//     authoritative; when the envelope carries only a Code, the class
//     table resolves it.
//  2. A dpkms-owned classification (see classifyErr), for an error that
//     reached here without passing through the RunE middleware.
//  3. fallbackExitCode.
//
// Previously this table had exactly one entry — ErrPolicyDenied → 4 —
// and everything else was 1. One central mapping rather than
// per-command exits: the classes are a property of the failure, not of
// which verb produced it, and a table each command keeps its own copy
// of is a table that drifts.
func ExitCodeFor(err error) int {
	if err == nil {
		return output.ExitOK
	}

	if code, ok := exitCodeFromEnvelope(err); ok {
		return code
	}

	if class, ok := classifyErr(err); ok {
		if code, known := output.ExitCodeForClass(class); known {
			return code
		}
	}

	return fallbackExitCode
}

// exitCodeFromEnvelope reads the exit code off a kit error envelope
// anywhere in err's chain. An envelope whose ExitCode is unset (a
// hand-built *output.Error that named only a Code) falls back to the
// class table, so the two spellings agree.
func exitCodeFromEnvelope(err error) (int, bool) {
	var env *output.Error
	if !errors.As(err, &env) || env == nil {
		return 0, false
	}
	if env.ExitCode != 0 {
		return env.ExitCode, true
	}
	if code, ok := output.ExitCodeForClass(env.Code); ok {
		return code, true
	}
	return 0, false
}

// Execute runs dpkms via fang (styled help + errors). We pass WithoutVersion
// so fang doesn't intercept --version; ctxt has its own format with --check.
func Execute() error {
	prepareTree()

	return fang.Execute(context.Background(), rootCmd,
		fang.WithoutVersion(),
		// Single stderr writer for a failed run: suppresses errors
		// WrapRunE already rendered so one failure is not printed
		// twice, once as an envelope and once as fang prose.
		fang.WithErrorHandler(envelopeErrorHandler),
	)
}

// prepareTree applies every pre-flight step Execute performs before
// handing the tree to fang: kit's setup and kit's RunE middleware
// chain.
//
// Split out of Execute so tests can assert on the prepared tree.
// Execute's remaining statement runs the CLI for real, which a test
// cannot drive, and the step most easily lost in a merge —
// root.WrapRunE — is invisible until a destructive command runs
// unguarded. Every step here is idempotent, so calling this twice is
// safe.
func prepareTree() {
	rootCmd.InitDefaultCompletionCmd()
	applyCommandGroups()
	root.ApplyGroupVisibility()
	for _, c := range rootCmd.Commands() {
		if c.Name() == "completion" {
			c.GroupID = "management"
			break
		}
	}

	// Kit's RunE middleware chain, of which the confirmation gate is
	// the outermost link. Without this call a kit/side-effect or
	// kit/destructive-token annotation on any leaf is an inert
	// declaration: --confirm-token is never registered as a flag and
	// destructive commands run unchallenged.
	//
	// kit's own Root.Execute calls this; dpkms replicates that method
	// rather than delegating (see the comment above) and had dropped
	// this step. Idempotent — leaves already wrapped are skipped — so
	// a second Execute in the same process is safe.
	//
	// Error classification is installed FIRST, and the order is load
	// bearing: kit's WrapRunE generalizes anything that does not
	// already carry an envelope to GENERIC / exit 1 at the outermost
	// layer. A classifier installed after it would only ever see that
	// GENERIC envelope and could no longer tell "unclassifiable" from
	// "not yet classified". Installed first, kit's wrapper finds the
	// envelope already present and passes it through untouched.
	installErrorClassification(rootCmd)
	root.WrapRunE()
}

func printVersion(cmd *cobra.Command) {
	check, _ := cmd.Flags().GetBool("check")
	date := strings.SplitN(buildTime, "_", 2)[0]

	if viper.GetString("output.format") == "json" {
		payload := map[string]string{
			"name":       "dpkms",
			"version":    version,
			"date":       date,
			"git_commit": gitCommit,
		}
		if check {
			r := internalversion.Check(version, nil)
			if r.FetchErr != nil {
				payload["update_check"] = "error: " + r.FetchErr.Error()
			} else if r.UpToDate {
				payload["update_check"] = "up_to_date"
			} else {
				payload["update_check"] = r.Latest
			}
		}
		out, _ := json.Marshal(payload)
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return
	}

	fmt.Fprintf(cmd.OutOrStdout(), "dpkms version %s (%s)\n", version, date)
	if check {
		r := internalversion.Check(version, nil)
		fmt.Fprintln(cmd.OutOrStdout(), internalversion.FormatResult(r))
	}
}

// initConfigErr carries a fatal config-bootstrap failure out of
// initConfig. cobra.OnInitialize hooks cannot return an error, and
// calling os.Exit from one would bypass fang's error rendering and
// break in-process tests. Instead initConfig stashes the error here and
// the PersistentPreRunE hook (which cobra runs immediately after the
// OnInitialize chain) returns it, so the failure travels the normal
// Execute() → main() path and yields a clean message plus exit 1.
//
// This matters most for dpkms: `housekeeping prune -c /typo.yaml` used
// to warn and then prune the DEFAULT database.
var initConfigErr error

func initConfig() {
	initConfigErr = nil
	// kit/cli's -c/--config global supports both bare paths and key=value
	// overrides. ConfigArgs splits the two halves so we can layer them
	// through kit/core/config.Load. A non-nil parse error means the user
	// explicitly passed -c and it is invalid — fatal, never a fallback to
	// the default config (see config.Bootstrap).
	paths, overrides, parseErr := root.ConfigArgs()
	// Legacy global: subcommands read this for the user-supplied path.
	// With kit's repeatable -c, later paths layer on top of earlier
	// ones, so the LAST is the effective (highest-precedence) file —
	// that's what targeting subcommands (edit, lint --fix, validate)
	// should operate on. Empty when no -c <path> was given.
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

	serverURL := viper.GetString("server.url")
	initPipelineClient(serverURL)
}

func SetVersionInfo(v, bt, gc string) {
	version = v
	buildTime = bt
	gitCommit = gc
	if root != nil {
		root.Config.Version = strings.TrimPrefix(v, "v")
	}
}

func SetVersionFetcher(f internalversion.Fetcher) {
	internalversion.DefaultFetcher = f
}
