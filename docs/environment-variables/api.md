# API Environment Variables

Listener ports of `dpkms serve`: REST (HTTP), gRPC and the browser-extension
cookie bridge.

---

## Precedence

Each port resolves from the first source that sets it:

1. Flag: `--port`, `--grpc-port`, `--cookie-bridge-port`
2. `-c key=value` override
3. Env var: `CH_SERVER_PORT`, `CH_GRPC_PORT`, `CH_COOKIE_BRIDGE_PORT`
4. Config file: `server.port`, `server.grpc_port`, `server.cookie_bridge_port`
5. Built-in default: `8080`, `9090`, `9377`

A port set by any of sources 1-4 is exact: serve binds that port or exits
non-zero if it is busy. Only an untouched built-in default falls back to an
OS-assigned port when busy. `0` asks for an OS-assigned port. An empty env
var counts as unset.

---

## Ports

### CH_SERVER_PORT

- **Type:** integer
- **Default:** `8080`
- **Config key:** `server.port`
- **Flag:** `--port`

REST (HTTP) API port.

```bash
CH_SERVER_PORT=8081
```

### CH_GRPC_PORT

- **Type:** integer
- **Default:** `9090`
- **Config key:** `server.grpc_port`
- **Flag:** `--grpc-port`

gRPC API port.

```bash
CH_GRPC_PORT=9091
```

### CH_COOKIE_BRIDGE_PORT

- **Type:** integer
- **Default:** `9377`
- **Config key:** `server.cookie_bridge_port`
- **Flag:** `--cookie-bridge-port`

Browser-extension cookie bridge port. Always binds `127.0.0.1`, whatever the
access class. The browser extension connects to `9377`; change it only if
the extension is pointed elsewhere.

```bash
CH_COOKIE_BRIDGE_PORT=9378
```

---

## Bind address

No env var. The REST and gRPC listeners bind by access class
(`server.access` in the config file):

| `server.access` | Bind address |
|-----------------|--------------|
| `private` (default) | `127.0.0.1` |
| `protected`, `public` | `0.0.0.0` (inbound auth required) |

---

## Examples

### Second local instance

```bash
CH_SERVER_PORT=8081
CH_GRPC_PORT=9091
CH_COOKIE_BRIDGE_PORT=9378
```

### Throwaway instance (every listener OS-assigned)

```bash
CH_SERVER_PORT=0
CH_GRPC_PORT=0
CH_COOKIE_BRIDGE_PORT=0
```

---

## Related

- [../manual/reference/config-and-permissions.md](../manual/reference/config-and-permissions.md) — ports table (flag, env, config key)
- [../api/api-rest.md](../api/api-rest.md) — REST API reference
- [../api/api-grpc.md](../api/api-grpc.md) — gRPC API reference
- [../scaling.md](../scaling.md#network--api-scaling) — API scaling
