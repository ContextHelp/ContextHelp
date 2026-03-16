package cmd

import (
	"encoding/json"
	"fmt"
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
	Use:   "dpkms",
	Short: "dPKMS - Decentralized knowledge substrate",
	Long: `dpkms is the infrastructure layer for ContextHelp.

dPKMS (Decentralized Personal Knowledge Management Substrate) provides
the storage, job queue, pipeline runtime, and API server that powers
ContextHelp's knowledge management capabilities.`,
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
		return cmd.Help()
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.config/contexthelp/config.yaml)")
	rootCmd.PersistentFlags().String("data-dir", "", "data directory override")
	rootCmd.PersistentFlags().String("server-url", "http://localhost:8080", "dpkms server URL")
	rootCmd.PersistentFlags().BoolP("verbose", "V", false, "enable verbose output")
	rootCmd.Flags().BoolP("version", "v", false, "print version and exit")

	// Bind flags to viper
	viper.BindPFlag("storage.path", rootCmd.PersistentFlags().Lookup("data-dir"))
	viper.BindPFlag("server.url", rootCmd.PersistentFlags().Lookup("server-url"))
}

func printVersion(cmd *cobra.Command) {
	date := strings.SplitN(buildTime, "_", 2)[0]
	if viper.GetString("output.format") == "json" {
		out, _ := json.Marshal(map[string]string{
			"name":       "dpkms",
			"version":    version,
			"date":       date,
			"git_commit": gitCommit,
		})
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "dpkms version %s (%s)\n", version, date)
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

	// Initialize API client
	serverURL := viper.GetString("server.url")
	initPipelineClient(serverURL)
}

// SetVersionInfo sets version information for the CLI
func SetVersionInfo(v, bt, gc string) {
	version = v
	buildTime = bt
	gitCommit = gc
}
