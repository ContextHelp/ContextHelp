//go:build darwin || linux

// Package bundle — passphrase resolution for encrypted bundles.
//
// Priority order:
//  1. Explicit value passed in (from --passphrase flag)
//  2. CTXT_BACKUP_PASSPHRASE environment variable
//  3. OS keychain entry "ctxt.backup" / "passphrase"
//  4. Interactive terminal prompt (fallback)
package bundle

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"golang.org/x/term"
)

const (
	// PassphraseEnvVar is the env var checked for a backup passphrase.
	PassphraseEnvVar = "CTXT_BACKUP_PASSPHRASE"
	// PassphraseKeychainService is the keychain service for the backup passphrase.
	PassphraseKeychainService = "ctxt.backup"
	// PassphraseKeychainAccount is the keychain account for the backup passphrase.
	PassphraseKeychainAccount = "passphrase"
)

// ResolvePassphrase returns the backup passphrase according to the priority chain:
//  1. explicit (non-empty string passed by caller, e.g. from --passphrase flag)
//  2. CTXT_BACKUP_PASSPHRASE env var
//  3. OS keychain lookup
//  4. interactive terminal prompt
//
// confirm=true triggers a confirmation re-prompt (used when encrypting, not decrypting).
func ResolvePassphrase(explicit string, confirm bool) (string, error) {
	// 1. Explicit value.
	if explicit != "" {
		return explicit, nil
	}

	// 2. Environment variable.
	if v := os.Getenv(PassphraseEnvVar); v != "" {
		return v, nil
	}

	// 3. OS keychain lookup (best-effort; not found is not an error).
	if kc, err := loadPassphraseFromKeychain(); err == nil && kc != "" {
		return kc, nil
	}

	// 4. Interactive prompt.
	return promptPassphrase(confirm)
}

// loadPassphraseFromKeychain attempts to read the passphrase from the OS keychain.
// Returns ("", nil) when not found so callers can fall through gracefully.
func loadPassphraseFromKeychain() (string, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("security", "find-generic-password",
			"-s", PassphraseKeychainService,
			"-a", PassphraseKeychainAccount,
			"-w")
	default: // linux
		cmd = exec.Command("secret-tool", "lookup",
			"service", PassphraseKeychainService,
			"account", PassphraseKeychainAccount)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", nil // not found; not an error
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// promptPassphrase reads a passphrase interactively from the terminal.
// When confirm=true, prompts twice and returns an error if they differ.
func promptPassphrase(confirm bool) (string, error) {
	if !isTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("bundle: passphrase: no passphrase source available " +
			"(set %s or use --passphrase)", PassphraseEnvVar)
	}

	pass, err := readPasswordPrompt("Enter backup passphrase: ")
	if err != nil {
		return "", fmt.Errorf("bundle: passphrase prompt: %w", err)
	}
	if pass == "" {
		return "", fmt.Errorf("bundle: passphrase: empty passphrase not allowed")
	}

	if confirm {
		pass2, err := readPasswordPrompt("Confirm backup passphrase: ")
		if err != nil {
			return "", fmt.Errorf("bundle: passphrase confirm prompt: %w", err)
		}
		if pass != pass2 {
			return "", fmt.Errorf("bundle: passphrase: passphrases do not match")
		}
	}

	return pass, nil
}

// readPasswordPrompt prints prompt to stderr and reads a password without echo.
// Falls back to a plain line reader when terminal raw mode is unavailable.
func readPasswordPrompt(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	fd := int(os.Stdin.Fd())
	if isTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr) // newline after hidden input
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	// Fallback: plain line reader (non-tty pipe in tests, etc.)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return scanner.Text(), nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("bundle: passphrase: EOF reading stdin")
}

// isTerminal reports whether fd is a terminal.
func isTerminal(fd int) bool {
	return term.IsTerminal(fd)
}
