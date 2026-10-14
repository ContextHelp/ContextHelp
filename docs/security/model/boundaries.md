# Security Boundaries

Where data crosses a trust boundary in ctxt and dpkms, and what guards each crossing today.

| Boundary | Trust | Guards today | Not guarded |
|----------|-------|--------------|-------------|
| Local machine | the user is trusted; other users and processes are not | file permissions you set; config permission check | encryption at rest, log redaction |
| External registries | untrusted | Ed25519 signatures, trust levels, entity integrity guard | unsigned registries unless `require_signatures` |
| AI providers | external service | opt-in keys and endpoints; HTTPS endpoints by default | content you send is visible to the provider |
| Plugins and installed steps | untrusted code | nothing runs them today | no sandbox |
| REST / gRPC / MCP | loopback trusted; remote untrusted | access class, static tokens + scopes, Host/origin guards | TLS, rate limiting |

## Boundary 1: Local machine

**What lives here**: the dpkms store (`<data_dir>/db.sqlite` by default, `data_dir` = `$XDG_DATA_HOME/contexthelp`), the blob store (`<data_dir>/blobs` for the `local` backend), `run/` pidfiles, config under `$XDG_CONFIG_HOME/contexthelp/`, policy rules in `policy/ctxt.yaml`, and process logs wherever you send stderr.

**Guards**:
- `ctxt config doctor` warns on group- or world-readable config files; `--fix` sets `0600`. Nothing checks the database or blob permissions.
- Provider keys can live in the OS keychain, an age-encrypted file, 1Password or GitHub secrets instead of config ([secrets backends](../secrets-backends.md)).

**Not guarded**: the database and blobs are stored in the clear; logs are not sanitized; `dpkms backup` archives are not encrypted. Use OS disk encryption (FileVault, LUKS, BitLocker) and keep the data directory private.

## Boundary 2: External registries

**What crosses**: entity and taxonomy data from `registries`, step manifests from step registries (`dpkms pipeline step install --registry <url>`), registry credentials.

**Guards**:
- Signature check: a registry that declares a `public_key` must sign its entity index (Ed25519, `Content-Signature` header or `.sig` sidecar); a bad signature fails the sync. `registries_global.require_signatures: true` refuses registries without a key. Off by default.
- Trust level per registry (`trust_level`): `trusted` writes entities; `untrusted` (default) and `sandboxed` registries' entities are skipped.
- Entity integrity guard rejects malformed or spoofed entities before write.
- Registry credentials sit in the OS keychain and are never sent over plain `http` to a non-loopback host.

## Boundary 3: AI providers

**What crosses**: object content sent for enrichment, transcription, vision and embeddings.

**Guards**:
- Hosted providers are used only when configured: with `providers.llm.backend: auto` a resolved Anthropic or OpenAI key selects that provider, otherwise local Ollama. Set `backend: ollama` to keep LLM calls local.
- Embeddings use Ollama (or `stub`); there is no hosted embedding backend.
- `--offline` refuses outbound HTTP for one invocation.

**Not guarded**: anything sent to a hosted provider is subject to that provider's terms. Request and response bodies are not redacted.

Reference: [AI providers environment variables](../../environment-variables/ai-providers.md).

## Boundary 4: Plugins and installed steps

No plugin is loaded at runtime: neither binary creates a plugin registry, and the `plugins:` config key is read by no code ([plugins.md](../../plugins/plugins.md)). `dpkms pipeline step install` records steps from a step registry, but no pipeline runs installed external steps today. There is no sandbox, capability enforcement or resource limit for either; manifest permissions are validated and not enforced. [ADR-027](../../decisions/ADR-027-plugin-isolation-sandboxing.md) and [plugin-isolation.md](../../plugins/plugin-isolation.md) are design.

## Boundary 5: REST, gRPC and MCP

**Guards**:
- `server.access`: `private` (default) binds `127.0.0.1` and grants every local caller every scope; `protected` / `public` bind all interfaces and refuse to start without `server.auth.provider: static` and at least one token.
- Static tokens (`Authorization: Bearer` or `X-API-Key`): 32+ characters, no `REPLACE_ME`, constant-time compare; roles `admin`, `writer`, `reader` expand to scopes; every `/api/v1` route and gRPC method needs one. `401` without a valid token, `403 INSUFFICIENT_SCOPE` without the scope.
- Host header check (always on `private`; on `protected` / `public` when `server.allowed_hosts` is set), cross-origin guard and JSON-only request bodies.
- `/healthz` details hidden from anonymous callers on `public` instances.
- Federation push on non-private instances also needs `federation.token`.
- MCP (`/api/v1/mcp/`, tools `search` and `schema`) sits behind the same auth with the `read:mcp` scope.

**Not guarded**: no TLS (terminate at a reverse proxy); no rate limiting; `oidc` and `mtls` are reserved provider names, not implemented.

Reference: [Inbound auth, roles and scopes](../../manual/reference/config-and-permissions.md#inbound-auth-roles-and-scopes), [Put dpkms Behind a Reverse Proxy](../../manual/operations/reverse-proxy.md).

## Related

- [threat.md](threat.md), [controls.md](controls.md)
- [../security-model.md](../security-model.md)
