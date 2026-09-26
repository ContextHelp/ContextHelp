# ADR-076 – Media as Text Representations: Provenance, Originals and Chunks

> **Status:** Accepted
> **Date:** 2026-09-26
> **Author:** jadb
> **Applies to:** dPKMS, ctxt: provider roles, pipelines, projection, embedding step, blob storage
> **Supersedes:** None
> **Amends:** ADR-046 (long text is now chunked for embedding), ADR-071 §6 "Chunking: single chunk for now" (Amendment 2026-09-26)
> **References:** ADR-063 (graph-canonical knowledge object), ADR-069 (meeting capture), ADR-070 (pipeline versioning), ADR-071 (per-model embedding index), P-060 (blob storage)
> **Plan:** [`docs/plans/2026-09-27-multimodal-content.md`](../plans/2026-09-27-multimodal-content.md) holds the survey, the provider roles and their defaults, the increments and the resolved decisions.

---

## Context

ctxt must accept images, audio, video and meetings as well as text. The owner has decided that phase 1 turns every medium into text and embeds it with the text model in the registry (`snowflake-arctic-embed2`, 1024 dimensions, local Ollama). Native multimodal embeddings come later, as extra models in the per-model index.

The survey in the plan shows the pieces exist but do not compose:

- **Each media step invents its own output.**
  - OCR writes a section and overwrites RawContent.
  - Vision writes a section and a summary node.
  - The transcriber writes a root summary holding the whole transcript, plus segment nodes.
  - The video timeline writes flat sections that the graph-first projection never reads.
- **Nothing records which text came from where.** Search cannot say "matched the OCR of frame 7", and a later image model has nothing to join on.
- **The original media is lost.** Extraction overwrites it, and no pipeline stores it.
- **Embedding takes one chunk cut at 8192 bytes,** so long transcripts are mostly unindexed.
- **Missing providers and stub fallbacks can put placeholder text, or raw file bytes, into the index.**
- **Provider resolution quietly prefers the cloud.** `auto` for vision and for the LLM picks whichever cloud API key is set, so client media or its transcript can leave the machine without anyone choosing that.

These are cross-cutting. Every media pipeline, the provider factory, the projection, the embedding step and storage must agree, so the convention is decided here once.

---

## Decision

**Every media-to-text process is a swappable, config-selected provider with a working local default and opt-in cloud. Every non-text medium becomes typed text representations, stored as graph nodes carrying a provenance convention. Originals are kept as bytes in a configurable blob store. Representations are embedded in provenance-carrying chunks, and placeholder or undecoded content is never indexed.**

### 0. Swappable providers, local by default

- **Every media-to-text process is a provider role behind a Go interface:** OCR, captioning (vision), speech recognition, diarization, frame extraction, summarization, and any added later. Steps depend on the interface only.
- **Config selects the backend:** `providers.<role>.backend`, overridable per pipeline with `pipelines.overrides.<pipeline>.providers.<role>`.
- **Every role has a working local default.**
- **Cloud backends are opt-in only.** They run only when config names them for that role. `auto` resolves to local backends or to "unavailable", never to a cloud backend, and no code path decides what to run by looking at cloud API keys. This applies to all roles, including the general `llm` role.
- **A named backend that is unavailable is a configuration error.** It never falls back to a stub or to another backend.

### 1. Representations

- A representation is a graph node holding text derived from the original.
  - Summaries are `summary` nodes; every other representation is a `section` node.
- Each node carries `representation` in its metadata, from this closed set: `caption`, `ocr`, `transcript`, `frame_caption`, `frame_ocr`, `summary`, `extracted_text`.
  - A new kind needs an amendment to this ADR.
- Non-speech sound has no representation in phase 1 and is not tracked.

### 2. Provenance keys

Representation nodes use these metadata keys. The existing keys are reused, not renamed.

| Key | Meaning |
|---|---|
| `representation` | the kind (§1) |
| `start_ms`, `end_ms` | time span within the original (existing on transcript segments) |
| `speaker` | diarized speaker label (existing) |
| `page_number` | page within a document (existing) |
| `frame_index`, `frame_ms` | selected video frame and its timestamp |
| `image_index` | image embedded in a document |
| `provider`, `model` | the backend and model that produced the text |
| `confidence` | 0–1, when the producer reports one |
| `origin` | `sidecar` for imported subtitles; absent otherwise |

- Node ordinals per node type come from one allocator shared by all steps.

### 3. Originals

- **Every original is kept.** Before any step runs, the original is stored as bytes in the blob store, content-addressed by SHA-256. There is no size cap.
- **An `artifact` node represents the original,** with `blob_key`, `content_type`, `size_bytes`, `sha256` and `file_name`. Selected video frames are kept as blobs with artifact nodes of their own.
- **Each representation node has a `derives_from` edge from the representation to the artifact** it came from.
- **Storage and delivery targets are configurable.**
  - The store is `storage.blob` (local filesystem by default, S3-compatible opt-in).
  - How clients fetch an original is a delivery setting: served directly (default), by presigned URL, or through a configured CDN base URL.
  - The local blob store is the default. The no-op stub is not a production default.
- **Steps read media only through the blob store.** `Source` is the human-readable origin and is never opened as a path.

### 4. Chunks

- The embedding step embeds chunks, not one projection string.
- **Chunk 0 is the object card** (title with summary, caption or head of text), which keeps dedup's chunk-0 contract.
- **Chunks 1..n follow the representations** in projection order.
  - A chunk never mixes representations or frames.
  - It splits only at segment, page or frame boundaries.
- **Each chunk row stores a `meta` JSON column:** `representation`, source node IDs, and anchor span.
- This applies to long plain text as well. It amends ADR-046 and replaces ADR-071 §6's single-chunk rule. The row key `(object_id, model_id, chunk_idx)` and the per-model ANN tables do not change.

### 5. No placeholder or undecoded content in the index

- A representation that failed, or produced nothing, is absent. Nothing stands in for it.
- A media pipeline never embeds RawContent that is still the original's bytes.
- Stub providers are for tests only.
- Pipelines declare which representations are required (a failure fails the job) and which are optional (a failure is recorded as `<representation>_error` in metadata).

---

## Rationale

- **Swappable providers keep the model choices reversible.** OCR engines, vision models, ASR models and frame extractors improve quickly. The owner is also bringing his own frame-extraction package. An interface per role lets each be replaced by config, and one contract test per role can hold every backend to the same behaviour.
- **Local-only `auto` makes the privacy property structural.** Today a cloud key set for an unrelated reason silently routes client media to that vendor. Making cloud a named choice per role removes that path entirely instead of documenting it.
- **Graph nodes are already canonical (ADR-063),** and the transcriber and vision steps already emit them. Putting provenance there, not in flat sections, gives one source for the projection, the document view and chunking.
- **A closed set of representation kinds lets retrieval, UI and evaluation reason about kinds,** for example "down-weight OCR noise" or "show the timestamp". Free-form labels would not.
- **Content-addressed originals give three things at once.** They are the only way to re-process with a better model, to add a native image model later without re-capture, and to fix processing that only works when CLI and daemon share a host.
- **Chunk `meta` costs one nullable column and no index change.** Without it, provenance would have to be recomputed from the projection, which drifts whenever the projection or chunker changes (ADR-070).
- **The "no placeholder" rule generalises the 2026-09-26 `doc.office` fix.** Placeholder vectors are worse than no vectors: they match each other and pollute near-duplicate detection.

### Alternatives considered

- **Keep `auto` as "best available, cloud included".** Rejected by the owner: cloud must be an explicit choice for every role.
- **Keep the single chunk and raise the cut.** The model's context is 8192 tokens, and a one-hour transcript exceeds it. One vector for a whole meeting also retrieves poorly.
- **One knowledge object per representation (caption object, OCR object, transcript object) linked by edges.** This multiplies objects, breaks "one capture = one object" for tags, profiles and dedup, and moves provenance into cross-object edges (ADR-049) meant for knowledge relations.
- **Provenance in flat `Section.Metadata` only.** The graph-first projection ignores flat sections whenever graph nodes exist, which is why video frame OCR is lost today.
- **Store only a path to the original.** It breaks on remote daemons and on moved or deleted files, and makes a later native embedding backfill impossible.
- **Cap stored originals by size.** Rejected by the owner: keep them all, and make the storage and delivery target configurable instead.

---

## Consequences

### Positive

- Images, audio, video and meetings become searchable through one text index, and each hit can name its representation and time or page.
- Every model and tool choice can be swapped by config, and a new backend proves itself against the role's contract tests.
- Client media stays on the machine unless the operator names a cloud backend.
- Re-processing with better models, and adding native multimodal models, needs no re-capture.
- Placeholder text and undecoded bytes cannot reach the index by construction.

### Negative

- **Behaviour change for cloud users.** Installs that relied on an API key being picked up by `auto` (vision, LLM) must now name the cloud backend explicitly.
- **Local setup.** Each role's local default must be installed and pulled (a vision model, whisper.cpp with its model, tesseract, a chat model), which `ctxt doctor` must make easy to see.
- **Disk.** All originals are stored, and video is large. Operators who cannot hold them locally point `storage.blob` at S3-compatible storage.
- **Migration.** A forward-only migration on both engines adds `embeddings.meta`, and objects re-embed under the chunker (an ADR-070 `reindex_auto`).
- **Latency.** Media ingest is slower: vision and ASR models run locally.
- **Edge direction.** The two existing `derives_from` edges point the wrong way and must be flipped. Any reader of those edges changes with them.

### Must not foreclose (later phase)

- Registry entries gain an input modality before any non-text model is registered. Until then, every model embeds text chunks.
- Chunk indices are stable per model. An image model's rows reference artifact and frame nodes through `meta`, never through text.
- Native embedding models follow §0: a local default, cloud opt-in.

---

## Implementation Notes

- The provider roles, their local defaults, the files where cloud-preferring `auto` is removed, the increments and the test gates are in the plan.
- The frame-extraction provider's default is the owner's own package. Its integration details are pending; ffmpeg serves as the interim local default behind the same interface.
- Every defect listed in the plan's survey gets a regression test that fails on the code at `2b57f39`.

## References

- [ADR-046 – Advanced Chunking Strategies for RAG: Not Adopted](ADR-046-chunking-strategies-rag-not-adopted.md)
- [ADR-063 – Graph-Canonical Knowledge Object](ADR-063-graph-canonical-knowledge-object.md)
- [ADR-069 – Meeting Capture Source](ADR-069-meeting-capture-source.md)
- [ADR-070 – Pipeline and Index Versioning](ADR-070-pipeline-and-index-versioning.md)
- [ADR-071 – Embedding Index Versioning](ADR-071-embedding-index-versioning.md)
- [P-060 – S3-Compatible Blob Storage](../plans/P-060-s3-media-storage.md)
