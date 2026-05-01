package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/logger"
	"github.com/ideacrafterslabs/ctxt/internal/telemetry"
	"github.com/ideacrafterslabs/ctxt/internal/tui"
	internalversion "github.com/ideacrafterslabs/ctxt/internal/version"
	"charm.land/fang/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	kitcli "hop.top/kit/go/console/cli"
)

const longDescription = `ctxt is the user-facing interface for ContextHelp.

ContextHelp provides universal capture, semantic search, and intelligent
composition of your knowledge. It's local-first, offline-capable, and
designed to augment both human and agent workflows.

If called without a subcommand, it defaults to 'analyze', capturing content
from arguments, stdin, or the clipboard.`

var (
	cfgFile string
	cfg     *config.Config
	tel     telemetry.Telemetry

	version   string
	buildTime string
	gitCommit string

	// root is initialised at package load — BEFORE any init() in this package
	// runs — so subcommands' init() functions can call rootCmd.AddCommand.
	root = kitcli.New(kitcli.Config{
		Name:    "ctxt",
		Version: "dev", // overwritten by SetVersionInfo
		Short:   "ContextHelp - Your agentic context brain",
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
			{Name: "config", Usage: "config file (default $XDG_CONFIG_HOME/contexthelp/config.yaml)"},
			{Name: "profile", Usage: "focus profile to use"},
			{Name: "offline", Usage: "disable all network calls; force local-only operation"},
			{Name: "instance", Usage: "target dpkms instance by name or port (overrides current-instance state and config)"},
		},
	})
	rootCmd = root.Cmd
)

func init() {
	rootCmd.Use = "ctxt [content]"
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
	rootCmd.SuggestionsMinimumDistance = 2

	// Take ownership of --version + --check from cobra/fang. We render
	// the legacy "ctxt version <semver> (<date>)" format and append the
	// optional update check inline. rootCmd.Version stays empty so cobra
	// doesn't auto-handle --version.
	rootCmd.Flags().BoolP("version", "v", false, "print version and exit")
	rootCmd.Flags().Bool("check", false, "check for a newer release (use with -v)")

	// Wrap RunE later so we can short-circuit on --version.

	// --output is a hidden alias for --format (kit/cli built-in) so existing
	// callers and scripts keep working. Both write to viper key "format".
	rootCmd.PersistentFlags().String("output", "", "(deprecated) alias for --format")
	_ = rootCmd.PersistentFlags().MarkHidden("output")
	root.Viper.RegisterAlias("output", "format")

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
	cfgFlag := rootCmd.PersistentFlags().Lookup("config")
	cfgFlag.NoOptDefVal = ""

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

	// Single PersistentPreRunE: chains kit/cli's chdir hook + verbose logger
	// init + the --output→--format compatibility shim.
	chdirHook := rootCmd.PersistentPreRunE
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if chdirHook != nil {
			if err := chdirHook(cmd, args); err != nil {
				return err
			}
		}
		if outFlag := cmd.Root().PersistentFlags().Lookup("output"); outFlag != nil && outFlag.Changed {
			val := outFlag.Value.String()
			_ = cmd.Root().PersistentFlags().Set("format", val)
			viper.Set("format", val)
		}
		count, _ := cmd.Root().PersistentFlags().GetCount("verbose")
		logger.Init(count > 0)
		return nil
	}

	cobra.OnInitialize(initConfig)
}

// commandGroups maps each top-level subcommand name to its help-output
// group. Subcommands not listed fall through to the default "COMMANDS"
// group. Edit here to move a command between groups; no need to touch
// individual <name>.go files.
var commandGroups = map[string]string{
	// CAPTURE — get content into ctxt
	"analyze": "capture", "import": "capture", "ingest": "capture",
	"inbox": "capture", "feed": "capture", "watch": "capture",

	// KNOWLEDGE — read & navigate the graph
	"find": "knowledge", "list": "knowledge", "show": "knowledge",
	"link": "knowledge",
	"log": "knowledge", "stats": "knowledge",

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
func Execute() error {
	ctx := context.Background()
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
	return fang.Execute(ctx, rootCmd,
		fang.WithoutVersion(),
	)
}

func printVersion(cmd *cobra.Command) {
	check, _ := cmd.Flags().GetBool("check")
	date := strings.SplitN(buildTime, "_", 2)[0]

	if viper.GetString("output.format") == "json" {
		payload := map[string]string{
			"name":       "ctxt",
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

	fmt.Fprintf(cmd.OutOrStdout(), "ctxt version %s (%s)\n", version, date)
	if check {
		r := internalversion.Check(version, nil)
		fmt.Fprintln(cmd.OutOrStdout(), internalversion.FormatResult(r))
	}
}

func initConfig() {
	cfgFile = viper.GetString("config")
	var err error
	cfg, err = config.Load(cfgFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load config: %v\n", err)
		cfg = &config.Config{}
	}

	if err := config.EnsureConfigDir(); err != nil {
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

func SetVersionFetcher(f internalversion.Fetcher) {
	internalversion.DefaultFetcher = f
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
