# US-0029: Install And Enable Plugin

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Platform Integrators](../../personas/platform-integrators.md),
[Operations](../../personas/operations.md)

---

## User Goal

As a platform integrator or operator, I want to install pipeline steps from a registry
and enable plugins via config so the system is extended without modifying core code.

---

## Context

dPKMS has two distinct extension mechanisms:

1. **Pipeline Steps** — installed from a step registry via REST API
   (`POST /api/v1/steps/install`). Appear in `GET /api/v1/steps` once installed.
   Used as named steps inside custom pipeline definitions.

2. **Plugins** — declared in the `plugins` YAML config key. Loaded at server startup.
   No REST install endpoint; enabled by editing the config file and restarting.

---

## Acceptance Criteria

- [ ] Operator can install a step from a registry via `dpkms pipeline step install`
- [ ] `--registry` flag is required; command fails without it
- [ ] Installed step `name` and `from_registry` fields are sent to server in request body
- [ ] Server stores step; `GET /api/v1/steps` listing includes the installed step
- [ ] Operator can list steps with optional `--source` filter
- [ ] Operator can uninstall a step; `GET /api/v1/steps/{name}` returns 404 after removal
- [ ] Operator can add a step registry with `dpkms pipeline step registry add`
- [ ] Registry URL is sent as `{"url":"..."}` to `POST /api/v1/steps/registries/fetch`
- [ ] Registered registry appears in `GET /api/v1/steps/registries`
- [ ] Operator can update a registry manifest via `dpkms pipeline step registry update`
- [ ] Plugins are enabled by adding entries under `plugins.load[]` in config YAML
- [ ] Server loads enabled plugins at startup; `PostIngest` hooks execute on new objects

---

## Implementation Notes

### Step Install CLI

```
# Add a registry first
dpkms pipeline step registry add my-registry https://registry.example.com/steps

# List available registries
dpkms pipeline step registry list

# Install step from registry
dpkms pipeline step install extract-entities --registry https://registry.example.com/steps

# List installed steps
dpkms pipeline step list
dpkms pipeline step list --source registry  # filter by source

# Uninstall step
dpkms pipeline step uninstall extract-entities
```

### Step Install REST API

```
POST /api/v1/steps/install
Content-Type: application/json

{ "name": "extract-entities", "from_registry": "https://registry.example.com/steps" }

→ 200 OK
{ "status": "installed" }

GET /api/v1/steps
→ 200 OK
{ "steps": [{ "name": "extract-entities", "source": "registry:...", ... }], "total": 1 }

GET /api/v1/steps/extract-entities
→ 200 OK
{ "name": "extract-entities", "source": "registry:...", "installed_at": "..." }

DELETE /api/v1/steps/extract-entities
→ 204 No Content
```

### Registry Fetch REST API

```
POST /api/v1/steps/registries/fetch
Content-Type: application/json

{ "url": "https://registry.example.com/steps" }

→ 200 OK

GET /api/v1/steps/registries
→ 200 OK
{ "registries": [{ "registry_url": "https://...", "last_fetched": "...", ... }], "total": 1 }
```

### Plugin Config (YAML)

```yaml
plugins:
  load:
    - name: alias-plugin
      config:
        definitions:
          urls: "list --type url"
    - name: rss-feed
      config:
        default_interval_seconds: 3600
        default_item_pipeline: text.long
```

---

## E2E Test Checklist

### Registry — Payload Fields Sent to Server
- [ ] RegistryAdd: `dpkms pipeline step registry add <name> <url>` sends
  `{"url":"<url>"}` to `POST /api/v1/steps/registries/fetch`
- [ ] RegistryAdd: Server responds `200 OK`
- [ ] RegistryStorage: `GET /api/v1/steps/registries` shows registry with matching `registry_url`
- [ ] RegistryUpdate: `dpkms pipeline step registry update <url>` sends request to
  `POST /api/v1/steps/registries/{url}/update`

### Step Install — Payload Fields Sent to Server
- [ ] Install: `dpkms pipeline step install <name> --registry <url>` sends
  `{"name":"<name>","from_registry":"<url>"}` to `POST /api/v1/steps/install`
- [ ] Install: Missing `--registry` flag → command exits with error before making request
- [ ] Install: Server responds `200 OK` with `{"status":"installed"}`
- [ ] Install: `GET /api/v1/steps/{name}` returns installed step with
  `name`, `source`, `installed_at` populated

### Step List — Source Filter Sent to Server
- [ ] List: `dpkms pipeline step list` sends `GET /api/v1/steps` (no filter)
- [ ] List: `dpkms pipeline step list --source registry` sends `GET /api/v1/steps?source=registry`
- [ ] List: Response includes `name`, `source`, `version` fields for each step

### Step Uninstall — Server-Side Removal
- [ ] Uninstall: `dpkms pipeline step uninstall <name>` sends `DELETE /api/v1/steps/{name}`
- [ ] Uninstall: Server responds `204 No Content`
- [ ] Uninstall: `GET /api/v1/steps/{name}` returns `404 NOT_FOUND` after removal

### Plugin Config
- [ ] PluginLoad: Adding entry to `plugins.load[]` in config YAML and restarting server
  causes plugin to be loaded (no error in server startup logs)
- [ ] PluginLoad: `PostIngest` hook fires on new object creation (observable via plugin side-effect)
- [ ] PluginConfig: Plugin `config` map values are passed to plugin `Init` method at startup
- [ ] PluginDisable: Removing entry from `plugins.load[]` and restarting → plugin absent

### Step Used in Custom Pipeline
- [ ] Integration: Installed step name can be referenced in a pipeline created via
  `POST /api/v1/pipelines`; enqueued job completes without "step not found" error

---

## Related Stories

- [US-0028](US-0028-register-custom-pipeline.md) — custom pipelines reference installed steps
- [US-0042](../plugins/US-0042-implement-custom-enrichment-plugin.md) — plugin authoring
- [US-0027](US-0027-configure-ai-provider.md) — AI provider config consumed by steps

---

## Personas

- [Maintainers](../../personas/maintainers.md)
- [Platform Engineer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/platform-engineer.md)
- [Operations](../../personas/operations.md)

---

## E2E Tests

- `test/integration/us0029_install_plugin_test.go::TestUS0029_StepStoredAndRetrievable`
- `test/integration/us0029_install_plugin_test.go::TestUS0029_InstalledStepAppearsInList`
- `test/integration/us0029_install_plugin_test.go::TestUS0029_UninstallStepRemovesIt`
- `test/integration/us0029_install_plugin_test.go::TestUS0029_PluginRegistersHook`
- `test/integration/us0029_install_plugin_test.go::TestUS0029_PluginHookFiredOnIngest`
