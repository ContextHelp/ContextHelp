package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ideacrafterslabs/ctxt/internal/bundle"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configuration operations",
	Long: `Manage ContextHelp configuration.

Examples:
  # Show current configuration
  ctxt config show

  # Show configuration file path
  ctxt config path

  # Validate configuration
  ctxt config validate

  # Edit configuration in default editor
  ctxt config edit`,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current configuration",
	RunE:  runConfigShow,
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Show configuration file path",
	RunE:  runConfigPath,
}

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration and optionally scan for plaintext secrets",
	Long: `Validate configuration and optionally scan for plaintext secrets.

Checks that the configuration file is well-formed, then scans string fields for
values that look like plaintext API keys or passwords.

Exit code 1 when validation errors or secret warnings are found.`,
	RunE: runConfigValidate,
}

var configEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Edit configuration in default editor",
	RunE:  runConfigEdit,
}

var configLintCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Lint configuration: schema, secrets, permissions, deprecated keys",
	Long: `Full config file linter combining multiple checks.

Checks performed:
  - Schema validation (required fields, type checks, value ranges)
  - Secret scan (warns on plaintext API keys / passwords)
  - File permission check (warns if config is world- or group-readable)
  - Deprecated key detection (warns on removed or renamed fields)

Severity levels:
  ERROR  blocking issue — must be fixed
  WARN   advisory issue — should be fixed

Use --fix to auto-apply safe fixes (file permissions).

Exit code 0 when no findings; 1 when any finding is present (CI-safe).`,
	RunE: runConfigLint,
}

var configBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Export a signed config bundle (.zip + .sig)",
	Long: `Export configuration files as a signed Ed25519 bundle.

Produces two files in the output directory:
  ctxt-config-bundle-<timestamp>.zip     — config files + embedded public key
  ctxt-config-bundle-<timestamp>.zip.sig — Ed25519 signature over the zip

The public key is embedded in the bundle manifest for self-contained verification.

Requires a keypair generated with 'ctxt key init'.

Examples:
  ctxt config backup
  ctxt config backup --output /tmp/my-backup`,
	RunE: runConfigBackup,
}

var configRestoreCmd = &cobra.Command{
	Use:   "restore <bundle.zip>",
	Short: "Restore configuration from a bundle",
	Args:  cobra.ExactArgs(1),
	Long: `Restore configuration from a previously exported bundle.

With --verify, the Ed25519 signature is verified before any files are written.
The signature file is expected alongside the zip: <bundle>.zip.sig

Without --verify, the bundle is extracted without signature checking.

Examples:
  ctxt config restore ctxt-config-bundle-2026-03-25T12-00-00Z.zip --verify
  ctxt config restore ctxt-config-bundle-2026-03-25T12-00-00Z.zip`,
	RunE: runConfigRestore,
}

func init() {
	rootCmd.AddCommand(configCmd)

	// Add subcommands
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configPathCmd)
	configCmd.AddCommand(configValidateCmd)
	configCmd.AddCommand(configEditCmd)
	configCmd.AddCommand(configLintCmd)
	configCmd.AddCommand(configBackupCmd)
	configCmd.AddCommand(configRestoreCmd)

	// Flags for validate subcommand
	configValidateCmd.Flags().Bool("check-secrets", true,
		"scan config fields for plaintext secrets and warn")

	// Flags for lint subcommand
	configLintCmd.Flags().Bool("fix", false,
		"auto-apply safe fixes (e.g. chmod 600 on world-readable config file)")

	// Flags for backup subcommand
	configBackupCmd.Flags().String("output", "",
		"output directory for bundle files (default: current directory)")
	configBackupCmd.Flags().Bool("encrypt", false,
		"encrypt the bundle with AES-256-GCM + Argon2id (overrides backup.encrypt_by_default)")
	configBackupCmd.Flags().String("passphrase", "",
		"passphrase for --encrypt (not recommended for scripts; prefer env var or keychain)")

	// Flags for restore subcommand
	configRestoreCmd.Flags().Bool("verify", false,
		"verify Ed25519 signature before restoring (recommended)")
	configRestoreCmd.Flags().Bool("dry-run", false,
		"verify and list bundle contents without writing any files")
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	if isJSONOutput() {
		return outputJSON(os.Stdout, cfg)
	}

	fmt.Println("Configuration:")
	fmt.Println()
	fmt.Printf("  Config file:  %s\n", config.GetConfigPath(binName))
	fmt.Println()
	fmt.Println("  storage:")
	fmt.Printf("    type:       %s\n", cfg.Storage.Type)
	fmt.Printf("    path:       %s\n", cfg.Storage.Path)
	fmt.Println()
	fmt.Println("  server:")
	fmt.Printf("    port:       %d\n", cfg.Server.Port)
	fmt.Printf("    grpc_port:  %d\n", cfg.Server.GRPCPort)
	fmt.Printf("    workers:    %d\n", cfg.Server.Workers)
	fmt.Println()
	fmt.Println("  profile:")
	fmt.Printf("    default:    %s\n", cfg.Profile.Default)
	fmt.Println()
	if len(cfg.Registries) > 0 {
		fmt.Println("  registries:")
		for _, r := range cfg.Registries {
			fmt.Printf("    - %s (%s)\n", r.Name, r.URL)
		}
		fmt.Println()
	}
	if cfg.I18n.Enabled {
		fmt.Println("  i18n:")
		fmt.Printf("    enabled:    true\n")
		fmt.Printf("    languages:  %v\n", cfg.I18n.PreferredLanguages)
	}

	return nil
}

func runConfigPath(cmd *cobra.Command, args []string) error {
	configPath := config.GetConfigPath(binName)
	fmt.Println(configPath)
	return nil
}

func runConfigValidate(cmd *cobra.Command, args []string) error {
	fmt.Println("Validating configuration...")

	// Re-load to get a fresh validation result (cfg may have been loaded early).
	// If load fails, report error; otherwise use cfg (already loaded by initConfig)
	// to avoid the migration write-back corrupting a temporary test file.
	if _, err := config.Load(binName, cfgFile); err != nil {
		fmt.Printf("  ✗ Configuration is invalid: %v\n", err)
		return err
	}

	fmt.Println("  ✓ Configuration is valid")

	checkSecrets, _ := cmd.Flags().GetBool("check-secrets")
	if !checkSecrets {
		return nil
	}

	// Use cfg (loaded by initConfig before migration write-back can affect the
	// file) rather than re-loading, which risks seeing the post-migration YAML
	// that may have field-name differences due to missing yaml struct tags.
	warnings := config.ScanSecrets(cfg)
	if len(warnings) == 0 {
		fmt.Println("  ✓ No plaintext secrets detected")
		return nil
	}

	fmt.Printf("  ✗ %d plaintext secret(s) detected:\n", len(warnings))
	for _, w := range warnings {
		fmt.Printf("      field: %s  value: %s  reason: %s\n", w.Field, w.Hint, w.Reason)
	}
	return fmt.Errorf("plaintext secrets found in configuration")
}

func runConfigEdit(cmd *cobra.Command, args []string) error {
	configPath := config.GetConfigPath(binName)

	// Get editor from environment or use default
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	// Open editor
	editorCmd := exec.Command(editor, configPath) // #nosec G204,G702 -- editor from $EDITOR or known default
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr

	fmt.Printf("Opening %s in %s...\n", configPath, editor)
	return editorCmd.Run()
}

func runConfigLint(cmd *cobra.Command, args []string) error {
	fix, _ := cmd.Flags().GetBool("fix")

	// Determine config file path used for permission checks.
	// cfgFile is the persistent flag value set by cobra before RunE is called.
	// Fall back to the default config path when no --config flag was provided.
	configPath := cfgFile
	if configPath == "" {
		configPath = config.GetConfigPath(binName)
	}

	// Apply auto-fixes before running lint so the results reflect the fixed state.
	if fix {
		if err := config.FixPermissions(configPath); err != nil {
			fmt.Printf("  ! failed to fix permissions on %s: %v\n", configPath, err)
		}
	}

	findings := config.LintConfig(cfg, configPath)

	if len(findings) == 0 {
		fmt.Println("  ✓ config doctor: no issues found")
		return nil
	}

	errCount, warnCount := 0, 0
	for _, f := range findings {
		switch f.Severity {
		case config.SeverityError:
			errCount++
			fmt.Printf("  ✗ %s\n", f)
		case config.SeverityWarn:
			warnCount++
			fmt.Printf("  ! %s\n", f)
		}
	}

	fmt.Printf("\n  %d error(s), %d warning(s)\n", errCount, warnCount)
	if !fix && warnCount > 0 {
		fmt.Println("  Run with --fix to auto-apply safe fixes.")
	}

	return fmt.Errorf("lint: %d error(s), %d warning(s) found", errCount, warnCount)
}

func runConfigBackup(cmd *cobra.Command, _ []string) error {
	outDir, _ := cmd.Flags().GetString("output")
	if outDir == "" {
		// Fall back to config backup.dir, then cwd.
		outDir = cfg.Backup.Dir
		if outDir == "" {
			var err error
			outDir, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("config backup: resolve output dir: %w", err)
			}
		}
	}

	// Determine whether encryption is requested.
	encryptFlag, _ := cmd.Flags().GetBool("encrypt")
	encrypt := encryptFlag || cfg.Backup.EncryptByDefault

	passphraseFlag, _ := cmd.Flags().GetString("passphrase")

	configPath := config.GetConfigPath(binName)
	configDir := filepath.Dir(configPath)

	// Load private key from OS keychain.
	privKey, err := bundle.LoadPrivateKey()
	if err != nil {
		return fmt.Errorf("config backup: %w\nRun 'ctxt key init' to generate a signing key.", err)
	}

	result, err := bundle.Build(bundle.BuildOpts{
		ConfigDir:  configDir,
		Files:      []string{filepath.Base(configPath)},
		OutputDir:  outDir,
		PrivateKey: privKey,
		PublicKey:  bundle.PublicFromPrivate(privKey),
	})
	if err != nil {
		return fmt.Errorf("config backup: %w", err)
	}

	// Optionally encrypt the bundle in-place.
	if encrypt {
		pass, err := bundle.ResolvePassphrase(passphraseFlag, true)
		if err != nil {
			return fmt.Errorf("config backup: resolve passphrase: %w", err)
		}
		if err := bundle.EncryptBundleFile(result.ZipPath, pass); err != nil {
			return fmt.Errorf("config backup: encrypt: %w", err)
		}
	}

	if isJSONOutput() {
		m := map[string]interface{}{
			"zip":         result.ZipPath,
			"sig":         result.SigPath,
			"fingerprint": result.Fingerprint,
			"encrypted":   encrypt,
		}
		return outputJSON(cmd.OutOrStdout(), m)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Config bundle created\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  bundle:      %s\n", result.ZipPath)
	fmt.Fprintf(cmd.OutOrStdout(), "  signature:   %s\n", result.SigPath)
	fmt.Fprintf(cmd.OutOrStdout(), "  fingerprint: %s\n", result.Fingerprint)
	if encrypt {
		fmt.Fprintf(cmd.OutOrStdout(), "  encrypted:   yes (AES-256-GCM + Argon2id)\n")
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\nVerify with: ctxt config restore --verify %s\n", result.ZipPath)
	return nil
}

func runConfigRestore(cmd *cobra.Command, args []string) error {
	zipPath := args[0]
	verify, _ := cmd.Flags().GetBool("verify")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	// Auto-detect and decrypt encrypted bundles before any further processing.
	decryptedPath, wasEncrypted, err := maybeDecryptBundleFile(zipPath)
	if err != nil {
		return fmt.Errorf("config restore: decrypt: %w", err)
	}
	if wasEncrypted {
		// decryptedPath is a temp file; clean up after restore.
		defer os.Remove(decryptedPath)
		zipPath = decryptedPath
		fmt.Fprintf(cmd.OutOrStdout(), "  decrypted:   yes\n")
	}

	sigPath := zipPath + ".sig"

	if verify || dryRun {
		result, err := bundle.VerifyAndOpen(bundle.VerifyOpts{
			ZipPath: zipPath,
			SigPath: sigPath,
		})
		if err != nil {
			return fmt.Errorf("config restore: verification failed: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Signature verified\n")
		fmt.Fprintf(cmd.OutOrStdout(), "  fingerprint:    %s\n", result.Manifest.Fingerprint)
		fmt.Fprintf(cmd.OutOrStdout(), "  schema_version: %d\n", result.Manifest.SchemaVersion)
		fmt.Fprintf(cmd.OutOrStdout(), "  created_at:     %s\n",
			result.Manifest.CreatedAt.Format("2006-01-02 15:04:05 UTC"))
		fmt.Fprintf(cmd.OutOrStdout(), "  files:\n")
		for name := range result.Files {
			fmt.Fprintf(cmd.OutOrStdout(), "    - %s\n", name)
		}

		if dryRun {
			fmt.Fprintf(cmd.OutOrStdout(), "\ndry-run: no files written\n")
			return nil
		}

		return writeRestoredFiles(cmd, result.Files)
	}

	// No --verify: extract without signature check (warn user).
	fmt.Fprintf(cmd.ErrOrStderr(),
		"Warning: restoring without signature verification. Use --verify to verify integrity.\n")

	files, err := bundle.ExtractOnly(zipPath)
	if err != nil {
		return fmt.Errorf("config restore: %w", err)
	}
	return writeRestoredFiles(cmd, files)
}

// maybeDecryptBundleFile reads the file at path; if it is an encrypted bundle,
// it prompts for the passphrase, decrypts to a temp file, and returns the temp
// path plus wasEncrypted=true.  Returns the original path unchanged when the
// bundle is not encrypted.
func maybeDecryptBundleFile(path string) (outPath string, wasEncrypted bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("read bundle: %w", err)
	}
	if !bundle.IsEncryptedBundle(data) {
		return path, false, nil
	}

	pass, err := bundle.ResolvePassphrase("", false)
	if err != nil {
		return "", false, fmt.Errorf("resolve passphrase: %w", err)
	}

	plain, err := bundle.DecryptBundle(data, pass)
	if err != nil {
		return "", false, err
	}

	// Write plaintext zip to a temp file alongside the original.
	tmp, err := os.CreateTemp(filepath.Dir(path), "ctxt-restore-*.zip")
	if err != nil {
		return "", false, fmt.Errorf("create temp file: %w", err)
	}
	if _, err := tmp.Write(plain); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", false, fmt.Errorf("write temp file: %w", err)
	}
	tmp.Close()
	return tmp.Name(), true, nil
}

// writeRestoredFiles writes bundle file contents to the config directory.
func writeRestoredFiles(cmd *cobra.Command, files map[string][]byte) error {
	configPath := config.GetConfigPath(binName)
	configDir := filepath.Dir(configPath)
	for name, data := range files {
		dst := filepath.Join(configDir, name)
		if err := os.WriteFile(dst, data, 0600); err != nil {
			return fmt.Errorf("config restore: write %s: %w", name, err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  restored: %s\n", dst)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\nRestart ctxt for changes to take effect.\n")
	return nil
}
