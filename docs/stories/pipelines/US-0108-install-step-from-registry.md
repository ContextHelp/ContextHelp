# US-0108: Install Step from Registry

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Platform Integrators](../../personas/README.md), [Plugin Developers](../../personas/README.md), [Registry Operators](../../personas/README.md)

---

## User Goal

As a platform integrator or registry operator, I want to install pipeline steps from a registry so that I can:
- Download step packages from registry
- Install steps locally
- Use steps in custom pipelines

## Context

Registries are web services that host:
- Step packages with MANIFEST.yaml
- Download URLs
- Update notifications
- Caching and ETag support

Steps are installed to `~/.config/contexthelp/steps/<name>/` directory.

## Acceptance Criteria

- **Registry is accessible** via configured URL
- **Steps can be downloaded** (requires network access)
- **Manifest format is standardized** (MANIFEST.yaml + step SKILL.md)
- **Step installation idempotent** (can reinstall safely)
- **Step is validated** (metadata check, supported languages)
- **System reminders created** for available versions
- **Uninstall option** for cleanup

## Implementation Notes

### Registry Manifest Format

**MANIFEST.yaml** at registry root:
```yaml
name: "contexthelp-steps"
description: "Collection of pipeline steps for ContextHelp"
version: "1.0"
steps:
  - name: "sentiment-analyzer"
    path: "/steps/sentiment-analyzer"
    license: "MIT"
    version: "1.2.0"
```

### Step Package Structure

```
~/.config/contexthelp/steps/<step-name>/
├── SKILL.md              # Agent Skills format
├── scripts/
│   └── analyze.go        # or any executable
├── references/
│       └── API.md            # optional documentation
├── README.md            # optional README
```

```

**SKILL.md Requirements:**
- Required fields: `name`, `description`
- Optional: `metadata.version`, `metadata.author`, `metadata.supported_languages`, `metadata.config_schema`

### CLI Commands

```bash
# Install from registry
dpkms pipeline step install <name> [--registry <url>]

# Install manually
dpkms pipeline step install <name> <path> <local-path>
```

# List installed steps
dpkms pipeline step list [--source <source>]

# Uninstall step
dpkms pipeline step uninstall <name>
```

### Installation Process

1. **Fetch manifest** from registry
2. **Download step package** (tar.gz, zip, or custom format)
3. Extract to `~/.config/contexthelp/steps/<name>/`
4. **Validate SKILL.md** or manifest structure
5. **Register step in StepStore**
6. **Create SystemReminder if new version available**

### Error Handling

- Registry unavailable → error with details
- Download failed → partial installation attempt
- Invalid manifest → 400 error
- Schema violation → validation error with details
- Duplicate name → 400 error (step already exists)

### E2E Test Checklist

- [ ] `dpkms pipeline step install <name> --registry <url>` → request contains both `name`
  and `registry` fields in payload (or as path/query params)
- [ ] `dpkms pipeline step install <name>` (no `--registry`) → request uses default configured
  registry URL
- [ ] `dpkms pipeline step install <name> <path> <local-path>` → request body contains
  `name`, `path`, and `local_path` fields
- [ ] Fetch manifest from known registry → returns valid manifest
- [ ] Download step from valid manifest → files created, step registered
- [ ] Verify step record created in DB after install: `SELECT * FROM steps WHERE name=?`
  returns row with correct `source` (e.g., `"registry:<url>"`), `path`, `metadata`
- [ ] System reminder created if version different from cached; verify reminder row in
  `system_reminders` table
- [ ] Install step appears in step list with correct metadata; response matches DB record
- [ ] `dpkms pipeline step uninstall <name>` → step record removed from DB
- [ ] Installing step from registry that doesn't exist → returns 404
- [ ] Step with invalid SKILL.md format → parsing fails with error

## Related Stories

- [US-0101](./US-0101-create-custom-pipeline.md) - Create pipelines using installed steps
- [US-0102](./US-0102-list-and-filter-pipelines.md) - List pipelines to discover available steps
- [US-0103](./US-0103-show-pipeline-details.md) - Show pipeline configuration
- [US-0107](./US-0107-discover-local-steps.md) - Discover steps on system
- [US-0109](./US-0109-fetch-registry-manifest.md) - Get available steps from registry
- [US-0110](./US-0110-check-registry-updates.md) - Check for updates
- [US-0111](./US-0111-configure-registry-autoupdate.md) - Configure auto-update
- [US-0112](./US-0112-configure-sandbox-per-pipeline.md) - Configure sandbox
- [US-0113](./US-0113-ctxt-analyze-api-client.md) - ctxt as API client