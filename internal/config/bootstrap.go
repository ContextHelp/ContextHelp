package config

import (
	"errors"
	"fmt"
)

// ErrConfigArgsInvalid marks a fatal `-c`/`--config` failure: the user
// explicitly supplied config tokens and at least one of them could not
// be parsed or resolved. Callers assert with errors.Is so the exit-code
// mapping and CLI tests do not depend on message text.
var ErrConfigArgsInvalid = errors.New("invalid -c/--config argument")

// ErrConfigLoadFailed marks a fatal config-load failure: the tokens
// parsed, but reading/merging the resulting layers failed. Distinct
// from ErrConfigArgsInvalid so callers can tell "you typed it wrong"
// from "the file exists but is broken".
//
// The sentinel text is deliberately terse: LoadWithOverrides already
// prefixes its own "failed to load config", so wrapping with the same
// phrase would double it in the rendered message.
var ErrConfigLoadFailed = errors.New("config unusable")

// Bootstrap resolves the effective configuration for a CLI binary from
// kit/cli's parsed `-c`/`--config` tokens.
//
// Fail-loud contract. Historically both binaries printed a warning on a
// `-c` parse error, dropped every user-supplied path and override, and
// carried on against the DEFAULT config — silently retargeting
// destructive commands (`dpkms housekeeping prune -c /typo.yaml`) at the
// user's default database. Bootstrap never falls back: any error is
// returned and the caller MUST abort.
//
// parseErr is the third return value of kit/cli Root.ConfigArgs(). That
// contract only produces a non-nil error when tokens were actually
// supplied and one of them is malformed — no `-c` at all yields
// (nil, nil, nil). So a non-nil parseErr unambiguously means "the user
// explicitly passed -c and it is invalid", and the normal
// default-cascade path (no `-c`) is untouched.
//
// effectivePath is the legacy `cfgFile` global: the LAST supplied path,
// which is the highest-precedence file and therefore the one that
// path-targeting subcommands (edit, lint --fix, validate) should
// operate on. Empty when no bare path token was given.
func Bootstrap(binName string, paths []string, overrides map[string]any, parseErr error) (cfg *Config, effectivePath string, err error) {
	if parseErr != nil {
		return nil, "", fmt.Errorf("%w: %w", ErrConfigArgsInvalid, parseErr)
	}

	cfg, err = LoadWithOverrides(binName, "", paths, overrides)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", ErrConfigLoadFailed, err)
	}

	if len(paths) > 0 {
		effectivePath = paths[len(paths)-1]
	}
	return cfg, effectivePath, nil
}
