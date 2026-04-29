package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"charm.land/fang/v2"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/logger"
	internalversion "github.com/ideacrafterslabs/ctxt/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	kitcli "hop.top/kit/go/console/cli"
)

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
		Help:    kitcli.HelpConfig{Disclaimer: dpkmsLong},
		Globals: []kitcli.Flag{
			{Name: "config", Usage: "config file (default $XDG_CONFIG_HOME/contexthelp/config.yaml)"},
			{Name: "data-dir", Usage: "data directory override"},
			{Name: "server-url", Default: "http://localhost:8080", Usage: "dpkms server URL"},
			{Name: "offline", Usage: "disable all network calls; force local-only operation"},
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

	// Hidden deprecated --output alias for --format.
	rootCmd.PersistentFlags().String("output", "", "(deprecated) alias for --format")
	_ = rootCmd.PersistentFlags().MarkHidden("output")

	rootCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if v, _ := cmd.Flags().GetBool("version"); v {
			printVersion(cmd)
			return nil
		}
		return cmd.Help()
	}

	// Mirror kit/cli bindings into the global viper used throughout the codebase.
	pf := rootCmd.PersistentFlags()
	for _, name := range []string{"format", "quiet", "no-color", "verbose", "no-hints", "chdir", "config", "data-dir", "server-url", "offline", "output"} {
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

	// Single PersistentPreRunE: chains kit's chdir hook + verbose logger init
	// + the --output→--format compatibility shim.
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

// Execute runs dpkms via fang (styled help + errors). We pass WithoutVersion
// so fang doesn't intercept --version; ctxt has its own format with --check.
func Execute() error {
	rootCmd.InitDefaultCompletionCmd()
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
