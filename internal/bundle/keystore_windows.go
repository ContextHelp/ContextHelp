//go:build windows

// Package bundle keystore — Windows Credential Manager integration for Ed25519 private keys.
package bundle

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
)

// StorePrivateKey persists the private key in Windows Credential Manager.
func StorePrivateKey(priv ed25519.PrivateKey) error {
	encoded := hex.EncodeToString(priv)
	script := fmt.Sprintf(
		`$v=(New-Object Windows.Security.Credentials.PasswordVault);`+
			`$c=(New-Object Windows.Security.Credentials.PasswordCredential('%s','%s','%s'));`+
			`$v.Add($c)`,
		KeychainService, KeychainAccount, encoded,
	)
	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("bundle: store private key: %w — %s", err, string(out))
	}
	return nil
}

// LoadPrivateKey retrieves the private key from Windows Credential Manager.
func LoadPrivateKey() (ed25519.PrivateKey, error) {
	script := fmt.Sprintf(
		`$v=(New-Object Windows.Security.Credentials.PasswordVault);`+
			`$c=$v.Retrieve('%s','%s');$c.RetrievePassword();$c.Password`,
		KeychainService, KeychainAccount,
	)
	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return nil, fmt.Errorf("bundle: load private key (run `ctxt key init` first): %w", err)
	}
	encoded := strings.TrimRight(string(out), "\r\n")
	decoded, err := hex.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("bundle: decode private key from credential manager: %w", err)
	}
	if len(decoded) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("bundle: private key wrong size %d (expected %d)", len(decoded), ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(decoded), nil
}
