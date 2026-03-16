//go:build darwin || linux

package secrets

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// KeychainResolver reads secrets from the OS keychain.
// macOS: uses `security find-generic-password`.
// Linux: uses `secret-tool lookup`.
type KeychainResolver struct {
	service string
}

// NewKeychainResolver creates a KeychainResolver for the given service name.
func NewKeychainResolver(service string) *KeychainResolver {
	return &KeychainResolver{service: service}
}

func (r *KeychainResolver) Get(key string) (string, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("security", "find-generic-password",
			"-s", r.service, "-a", key, "-w")
	default: // linux
		cmd = exec.Command("secret-tool", "lookup", "service", r.service, "account", key)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", ErrNotFound{Key: key}
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func (r *KeychainResolver) Set(key, value string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("security", "add-generic-password",
			"-s", r.service, "-a", key, "-w", value, "-U")
	default: // linux
		cmd = exec.Command("secret-tool", "store",
			"--label", fmt.Sprintf("%s/%s", r.service, key),
			"service", r.service, "account", key)
		cmd.Stdin = strings.NewReader(value)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("secrets: keychain set %q: %w — %s", key, err, string(out))
	}
	return nil
}
