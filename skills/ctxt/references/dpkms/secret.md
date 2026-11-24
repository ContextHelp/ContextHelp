# `dpkms secret`

Read, list, and securely write secrets via the configured backend (OS keychain, age file, 1Password, GitHub Secrets, or env).

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `secret get <key>` / `list` | `read` | `yes` |
| `secret set <key>` | `write` | `yes` |

## Subcommands & Flags

```bash
dpkms secret list
dpkms secret get <key>
dpkms secret set <key> [--from-file <path|->] [--prompt]
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--from-file <path>` | `set` | `""` | Read secret value from file (`-` for stdin) |
| `--prompt` | `set` | `false` | Interactive masked prompt |

## Agent Recipes

```bash
dpkms secret list
dpkms secret set OPENAI_API_KEY --from-file /run/secrets/openai_key
dpkms secret set VOYAGE_API_KEY --from-file - < /run/secrets/voyage_key
```

## Gotchas & Next Steps

- **Never print or log secret values** retrieved by `secret get` into chat transcripts or repo files.
- Verify no plaintext secrets exist in config via `ctxt config validate --check-secrets` ([../ctxt/config.md](../ctxt/config.md)).
