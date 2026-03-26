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

// Keys lists all secret names stored under this service in the OS keychain.
// macOS: parses `security dump-keychain` output.
// Linux: calls `secret-tool search service <service>` and parses attribute lines.
// Implements Lister.
func (r *KeychainResolver) Keys() ([]string, error) {
	switch runtime.GOOS {
	case "darwin":
		return r.keysDarwin()
	default:
		return r.keysLinux()
	}
}

func (r *KeychainResolver) keysDarwin() ([]string, error) {
	out, err := exec.Command("security", "dump-keychain").Output()
	if err != nil {
		return nil, fmt.Errorf("secrets: keychain dump: %w", err)
	}
	return parseKeychainDump(string(out), r.service), nil
}

// parseKeychainDump extracts account names for the given service from
// the text output of `security dump-keychain`.
// Each entry is a block starting with "keychain:"; within a block,
// "svce"<blob>="<service>" and "acct"<blob>="<account>" appear on separate lines.
func parseKeychainDump(dump, service string) []string {
	svcTag := `"svce"<blob>="` + service + `"`
	var keys []string
	// Split into per-entry blocks on the "keychain:" header line.
	blocks := strings.Split(dump, "\nkeychain:")
	for _, block := range blocks {
		if !strings.Contains(block, svcTag) {
			continue
		}
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, `"acct"<blob>="`) {
				continue
			}
			// Extract value between the outer quotes after the = sign.
			rest := line[len(`"acct"<blob>="`):] // e.g. `MY_KEY"`
			if idx := strings.Index(rest, `"`); idx >= 0 {
				keys = append(keys, rest[:idx])
			}
		}
	}
	return keys
}

func (r *KeychainResolver) keysLinux() ([]string, error) {
	out, err := exec.Command("secret-tool", "search", "service", r.service).Output()
	if err != nil {
		// secret-tool exits non-zero when no results found.
		return nil, nil
	}
	var keys []string
	for _, line := range strings.Split(string(out), "\n") {
		// Lines look like: attribute.account = MY_KEY
		if strings.Contains(line, "attribute.account") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				if k := strings.TrimSpace(parts[1]); k != "" {
					keys = append(keys, k)
				}
			}
		}
	}
	return keys, nil
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

// Delete removes the secret identified by key from the OS keychain.
// macOS: uses `security delete-generic-password`.
// Linux: uses `secret-tool clear`.
func (r *KeychainResolver) Delete(key string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("security", "delete-generic-password",
			"-s", r.service, "-a", key)
	default: // linux
		cmd = exec.Command("secret-tool", "clear",
			"service", r.service, "account", key)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("secrets: keychain delete %q: %w — %s", key, err, string(out))
	}
	return nil
}
