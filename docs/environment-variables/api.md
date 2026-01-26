# API Environment Variables

REST and gRPC API server configuration.

---

## REST API

### API_HOST
**Type:** string  
**Default:** `localhost`

Bind address for REST API.

```bash
API_HOST=0.0.0.0  # Listen on all interfaces
```

### API_PORT
**Type:** integer  
**Default:** `8080`

REST API port.

```bash
API_PORT=8080
```

### API_READ_TIMEOUT
**Type:** duration  
**Default:** `30s`

Request read timeout.

```bash
API_READ_TIMEOUT=60s
```

### API_WRITE_TIMEOUT
**Type:** duration  
**Default:** `30s`

Response write timeout.

```bash
API_WRITE_TIMEOUT=60s
```

---

## gRPC API

### GRPC_PORT
**Type:** integer  
**Default:** `9090`

gRPC server port.

```bash
GRPC_PORT=9090
```

### GRPC_MAX_RECV_SIZE
**Type:** integer (bytes)  
**Default:** `16777216` (16MB)

Maximum message size for receiving.

```bash
GRPC_MAX_RECV_SIZE=33554432  # 32MB
```

---

## CORS

### CORS_ALLOWED_ORIGINS
**Type:** comma-separated strings  
**Default:** `http://localhost:3000,http://localhost:8080`

Allowed CORS origins.

```bash
CORS_ALLOWED_ORIGINS=https://app.example.com,https://admin.example.com
```

### CORS_ALLOWED_METHODS
**Type:** comma-separated strings  
**Default:** `GET,POST,PUT,DELETE,OPTIONS`

Allowed HTTP methods.

```bash
CORS_ALLOWED_METHODS=GET,POST,PUT,DELETE,OPTIONS
```

### CORS_ALLOWED_HEADERS
**Type:** comma-separated strings  
**Default:** `Content-Type,Authorization`

Allowed request headers.

```bash
CORS_ALLOWED_HEADERS=Content-Type,Authorization,X-Request-ID
```

---

## Examples

### Development
```bash
API_HOST=localhost
API_PORT=8080
API_READ_TIMEOUT=30s
API_WRITE_TIMEOUT=30s

GRPC_PORT=9090

CORS_ALLOWED_ORIGINS=http://localhost:3000
```

### Production
```bash
API_HOST=0.0.0.0
API_PORT=8080
API_READ_TIMEOUT=60s
API_WRITE_TIMEOUT=60s

GRPC_PORT=9090
GRPC_MAX_RECV_SIZE=33554432

CORS_ALLOWED_ORIGINS=https://app.example.com
CORS_ALLOWED_METHODS=GET,POST,PUT,DELETE,OPTIONS
CORS_ALLOWED_HEADERS=Content-Type,Authorization,X-Request-ID,X-Trace-ID
```

---

## Related

- [../api/api-rest.md](../api/api-rest.md) — REST API reference
- [../api/api-grpc.md](../api/api-grpc.md) — gRPC API reference
- [../scaling.md](../scaling.md#network--api-scaling) — API scaling
