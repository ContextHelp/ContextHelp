# Web2/Web3 Hybrid Deployment

Not implemented. There are no IPFS, Arweave, Filecoin, blockchain or libp2p
drivers or transports in the code. What a deployment can use today:

- **Storage:** `sqlite` or `postgres` (`storage.type`), with blobs on `local`
  disk or `s3`-compatible object storage (`storage.blob.backend`). See
  [../scaling.md](../scaling.md) and [../dpkms/storage.md](../dpkms/storage.md).
- **Transport:** the dpkms HTTP API (`/api/v1`) and gRPC API. See [../api/](../api/).
- **Federation:** one-way push of objects to peers listed under `federations`
  (a `file://` SQLite path or an `http(s)://` dpkms instance, which receives them
  at `POST /api/v1/federation/push`). Pull and bidirectional sync are not
  implemented (`internal/federation/pull.go`). See
  [../ctxt/configuration.md](../ctxt/configuration.md).

For running dpkms on a server, see [docker.md](docker.md).
