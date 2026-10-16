package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate completion script",
	Long: `Generate shell completion scripts for dpkms.

To load completions:

Bash:
  $ source <(dpkms completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ dpkms completion bash > /etc/bash_completion.d/dpkms
  # macOS:
  $ dpkms completion bash > /usr/local/etc/bash_completion.d/dpkms

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it.  You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ dpkms completion zsh > "${fpath[1]}/_dpkms"

  # You will need to start a new shell for this setup to take effect.

Fish:
  $ dpkms completion fish | source

  # To load completions for each session, execute once:
  $ dpkms completion fish > ~/.config/fish/completions/dpkms.fish

PowerShell:
  PS> dpkms completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> dpkms completion powershell > dpkms.ps1
  # and source this file from your PowerShell profile.
`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		var err error
		switch args[0] {
		case "bash":
			err = cmd.Root().GenBashCompletion(os.Stdout)
		case "zsh":
			err = cmd.Root().GenZshCompletion(os.Stdout)
		case "fish":
			err = cmd.Root().GenFishCompletion(os.Stdout, true)
		case "powershell":
			err = cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
		}
		if err != nil {
			return fmt.Errorf("write %s completion script: %w", args[0], err)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(completionCmd)
}
