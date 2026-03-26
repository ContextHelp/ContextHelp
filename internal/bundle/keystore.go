//go:build darwin || linux

// Package bundle keystore — OS keychain integration for Ed25519 private keys.
package bundle

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// StorePrivateKey persists the private key in the OS keychain under KeychainService/KeychainAccount.
// macOS: uses `security add-generic-password`.
// Linux: uses `secret-tool store`.
func StorePrivateKey(priv ed25519.PrivateKey) error {
	encoded := hex.EncodeToString(priv)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("security", "add-generic-password",
			"-s", KeychainService,
			"-a", KeychainAccount,
			"-w", encoded,
			"-U") // -U = update if exists
	default: // linux
		cmd = exec.Command("secret-tool", "store",
			"--label", KeychainService+"/"+KeychainAccount,
			"service", KeychainService,
			"account", KeychainAccount)
		cmd.Stdin = strings.NewReader(encoded)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bundle: store private key: %w — %s", err, string(out))
	}
	return nil
}

// LoadPrivateKey retrieves the private key from the OS keychain.
func LoadPrivateKey() (ed25519.PrivateKey, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("security", "find-generic-password",
			"-s", KeychainService,
			"-a", KeychainAccount,
			"-w")
	default: // linux
		cmd = exec.Command("secret-tool", "lookup",
			"service", KeychainService,
			"account", KeychainAccount)
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("bundle: load private key (run `ctxt key init` first): %w", err)
	}
	encoded := strings.TrimRight(string(out), "\n")
	decoded, err := hex.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("bundle: decode private key from keychain: %w", err)
	}
	if len(decoded) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("bundle: private key wrong size %d (expected %d)", len(decoded), ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(decoded), nil
}
