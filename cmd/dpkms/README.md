# dpkms

dPKMS — Decentralized Personal Knowledge Management Substrate. Storage, job queue, pipeline runtime, and API server that powers ContextHelp.

## What it is

- The infrastructure layer underneath the `ctxt` user-facing CLI.
- A long-running daemon (`dpkms serve`) plus operator subcommands for backup, restore, housekeeping, and pipeline management.
- Local-first: durable storage, transactional job execution, deterministic replay.
- Federated: registries, peer mesh, and signed knowledge distribution.

## What it isn't

- Not a user-facing capture/search CLI — that's `ctxt`. dpkms exposes the API; ctxt is one client.
- Not opinionated about *what* knowledge is valuable. dpkms runs work correctly; relevance/ranking is delegated.
- Not a hosted service. Every installation is sovereign.

## Quickstart

```bash
# Start the worker + API server (foreground)
dpkms serve

# Inspect running instances
dpkms ps

# Graceful shutdown / restart
dpkms shutdown
dpkms reboot

# Snapshot user data
dpkms backup --output-dir ~/backups/dpkms

# Restore from an archive
dpkms restore ~/backups/dpkms/archive.tar.gz

# Database maintenance
dpkms housekeeping vacuum
dpkms housekeeping reindex

# Pipeline + detector + job inspection
dpkms pipeline list
dpkms detector list
dpkms job list

# Signing keys + secrets
dpkms key list
dpkms secret list

# Developer/maintenance utilities
dpkms dev --help
```

`dpkms --help-all` reveals MANAGEMENT-group commands hidden by default (`completion`, `version`, `install-deps`).

## Layout

- `main.go` — entry point; wires version info into the cobra root.
- `cmd/` — cobra subcommands. Root command, group taxonomy, and global flags live in `cmd/root.go`.

## Configuration

dpkms reads `$XDG_CONFIG_HOME/contexthelp/config.yaml` by default. Override with `--config <path>` or the `CTXT_CONFIG` env var. Data directory defaults to `$XDG_DATA_HOME/contexthelp` and is overridable with `--data-dir` or `CTXT_DATA_DIR`.

## Related

- Project install/operations: [`INSTALL.md`](../../INSTALL.md)
- dPKMS architecture and design notes: [`docs/dpkms/README.md`](../../docs/dpkms/README.md)
- Sibling CLI client (user-facing): [`cmd/ctxt/`](../ctxt)
- CLI conventions (kit-aligned): `~/.ops/docs/cli-conventions-with-kit.md`
