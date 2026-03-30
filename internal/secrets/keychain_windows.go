//go:build windows

package secrets

import (
	"fmt"
	"os/exec"
	"strings"
)

// KeychainResolver reads and writes secrets via Windows Credential Manager (cmdkey).
// Credential target names are formatted as "<service>/<key>".
type KeychainResolver struct {
	service string
}

// NewKeychainResolver creates a KeychainResolver for the given service name.
func NewKeychainResolver(service string) *KeychainResolver {
	return &KeychainResolver{service: service}
}

func (r *KeychainResolver) target(key string) string {
	return r.service + "/" + key
}

func (r *KeychainResolver) Get(key string) (string, error) {
	// cmdkey /list:<target> prints the entry but not the password.
	// We use PowerShell's Get-StoredCredential (or the built-in
	// [System.Net.NetworkCredential] trick) via a one-liner to read
	// the password out of Credential Manager.
	script := fmt.Sprintf(
		`(New-Object System.Net.NetworkCredential('', `+
			`(Get-StoredCredential -Target '%s').Password)).Password`,
		r.target(key),
	)
	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		// Fallback: try reading via [Windows.Security.Credentials.PasswordVault]
		// which is available without external modules.
		script2 := fmt.Sprintf(
			`$v=(New-Object Windows.Security.Credentials.PasswordVault);`+
				`$c=$v.Retrieve('%s','%s');$c.RetrievePassword();$c.Password`,
			r.service, key,
		)
		out2, err2 := exec.Command("powershell", "-NoProfile", "-Command", script2).Output()
		if err2 != nil || strings.TrimSpace(string(out2)) == "" {
			return "", ErrNotFound{Key: key}
		}
		return strings.TrimRight(string(out2), "\r\n"), nil
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

func (r *KeychainResolver) Set(key, value string) error {
	// Windows.Security.Credentials.PasswordVault — available on all modern Windows.
	script := fmt.Sprintf(
		`$v=(New-Object Windows.Security.Credentials.PasswordVault);`+
			`$c=(New-Object Windows.Security.Credentials.PasswordCredential('%s','%s','%s'));`+
			`$v.Add($c)`,
		r.service, key, value,
	)
	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("secrets: keychain set %q: %w — %s", key, err, string(out))
	}
	return nil
}

func (r *KeychainResolver) Delete(key string) error {
	script := fmt.Sprintf(
		`$v=(New-Object Windows.Security.Credentials.PasswordVault);`+
			`$c=$v.Retrieve('%s','%s');$v.Remove($c)`,
		r.service, key,
	)
	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("secrets: keychain delete %q: %w — %s", key, err, string(out))
	}
	return nil
}

// Keys lists all secret names stored under this service in Windows Credential Manager.
func (r *KeychainResolver) Keys() ([]string, error) {
	script := fmt.Sprintf(
		`$v=(New-Object Windows.Security.Credentials.PasswordVault);`+
			`$v.FindAllByResource('%s')|ForEach-Object{$_.UserName}`,
		r.service,
	)
	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).Output()
	if err != nil {
		// FindAllByResource throws if no entries exist — treat as empty.
		return nil, nil
	}
	var keys []string
	for _, line := range strings.Split(string(out), "\n") {
		if k := strings.TrimRight(line, "\r\n"); k != "" {
			keys = append(keys, k)
		}
	}
	return keys, nil
}
