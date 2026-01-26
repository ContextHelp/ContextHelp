# Tag Schema

This document defines the **Tag** structure used inside ContextHelp.
Tags represent **AI-generated semantic labels** applied to enriched content.
They reflect the meaning, polarity, weight, and origin of interpretations made during pipeline execution.

Tags are validated—when possible—against **taxonomy registries** that the user or agent profile has subscribed to.

---

# Overview

A **tag** is a structured data object representing a semantic label assigned to a bookmark.
Tags encode:

- **What** the AI thinks the content represents
- **How strongly** it represents it (`weight`)
- **The attitude** or classification direction (`polarity`)
- **Why** it was chosen (influencing hints or signals)
- **Which registry** validated or mapped it (optional)

Tags appear inside the `tags[]` array of a bookmark.

---

# JSON Structure

The canonical structure:

```json
{
  "label": "ui.hero.antipattern",
  "weight": 0.92,
  "polarity": "negative",
  "influencedBy": ["#ui", "#bad", "#landingpage"],
  "source": "pipeline:image.landing",
  "registry": "global-default",
  "confidence": 0.87
}
```

---

# Field Definitions

### `label` *(string, required)*
The normalized tag identifier.
Must follow:

- lowercase
- dot-notation hierarchy
- registry taxonomy rules (if applicable)

Example:
- `growth.experiment.idea`
- `ui.hero.antipattern`
- `code.security.vulnerability`

If the label is not found in any subscribed taxonomy registry, it remains valid but is marked as **unregistered**.

---

### `weight` *(number, required)*
A floating-point value between `0.0` and `1.0` indicating tag strength.

Interpretation guidelines:

- **0.00–0.29** → weak signal
- **0.30–0.59** → moderate relevance
- **0.60–0.89** → strong match
- **0.90–1.00** → extremely strong / defining characteristic

Weight merging uses:

- Hint influence
- Pipeline evidence
- Registry-provided priors (optional)
- Model confidence

---

### `polarity` *(string, optional; default `"neutral"`)*

Specifies whether the tag describes:

- `"positive"` → desirable trait
- `"negative"` → flaw, anti-pattern, critique
- `"neutral"` → factual classification

Used primarily for:

- UX/UI critique
- Code reviews
- Quality assessment pipelines
- Sentiment-aligned knowledge indexing

---

### `influencedBy` *(array<string>, optional)*

List of **hints**, **features**, or **signals** that directly contributed to generating the tag.

Examples:
- `["#ui", "#bad", "#landingpage"]`
- `["#growth", "extract:topics", "registry:ux"]`

This enables **explainability**, **traceability**, and **agent reasoning** over why a classification was chosen.

---

### `source` *(string, required)*

Indicates which engine subsystem produced the tag.

Formats:

- `pipeline:text.short`
- `pipeline:url.generic`
- `model:gpt-4o`
- `registry:ux-critique`
- `merge:multi`

This is essential for debugging and reasoning.

---

### `registry` *(string, optional)*

If the tag was validated or derived from a taxonomy registry, this identifies the registry name.

Examples:

- `"global-default"`
- `"ux-critique"`
- `"growth-marketing"`
- `"org/team-registry"`

Tags may come from:

- Zero registries (AI-generated only)
- A single registry (taxonomy-aligned)
- Multiple registries (merged; rare)

---

### `confidence` *(number, optional)*

Model-level confidence score separate from `weight`.

Use-case:

- `weight` = relevance to content
- `confidence` = certainty of model classification

Example:

```json
{
  "label": "ux.layout.grid",
  "weight": 0.78,
  "confidence": 0.66
}
```

If omitted, pipelines treat confidence as equal to weight.

---

# Validation Rules

1. **Label normalization**
   - Lowercase
   - Dot-hierarchy structure
   - No whitespace

2. **Weight range**
   Must be `0.0 ≤ weight ≤ 1.0`.

3. **Allowed polarities**
   `positive`, `negative`, `neutral`.

4. **Registry mapping**
   If label appears in multiple taxonomy registries, the agent or pipeline configuration determines resolution priority.

5. **Redundant tags**
   Tags with identical `label` but different sources may be merged depending on pipeline rules.

---

# Tag Lifecycle

1. **Generation**
   Produced by a pipeline (LLM, model chain, heuristics).

2. **Normalization**
   - Label cleanup
   - Meaning alignment
   - Polarity derivation
   - Weight scaling

3. **Registry Alignment**
   - Mapping or validating labels
   - Applying priors
   - Checking allowed hierarchies

4. **Merge & Deduplicate**
   Across pipeline steps.

5. **Storage**
   Stored in the bookmark object.

6. **Consumption**
   Agents use tags to filter, sort, or weigh context.

---

# Example Tag Set

```json
[
  {
    "label": "ui.hero.antipattern",
    "weight": 0.92,
    "polarity": "negative",
    "influencedBy": ["#ui", "#bad", "#landingpage"],
    "source": "pipeline:image.landing",
    "registry": "ux-critique",
    "confidence": 0.89
  },
  {
    "label": "copywriting.clarity.issue",
    "weight": 0.77,
    "polarity": "negative",
    "influencedBy": ["ocr:text", "analysis:copy-cleanup"],
    "source": "pipeline:image.ocr",
    "registry": null,
    "confidence": 0.72
  },
  {
    "label": "topic.marketing",
    "weight": 0.64,
    "polarity": "neutral",
    "influencedBy": ["#growth"],
    "source": "pipeline:text.long",
    "registry": "global-default",
    "confidence": 0.80
  }
]
```

---

# Summary

The **Tag Schema** defines how ContextHelp transforms raw AI outputs and pipeline analytics into structured, validated semantic labels.
Tags enable:

- high-precision retrieval
- agent reasoning
- personalized knowledge graphs
- registry-aligned classification
- explainable enrichment pipelines

This schema is foundational across the entire ContextHelp platform.
