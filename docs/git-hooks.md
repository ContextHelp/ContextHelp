# Git Hooks for Secret Detection

ContextHelp ships git hooks in the tracked `.githooks/` directory. The
pre-commit hook scans staged changes for secrets before a commit lands, so
API keys, passwords and other credentials never reach history.

Secret scanning runs locally rather than in CI: `gitleaks-action` needs a
license for organization-owned repos (see the disabled `secret-scan` job in
`.github/workflows/ci.yml`). The hook is the stand-in.

---

## Overview

| Hook | What it does |
|------|--------------|
| `pre-commit` | 1. `gitleaks git --staged` with `.gitleaks.toml` (skipped with a warning when gitleaks is missing). 2. The lint gate from `.pre-commit-config.yaml`, scoped to staged files (skipped when `pre-commit` is missing). |
| `pre-push` | Chains to your global pre-push hook, if any. `core.hooksPath` replaces the global hooks dir instead of adding to it. |

---

## Installation

```bash
make install-hooks
```

This sets `core.hooksPath` to `.githooks`, so git runs the tracked hooks
directly. Nothing is copied into `.git/hooks`. It works the same in a clone
and in a linked worktree, where `.git` is a file. `scripts/install-hooks.sh`
is a wrapper around the same target, and `scripts/dev-setup.sh` runs it too.

Do not run `pre-commit install`. It refuses to run while `core.hooksPath`
is set; `.githooks/pre-commit` chains to `pre-commit hook-impl` instead.

### Install gitleaks (required for the scan)

```bash
make install-gitleaks   # or: brew install gitleaks
```

Other platforms: <https://github.com/gitleaks/gitleaks#installing>.

---

## Usage

The hook runs on every `git commit`. If gitleaks finds a secret, the commit
is blocked and gitleaks prints the file, line and rule that matched
(secrets redacted). Remove the secret, re-stage, and commit again.

Scan the whole working tree on demand:

```bash
make secret-scan-local
```

### Bypass (use sparingly)

```bash
git commit --no-verify -m "message"
```

This skips every pre-commit check, including the secret scan. The hook has
no per-check skip variables.

---

## Detected Secret Types

Rules live in `.gitleaks.toml`: the gitleaks default rules
(`[extend] useDefault = true`) plus the custom `ctxt-*` rules below. The
custom rules cover:

- **AI providers**: OpenAI (`sk-proj-...`, legacy `sk-...`), Anthropic (`sk-ant-...`)
- **Generic**: `api_key = "..."`, `secret = "..."`, password assignments, env-var assignments
- **Tokens**: JWTs (`eyJ...`), GitHub tokens (`ghp_`, `gho_`, `ghu_`, `ghs_`, `ghr_`)
- **Cloud**: AWS access key IDs (`AKIA...`) and secret access keys
- **Databases**: PostgreSQL connection strings with a password
- **Webhooks**: Slack webhook URLs
- **Keys**: PEM private keys (RSA, DSA, EC, OpenSSH)
- **Auth headers**: `Authorization: Basic ...`, `Authorization: Bearer ...`
- **ctxt**: `CTXT_API_KEY`, `CTXT_SECRET`/`TOKEN`/`PASSWORD`, `CTXT_DB_*` DSNs, `CTXT_*_API_KEY`

---

## Configuration

### Add a rule

Add rules to `.gitleaks.toml`, not to the hook. Give the id the `ctxt-`
prefix: a rule with the same id as a gitleaks default rule does not add a
rule, it overrides the default one and keeps its keywords and allowlists,
which can stop your regex from ever matching. `scripts/` tests fail on an
unprefixed id.

```toml
[[rules]]
id = "ctxt-custom-api-key"
description = "My Custom API Key Format"
regex = '''myapi-[a-zA-Z0-9]{32}'''
tags = ["api-key", "custom"]
```

### Allowlist false positives

```toml
[[allowlists]]
paths = [
  '''config\.example\.yaml$''',
  '''testdata/''',
]

regexes = [
  '''\$\{[A-Z_]+\}''',         # ${ENV_VAR} references
  '''your[_-]?key[_-]?here''', # placeholder text
]
```

The current allowlist already covers `*.md`, `docs/`, `testdata/`, the
`config.*.yaml` examples and `.env.example`.

Allowlist entries must not match real keys. Avoid length-only regexes (a
regex like `^[a-zA-Z0-9+/=]{8,20}$` matches every AWS access key id) and
short stopwords: a finding whose secret contains a stopword anywhere,
in any case, is skipped.

---

## Testing

```bash
./scripts/test-hooks.sh
```

This runs the installed `.githooks/pre-commit` against eight scenarios:

1. allow: config with an `${ENV_VAR}` reference
2. block: OpenAI key
3. block: Anthropic key
4. allow: markdown documentation
5. block: AWS access key
6. block: GitHub token
7. block: GitLab token (caught by a gitleaks default rule only)
8. allow: example config with a placeholder

The fake keys are built at run time and avoid the placeholder shapes
gitleaks skips by design, such as `abcdefghijklmnopqrstuvwxyz` runs or
AWS's documented `AKIAIOSFODNN7EXAMPLE`. `go test ./scripts/` runs the
script against the real gitleaks binary when it is installed.

Each scenario is staged in a throwaway repo, so your index, working tree
and `.env` are not touched. The script needs gitleaks installed and exits
non-zero if the hooks are not installed or any scenario fails.

---

## Troubleshooting

### Hook not running

```bash
git rev-parse --git-path hooks   # should print .githooks
make install-hooks               # if it does not
ls -l .githooks/pre-commit       # should be executable
```

### "gitleaks not found; skipping secret scan."

Install it (`make install-gitleaks`). Until you do, commits go through
without a secret scan.

### False positives

Add an allowlist entry to `.gitleaks.toml` (see above). Keep the secret
out of the file entirely: providers read keys such as `OPENAI_API_KEY`
from the environment, and config files do not expand env vars.

### Lint gate fails on code you did not touch

It should not. The hooks are scoped to staged files and new findings only;
see the comments in `.pre-commit-config.yaml`. Run the full check with
`pre-commit run --all-files`.

---

## Best Practices

- Fix the finding rather than reaching for `--no-verify`.
- Keep keys in the environment or the secrets store, never in tracked files.
- Keep `.gitignore` covering `.env`, `.env.*`, `*.key`, `*.token`, `*.pem`.
- Keep gitleaks current: `brew upgrade gitleaks`.

---

## Additional Resources

- [SECURITY.md](SECURITY.md): security policy
- [Environment Variables](environment-variables/README.md): secret management
- [Configuration Guide](ctxt/configuration.md): config file practices
- [Gitleaks](https://github.com/gitleaks/gitleaks)
- [OWASP Secrets Management](https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html)
