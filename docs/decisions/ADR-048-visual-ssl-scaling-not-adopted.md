# ADR-048 – Visual SSL Scaling: Informative, Not Directly Adopted

> **Status:** Accepted
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **Superseded by:** None
>
> **Reference:** [Scaling Language-Free Visual Representation Learning](https://arxiv.org/abs/2504.01017) (arXiv:2504.01017, Apr 2025 — Fan, Tong et al., Meta FAIR / NYU)

---

## Context

This paper (Fan et al., 2025) demonstrates that pure visual self-supervised learning (SSL) — without language supervision — matches CLIP-level performance when trained at scale:

- Web-DINO ViT-7B: 53.9% avg VQA (vs. MetaCLIP 54.8%, SigLIP 55.4%)
- Log-linear scaling to 7B parameters; CLIP saturates beyond 3B
- 86.0% ImageNet-1k linear probing
- Text-filtered data: +13.6% OCR/Chart improvement
- At 518px: 59.9% avg VQA (within 0.1% of SigLIP 60.0%)

dPKMS + ctxt designs for multimodal enrichment as a core capability, with five visual pipelines:

- `image.ocr` — OCR text extraction (US-0003)
- `image.landing` — Screenshot analysis, UI pattern detection
- `image.diagram` — Diagram understanding, entity extraction
- `video.transcription` — Frame sampling + audio transcription (US-0005)
- `video.analysis` — Scene detection, visual understanding

Additionally, the embeddings schema already supports multi-model embeddings per knowledge object (`model TEXT NOT NULL`), and US-0051 designs for vector similarity search.

The question: **Does this paper inform how dPKMS should handle visual embeddings for image similarity search and first-class image support?**

---

## Decision

**Not directly adopted** (we don't train or select vision encoders), but the paper's findings are **informative for two design decisions** that affect how images become first-class knowledge objects:

1. **Visual embedding strategy** — image knowledge objects should carry visual embeddings alongside text embeddings
2. **Model selection criteria** — when choosing embedding providers for images, vision-only SSL models scale better and should be preferred when available

---

## Analysis: Two Roles Vision Models Play in dPKMS

### Role 1: Image → Text Description (VLM — Already Planned)

Most visual pipelines need a VLM that generates text:

```
image.landing:  Screenshot → "Dashboard showing revenue chart with Q3 spike..."
image.diagram:  Architecture diagram → entities: [API Gateway, Auth Service, DB]
image.ocr:      Whiteboard photo → extracted text content
```

This paper does NOT help here. Vision-only SSL produces embeddings, not text. These pipelines will use VLM APIs (Claude, GPT-4V, Gemini, Llava) that bundle vision encoders with language models internally.

### Role 2: Image → Visual Embedding (Similarity Search — Gap Identified)

A user captures a whiteboard photo. Later they want to find "images that look like this" — similar diagrams, related screenshots, visually connected content.

```
Current design:
  Image → VLM generates text description → text embedding → text similarity search
  Problem: "find similar images" becomes "find similar text descriptions of images"
  Loss: Visual similarity ≠ textual similarity of descriptions

With visual embeddings:
  Image → Vision encoder produces visual embedding → stored alongside text embedding
  Query: User provides image → visual embedding → cosine similarity against all image embeddings
  Result: Visually similar images returned, regardless of how their descriptions were worded
```

This is where the paper is directly relevant:
- **Vision-only SSL (Web-DINO) produces high-quality visual embeddings** that capture visual structure, layout, color, and composition
- **These embeddings scale better than CLIP** — at 7B parameters, they match or exceed CLIP without needing language alignment
- **Text-filtered training** dramatically improves document/chart/diagram understanding (+13.6%), exactly the kind of images knowledge workers capture

---

## What This Means for dPKMS Architecture

### The Embeddings Schema Already Supports This

```sql
CREATE TABLE embeddings (
    object_id TEXT NOT NULL,
    model TEXT NOT NULL,      -- ← distinguishes text vs. visual embeddings
    vector BLOB NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (object_id) REFERENCES objects(id) ON DELETE CASCADE
);
```

An image knowledge object can carry multiple embeddings:

| object_id | model | vector | purpose |
|-----------|-------|--------|---------|
| img-001 | `text-embedding-3-small` | [0.12, -0.34, ...] | Search by text description |
| img-001 | `dinov2-vitl14` | [0.56, 0.78, ...] | Search by visual similarity |

No schema changes needed. The pluggable embedding backend (ADR-021) already accommodates this.

### Image Enrichment Pipeline (Enhanced)

```
Image captured by user
  │
  ├─ VLM API call (existing design):
  │   ├─ Generate text description
  │   ├─ Extract entities, mentions
  │   ├─ OCR text content
  │   └─ Produce text embedding of description
  │
  ├─ Visual embedding (new, informed by this paper):
  │   ├─ Produce visual embedding via vision encoder API
  │   ├─ Store as separate embedding row (model = "dinov2" or equivalent)
  │   └─ Enables: "find images that look like this"
  │
  └─ Knowledge object created:
      ├─ raw_content: VLM description + OCR text
      ├─ sections, entities, mentions, tags
      ├─ embeddings[0]: text embedding (for text-based search)
      ├─ embeddings[1]: visual embedding (for image similarity)
      └─ attachment: original image blob
```

### Search Enhancement

```
Current (text-only similarity):
  ctxt search "architecture diagram" → text similarity against descriptions

Enhanced (dual-mode):
  ctxt search "architecture diagram" → text similarity (existing)
  ctxt search --similar-to image.png  → visual similarity against image embeddings
  ctxt search --similar-to obj-123    → visual similarity using object's stored image
```

---

## What We Don't Adopt

- ❌ Training vision encoders (we consume via API)
- ❌ Selecting between CLIP and SSL at the encoder level (that's the provider's decision)
- ❌ Any specific model (Web-DINO, DINOv2, etc.) — we stay provider-agnostic

## What We Take Forward

- ✅ **Design principle:** Image knowledge objects should carry both text and visual embeddings
- ✅ **Model selection criterion:** When visual embedding APIs become available, prefer models with strong document/chart understanding (paper shows text-filtered training helps +13.6%)
- ✅ **Scaling insight:** Vision-only SSL scales better than CLIP — future visual embedding services will likely use these architectures, meaning quality will improve over time
- ✅ **Architecture validation:** Our multi-model embeddings schema already supports this without changes

---

## Implementation Timeline

**No immediate action.** Visual embeddings become relevant when:

1. `image.*` pipelines are implemented (currently scaffolded as US-0003)
2. Visual embedding APIs are available from providers we already use
3. Image similarity search is prioritized as a feature

**When implemented:**
- Add visual embedding step to `image.*` pipeline recipes
- Add `--similar-to` flag to search CLI
- Vector search strategy gains a "visual" mode alongside "semantic" (text)

---

## Consequences

### Positive

- Establishes that images should carry dual embeddings (text + visual) — low cost, high future value
- Validates that the embeddings schema (multi-model per object) was well-designed
- Provides model selection guidance when visual embedding APIs mature

### Negative

- None. No architectural changes required.

### Neutral

- Visual embedding APIs are not yet widely available as simple API calls (unlike text embeddings). This will change as providers commoditize DINOv2/SigLIP-style models. Our pluggable backend is ready when they do.

---

## Related ADRs

- **ADR-021:** Multi-Backend Storage (pluggable embedding models — already supports multi-model)
- **ADR-035:** Docling (document parsing; processes document images)
- **US-0003:** Image OCR and Analysis (scaffolded; will be primary consumer)
- **US-0005:** Video Processing with Scenes (scaffolded; frame embeddings)
- **US-0051:** Semantic Search with Embeddings (vector similarity search)

---

## References

- [Scaling Language-Free Visual Representation Learning](https://arxiv.org/abs/2504.01017) (Fan et al., 2025)
- Related: ADR-021 (Multi-Backend), US-0003 (Image OCR), US-0005 (Video), US-0051 (Semantic Search)
