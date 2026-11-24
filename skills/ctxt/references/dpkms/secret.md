# `dpkms secret`

Read, list, and securely write secrets via the configured secret backend (OS keychain, age file, 1Password, GitHub Secrets, or env). Replaces deprecated `ctxt secret`.

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `secret get <key>` | `read` | `yes` |
| `secret list` | `read` | `yes` |
| `secret set <key>` | `write` | `yes` |

## Subcommands & Flags

```bash
dpkms secret list
dpkms secret get <key>
dpkms secret set <key> [--from-file <path|->] [--prompt]
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--from-file <path>` | `set` | `""` | Read secret value from file path (use `-` for stdin) |
| `--prompt` | `set` | `false` | Read secret value from an interactive masked prompt |

## Agent Recipes

```bash
# Inspect configured backend and available secret keys
dpkms secret list

# Store a secret safely from a protected file (never pass values as CLI args)
dpkms secret set OPENAI_API_KEY --from-file /run/secrets/openai_key

# Pipe a secret via stdin
dpkms secret set VOYAGE_API_KEY --from-file - < /run/secrets/voyage_key
```

## Gotchas & Next Steps

- **Never print or log secret values** retrieved by `dpkms secret get` into chat transcripts or committed files.
- `secret set` intentionally rejects positional secret values so secrets never leak into shell history or `ps` output.
- Verify no plaintext secrets exist in config with `ctxt config validate --check-secrets` ([../ctxt/config.md](../ctxt/config.md)).
