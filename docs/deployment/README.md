# Deployment Documentation

How to run dpkms. A node is one `dpkms serve` process; `ctxt` clients reach it
over HTTP (and gRPC).

| Guide | When |
|---|---|
| [docker.md](docker.md) | Self-hosted server with Docker Compose (TLS, secrets, blob storage, upgrades) |
| [docker-dev.md](docker-dev.md) | Local development stack in Docker |
| `dpkms install --help` | Register dpkms with launchd (macOS) or systemd (Linux) on one machine |
| [../scaling.md](../scaling.md) | SQLite vs PostgreSQL, workers, ports, backups |

## What a deployment can use

- **Storage:** `storage.type` `sqlite` (default) or `postgres`; large content
  in a blob store, `local`, `s3` or `garage` (`storage.blob.backend`).
- **Transport:** HTTP API under `/api/v1` and gRPC.
- **Federation:** one-way push to peers under `federations` (`file://` SQLite
  path or `http(s)://` dpkms instance). No pull or bidirectional sync.

## Not implemented

The Web2/Web3 hybrid model — IPFS, Arweave and blockchain storage drivers,
libp2p and blockchain RPC transports, multi-target sync orchestration and
on-chain settlement — has no code.

## Related

- [../architecture.md](../architecture.md#deployment) — deployment summary
- [../dpkms/storage.md](../dpkms/storage.md) — storage backends
- [../cloud/](../cloud/) — context.help cloud
