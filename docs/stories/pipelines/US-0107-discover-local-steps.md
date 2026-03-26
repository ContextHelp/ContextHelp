# US-0107: Discover Local Steps

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Platform Integrators](../../personas/README.md), [Plugin Developers](../../personas/README.md), [Registry Operators](../../personas/README.md)

---

## User Goal

As a platform integrator, registry operator, or plugin developer, I want to discover available pipeline steps installed on my system so that I can:
- See what steps are available for pipeline creation
- Install new steps from registries
- Uninstall unused or broken steps
- Understand step capabilities (config schemas, supported languages)

## Context

Steps are discovered from:
- **Local filesystem** (`~/.config/contexthelp/steps/`)
- **Remote registries** (configured via `dpkms pipeline step registry`)

Each step must provide:
- **SKILL.md** file with Agent Skills frontmatter
  Or **Manifest.yaml** file (fallback for registries)

Steps must be registered before pipelines can use them.

## Acceptance Criteria

- **Local steps are scanned on startup** and registered
- Automatically discovered
- **Steps can be installed from registries** (download and register)
- **Step registry queries show available steps**
- **Step metadata is cached** (name, version, config schema, supported languages)
- **Custom steps can be uninstalled** when no longer needed

## Implementation Notes

### Discovery Process

#### Local Discovery

1. **Scan directory structure**
```go
path := os.Getenv("DPKMS_STEPS_PATH")
if path == "" {
    path = filepath.Join(os.Getenv("HOME"), ".config", "contexthelp", "steps")
}
// Scan for SKILL.md directories
dirs, _ := os.ReadDir(path)
for _, dir := range dirs {
    skillPath := filepath.Join(path, dir.Name(), "SKILL.md")
    if _, err := os.Stat(skillPath); err == nil {
        // Parse SKILL.md
        step, err := parseSkillFile(skillPath)
        if err != nil {
            // Register step in StepStore
        }
    }
}
```

2. **Parse SKILL.md**
```go
type SkillMetadata struct {
    Name           string                 // from frontmatter
    Description    string
    License        string
    Version        string
    Author         string
    ConfigSchema   map[string]any          // JSON Schema for config validation
    SupportedLangs []string               // languages step script supports
}
}
```

3. **Step registration**
- Each step is stored in database with:
  - Name (primary key)
  - Source ("builtin", "local:<path>", or "registry:<url>")
  - Metadata (serialized JSON)
  - Path (filesystem path or registry URL)
  - InstalledAt, UpdatedAt timestamps
  - Config schema validation if provided

### Registry Fetching

**Fetch manifest** from URL:
```bash
GET {registry-url}/MANIFEST.yaml
GET {registry-url}/manifest.yaml
```

**Parse manifest:**
```yaml
name: "contexthelp-steps"
description: "Collection of pipeline steps"
version: "1.0"
steps:
  - name: "sentiment-analyzer"
    path: "/steps/sentiment-analyzer"
    license: "MIT"
    version: "1.2.0"
```

**Cache management:**
- Store manifest in `registry_cache` table with:
  - RegistryURL (primary key)
  - Manifest (JSON blob)
  - LastFetched (timestamp string)
  - ETag (for change detection)
  - AutoUpdate (bool, per-registry config)
```

4. **Update detection**
- Compare cached vs fetched manifests:
- Same version → no action needed
- Different version → create system reminder
- New version → create system reminder
- ETag changed → detect change, old and new versions stored

### Step Installation

**Download and Install:**
1. Download step package from registry
2. Extract to `~/.config/contexthelp/steps/<name>/` directory
3. Validate SKILL.md or manifest
4. Register step in StepStore
5. Add entry to SystemReminder for version if applicable

### CLI Commands

```bash
# List available steps
dpkms pipeline step list [--source <source>]
dpkms pipeline step list --source builtin  # Built-in steps only

# Install step from registry
dpkms pipeline step install <name> [--registry <url>]

# Get step details
dpkms pipeline step show <name>

# Uninstall step
dpkms pipeline step uninstall <name>

# Manage registries
dpkms pipeline step registry add <name> <url>
dpkms pipeline step registry list
dpkms pipeline step registry update <url> # Manual update trigger
dpkms pipeline step registry autoupdate <url> [--enable|--disable]
```

# List system reminders
dpkms system reminders list [--active]
```

# Dismiss reminder
dpkms system reminders dismiss <id>
```

## E2E Test Checklist

- [ ] Local steps directory is scanned on startup
- [ ] All SKILL.md files are parsed and registered
- [ ] Verify step metadata stored in DB: `SELECT * FROM steps WHERE name=?` returns row with
  correct `name`, `source`, `metadata`, `path`, `installed_at` fields
- [ ] Steps with valid SKILL.md format are registered
- [ ] Duplicate step names are detected and handled (newest wins or error)
- [ ] Invalid SKILL.md format returns parsing error
- [ ] `dpkms pipeline step list` → request sent to steps list endpoint; response matches DB
  records in `steps` table
- [ ] `dpkms pipeline step list --source builtin` → request contains `source=builtin` query
  param; response includes only steps with `source="builtin"` in DB
- [ ] `dpkms pipeline step show <name>` → request sent to step detail endpoint; response fields
  match DB record for that step name
- [ ] Registry manifest fetch works (GET returns manifest)
- [ ] Registry manifest is cached with ETag; verify ETag stored in `registry_cache` table
- [ ] System reminders are created for new versions
- [ ] Download and install steps from registry; verify step record created in DB after install

## Related Stories

- [US-0101](./US-0101-create-custom-pipeline.md) - Create pipelines using discovered steps
- [US-0102](./US-0102-list-and-filter-pipelines.md) - List pipelines to reference available steps
- [US-0103](./US-0103-show-pipeline-details.md) - Show pipeline configuration
- [US-0104](./US-0104-delete-custom-pipeline.md) - Delete pipeline if no longer needed
- [US-0105](./US-0105-archive-pipeline.md) - Archive pipelines instead of delete
- [US-0106](./US-0106-enqueue-content-via-dpkms.md) - Use listed pipelines in enqueue
- [US-0108](./US-0108-install-step-from-registry.md) - Install from registry
- [US-0109](./US-0109-fetch-registry-manifest.md) - Get available steps from registry
- [US-0110](./US-0110-check-registry-updates.md) - Check for updates
- [US-0111](./US-0111-configure-registry-autoupdate.md) - Configure auto-update
- [US-0112](./US-0112-configure-sandbox-per-pipeline.md) - Configure sandbox
- [US-0113](./US-0113-ctxt-analyze-api-client.md) - ctxt as API client

---

## Personas

- [Maintainers](../../personas/maintainers.md)
- [Platform Integrators](../../personas/platform-integrators.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)

---

## E2E Tests

[us0107_steps_test.go](../../../test/integration/us0107_steps_test.go)
