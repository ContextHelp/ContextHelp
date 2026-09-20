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
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/logger"
	internalversion "github.com/ideacrafterslabs/ctxt/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	kitcli "hop.top/kit/go/console/cli"
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
		// progress); we use it for the --output→--format compatibility shim
		// and verbose logger init.
		Hooks: kitcli.Hooks{
			PrePersistentRunE: func(cmd *cobra.Command, _ []string) error {
				// Fatal -c/--config failure stashed by initConfig
				// (cobra.OnInitialize cannot return). Abort before any
				// command body can run against the default config.
				if initConfigErr != nil {
					return initConfigErr
				}
				if outFlag := cmd.Root().PersistentFlags().Lookup("output"); outFlag != nil && outFlag.Changed {
					val := outFlag.Value.String()
					_ = cmd.Root().PersistentFlags().Set("format", val)
					viper.Set("format", val)
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
	//   chdir → identity → peer → progress → our hook (output shim + logger init)

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

// ExitCodeFor maps an error returned from Execute to a process exit
// code. PolicyDeniedError (and the equivalent ErrPolicyDenied surfaced
// by the API client when the daemon returns 409 POLICY_DENIED) maps
// to 4 — the kit-canonical CONFLICT exit. Any other non-nil error
// maps to 1; nil maps to 0.
func ExitCodeFor(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, ErrPolicyDenied) {
		return 4
	}
	return 1
}

// Execute runs dpkms via fang (styled help + errors). We pass WithoutVersion
// so fang doesn't intercept --version; ctxt has its own format with --check.
func Execute() error {
	rootCmd.InitDefaultCompletionCmd()
	applyCommandGroups()
	root.ApplyGroupVisibility()
	for _, c := range rootCmd.Commands() {
		if c.Name() == "completion" {
			c.GroupID = "management"
			break
		}
	}
	return fang.Execute(context.Background(), rootCmd,
		fang.WithoutVersion(),
	)
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
