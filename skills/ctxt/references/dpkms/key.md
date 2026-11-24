# `dpkms key`

Generate and rotate Ed25519 signing keys used for signed config bundles and artifact provenance. Replaces deprecated `ctxt key`.

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `key init` | `write-local` (creates keypair) | `conditional` (fails if exists unless `--force`) |
| `key rotate` | `write-local` (rotates active key) | `no` |

## Subcommands & Flags

```bash
dpkms key init [--force]
dpkms key rotate
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--force` | `init` | `false` | Overwrite existing keypair without prompting (**destructive**) |

## Agent Recipes

```bash
# Initialize signing keypair on a fresh machine
dpkms key init

# Rotate the active signing key without clobbering history
dpkms key rotate
```

## Gotchas & Next Steps

- Do not pass `--force` to `key init` on an existing installation unless you intend to invalidate prior signatures; use `dpkms key rotate` for planned rotation.
- Next: create a signed config backup ([../ctxt/config.md](../ctxt/config.md)).
