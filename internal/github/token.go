package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// TokenSource yields the API token for requests about a repository
// owner. An empty token means "call anonymously": the client sends no
// Authorization header and lives within the reduced public rate budget.
type TokenSource interface {
	Token(ctx context.Context, owner string) (string, error)
}

// StaticToken is a TokenSource returning one fixed token for every owner.
type StaticToken string

// Token implements TokenSource.
func (t StaticToken) Token(context.Context, string) (string, error) { return string(t), nil }

// SecretGetter reads one secret by name. A missing secret returns
// ("", nil): the caller falls back to anonymous access.
type SecretGetter func(ctx context.Context, name string) (string, error)

// ConfigTokens is the TokenSource built from config.GitHubConfig: the
// first account listing the owner wins, else the default token. Secrets
// are read on use, so a rotated token is picked up without a restart.
type ConfigTokens struct {
	cfg config.GitHubConfig
	get SecretGetter
}

// NewConfigTokens builds a TokenSource from cfg, reading secrets via get.
func NewConfigTokens(cfg config.GitHubConfig, get SecretGetter) *ConfigTokens {
	return &ConfigTokens{cfg: cfg, get: get}
}

// Token implements TokenSource.
func (c *ConfigTokens) Token(ctx context.Context, owner string) (string, error) {
	for _, a := range c.cfg.Accounts {
		for _, o := range a.Owners {
			if strings.EqualFold(strings.TrimSpace(o), owner) {
				tok, err := c.get(ctx, a.TokenSecret)
				if err != nil {
					return "", fmt.Errorf("github: token for account %q: %w", a.Name, err)
				}
				return strings.TrimSpace(tok), nil
			}
		}
	}
	name := c.cfg.TokenSecret
	if name == "" {
		name = config.DefaultGitHubTokenSecret
	}
	tok, err := c.get(ctx, name)
	if err != nil {
		return "", fmt.Errorf("github: default token: %w", err)
	}
	return strings.TrimSpace(tok), nil
}
