package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/secrets"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"hop.top/kit/go/storage/secret"
)

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Manage secrets",
	Long: `Read and write secrets from the configured backend.

The active backend is set via secrets.backend in your config file.
Supported backends: env, keyring, agefile, onepassword, ghsecrets.

Examples:
  # Get a secret
  dpkms secret get OPENAI_API_KEY

  # Set a secret from stdin (default when stdin is not a TTY)
  printf '%s' "$VALUE" | dpkms secret set OPENAI_API_KEY

  # Set a secret from a file
  dpkms secret set OPENAI_API_KEY --from-file /path/to/secret

  # Set a secret via interactive secure prompt (no echo)
  dpkms secret set OPENAI_API_KEY --prompt

  # Show active backend config
  dpkms secret list

Note: dpkms refuses to accept secret values as positional CLI arguments
because they would leak into shell history, ps(1) output, and CI logs.
Always pass secrets via stdin, --from-file, or --prompt.`,
}

var secretGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a secret value",
	Args:  cobra.ExactArgs(1),
	RunE:  runSecretGet,
}

var (
	secretSetFromFile string
	secretSetPrompt   bool
)

var secretSetCmd = &cobra.Command{
	Use:   "set <key>",
	Short: "Set a secret value (read from stdin, --from-file, or --prompt)",
	Long: `Set a secret value for the given key.

The value MUST come from one of:
  - stdin (default when stdin is not a TTY)
  - --from-file <path>
  - --prompt (interactive secure prompt, no echo)

Passing the value as a positional argument is rejected to avoid leaking
the secret into shell history, ps(1) output, and CI logs.`,
	Args: cobra.ArbitraryArgs,
	RunE: runSecretSet,
}

var secretListCmd = &cobra.Command{
	Use:   "list",
	Short: "List secrets (or show backend config if enumeration not supported)",
	RunE:  runSecretList,
}

func init() {
	rootCmd.AddCommand(secretCmd)
	secretCmd.AddCommand(secretGetCmd)
	secretCmd.AddCommand(secretSetCmd)
	secretCmd.AddCommand(secretListCmd)

	secretSetCmd.Flags().StringVar(&secretSetFromFile, "from-file", "",
		"read secret value from file path (use - for stdin)")
	secretSetCmd.Flags().BoolVar(&secretSetPrompt, "prompt", false,
		"read secret value from an interactive secure prompt")

	cliconv.WithSideEffect(secretGetCmd, cliconv.SideEffectRead)
	cliconv.WithSideEffect(secretListCmd, cliconv.SideEffectRead)
	// secret set overwrites any existing value at the key, and the prior
	// value is not recoverable from the store. Destructive.
	cliconv.WithSideEffect(secretSetCmd, cliconv.SideEffectDestructive)
}

func runSecretGet(cmd *cobra.Command, args []string) error {
	key := args[0]

	store, err := secrets.New(cfg.Secrets)
	if err != nil {
		return fmt.Errorf("secret get: %w", err)
	}

	got, err := store.Get(context.Background(), key)
	if err != nil {
		return err
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]string{"key": key, "value": string(got.Value)})
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(got.Value))
	return nil
}

// readSecretValue returns the secret value from one of the allowed sources.
// It rejects positional values entirely — passing them is a usage error.
func readSecretValue(args []string, fromFile string, prompt bool, stdin *os.File) ([]byte, error) {
	// Reject positional secret values: they leak via shell history, ps(1), and CI logs.
	if len(args) > 1 {
		return nil, errors.New(
			"secret value must come from stdin, --from-file, or --prompt — never as a CLI argument (would leak via shell history)")
	}
	if len(args) < 1 {
		return nil, errors.New("secret set requires <key> argument")
	}

	sources := 0
	if fromFile != "" {
		sources++
	}
	if prompt {
		sources++
	}
	if sources > 1 {
		return nil, errors.New("--from-file and --prompt are mutually exclusive")
	}

	switch {
	case prompt:
		fmt.Fprint(os.Stderr, "Enter secret value: ")
		fd := int(stdin.Fd()) // #nosec G115 -- Fd() fits in int on supported platforms
		if !term.IsTerminal(fd) {
			return nil, errors.New("--prompt requires a TTY on stdin")
		}
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr) // newline after hidden input
		if err != nil {
			return nil, fmt.Errorf("prompt: %w", err)
		}
		if len(b) == 0 {
			return nil, errors.New("empty secret value not allowed")
		}
		return b, nil

	case fromFile != "":
		var b []byte
		var err error
		if fromFile == "-" {
			b, err = io.ReadAll(stdin)
		} else {
			b, err = os.ReadFile(fromFile) // #nosec G304 -- user-supplied path is intended
		}
		if err != nil {
			return nil, fmt.Errorf("read --from-file: %w", err)
		}
		// Trim a single trailing newline (common when piping `printf`
		// or reading lines from heredocs/files).
		b = trimSingleTrailingNewline(b)
		if len(b) == 0 {
			return nil, errors.New("empty secret value not allowed")
		}
		return b, nil

	default:
		// Default: read from stdin, refusing to accept input from a TTY
		// because the user would have to type the secret in plaintext.
		fd := int(stdin.Fd()) // #nosec G115 -- Fd() fits in int on supported platforms
		if term.IsTerminal(fd) {
			return nil, errors.New(
				"no secret source provided: pipe value via stdin, or use --from-file/--prompt")
		}
		b, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		b = trimSingleTrailingNewline(b)
		if len(b) == 0 {
			return nil, errors.New("empty secret value not allowed")
		}
		return b, nil
	}
}

func trimSingleTrailingNewline(b []byte) []byte {
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	if len(b) > 0 && b[len(b)-1] == '\r' {
		b = b[:len(b)-1]
	}
	return b
}

func runSecretSet(cmd *cobra.Command, args []string) error {
	value, err := readSecretValue(args, secretSetFromFile, secretSetPrompt, os.Stdin)
	if err != nil {
		return fmt.Errorf("secret set: %w", err)
	}
	key := args[0]

	store, err := secrets.New(cfg.Secrets)
	if err != nil {
		return fmt.Errorf("secret set: %w", err)
	}

	if err := store.Set(context.Background(), key, value); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Set %s\n", key)
	return nil
}

func runSecretList(cmd *cobra.Command, _ []string) error {
	backend := cfg.Secrets.Backend
	if backend == "" {
		backend = "env"
	}

	store, err := secrets.New(cfg.Secrets)
	if err != nil {
		return fmt.Errorf("secret list: %w", err)
	}

	keys, err := store.List(context.Background(), "")
	if err == nil {
		if isJSONOutput() {
			return outputJSON(os.Stdout, map[string]any{"backend": backend, "keys": keys})
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Secrets backend: %s\n", backend)
		if len(keys) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "  (no secrets stored)")
		} else {
			for _, k := range keys {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", k)
			}
		}
		return nil
	}
	if !errors.Is(err, secret.ErrNotSupported) {
		return fmt.Errorf("secret list: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"backend":           backend,
			"enumerable":        false,
			"keychain_service":  cfg.Secrets.KeychainService,
			"age_file":          cfg.Secrets.AgeFile,
			"age_identity_file": cfg.Secrets.AgeIdentityFile,
			"onepassword_vault": cfg.Secrets.OnePasswordVault,
			"gh_repo":           cfg.Secrets.GHRepo,
		})
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Secrets backend: %s\n", backend)
	switch backend {
	case "env":
		fmt.Fprintln(cmd.OutOrStdout(),
			"  Key enumeration not supported — set env vars manually.")
	case "keyring", "keychain":
		svc := cfg.Secrets.KeychainService
		if svc == "" {
			svc = "ctxt"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  service: %s\n", svc)
	case "onepassword", "1password":
		fmt.Fprintf(cmd.OutOrStdout(), "  vault: %s\n", cfg.Secrets.OnePasswordVault)
		fmt.Fprintln(cmd.OutOrStdout(),
			"  Key enumeration not supported — use: op item list --vault <vault>")
	}
	return nil
}
