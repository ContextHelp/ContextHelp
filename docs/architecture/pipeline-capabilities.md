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
regardless of `--verbose`).

---

## Capability reference

| Capability | Step(s) that require it | What satisfies it |
|---|---|---|
| `io` | `filereader` | Always true — filesystem access is assumed available |
| `ocr` | `ocr_extractor`, `frame_ocr` | `tesseract` binary on PATH |
| `transcription` | `audio_transcriber` | `whisper-cli` (or `whisper-cpp`) binary on PATH **and** `providers.transcription.model` naming a whisper.cpp model file |
| `video` | `audio_extractor` | `ffmpeg` and `ffprobe` on PATH |
| `frames` | `frame_sampler` | `ffmpeg` and `ffprobe` on PATH (the `ffmpeg` frames backend) |
| `document` | `pdf_extractor`, `office_extractor` | The pure-Go `golib` provider (PDF text also needs `pdftotext`) |
| `diarization` | `speaker_diarizer` | `pyannote-audio` / `pyannote` binary **or** `python3 -c "import pyannote.audio"` succeeds |
| `vision` | `vision_analyzer` | Ollama reachable at the endpoint with the configured vision model installed, **or** a hosted vision backend named in config. API keys alone never satisfy it |
| `llm` | `graph_extractor`, `structured_metadata` | A local Ollama LLM (the `auto` default), **or** a hosted backend named in config. API keys alone never select one |
| `summarization` | none yet | A local Ollama summarizer (the `auto` default), **or** a hosted backend named in config |
| `blob-externalize` | `externalize_content` | A `BlobStore` is configured (non-nil) |

The media capabilities (`ocr`, `vision`, `transcription`, `video`, `frames`,
`document`) are never pruned: a step that needs one makes its pipeline
unavailable (422) instead. `diarization`, `llm`, `summarization`, `browser`
and `blob-externalize` steps are pruned. A stub or a failed provider (a
named backend that cannot run) never grants a capability.

### Auto-detection order

`auto` is local-only for every role. No step, role or probe reads a hosted
API key to decide what runs; a hosted backend runs only when
`providers.<role>.backend` (or a per-pipeline override) names it.

**Vision** — local Ollama only: GET `/api/tags` (2 s timeout) must answer
and list the configured model (`providers.vision.model`). Otherwise the
role is unavailable. The result is resolved once per process.

**LLM** — local Ollama at the configured endpoint (default
`http://localhost:11434`). The call fails with "model not configured" when
`providers.llm.model` is empty.

**Summarization** — local Ollama like the LLM; the call fails with "no model
configured" when `providers.summarization.model` is empty.

**OCR / Transcription / Diarization / Video / Frames** — binary lookup via
`exec.LookPath` plus the common directories `/opt/homebrew/bin`,
`/usr/local/bin`, `~/.local/bin`. A named backend whose tool is missing is
a configuration error at startup, not a missing capability.

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
which whisper-cli || which whisper-cpp

# Video and frames
which ffmpeg && which ffprobe

# Diarization
which pyannote-audio || python3 -c "import pyannote.audio"

# Vision / LLM — is Ollama up and the model installed?
ollama list

# Vision via Ollama
curl -s http://localhost:11434/api/tags | head -c 80
```

3. If the tool is installed via a shim manager (e.g. `tip`, `mise`, `asdf`) the
   binary may appear on PATH but dispatch to a package that was never actually
   installed. Test the tool directly:

```bash
tesseract --version
whisper-cli --help
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

If you write a built-in step that requires a provider:

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
- [`docs/development.md`](../development.md#adding-a-pipeline-step) — adding a pipeline step
