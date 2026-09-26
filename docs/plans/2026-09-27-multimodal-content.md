# Multimodal content: phase 1 as text, later native embeddings

> **Date:** 2026-09-27
> **Decision record:** [ADR-076 – Media as Text Representations: Provenance, Originals and Chunks](../decisions/ADR-076-media-text-representations.md) (Proposed)
> **Status:** design only. Nothing here is implemented.
> **Audience:** the owner deciding on ADR-076 and the open decisions below, then whoever implements the increments

ctxt must accept every content type: text, images, video, sound and speech, and meetings. The owner has set the approach:

- **Phase 1** represents every medium as text and embeds that text with the text model the registry already runs: `snowflake-arctic-embed2` (multilingual, 1024 dimensions) on local Ollama.
- **The later phase** adds native multimodal embeddings (CLIP or SigLIP class, nomic-embed-vision, jina-embeddings-v4). The per-model index from the [ADR-071 amendment](../decisions/ADR-071-embedding-index-versioning.md#amendment-2026-09-26) already lets models sit side by side. This plan only records what phase 1 must not foreclose.

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

| Type | Accepted | Text produced | Embedded | Original kept | Providers required | Status on this machine | Main gaps |
|---|---|---|---|---|---|---|---|
| Text | yes (`text.*`, `doc.markdown`, `doc.code`) | body | yes, one chunk | the text is the object | none | works | cut at 8192 bytes |
| Image | by extension; pipelines skipped | OCR only; caption only via `--pipeline image.analysis` | would be | no | tesseract; vision model for captions | **422**: tesseract missing, no vision model | captions not in the default route; vision reads OCR text; bytes corrupted over HTTP |
| Audio (speech) | yes | transcript, per-segment sections | yes, doubled, cut at 8192 bytes | no | whisper **and** pyannote | **job fails**: whisper flag mismatch | whisper.cpp not found (`whisper-cli`); diarization never runs yet is required; server-host path |
| Sound (non-speech) | as audio | nothing meaningful | — | no | — | — | no description of non-speech audio |
| Video | by extension; pipelines never register | would be transcript, fake scenes, broken frame OCR | would embed placeholders | no | ffmpeg, whisper, tesseract | **422 always** (video capability) | transcribes the wrong file, OCRs path strings, no frame captions, leaves temp files |
| Meeting | no recorder; the enqueuer targets `video.full` or `audio.transcribe` | as audio or video | — | no | as audio or video, plus OS capture APIs | not implemented | no summary, no speakers, no `source`, docs overstate |
| PDF | yes | per-page text | yes | no | pdftotext (golib placeholder otherwise) | works with pdftotext | scanned pages yield nothing; embedded images ignored |
| DOCX | yes | paragraphs | yes | no | none (pure Go) | works | embedded images ignored; `.odt` and `.epub` pending |

## Phase 1 design

### Representations per type

Every medium becomes one or more **representations**: text units, each with a kind and an anchor. They are stored as graph nodes (ADR-063 makes the graph canonical). Section nodes hold the representations; summary nodes hold summaries.

| Type | Representations (in projection order) | Anchor |
|---|---|---|
| Image | `caption` (vision model: what the image shows, including diagram structure), `ocr` (text in the image) | none |
| Audio, speech | `summary` (optional; local LLM, only above a length threshold), `transcript` (one node per ASR segment) | `start_ms`, `end_ms`, `speaker` when diarized |
| Audio, non-speech | `sound_description` (only if a sound-tagging backend is chosen), otherwise no text and `speech_detected: false` in metadata | whole file |
| Video | `summary`, `transcript` (from the extracted audio), `frame_caption` and `frame_ocr` for each selected frame | `start_ms`, `end_ms`; `frame_index`, `frame_ms` |
| Meeting | `summary` (decisions, action items, topics), `transcript` with `speaker`, plus the video representations when there is video | as audio and video |
| PDF, DOCX | `extracted_text` per page or section; `ocr` for image-only pages; `caption` and `ocr` for embedded images | `page_number`; `image_index` |
| Subtitle sidecar (`.vtt`, `.srt`) | `transcript` with `origin: sidecar` | cue times |

The vector for a representation is the vector of its **text**. There are no placeholders: a representation whose producer failed or returned nothing is absent, never "[…]".

### Provenance convention

ADR-076 decides this. In short:

- **Node metadata on every representation node:**
  - `representation`: one of `caption`, `ocr`, `transcript`, `frame_caption`, `frame_ocr`, `summary`, `sound_description`, `extracted_text`
  - the anchor keys, reusing the existing ones: `start_ms`, `end_ms`, `speaker`, `page_number`, plus the new `frame_index`, `frame_ms`, `image_index`
  - `provider` and `model` (for example `ollama` and `qwen2.5vl:7b`)
  - `confidence` when the producer reports one
  - `origin` (`sidecar` for imported subtitles)
- **One `artifact` node per original** (and per extracted frame worth keeping). Its metadata holds `blob_key`, `content_type`, `size_bytes`, `sha256` and `file_name`. Each representation node has a `derives_from` edge **from the representation to the artifact**. The two existing edges are flipped.
- **Unique node ordinals.** Several steps now emit section nodes, so the ordinal per node type comes from a shared allocator, not from each step's own counter.
- **Chunks carry provenance.** Each embedded chunk records its representation, the IDs of its source nodes, and its anchor span (see Chunking). Search can then say "matched the transcript at 12:03–12:41" or "matched the OCR of frame 7".

### Original retention

- The original is stored **as bytes** in the blob store, content-addressed by SHA-256, before any step runs.
- **Blob store:** wire the existing `blob.New` factory into dpkms and the CLI's direct paths. The local filesystem store is the default and S3 is opt-in ([P-060](P-060-s3-media-storage.md)). Remove the no-op stub from the production default.
- **Reading media:** steps read media through one accessor that materialises the blob to a private per-job temp file (mode 0700 directory, removed when the job ends). `draft.Source` stays the human-readable origin and is **never used as a path to open**. This fixes shared-host-only processing and the meeting enqueuer's missing source.
- **Transport:** `ctxt capture <file>` uploads bytes, either as multipart or as a blob `PUT` followed by an enqueue that references the blob key. Binary is never JSON-string content. Text files keep today's path.
- **Deletion and redaction:** they remove the blob when no other object references it, and the redact-as-supersede flow in ADR-069 must purge derived nodes and chunks together with the media.

### Chunking: phase 1 needs more than one chunk

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

Everything runs locally by default. Candidate defaults, pending the owner's decisions below:

| Role | Default | Install | Notes |
|---|---|---|---|
| Image and frame captions | Ollama vision model, `qwen2.5vl:7b` proposed | `ollama pull qwen2.5vl:7b` (several GB; not pulled today) | multilingual, strong on screenshots and documents; `llava` stays selectable |
| OCR | tesseract | `brew install tesseract` (plus `tesseract-lang` for non-English) | the VLM can also read text; see decisions |
| Speech to text | whisper.cpp `whisper-cli` with a multilingual model (for example `large-v3-turbo`) | `brew install whisper-cpp`, then download a ggml model; ctxt takes its path from config | the provider must look for `whisper-cli`, pass `--model` and read the JSON output file. openai-whisper stays supported through its own flags |
| Frames and audio extraction | ffmpeg | already installed | scene detection through ffmpeg's `select='gt(scene,T)'` filter; no extra dependency |
| Diarization | pyannote, opt-in | pipx `pyannote-audio`, a one-time Hugging Face token for the gated model, and a working torchcodec | ctxt must call `apply`, or a small bundled script, and parse RTTM |
| Summaries | Ollama chat model | `ollama pull` a 7–8B instruct model | not pulled today |

`ctxt doctor` (and `ctxt setup`) report each media role as available, missing (with the exact install command) or misconfigured.

### Failure behaviour: fail loudly, never embed placeholders

These rules extend the 2026-09-26 `doc.office` fix to every medium:

1. **An explicitly configured backend that is missing is a configuration error**, reported at registry build and by `doctor`. It never silently falls back to a stub. Stub providers are reachable only from tests.
2. **A media pipeline whose required role is unavailable is not registered.** Capturing that type returns `422` with the pipeline, the missing role and the install hint, for example: "`image.default` needs `ocr` (install tesseract) and `vision` (pull an Ollama vision model)".
3. **Provider steps in media pipelines are never pruned.** A build that would prune one fails instead, so raw bytes can never become embedding text.
4. **Required and optional representations are separate.**
   - A required representation that fails fails the job, for example the caption or OCR of an image, or the transcript of audio or video.
   - An optional one records `<representation>_error` in metadata, as `diarization_error` does today, and emits no text. Optional ones are speakers, `sound_description`, `summary`, and individual frames beyond a minimum.
5. **Empty is not a failure.** An image without text has no `ocr` node, and silent audio has no `transcript` node and `speech_detected: false`. Neither case emits a placeholder.
6. **Diarization is no longer a required provider** of `audio.transcribe`. The `video` capability is set from the factory.

### Privacy: client media stays on the machine

- **`auto` resolves media roles to local backends only:** OCR, vision, transcription, diarization, video. Today vision `auto` falls through to the first cloud API key found. Cloud backends for media roles need an explicit per-role opt-in in config.
- **LLM steps that read media-derived text stay local by default.** That covers the tagger, the sectioner and the new summariser. Today `llm: auto` prefers Anthropic or OpenAI keys whenever one is set, so a transcript would leave the machine. Whether to change `auto` for all content or only for media-derived content is an owner decision.
- **Temp files and blobs stay private.** Per-job temp files live in a private directory and are removed. Nothing is written beside the user's files. Blobs stay in the local store unless S3 is configured.
- **One privacy test per media role:** with cloud keys set and no opt-in, the resolved backend is local, or the pipeline is unregistered.

## Gap list, in increments

| # | Increment | Delivers | Depends on |
|---|---|---|---|
| 1 | **Stop false success** | fail-loud rules 1–6; placeholders removed; local-only `auto` for media roles; exec and HTTP cassette harness with small real fixtures | — |
| 2 | **Keep the original** | blob store wired; binary-safe capture upload; media accessor with a private temp dir; artifact nodes with blob keys | ADR-076 |
| 3 | **Representations and chunks** | provenance keys on every media node; projection built from representation nodes (fixes the doubled transcript and the dropped flat sections); rune-safe `/api/embed`; multi-chunk embedding with chunk `meta`; ben recall gate | ADR-076 |
| 4 | **Images** | default image route produces `caption` and `ocr`; the vision step receives the original bytes; configurable Ollama vision model; embedded images and image-only pages in PDF and DOCX | 1–3 |
| 5 | **Audio and meetings** | whisper.cpp and openai-whisper providers fixed; transcript nodes from segments; opt-in pyannote; `.vtt`/`.srt` sidecar import; meeting summary with a local LLM; the meeting enqueuer sends blob and source; non-speech handling | 1–3 |
| 6 | **Video** | transcript from the extracted audio; frames by scene detection within bounds; `frame_caption` and `frame_ocr` per frame; fake scene detector removed; `video.audio_only` selectable | 4, 5 |
| 7 | **Operator surface and truth in docs** | `ctxt doctor` media report; install guide; story statuses corrected; `meeting-capture.md` marked as design until a recorder ships | 1 |

The per-OS meeting recorder (ScreenCaptureKit, WASAPI, PipeWire) is ADR-069's own work and is not in this list. Phase 1 accepts meeting *recordings*, whether exported from Zoom, Meet or Teams or produced by any recorder, together with their transcript sidecars.

### Test gates

- **Real fixtures:** every provider is exercised against small real fixtures (a PNG with known text, a two-speaker WAV of a few seconds, a short MP4). CI replays xrr `exec` cassettes for tesseract, whisper, ffmpeg and pyannote, and HTTP cassettes for Ollama vision and chat.
- **Coverage test:** `embedding_coverage_test` asserts that the embedded text contains each expected representation's fixture text and **no bracketed placeholder and no file magic bytes**. It stops treating stub output as success.
- **Mutation-tested defects:** each defect in the survey gets a test that fails on today's code: video capability, vision bytes, the diarizer flag, `[]byte(path)` in `frame_ocr`, transcription of the extracted audio, the doubled transcript, byte truncation and edge direction.
- **Privacy test:** see the Privacy section.
- **Recall:** `hop.top/ben` shows no regression on text objects before the chunker becomes default.

## Later phase: native multimodal embeddings (plan only)

**What it adds:**

- A second registered model that embeds pixels, and later audio, into its own per-model index. Candidates are SigLIP or CLIP class models, nomic-embed-vision (paired with nomic-embed-text), and jina-embeddings-v4.
- Image-to-image and text-to-image search, and fusion with the text leg by RRF, as [US-0061](../stories/search/US-0061-visual-similarity-search.md) describes.

**Constraints phase 1 must respect so this is not foreclosed:**

1. **Originals are retrievable by blob key.** Selected video frames are kept as blobs, or can be re-extracted deterministically from the original at `frame_ms`, so a backfill can embed pixels without re-capturing.
2. **Chunks record their representation and source nodes.** A vision model's rows can then point at the same artifact or frame nodes as the text rows. Fusion joins on node IDs, not on text.
3. **The registry does not assume text.** Today every populating model embeds every object. Phase 2 adds an input modality to each registry entry (`text`, `image`, `audio`) so that an image model embeds only artifact and frame nodes. Phase 1 must not hard-code "every model gets `EmbeddingText`" anywhere except the embedding step's text path.
4. **Chunk indices are stable per model** and are not reused across modalities. A model's rows are replaced only by that model's `Put`.
5. **Dimensions come from the registry probe,** never from provider defaults (ADR-071 already requires this).

## Decisions the owner must make

1. **Vision model:** `qwen2.5vl:7b` (proposed), `llama3.2-vision:11b`, `gemma3` (4B or 12B), `minicpm-v`, or the small `moondream` for low-memory machines. It must be pulled before images work.
2. **OCR:** keep tesseract as the OCR representation, with the VLM producing only captions (proposed: deterministic, fast, separate provenance). Alternatively let the VLM also transcribe text (better on screenshots and handwriting, slower, can hallucinate), or run both.
3. **ASR:** whisper.cpp `whisper-cli` with `large-v3-turbo` (proposed: Metal-accelerated, multilingual), openai-whisper (installed, slower on CPU), or faster-whisper.
4. **Video frames:** ffmpeg scene detection with a floor and a ceiling, for example at least one frame per 60 s, at most one per 5 s and at most 120 per video, plus near-duplicate frame suppression (proposed). The alternative is fixed-interval sampling.
5. **Diarization:** opt-in pyannote (proposed), always on when installed, or deferred.
6. **Non-speech sound:** record only `speech_detected: false` in phase 1 (proposed), or add a local audio tagger (YAMNet or PANNs class) for `sound_description`.
7. **Chunking:** the chunk size and overlap, the chunk-0 card, and a `meta` column on `embeddings` (proposed) rather than recomputing chunk provenance from the projection. The ADR-046 amendment for long text.
8. **Cloud use:** whether `llm: auto` stops preferring cloud keys for all content or only for media-derived text, and the shape of the per-role cloud opt-in.
9. **Originals:** keep all originals (proposed), or cap by size (video is large) and keep only representations above the cap. Keep extracted frames as blobs, or re-extract them on demand.
10. **Summaries:** which local chat model writes meeting and video summaries, and the length threshold above which audio gets one.
