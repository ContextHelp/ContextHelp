//go:build windows

// Package bundle — passphrase resolution for encrypted bundles (Windows).
//
// Priority order:
//  1. Explicit value passed in (from --passphrase flag)
//  2. CTXT_BACKUP_PASSPHRASE environment variable
//  3. Windows Credential Manager entry "ctxt.backup" / "passphrase"
//  4. Interactive terminal prompt (fallback)
package bundle

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"
)

const (
	PassphraseEnvVar          = "CTXT_BACKUP_PASSPHRASE"
	PassphraseKeychainService = "ctxt.backup"
	PassphraseKeychainAccount = "passphrase"
)

// ResolvePassphrase returns the backup passphrase according to the priority chain.
func ResolvePassphrase(explicit string, confirm bool) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if v := os.Getenv(PassphraseEnvVar); v != "" {
		return v, nil
	}
	if kc, err := loadPassphraseFromKeychain(); err == nil && kc != "" {
		return kc, nil
	}
	return promptPassphrase(confirm)
}

func loadPassphraseFromKeychain() (string, error) {
	script := fmt.Sprintf(
		`$v=(New-Object Windows.Security.Credentials.PasswordVault);`+
			`$c=$v.Retrieve('%s','%s');$c.RetrievePassword();$c.Password`,
		PassphraseKeychainService, PassphraseKeychainAccount,
	)
	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).Output()
	if err != nil {
		return "", nil
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

func promptPassphrase(confirm bool) (string, error) {
	if !isTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("bundle: passphrase: no passphrase source available "+
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

func readPasswordPrompt(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	fd := int(os.Stdin.Fd())
	if isTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return scanner.Text(), nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("bundle: passphrase: EOF reading stdin")
}

func isTerminal(fd int) bool {
	return term.IsTerminal(fd)
}
