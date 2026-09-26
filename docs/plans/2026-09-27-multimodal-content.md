# Multimodal content: phase 1 as text, later native embeddings

> **Date:** 2026-09-27 (decisions resolved 2026-09-26)
> **Decision record:** [ADR-076 – Media as Text Representations: Provenance, Originals and Chunks](../decisions/ADR-076-media-text-representations.md) (Accepted)
> **Status:** design only. Nothing here is implemented. All owner decisions are resolved (see [Resolved decisions](#resolved-decisions)); one integration detail is pending from the owner (the frame-extraction package).
> **Audience:** whoever implements the increments

ctxt must accept every content type: text, images, video, speech and meetings. The owner has set the approach:

- **Phase 1** represents every medium as text and embeds that text with the text model the registry already runs: `snowflake-arctic-embed2` (multilingual, 1024 dimensions) on local Ollama.
- **The later phase** adds native multimodal embeddings (CLIP or SigLIP class, nomic-embed-vision, jina-embeddings-v4). The per-model index from the [ADR-071 amendment](../decisions/ADR-071-embedding-index-versioning.md#amendment-2026-09-26) already lets models sit side by side. This plan only records what phase 1 must not foreclose.

**Out of scope for phase 1:** non-speech sound. Sound without speech is neither stored as a representation nor tracked in metadata (owner decision 6). Per-OS meeting recording is ADR-069's work.

## Overarching principle: every media-to-text process is a swappable provider

This rule applies to every process that turns media into text: OCR, captioning, speech recognition, diarization, frame extraction, summarization, and any process added later.

- **Behind an interface.** Each process is a provider role with a Go interface in `internal/providers`. Pipeline steps depend on the interface, never on a tool.
- **Selected by config.** `providers.<role>.backend` picks the implementation, with `model` and `endpoint` where they apply. `pipelines.overrides.<pipeline>.providers.<role>` overrides it for one pipeline.
- **A working local default.** Every role has a local default that runs on the owner's machine with no network access beyond local Ollama.
- **Cloud is opt-in only.** A cloud backend runs only when config names it explicitly for that role. `auto` resolves to local backends and nothing else. No step, role or probe looks at cloud API keys to decide what to run.
- **No silent fallback.** A named backend that is unavailable is an error. It never degrades to a stub, and never to another backend.

This applies to all roles, including the non-media `llm` role. The [cloud-resolution changes](#where-the-cloud-preferring-auto-changes-land) say where the code changes land.

The survey below was checked against the code at `2b57f39` and probed on the owner's machine on 2026-09-26. Every "fails", "skipped" or "missing" in it was observed, not inferred, unless the text says it was read from code.

## Survey: what exists today

### Routing

A capture reaches `Service.Analyze`. It runs the registry's detectors, then the **source rules** (URL pattern, then file extension), then the **content rules** (content tests, `text.short` last). See `internal/pipeline/registry.go` and `internal/pipeline/builtins/builtins.go`.

- Media routes only by file extension. There are no content tests for media.
- A pipeline whose `Providers` are not all available is **not registered** ("skipping pipeline … required provider(s) … not available"). Its extensions still route to it, so the capture fails with `422 PIPELINE_NOT_FOUND`. There is no fallback, which is correct, but the message does not name the missing tool.

### Pipelines and what they produce

| Pipeline | Claims | Steps that produce text | Text produced |
|---|---|---|---|
| `image.ocr` | `.png .jpg .jpeg .webp .tiff .tif .bmp .gif` | `ocr_extractor` | one "OCR Text" section; RawContent replaced by the OCR text |
| `image.analysis` | nothing (named only with `--pipeline`) | `ocr_extractor`, `vision_analyzer` | OCR section plus a "Vision Analysis" section and summary node |
| `audio.transcribe` | `.mp3 .wav .ogg .flac .m4a` | `audio_transcriber`, `timestamp_aligner` | full transcript as RawContent and a root summary node; one section per segment |
| `video.full` | `.mp4 .mov .avi .mkv .webm` | `audio_transcriber`, `frame_ocr`, `timeline_assembler` | transcript, plus flat sections for fake scenes and joined frame OCR |
| `video.audio_only` | nothing | as `audio.transcribe` | transcript |
| `doc.pdf` | `.pdf` | `pdf_extractor` | one section per page |
| `doc.office` | `.docx` | `office_extractor` | paragraphs (fixed on 2026-09-26: fails loudly, never embeds zip bytes) |

### Providers and their state on this machine

| Role | Backends | Selected by `auto` | Probe result |
|---|---|---|---|
| `ocr` | tesseract | tesseract if on PATH | **missing**, so `image.ocr` and `image.analysis` are skipped and `.png` captures return 422 |
| `vision` | Ollama (default model `llava`), OpenAI, Anthropic, Gemini, OpenRouter | Ollama if `/api/tags` answers, **else the first cloud API key found** | Ollama answers on :11434 but holds only `snowflake-arctic-embed2` and `nomic-embed-text`. No vision model is pulled, so an analysis would fail at run time |
| `transcription` | whisper CLI; `ollama` is a stub | `whisper-cpp`, then `whisper` on PATH | `whisper` is openai-whisper (pipx). It **rejects the flags ctxt passes** (`-f … -oj`, then `--file … --output-json`) and exits 2. Homebrew's whisper.cpp installs `whisper-cli`, which the factory never looks for; it also needs `--model` |
| `diarization` | pyannote | `pyannote-audio` or `pyannote` binary, or the python module | `pyannote-audio` is found, but its CLI rejects `--input` (it takes `apply PIPELINE AUDIO`), and its torchcodec import is broken |
| `video` | ffmpeg | ffmpeg and ffprobe on PATH | present, but see "video capability" below |
| `document` | golib, pdftotext | golib (falls back to pdftotext for PDF) | pdftotext present |
| `llm` (tagger, sectioner) | Anthropic, OpenAI, Ollama | **Anthropic or OpenAI key first**, Ollama last | no chat model is pulled on :11434 |

When an explicitly configured backend is missing, the factory silently returns the **stub** provider. The capability check then treats the role as unavailable.

### Defects found (read from code, confirmed by probe where marked)

1. **`video.full` and `video.audio_only` can never register.** `CapabilitiesFromFactory` never sets the `video` capability, so both are skipped even with ffmpeg installed (probed).
2. **`image.analysis` sends text to the vision model.** `ocr_extractor` overwrites RawContent with the OCR text, and `vision_analyzer` then analyses RawContent.
3. **`speaker_diarizer` never runs.** Both constructors build it with `enabled=false`. Yet `audio.transcribe` lists `diarization` as a required provider, so a missing pyannote blocks all transcription.
4. **Binary captures are corrupted in transit.** `ctxt capture <file>` sends the file as a JSON string, and `encoding/json` replaces invalid UTF-8 with U+FFFD. Audio, video and PDF steps instead read `draft.Source` as a path **on the daemon's host**, which works only when CLI and daemon share a filesystem. The ambient meeting enqueuer sends no `source` at all.
5. **The original media is not kept.** Extraction steps overwrite RawContent. `externalize_content` is in no pipeline, and the drivers' default BlobStore is a no-op stub. Only backup calls `blob.New`, although local and S3 stores exist. The `source-image` artifact node's Content is just the content type.
6. **Placeholder text can reach the index.**
   - `timeline_assembler` writes `Scene detected: map[…]` sections from a fake scene heuristic.
   - The stub OCR, vision and transcription providers return bracketed placeholder strings.
   - A pipeline built with stub providers (a per-pipeline provider override) has its provider steps **pruned**, and then embeds the raw file. A probe with stub providers produced `EmbeddingText` starting `\x89PNG` for `image.ocr`, `RIFF…WAVE` for `audio.transcribe`, and the MP4 header for `video.full`.
   - The PDF placeholder is filed separately.
7. **`video.full` has more broken steps.**
   - `audio_transcriber` transcribes the video path, not the extracted audio.
   - `frame_ocr` passes the frame's *path string* to OCR as image bytes.
   - ffmpeg writes the WAV and the frame PNGs next to the user's source file and never removes them.
   - There is no frame captioning.
8. **The projection loses or doubles media text.** With graph nodes present, `ProjectIndex` reads only summary and section nodes. So the flat sections from `timestamp_aligner` and `timeline_assembler` (frame OCR included) are never indexed. Audio indexes the transcript twice: the root summary holds the full text, and each segment node repeats a slice of it. (Exact duplicates are dropped; slices are not.)
9. **Embedding sees at most 8192 bytes.** `OllamaEmbeddingProvider.Embed` cuts at byte 8192, which can split a UTF-8 rune, and uses the legacy `/api/embeddings`. A one-hour meeting transcript (roughly 50–60 KB) is about 85% unindexed.
10. **The graph edge direction is inverted.** The two `derives_from` edges point from the artifact to the derived text.

### Tests

- Media steps and providers are tested with stubs, or through pure parsing functions (RTTM, frame rate, whisper JSON).
- `embedding_coverage_test.go` builds every media pipeline with stub providers and asserts that vectors exist. That proves wiring, but it passes on placeholder text.
- There are no xrr cassettes for tesseract, whisper, ffmpeg, pyannote or the Ollama vision API. The only cassettes are for Ollama embeddings.

### Docs that overstate what ships

- [US-0003](../stories/ingestion/US-0003-image-ocr-and-analysis.md), [US-0004](../stories/ingestion/US-0004-audio-transcription-and-indexing.md) and [US-0005](../stories/ingestion/US-0005-video-processing-with-scenes.md) are marked `shipped`.
- [US-0061](../stories/search/US-0061-visual-similarity-search.md) (image-as-query, dual embedding) is marked `shipped`.
- [`docs/meeting-capture.md`](../meeting-capture.md) documents `ctxt capture meeting start|stop|redact|export`. The code has the meeting source's interface ([ADR-069](../decisions/ADR-069-meeting-capture-source.md)) but no per-OS recorder and no CLI.

### Coverage

| Type | Accepted | Text produced | Embedded | Original kept | Needs | Status on this machine | Main gaps |
|---|---|---|---|---|---|---|---|
| Text | yes (`text.*`, `doc.markdown`, `doc.code`) | body | yes, one chunk | the text is the object | none | works | cut at 8192 bytes |
| Image | by extension; pipelines skipped | OCR only; caption only via `--pipeline image.analysis` | would be | no | tesseract; vision model for captions | **422**: tesseract missing, no vision model | captions not in the default route; vision reads OCR text; bytes corrupted over HTTP |
| Audio (speech) | yes | transcript, per-segment sections | yes, doubled, cut at 8192 bytes | no | whisper **and** pyannote | **job fails**: whisper flag mismatch | whisper.cpp not found (`whisper-cli`); diarization never runs yet is required; server-host path |
| Sound (non-speech) | as audio | nothing meaningful | — | no | — | — | out of scope for phase 1 (owner decision 6) |
| Video | by extension; pipelines never register | would be transcript, fake scenes, broken frame OCR | would embed placeholders | no | ffmpeg, whisper, tesseract | **422 always** (video capability) | transcribes the wrong file, OCRs path strings, no frame captions, leaves temp files |
| Meeting | no recorder; the enqueuer targets `video.full` or `audio.transcribe` | as audio or video | — | no | as audio or video, plus OS capture APIs | not implemented | no summary, no speakers, no `source`, docs overstate |
| PDF | yes | per-page text | yes | no | pdftotext (golib placeholder otherwise) | works with pdftotext | scanned pages yield nothing; embedded images ignored |
| DOCX | yes | paragraphs | yes | no | none (pure Go) | works | embedded images ignored; `.odt` and `.epub` pending |

## Phase 1 design

### Provider roles

Each media-to-text process is a role, per the [overarching principle](#overarching-principle-every-media-to-text-process-is-a-swappable-provider).

| Role | Interface (in `internal/providers`) | Local default | Cloud backends (opt-in only) | Status |
|---|---|---|---|---|
| `ocr` | `OCRProvider` (exists) | tesseract | none today | exists; must stop falling back to the stub |
| `vision` (captions only) | `vision.Provider` (exists) | Ollama `qwen2.5vl:7b` | OpenAI, Anthropic, Gemini, OpenRouter (exist) | exists; `auto` must stop reading cloud keys |
| `transcription` | `TranscriptionProvider` (exists) | whisper.cpp `whisper-cli` with `large-v3-turbo` | none today | exists; the whisper.cpp and openai-whisper invocations must be fixed |
| `diarization` | `DiarizationProvider` (exists) | pyannote, run locally; opt-in per pipeline | none today | exists; the pyannote invocation must be fixed |
| `frames` (keyframe selection and extraction) | **new** `FrameProvider` | **the owner's frame-extraction package** (details pending); ffmpeg scene detection as the interim local default | none | new; split from `VideoProvider.SampleFrames` |
| `video` (probe, audio track) | `VideoProvider` (exists, minus frame sampling) | ffmpeg | none | exists |
| `summarization` | **new** `SummarizationProvider` | local Ollama chat model with a built-in prompt | OpenAI, Anthropic through the `llm` backends, opt-in | new; see [Summaries](#summaries) |
| `llm` (tagger, sectioner, other enrichment) | `LLMProvider` (exists) | local Ollama | OpenAI, Anthropic (exist) | exists; `auto` must stop preferring cloud keys |
| `document` | `DocumentProvider` (exists) | golib, with pdftotext for PDF | none | exists |

- **Config keys.** Each role uses `providers.<role>.{backend, model, endpoint}`. `frames` and `summarization` are new config keys. The frame role's key carries the scene-detection bounds (decision 4).
- **Doctor and capabilities.** Capabilities are derived per role from the resolved local backend, never from environment keys. `ctxt doctor` reports every role (see increment 7).

### Frame extraction: pending integration

The owner has built a package for frame extraction and will point to it. Until then:

- **Placeholder.** Integrate the owner's frame-extraction package as the `frames` provider's local default. The details (package path, API, dependency route, how it expresses scene detection and rate bounds) are pending from the owner. This plan does not design the extraction internals.
- **Contract the step needs from any `frames` backend.**
  - Input: the video, read through the media accessor.
  - Output: frames with `frame_index`, `frame_ms` and image bytes (or a blob key).
  - Honours a minimum and a maximum frame rate, with scene detection choosing frames between them.
- **Interim default.** Until the package is integrated, the existing ffmpeg backend serves as the local default behind the same interface: ffmpeg scene detection bounded by the minimum and maximum rates. It is replaced, not extended, when the package lands.

### Representations per type

Every medium becomes one or more **representations**: text units, each with a kind and an anchor. They are stored as graph nodes (ADR-063 makes the graph canonical). Section nodes hold the representations; summary nodes hold summaries.

| Type | Representations (in projection order) | Anchor |
|---|---|---|
| Image | `caption` (vision provider: what the image shows, including diagram structure), `ocr` (OCR provider: text in the image) | none |
| Audio, speech | `summary` (summarization provider, above a length threshold), `transcript` (one node per ASR segment) | `start_ms`, `end_ms`, `speaker` when diarized |
| Video | `summary`, `transcript` (from the extracted audio), `frame_caption` and `frame_ocr` for each selected frame | `start_ms`, `end_ms`; `frame_index`, `frame_ms` |
| Meeting | `summary` (decisions, action items, topics), `transcript` with `speaker`, plus the video representations when there is video | as audio and video |
| PDF, DOCX | `extracted_text` per page or section; `ocr` for image-only pages; `caption` and `ocr` for embedded images | `page_number`; `image_index` |
| Subtitle sidecar (`.vtt`, `.srt`) | `transcript` with `origin: sidecar` | cue times |

- The vector for a representation is the vector of its **text**.
- There are no placeholders. A representation whose producer failed or returned nothing is absent, never "[…]".
- Audio with no speech produces no representation, and nothing about the non-speech sound is recorded (out of scope).

### Provenance convention

ADR-076 decides this. In short:

- **Node metadata on every representation node:**
  - `representation`: one of `caption`, `ocr`, `transcript`, `frame_caption`, `frame_ocr`, `summary`, `extracted_text`
  - the anchor keys, reusing the existing ones: `start_ms`, `end_ms`, `speaker`, `page_number`, plus the new `frame_index`, `frame_ms`, `image_index`
  - `provider` and `model` (for example `ollama` and `qwen2.5vl:7b`)
  - `confidence` when the producer reports one
  - `origin` (`sidecar` for imported subtitles)
- **One `artifact` node per original** (and per extracted frame worth keeping). Its metadata holds `blob_key`, `content_type`, `size_bytes`, `sha256` and `file_name`. Each representation node has a `derives_from` edge **from the representation to the artifact**. The two existing edges are flipped.
- **Unique node ordinals.** Several steps now emit section nodes, so the ordinal per node type comes from a shared allocator, not from each step's own counter.
- **Chunks carry provenance.** Each embedded chunk records its representation, the IDs of its source nodes, and its anchor span (see Chunking). Search can then say "matched the transcript at 12:03–12:41" or "matched the OCR of frame 7".

### Original retention: keep all, configurable store and CDN

All originals are kept (decision 9), with no size cap.

- **Storage.**
  - Before any step runs, the original is stored **as bytes** in the blob store, content-addressed by SHA-256.
  - The store is configurable through the existing `storage.blob` config: `backend` (`local` default, `s3`), `local.path`, and `s3.*` ([P-060](P-060-s3-media-storage.md)).
  - The local blob store is the default. Wire the existing `blob.New` factory into dpkms and the CLI's direct paths, and remove the no-op stub from the production default.
- **Delivery (CDN).** New `storage.blob.delivery` config selects how clients fetch an original:
  - `direct` (default): the local daemon serves it.
  - `presigned`: an S3 presigned URL (`s3.presign_expiry` exists).
  - `cdn`: a URL built from a configured `base_url` in front of the bucket.

  Retrieval and the object view read this config. They never build URLs themselves.
- **Reading media.** Steps read media through one accessor that materialises the blob to a private per-job temp file (mode 0700 directory, removed when the job ends). `draft.Source` stays the human-readable origin and is **never used as a path to open**. This fixes shared-host-only processing and the meeting enqueuer's missing source.
- **Transport.** `ctxt capture <file>` uploads bytes, either as multipart or as a blob `PUT` followed by an enqueue that references the blob key. Binary is never JSON-string content. Text files keep today's path.
- **Deletion and redaction.** They remove the blob when no other object references it, and the redact-as-supersede flow in ADR-069 must purge derived nodes and chunks together with the media.
- **Frames.** Frames the `frames` provider selects are kept as blobs, so the later native-embedding phase can embed them without re-extraction.

### Summaries

Summaries are local-first and customisable, with a default fallback.

- **Terminology.** The repository has no concept named "ingestion strategy", so this plan does not introduce that term. "Strategy" names lateral discovery (`internal/lateral/strategies`) and per-profile search (`search_strategy`), neither of which is ingestion. Ingestion is customised per pipeline through `pipelines.overrides.<pipeline>` (`internal/config/config.go`), which already takes `providers.<role>`, `skip_steps` and `extra_steps`. Summaries are customised through that mechanism.
- **Design.**
  - A `summarizer` step backed by the `summarization` provider role.
  - Default: a local Ollama chat model with a built-in prompt per medium (meeting: decisions, action items, topics; video and long audio: an overview).
  - Customisation per pipeline:
    - `pipelines.overrides.<pipeline>.providers.summarization` sets the backend, model and prompt template;
    - `skip_steps: [summarizer]` turns summaries off;
    - cloud only by naming a cloud backend.
  - Fallback: without an override the default local backend and prompt apply. If no local chat model is available, the summary is an optional representation. The job keeps its transcript and records `summary_error`; nothing stands in for the summary.
- **Chat model is configuration, not code.** No chat model is baked into ctxt. The summarization (and `llm`) backend, endpoint and model are configuration values (`providers.summarization.*`, `providers.llm.*`) resolved local-first. An OpenAI-compatible backend pointed at a local or LAN endpoint (for example a larger model served on another machine the operator owns) counts as local; only a cloud endpoint needs the per-role opt-in. With nothing configured and no local chat model reachable, the summary is simply absent (an optional representation) and the job keeps its other representations.

### Chunking (accepted as proposed)

Single-chunk embedding cannot hold a transcript. Phase 1 therefore lands a chunker. The data model is ready: `(object_id, model_id, chunk_idx)` rows, and `Search` already collapses to the best chunk per object.

- **Chunk 0 is the object card:** title plus summary, caption or the head of the text, within the model's input limit. Dedup reads only chunk 0, so its behaviour is kept.
- **Chunks 1..n cover the representations in projection order.**
  - A chunk never mixes two representations or two frames.
  - Transcripts pack consecutive segments up to about 1,500–2,000 characters with one segment of overlap, and split only at segment boundaries. Pages and frames are split the same way.
- **Each chunk records** `representation`, its source node IDs, and its anchor span. This needs a nullable `meta` JSON column on the canonical `embeddings` rows, added by forward-only migrations on SQLite and Postgres. The per-model ANN tables are unchanged.
- **Long plain text chunks the same way.** That amends [ADR-046](../decisions/ADR-046-chunking-strategies-rag-not-adopted.md) ("dPKMS does not chunk documents"), which ADR-076 records.
- **Provider hygiene:**
  - truncation becomes rune-safe;
  - the provider moves to `/api/embed` with an explicit `truncate`;
  - the chunker's size limit is expressed in characters and checked against the model's context length (8192 tokens for arctic-embed2).
- **Gate:** the `hop.top/ben` recall suite from ADR-071 must show no regression on text objects before the chunker becomes default.

### Local defaults and what the owner installs

| Role | Default | Install |
|---|---|---|
| `vision` (captions) | Ollama `qwen2.5vl:7b` | `ollama pull qwen2.5vl:7b` (several GB; not pulled today) |
| `ocr` | tesseract | `brew install tesseract` (plus `tesseract-lang` for non-English) |
| `transcription` | whisper.cpp `whisper-cli`, model `large-v3-turbo` | `brew install whisper-cpp`, then download the ggml `large-v3-turbo` model; ctxt reads its path from `providers.transcription.model` |
| `frames` | the owner's package (pending); interim ffmpeg | ffmpeg already installed |
| `video` | ffmpeg | already installed |
| `diarization` | pyannote, local, opt-in | pipx `pyannote-audio` (installed), a one-time Hugging Face token for the gated model, and a working torchcodec (broken locally today) |
| `summarization`, `llm` | configured local chat model | point `providers.summarization.*` / `providers.llm.*` at a local backend (Ollama, or an OpenAI-compatible local/LAN server) |

`ctxt doctor` (and `ctxt setup`) report each role as available, missing (with the exact install command) or misconfigured.

### Failure behaviour: fail loudly, never embed placeholders

These rules extend the 2026-09-26 `doc.office` fix to every medium:

1. **An explicitly configured backend that is missing is a configuration error**, reported at registry build and by `doctor`. It never silently falls back to a stub, or to any other backend. Stub providers are reachable only from tests.
2. **A media pipeline whose required role is unavailable is not registered.** Capturing that type returns `422` with the pipeline, the missing role and the install hint, for example: "`image.default` needs `ocr` (install tesseract) and `vision` (pull `qwen2.5vl:7b`)".
3. **Provider steps in media pipelines are never pruned.** A build that would prune one fails instead, so raw bytes can never become embedding text.
4. **Required and optional representations are separate.**
   - A required representation that fails fails the job, for example the caption or OCR of an image, or the transcript of audio or video.
   - An optional one records `<representation>_error` in metadata, as `diarization_error` does today, and emits no text. Optional ones are speakers, `summary`, and individual frames beyond a minimum.
5. **Empty is not a failure.** An image without text has no `ocr` node, and audio without speech has no `transcript` node. Neither case emits a placeholder.
6. **Diarization is no longer a required provider** of `audio.transcribe`. The `video` capability is set from the factory.

### Local by default, cloud opt-in everywhere

Decision 8 applies to every role, media or not.

- **`auto` means local.** It resolves to a local backend or to "unavailable", never to a cloud backend.
- **A cloud backend needs its name in config** for that role (`providers.<role>.backend: anthropic`, for example), or in a per-pipeline override.
- **Nothing is written beside the user's files.** Per-job temp files live in a private directory and are removed.
- **Blobs stay where storage config points them.** That is the local store by default (see [Original retention](#original-retention-keep-all-configurable-store-and-cdn)).
- **One privacy test per role:** with every cloud API key set and no explicit cloud backend, the resolved backend is local, or the pipeline is unregistered.

#### Where the cloud-preferring `auto` changes land

| File | Today | Change |
|---|---|---|
| `internal/providers/factory.go`, `Vision()` | `auto` tries Ollama, then the first of `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, `OPENROUTER_API_KEY` | `auto` = local Ollama with the configured model, else unavailable |
| `internal/providers/factory.go`, `LLM()` | `auto` prefers `ANTHROPIC_API_KEY`, then `OPENAI_API_KEY`, then Ollama | `auto` = local Ollama; cloud only when named |
| `internal/providers/factory.go`, `must*` helpers | a named backend that is missing returns the stub | returns a configuration error |
| `internal/pipeline/builtins/capabilities.go`, `probeToolCapabilities` | sets the `vision` capability when any cloud API key is present | capability only from a resolved local backend, or an explicitly named cloud one |
| `internal/config/config.go`, provider defaults | `providers.vision.model` defaults to `llava`; the comment says `auto` "falls back to stub" | default `qwen2.5vl:7b`; new `frames`, `summarization` and `storage.blob.delivery` defaults; comment corrected |
| `internal/providers/llm_ollama.go` | an empty model silently becomes `llama3` | the model comes from config; empty is a configuration error reported by `doctor` |
| `cmd/ctxt/cmd/setup.go` | the wizard offers only OpenAI, Anthropic or skip | local Ollama is the first and default choice; cloud stays an explicit choice |

The embedding resolver (`internal/embeddings`) already defaults to local Ollama and needs no change.

## Gap list, in increments

| # | Increment | Delivers | Depends on |
|---|---|---|---|
| 1 | **Stop false success** | fail-loud rules 1–6; placeholders removed; exec and HTTP cassette harness with small real fixtures | — |
| 1b | **Local by default, swappable providers** | cloud-preferring `auto` removed in every role (see the table above); `frames` and `summarization` interfaces and config; frame sampling split out of `VideoProvider`; setup wizard offers local first | — |
| 2 | **Keep the original** | blob store wired from `storage.blob`; delivery (`direct`, `presigned`, `cdn`) config; binary-safe capture upload; media accessor with a private temp dir; artifact nodes with blob keys | ADR-076 |
| 3 | **Representations and chunks** | provenance keys on every media node; projection built from representation nodes (fixes the doubled transcript and the dropped flat sections); rune-safe `/api/embed`; multi-chunk embedding with chunk `meta`; ben recall gate | ADR-076 |
| 4 | **Images** | default image route produces `caption` (`qwen2.5vl:7b`) and `ocr` (tesseract); the vision step receives the original bytes; embedded images and image-only pages in PDF and DOCX | 1, 1b, 2, 3 |
| 5 | **Audio and meetings** | whisper.cpp (`large-v3-turbo`) and openai-whisper providers fixed; transcript nodes from segments; local pyannote, opt-in; `.vtt`/`.srt` sidecar import; summarizer step with local default and per-pipeline customisation; the meeting enqueuer sends blob and source | 1, 1b, 2, 3 |
| 6 | **Video** | transcript from the extracted audio; frames through the `frames` provider (interim ffmpeg scene detection within rate bounds); `frame_caption` and `frame_ocr` per frame; fake scene detector removed; `video.audio_only` selectable | 4, 5 |
| 6b | **Owner's frame-extraction package** | integrate the owner's package as the `frames` local default; details pending from the owner | 6 |
| 7 | **Operator surface and truth in docs** | `ctxt doctor` report for every role; install guide; story statuses corrected; `meeting-capture.md` marked as design until a recorder ships | 1, 1b |

The per-OS meeting recorder (ScreenCaptureKit, WASAPI, PipeWire) is ADR-069's own work and is not in this list. Phase 1 accepts meeting *recordings*, whether exported from Zoom, Meet or Teams or produced by any recorder, together with their transcript sidecars.

### Test gates

- **Real fixtures:** every provider is exercised against small real fixtures (a PNG with known text, a two-speaker WAV of a few seconds, a short MP4). CI replays xrr `exec` cassettes for tesseract, whisper, ffmpeg and pyannote, and HTTP cassettes for Ollama vision and chat.
- **Coverage test:** `embedding_coverage_test` asserts that the embedded text contains each expected representation's fixture text and **no bracketed placeholder and no file magic bytes**. It stops treating stub output as success.
- **Mutation-tested defects:** each defect in the survey gets a test that fails on today's code: video capability, vision bytes, the diarizer flag, `[]byte(path)` in `frame_ocr`, transcription of the extracted audio, the doubled transcript, byte truncation and edge direction.
- **Swappability:** each role has a contract test run against every backend, local and cloud (cloud through HTTP cassettes), so a new backend proves itself against the same tests.
- **Privacy:** see [Local by default, cloud opt-in everywhere](#local-by-default-cloud-opt-in-everywhere).
- **Recall:** `hop.top/ben` shows no regression on text objects before the chunker becomes default.

## Later phase: native multimodal embeddings (plan only)

**What it adds:**

- A second registered model that embeds pixels, and later audio, into its own per-model index. Candidates are SigLIP or CLIP class models, nomic-embed-vision (paired with nomic-embed-text), and jina-embeddings-v4.
- Image-to-image and text-to-image search, and fusion with the text leg by RRF, as [US-0061](../stories/search/US-0061-visual-similarity-search.md) describes.

**Constraints phase 1 must respect so this is not foreclosed:**

1. **Originals and selected frames are retrievable by blob key,** so a backfill can embed pixels without re-capturing.
2. **Chunks record their representation and source nodes.** A vision model's rows can then point at the same artifact or frame nodes as the text rows. Fusion joins on node IDs, not on text.
3. **The registry does not assume text.** Today every populating model embeds every object. Phase 2 adds an input modality to each registry entry (`text`, `image`, `audio`) so that an image model embeds only artifact and frame nodes. Phase 1 must not hard-code "every model gets `EmbeddingText`" anywhere except the embedding step's text path.
4. **Chunk indices are stable per model** and are not reused across modalities. A model's rows are replaced only by that model's `Put`.
5. **Dimensions come from the registry probe,** never from provider defaults (ADR-071 already requires this).
6. **Native embedding models follow the same principle:** a local default, cloud opt-in.

## Resolved decisions

The owner resolved these on 2026-09-26.

| # | Topic | Decision |
|---|---|---|
| 1 | Vision model | Ollama `qwen2.5vl:7b`, local, is the default; configurable via `providers.vision.model` |
| 2 | OCR | tesseract produces the `ocr` representation; the vision model produces captions only. OCR is a swappable provider selected by config |
| 3 | ASR | whisper.cpp (`whisper-cli`) with `large-v3-turbo` is the default |
| 4 | Video frames | scene detection with minimum and maximum frame rates. Frame extraction is a swappable `frames` provider; the owner's own package becomes its default (details pending); ffmpeg is the interim local default |
| 5 | Diarization | local by default (pyannote run locally), configurable, opt-in |
| 6 | Non-speech sound | not stored and not tracked in phase 1; the tagging work is dropped |
| 7 | Chunking | accepted as proposed: chunk size and overlap, summary card as chunk 0, `meta` column on `embeddings`; amends ADR-046 |
| 8 | Cloud | opt-in only; every role has a local default; `auto` never prefers cloud keys, for media and generally |
| 9 | Originals | keep them all; the storage and CDN target are configurable, with the local blob store as the default |
| 10 | Summaries | local-first and customisable per pipeline through `pipelines.overrides`, with a default local prompt and backend as the fallback |

### Pending from the owner

- **The frame-extraction package:** location, API, dependency route and how it expresses scene detection and rate bounds. Scheduled last; ffmpeg remains the interim local default until then.
