# Registries Environment Variables

Registry synchronization and federation configuration.

---

## Registry Sync

### REGISTRY_SYNC_INTERVAL
**Type:** duration  
**Default:** `24h`

How often to sync with remote registries.

```bash
REGISTRY_SYNC_INTERVAL=12h
```

### REGISTRY_CACHE_TTL
**Type:** duration  
**Default:** `7d`

How long to cache registry data.

```bash
REGISTRY_CACHE_TTL=3d
```

### REGISTRY_AUTH_TOKEN
**Type:** string (secret)  
**Default:** _(none)_

Authentication token for registry access. **Never commit to version control.**

```bash
REGISTRY_AUTH_TOKEN=registry_token_here
```

**Security:** Use secret management:
```bash
REGISTRY_AUTH_TOKEN=$(vault kv get -field=token secret/registry)
```

---

## Examples

### Public Registries (No Auth)
```bash
REGISTRY_SYNC_INTERVAL=24h
REGISTRY_CACHE_TTL=7d
```

### Private Registries (With Auth)
```bash
REGISTRY_SYNC_INTERVAL=12h
REGISTRY_CACHE_TTL=3d
REGISTRY_AUTH_TOKEN=$(vault kv get -field=token secret/registry)
```

### Frequent Sync (Active Development)
```bash
REGISTRY_SYNC_INTERVAL=1h
REGISTRY_CACHE_TTL=1d
```

---

## Registry Configuration File

Registries are primarily configured in `config.yaml`:

```yaml
registries:
  enabled:
    - name: local-taxonomy
      url: file://~/.config/contexthelp/taxonomy/
      type: taxonomy
    
    - name: uxpatterns
      url: https://api.uxpatterns.io
      type: taxonomy
      auth:
        token: ${REGISTRY_AUTH_TOKEN}
```

Environment variables supplement file-based configuration.

---

## Related

- [../dpkms/registries.md](../dpkms/registries.md) — Registry architecture
- [../dpkms/registry-protocol.md](../dpkms/registry-protocol.md) — Registry protocol
- [../ctxt/configuration.md](../ctxt/configuration.md) — Profile-registry binding
