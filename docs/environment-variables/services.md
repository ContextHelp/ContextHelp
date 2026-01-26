# Optional Services Environment Variables

Configuration for Redis and Qdrant vector search.

---

## Redis (Distributed Queue & Cache)

### REDIS_ENABLED
**Type:** boolean  
**Default:** `false`

Enable Redis for distributed job queue and caching.

```bash
REDIS_ENABLED=true
```

### REDIS_HOST
**Type:** string  
**Default:** `localhost`

Redis server hostname.

```bash
REDIS_HOST=redis.internal
```

### REDIS_PORT
**Type:** integer  
**Default:** `6379`

Redis server port.

```bash
REDIS_PORT=6379
```

### REDIS_PASSWORD
**Type:** string (secret)  
**Default:** _(none)_

Redis authentication password.

```bash
REDIS_PASSWORD=redis_password_here
```

### REDIS_DB
**Type:** integer  
**Default:** `0`

Redis database number (0-15).

```bash
REDIS_DB=1
```

---

## Qdrant (Vector Search)

### QDRANT_ENABLED
**Type:** boolean  
**Default:** `false`

Enable Qdrant vector database.

```bash
QDRANT_ENABLED=true
```

### QDRANT_HOST
**Type:** string  
**Default:** `localhost`

Qdrant server hostname.

```bash
QDRANT_HOST=qdrant.internal
```

### QDRANT_PORT
**Type:** integer  
**Default:** `6333`

Qdrant gRPC port.

```bash
QDRANT_PORT=6333
```

### QDRANT_API_KEY
**Type:** string (secret)  
**Default:** _(none)_

Qdrant API key for authentication.

```bash
QDRANT_API_KEY=qdrant_key_here
```

### QDRANT_COLLECTION
**Type:** string  
**Default:** `contexthelp`

Qdrant collection name.

```bash
QDRANT_COLLECTION=embeddings_prod
```

---

## Examples

### Redis Only
```bash
REDIS_ENABLED=true
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_DB=0
```

### Qdrant Only
```bash
QDRANT_ENABLED=true
QDRANT_HOST=localhost
QDRANT_PORT=6333
QDRANT_COLLECTION=contexthelp
```

### Both Services (Production)
```bash
REDIS_ENABLED=true
REDIS_HOST=redis.internal
REDIS_PORT=6379
REDIS_PASSWORD=$(vault kv get -field=password secret/redis)
REDIS_DB=0

QDRANT_ENABLED=true
QDRANT_HOST=qdrant.internal
QDRANT_PORT=6333
QDRANT_API_KEY=$(vault kv get -field=key secret/qdrant)
QDRANT_COLLECTION=embeddings_prod
```

---

## Docker Compose Setup

Start services locally:

```bash
# Start services
task services:up

# Or manually
docker-compose -f docker-compose.dev.yml up -d
```

Then configure:
```bash
REDIS_ENABLED=true
REDIS_HOST=localhost
REDIS_PORT=6379

QDRANT_ENABLED=true
QDRANT_HOST=localhost
QDRANT_PORT=6333
```

---

## Related

- [../scaling.md](../scaling.md#optional-services) — Service scaling
- [docker-compose.dev.yml](../../docker-compose.dev.yml) — Local service configuration
