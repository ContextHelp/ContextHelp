package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/logger"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	cfg     *config.Config

	// Version information
	version   string
	buildTime string
	gitCommit string
)

var rootCmd = &cobra.Command{
	Use:   "ctxt [content]",
	Short: "ContextHelp - Your agentic context brain",
	Long: `ctxt is the user-facing interface for ContextHelp.

ContextHelp provides universal capture, semantic search, and intelligent
composition of your knowledge. It's local-first, offline-capable, and
designed to augment both human and agent workflows.

If called without a subcommand, it defaults to 'analyze', capturing content 
from arguments, stdin, or the clipboard.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		verbose, _ := cmd.PersistentFlags().GetBool("verbose")
		logger.Init(verbose)
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if ok, _ := cmd.Flags().GetBool("version"); ok {
			printVersion(cmd)
			return nil
		}
		if len(args) == 1 && strings.HasPrefix(args[0], "ctxt://") {
			return dispatchURI(cmd, args[0])
		}
		return RunAnalyze(cmd, args)
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.config/contexthelp/config.yaml)")
	rootCmd.PersistentFlags().String("profile", "", "focus profile to use")
	rootCmd.PersistentFlags().String("output", "text", "output format (text|json|yaml)")
	rootCmd.PersistentFlags().BoolP("verbose", "V", false, "enable verbose output")
	rootCmd.Flags().BoolP("version", "v", false, "print version and exit")

	// Bind flags to viper
	viper.BindPFlag("profile.default", rootCmd.PersistentFlags().Lookup("profile"))
	viper.BindPFlag("output.format", rootCmd.PersistentFlags().Lookup("output"))
	viper.BindPFlag("cli.verbose", rootCmd.PersistentFlags().Lookup("verbose"))
}

func printVersion(cmd *cobra.Command) {
	date := strings.SplitN(buildTime, "_", 2)[0]
	if viper.GetString("output.format") == "json" {
		out, _ := json.Marshal(map[string]string{
			"name":       "ctxt",
			"version":    version,
			"date":       date,
			"git_commit": gitCommit,
		})
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "ctxt version %s (%s)\n", version, date)
}

func initConfig() {
	var err error
	cfg, err = config.Load(cfgFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load config: %v\n", err)
		// Continue with defaults
		cfg = &config.Config{}
	}

	// Ensure required directories exist
	if err := config.EnsureConfigDir(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to ensure config directory: %v\n", err)
	}
	if err := config.EnsureDataDir(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to ensure data directory: %v\n", err)
	}
}

// SetVersionInfo sets version information for the CLI
func SetVersionInfo(v, bt, gc string) {
	version = v
	buildTime = bt
	gitCommit = gc
}

// dispatchURI handles ctxt:// URIs passed directly as an argument (e.g. from
// OS URL handler after `ctxt uri register`).
//
// Routing:
//   - ctxt://<objectID>         → ctxt open <objectID>
//   - ctxt://search/<query>     → ctxt find <query>
func dispatchURI(cmd *cobra.Command, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid ctxt:// URI %q: %w", raw, err)
	}

	switch {
	case u.Host == "search":
		query := strings.TrimPrefix(u.Path, "/")
		return runFind(cmd, []string{query})
	default:
		objectID := u.Host
		if objectID == "" {
			return fmt.Errorf("empty object ID in URI %q", raw)
		}
		return runOpen(cmd, []string{objectID})
	}
}
