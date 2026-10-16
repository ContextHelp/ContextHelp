# External Integrations

How external systems and AI models connect to ctxt and dpkms.

## Ingestion adapters

- [ingestion-adapters.md](ingestion-adapters.md) - Adapters run by `ctxt ingest` (cardamum contacts, himalaya email)
- [inbound-contract.md](inbound-contract.md) - Contract for hooks/adapters writing to ctxt
  (mention conventions, identity rules, idempotency)

Bulk importers (`ctxt import chrome|pinboard|notion|slack|...`) are built in; `ctxt import --help` lists them.

## AI providers

Providers are compiled into dpkms and picked by config (`providers.<role>.backend`); there is no plugin system:

| Role | Backends |
|------|----------|
| LLM | `anthropic`, `openai`, `ollama`, `stub` (`auto` picks Anthropic, then OpenAI by available key, else Ollama) |
| Vision | `ollama`, `openai`, `anthropic`, `gemini`, `openrouter`, `stub` |
| Embeddings | `ollama`, `stub` |
| Transcription | `whisper-cli`, `ollama`, `stub` |
| OCR / documents / video / diarization | `tesseract`; `pdftotext`, `golib`; `ffmpeg`; `pyannote` (each with `stub`) |

Configuration and keys: [../environment-variables/ai-providers.md](../environment-variables/ai-providers.md).

## Other paths in

- **REST API** - any client can post to `/api/v1/analyze`, `/api/v1/capture/*` or `/api/v1/inbox`; see [../api/](../api/).
- **Federation** - push-only replication between dpkms instances; see [../ctxt/configuration.md#federation-configuration](../ctxt/configuration.md#federation-configuration).
- **MCP** - dpkms serves `/api/v1/mcp/` with the read tools `search` and `schema`; see [../manual/workflows/mcp-agents.md](../manual/workflows/mcp-agents.md).

## See Also

- [../decisions/ADR-078-no-plugin-system.md](../decisions/ADR-078-no-plugin-system.md) - No plugin system
- [../ctxt/pipelines.md](../ctxt/pipelines.md) - Enrichment pipelines using AI providers
- [../dpkms/registries.md](../dpkms/registries.md) - External registry integration
