# External Integrations

This directory contains documentation for integrations with external systems and AI models.

## Available Integrations

### AI Model Integrations
- [glm-pov.md](glm-pov.md) - GLM (General Language Model) integration perspective
- [gemini-pov.md](gemini-pov.md) - Google Gemini integration perspective

### External Systems
- [leann-integration.md](leann-integration.md) - Leann system integration

## Integration Architecture

Integrations typically extend the system through:

1. **Plugin System** - Most integrations are implemented as plugins
2. **API Providers** - AI models, external services
3. **Data Connectors** - Import/export, sync mechanisms

## Integration Patterns

### AI Provider Integration
- Implement provider interface
- Configure in `configuration.md`
- Used by enrichment pipelines
- Examples: OpenAI, Anthropic, GLM, Gemini

### External System Integration
- Define connector plugin
- Map external schema to internal objects
- Handle sync and updates
- Preserve provenance

## See Also

- [../plugins/](../plugins/) - Plugin system documentation
- [../ctxt/pipelines.md](../ctxt/pipelines.md) - Enrichment pipelines using AI providers
- [../dpkms/registries.md](../dpkms/registries.md) - External registry integration
