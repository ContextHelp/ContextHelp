# `dpkms key`

Initialize and rotate Ed25519 signing keys used for signed config bundles and artifact provenance.

- **Use `dpkms key`** for Ed25519 signing keys.
- **Use `dpkms storage rekey`** for storage-at-rest KEK/DEK rotation ([storage.md](storage.md)).

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `key init` | `write-local` | `conditional` (fails if exists unless `--force`) |
| `key rotate` | `write-local` | `no` |

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
dpkms key init
dpkms key rotate
```
