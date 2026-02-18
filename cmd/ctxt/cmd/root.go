package cmd

import (
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/config"
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

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "ctxt",
	Short: "ContextHelp - Your agentic context brain",
	Long: `ctxt is the user-facing interface for ContextHelp.

ContextHelp provides universal capture, semantic search, and intelligent
composition of your knowledge. It's local-first, offline-capable, and
designed to augment both human and agent workflows.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.config/contexthelp/config.yaml)")
	rootCmd.PersistentFlags().String("profile", "", "focus profile to use")
	rootCmd.PersistentFlags().String("output", "text", "output format (text|json|yaml)")

	// Bind flags to viper
	viper.BindPFlag("profile.default", rootCmd.PersistentFlags().Lookup("profile"))
	viper.BindPFlag("output.format", rootCmd.PersistentFlags().Lookup("output"))
}

// initConfig reads in config file and ENV variables if set.
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
