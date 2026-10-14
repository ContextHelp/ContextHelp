# Pipelines & Profiles Environment Variables

Focus profile selection. Pipelines have no env vars: per-pipeline settings
live under `pipelines.overrides` in the config file.

---

## Profile Configuration

### CTXT_PROFILE
**Type:** string\
**Default:** _(none)_\
**Config key:** `profile.default`\
**Flag:** `--profile`

Default focus profile: the name of an entry under `profile.profiles` in the
config file.

```bash
CTXT_PROFILE=founder
```

Profiles are defined in the config file, not in a separate directory:

```yaml
profile:
  default: founder
  profiles:
    founder:
      description: Founder/CEO focus
```

---

## Related

- [../ctxt/pipelines.md](../ctxt/pipelines.md) — Pipeline architecture
- [../ctxt/configuration.md](../ctxt/configuration.md) — Focus profiles
- [../manual/reference/config-and-permissions.md](../manual/reference/config-and-permissions.md#profile-controls) — Profile controls
