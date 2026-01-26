# Pipelines & Profiles Environment Variables

Pipeline execution and focus profile configuration.

---

## Pipeline Configuration

### PIPELINE_DEFAULT_TIMEOUT
**Type:** duration  
**Default:** `5m`

Default pipeline execution timeout.

```bash
PIPELINE_DEFAULT_TIMEOUT=10m
```

### PIPELINE_CACHE_ENABLED
**Type:** boolean  
**Default:** `true`

Enable pipeline step caching.

```bash
PIPELINE_CACHE_ENABLED=true
```

### PIPELINE_MAX_CONCURRENT
**Type:** integer  
**Default:** `4`

Maximum concurrent pipeline executions.

```bash
PIPELINE_MAX_CONCURRENT=8
```

---

## Profile Configuration

### PROFILE_DEFAULT
**Type:** string  
**Default:** `general`

Default focus profile.

```bash
PROFILE_DEFAULT=founder
```

**Available profiles:**
- `general` — General-purpose
- `founder` — Founder/CEO focus
- `engineer` — Engineering focus
- `research` — Research focus
- Custom profiles in `PROFILE_DIRECTORY`

### PROFILE_DIRECTORY
**Type:** string  
**Default:** `./config/profiles`

Directory containing profile configurations.

```bash
PROFILE_DIRECTORY=~/.config/contexthelp/profiles
```

---

## Examples

### Development
```bash
PIPELINE_DEFAULT_TIMEOUT=5m
PIPELINE_CACHE_ENABLED=true
PIPELINE_MAX_CONCURRENT=2

PROFILE_DEFAULT=general
PROFILE_DIRECTORY=./config/profiles
```

### Production
```bash
PIPELINE_DEFAULT_TIMEOUT=10m
PIPELINE_CACHE_ENABLED=true
PIPELINE_MAX_CONCURRENT=8

PROFILE_DEFAULT=founder
PROFILE_DIRECTORY=/etc/contexthelp/profiles
```

---

## Related

- [../ctxt/pipelines.md](../ctxt/pipelines.md) — Pipeline architecture
- [../ctxt/configuration.md](../ctxt/configuration.md) — Focus profiles
