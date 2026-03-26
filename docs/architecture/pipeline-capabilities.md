# Pipeline Capability System

Pipeline steps declare what runtime capabilities they need. At startup the system
probes the environment and records which capabilities are available. Steps whose
requirements cannot be met are **pruned** (silently removed from the pipeline)
rather than causing a hard failure.

This document explains what each capability is, what satisfies it, and what to
do when a step is pruned.

---

## How it works

```
CapabilitiesFromFactory(providers.Factory)
  → probes each provider
  → builds a CapabilitySet { "ocr": true, "llm": true, … }

ValidateCapabilities(steps, caps)
  → returns indices of steps whose Contract().Capabilities are unsatisfied

buildPipeline (non-strict mode)
  → logs: builtins: pipeline "X": pruning step "Y" (missing capability)
  → removes the step and continues
```

The log line is emitted via the standard `log` package (always visible,
regardless of the `--verbose` / `CTXT_DEBUG` level).

---

## Capability reference

| Capability | Step(s) that require it | What satisfies it |
|---|---|---|
| `io` | `filereader` | Always true — filesystem access is assumed available |
| `ocr` | `ocr_extractor`, `frame_ocr` | `tesseract` binary on PATH |
| `transcription` | `audio_transcriber` | `whisper-cpp` or `whisper` binary on PATH |
| `diarization` | `speaker_diarizer` | `pyannote-audio` / `pyannote` binary **or** `python3 -c "import pyannote.audio"` succeeds |
| `vision` | `vision_analyzer` | Ollama reachable at endpoint **or** any of `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, `OPENROUTER_API_KEY` set |
| `llm` | `autosuggest` plugin step | `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` set **or** Ollama reachable |
| `blob-externalize` | `externalize_content` | A `BlobStore` is configured (non-nil) |

### Auto-detection order

**Vision** — Ollama first (GET `/api/tags`, 2 s timeout), then API keys in this
order: OpenAI → Anthropic → Gemini → OpenRouter.

**LLM** — API keys first (Anthropic → OpenAI), then Ollama at
`http://localhost:11434` (always attempted as last resort; returns stub only
when explicitly configured `backend: stub`).

**OCR / Transcription / Diarization** — binary lookup via `exec.LookPath` plus
the common directories `/opt/homebrew/bin`, `/usr/local/bin`,
`~/.local/bin`.

---

## Diagnosing a pruning warning

```
builtins: pipeline "audio.transcript": pruning step "audio_transcriber" (missing capability)
```

1. Identify the capability from the table above (step → capability).
2. Verify the tool or key is actually present:

```bash
# OCR
which tesseract

# Transcription
which whisper-cpp || which whisper

# Diarization
which pyannote-audio || python3 -c "import pyannote.audio"

# Vision / LLM — check env
printenv OPENAI_API_KEY ANTHROPIC_API_KEY GEMINI_API_KEY OPENROUTER_API_KEY

# Vision via Ollama
curl -s http://localhost:11434/api/tags | head -c 80
```

3. If the tool is installed via a shim manager (e.g. `tip`, `mise`, `asdf`) the
   binary may appear on PATH but dispatch to a package that was never actually
   installed. Test the tool directly:

```bash
tesseract --version
whisper-cpp --help
```

4. If the tool works but the step is still pruned, the PATH visible to the
   server process may differ from your shell. Set the backend explicitly in
   config instead of relying on auto-detection:

```yaml
providers:
  ocr:
    backend: tesseract
  transcription:
    backend: whisper-cli
```

---

## Adding a capability to a new step

If you write a step (built-in or plugin) that requires a provider:

1. Declare the capability in the step's `Contract()`:

```go
Capabilities: []string{"ocr"},   // use an existing token where possible
```

2. If you are introducing a **new** capability token, add the corresponding
   probe to `CapabilitiesFromFactory` in
   `internal/pipeline/builtins/capabilities.go`:

```go
if _, isStub := f.MyProvider().(*providers.StubMyProvider); !isStub {
    caps["my-capability"] = true
}
```

   The stub type **must be exported** for the type assertion to work from a
   different package. Follow the pattern of `StubOCRProvider`, `StubLLMProvider`,
   etc. in `internal/providers/`.

3. Add the new capability to the reference table in this document.

---

## Related

- `internal/pipeline/builtins/capabilities.go` — probe logic
- `internal/pipeline/validate.go` — `ValidateCapabilities`
- `internal/providers/factory.go` — provider resolution
- `docs/environment-variables/ai-providers.md` — API key configuration
- `docs/manual/admin-extensibility/plugin-development.md` — plugin authoring guide
