# ADR-076 – Media as Text Representations: Provenance, Originals and Chunks

> **Status:** Proposed
> **Date:** 2026-09-27
> **Author:** jadb
> **Applies to:** dPKMS, ctxt: pipelines, projection, embedding step, blob storage
> **Supersedes:** None
> **Amends:** ADR-046 (long text is now chunked for embedding), ADR-071 §6 "Chunking: single chunk for now" (Amendment 2026-09-26)
> **References:** ADR-063 (graph-canonical knowledge object), ADR-069 (meeting capture), ADR-070 (pipeline versioning), ADR-071 (per-model embedding index), P-060 (blob storage)
> **Plan:** [`docs/plans/2026-09-27-multimodal-content.md`](../plans/2026-09-27-multimodal-content.md) holds the survey, the increments and the open decisions.

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

These are cross-cutting. Every media pipeline, the projection, the embedding step and storage must agree, so the convention is decided here once.

---

## Decision

**We will represent every non-text medium as typed text representations, stored as graph nodes carrying a provenance convention; keep the original bytes in the blob store; embed representations in provenance-carrying chunks; and never index placeholder or undecoded content.**

### 1. Representations

- A representation is a graph node holding text derived from the original.
  - Summaries are `summary` nodes; every other representation is a `section` node.
- Each node carries `representation` in its metadata, from this closed set: `caption`, `ocr`, `transcript`, `frame_caption`, `frame_ocr`, `summary`, `sound_description`, `extracted_text`.
  - A new kind needs an amendment to this ADR.

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

- Before any step runs, the original is stored as bytes in the blob store, content-addressed by SHA-256.
- An `artifact` node represents it, with `blob_key`, `content_type`, `size_bytes`, `sha256` and `file_name`. Selected video frames may have artifact nodes of their own.
- Each representation node has a `derives_from` edge **from the representation to the artifact** it came from.
- Steps read media only through the blob store. `Source` is the human-readable origin and is never opened as a path.
- The local filesystem store is the default; the no-op stub is not a production default.

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

### 6. Local by default

- Media roles resolve to local backends unless the operator opts a role into a cloud backend explicitly: OCR, vision, transcription, diarization and video, plus LLM steps that read media-derived text.

---

## Rationale

- **Graph nodes are already canonical (ADR-063),** and the transcriber and vision steps already emit them. Putting provenance there, not in flat sections, gives one source for the projection, the document view and chunking.
- **A closed set of representation kinds lets retrieval, UI and evaluation reason about kinds,** for example "down-weight OCR noise" or "show the timestamp". Free-form labels would not.
- **Content-addressed originals give three things at once.** They are the only way to re-process with a better model, to add a native image model later without re-capture, and to fix processing that only works when CLI and daemon share a host.
- **Chunk `meta` costs one nullable column and no index change.** Without it, provenance would have to be recomputed from the projection, which drifts whenever the projection or chunker changes (ADR-070).
- **The "no placeholder" rule generalises the 2026-09-26 `doc.office` fix.** Placeholder vectors are worse than no vectors: they match each other and pollute near-duplicate detection.

### Alternatives considered

- **Keep the single chunk and raise the cut.** The model's context is 8192 tokens, and a one-hour transcript exceeds it. One vector for a whole meeting also retrieves poorly.
- **One knowledge object per representation (caption object, OCR object, transcript object) linked by edges.** This multiplies objects, breaks "one capture = one object" for tags, profiles and dedup, and moves provenance into cross-object edges (ADR-049) meant for knowledge relations.
- **Provenance in flat `Section.Metadata` only.** The graph-first projection ignores flat sections whenever graph nodes exist, which is why video frame OCR is lost today.
- **Store only a path to the original.** It breaks on remote daemons and on moved or deleted files, and makes a later native embedding backfill impossible.

---

## Consequences

### Positive

- Images, audio, video and meetings become searchable through one text index, and each hit can name its representation and time or page.
- Re-processing with better models, and adding native multimodal models, needs no re-capture.
- Placeholder text and undecoded bytes cannot reach the index by construction.

### Negative

- **Disk.** Originals are stored, and video is large, so a retention or size policy is needed.
- **Migration.** A forward-only migration on both engines adds `embeddings.meta`, and objects re-embed under the chunker (an ADR-070 `reindex_auto`).
- **Latency.** Media ingest is slower: vision and ASR models run locally.
- **Edge direction.** The two existing `derives_from` edges point the wrong way and must be flipped. Any reader of those edges changes with them.

### Must not foreclose (later phase)

- Registry entries gain an input modality before any non-text model is registered. Until then, every model embeds text chunks.
- Chunk indices are stable per model. An image model's rows reference artifact and frame nodes through `meta`, never through text.

---

## Implementation Notes

- The increments, test gates and install instructions are in the plan.
- Every defect listed in the plan's survey gets a regression test that fails on the code at `2b57f39`.

## References

- [ADR-046 – Advanced Chunking Strategies for RAG: Not Adopted](ADR-046-chunking-strategies-rag-not-adopted.md)
- [ADR-063 – Graph-Canonical Knowledge Object](ADR-063-graph-canonical-knowledge-object.md)
- [ADR-069 – Meeting Capture Source](ADR-069-meeting-capture-source.md)
- [ADR-070 – Pipeline and Index Versioning](ADR-070-pipeline-and-index-versioning.md)
- [ADR-071 – Embedding Index Versioning](ADR-071-embedding-index-versioning.md)
- [P-060 – S3-Compatible Blob Storage](../plans/P-060-s3-media-storage.md)
