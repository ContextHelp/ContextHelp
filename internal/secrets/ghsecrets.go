package secrets

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// GHSecretsResolver writes secrets to GitHub Actions repository secrets via the `gh` CLI.
// Get() falls back to environment variables because GitHub secrets are write-only by design.
// Set() calls `gh secret set` to store the value in the repository.
type GHSecretsResolver struct {
	repo string // "owner/repo" or "" for the current repo
}

// NewGHSecretsResolver creates a GHSecretsResolver for the given repo.
// Pass an empty string to use the current repository (detected by gh CLI).
func NewGHSecretsResolver(repo string) *GHSecretsResolver {
	return &GHSecretsResolver{repo: repo}
}

// Get reads the secret from the environment (GitHub secrets are not readable via API).
func (r *GHSecretsResolver) Get(key string) (string, error) {
	if v := os.Getenv(key); v != "" {
		return v, nil
	}
	return "", ErrNotFound{Key: key}
}

// Keys lists secret names from GitHub Actions via `gh secret list`.
// Implements Lister.
func (r *GHSecretsResolver) Keys() ([]string, error) {
	args := []string{"secret", "list", "--json", "name", "--jq", ".[].name"}
	if r.repo != "" {
		args = append(args, "--repo", r.repo)
	}
	out, err := exec.Command("gh", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("secrets: gh secret list: %w", err)
	}
	var keys []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			keys = append(keys, line)
		}
	}
	return keys, nil
}

// Set stores the secret in GitHub Actions repository secrets via `gh secret set`.
func (r *GHSecretsResolver) Set(key, value string) error {
	args := []string{"secret", "set", key, "--body", value}
	if r.repo != "" {
		args = append(args, "--repo", r.repo)
	}
	if out, err := exec.Command("gh", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("secrets: gh secret set %q: %w — %s", key, err, strings.TrimSpace(string(out)))
	}
	return nil
}
