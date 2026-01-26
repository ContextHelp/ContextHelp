# ADR-025 – Multilingual Support Strategy (I18N/L10N)

> **Status:** Accepted
> **Date:** 2026-01-26
> **Author:** @jadb
> **Applies to:** dPKMS + ctxt
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context and Problem Statement

The **Polyglot Principle** states: "Speaks every language of knowledge and communication. Notes, tasks, code, media, research, and mixed Arabic/English/French stay usable and linkable."

The **Language-Native Principle** states: "Multilingual knowledge is first-class. Entities, labels, and indexing support mixed languages without hacks or loss."

Users capture and query knowledge in multiple languages, often mixing languages within a single piece of content (code comments in Arabic, documentation in French, references in English). The system must:

1. **Detect** language(s) in input content automatically
2. **Index** multilingual content for search and retrieval
3. **Store** translations without destroying original content
4. **Sync** translations across federated registries
5. **Query** across language boundaries seamlessly
6. **Display** localized UI elements and labels

### Key Questions

- How are translations stored alongside original content?
- Where does language detection happen in the pipeline?
- How do translations sync across federated registries?
- How does semantic search handle multilingual queries?
- How are entity labels, tag labels, and UI elements localized?
- What plugins are responsible for translation services?

---

## Decision Drivers

- **Polyglot Principle:** Mixed-language content must remain usable and linkable
- **Language-Native Principle:** No hacks, no loss of information across languages
- **Federated Knowledge:** Translations must sync across registries cleanly
- **Plugin Extensibility:** Translation providers must be pluggable
- **Performance:** Language detection and translation must not block ingestion
- **Determinism:** Same input and config must produce same language metadata

---

## Considered Options

### Option 1: External Translation with Original Preservation

**Approach:**
- Store original content unchanged in canonical field
- Store translations in separate `translations` map keyed by language code (ISO 639-1)
- Plugins detect language(s) during pipeline execution
- Plugins optionally call translation APIs (cached)
- Registry sync includes translations as part of knowledge object schema

**Pros:**
- Original content never modified
- Translations are first-class metadata, not side effects
- Clean federation (translations sync like any other field)
- Easy to add/remove translations without affecting original
- Plugin-driven translation enables multiple providers

**Cons:**
- Requires clear contract for translation storage format
- Increases storage size when translations are enabled
- Must handle partial translations (some fields translated, not all)

### Option 2: Inline Language Tags with Content Segmentation

**Approach:**
- Detect language boundaries within content
- Tag each segment with language metadata inline
- Store as structured segments with language attributes
- Search indexes each segment separately

**Pros:**
- Handles fine-grained mixed-language content
- No duplication of content

**Cons:**
- Complex segmentation logic
- Breaks atomicity of knowledge objects
- Federation becomes complex (segment-level sync)
- Hard to present to user (which segments to show?)

### Option 3: Translation at Query Time

**Approach:**
- Store only original content
- Translate on-the-fly during search or display
- Cache translations per-session

**Pros:**
- Minimal storage overhead
- Always uses latest translation models

**Cons:**
- High latency for every query
- Non-deterministic (translations change over time)
- Cannot federate translations (every node translates independently)
- Expensive API costs for repeated translations

---

## Decision Outcome

**Chosen Option:** Option 1 — External Translation with Original Preservation

**Rationale:**

This approach aligns with all architectural principles:

- **Language-Native:** Translations are first-class fields, not hacks
- **Polyglot:** Original content preserved, translations are additive
- **Federated:** Translations sync cleanly across registries
- **Deterministic:** Translation is part of pipeline, cached, reproducible
- **Plugin-Extensible:** Multiple translation providers (OpenAI, DeepL, local models) can be swapped
- **Performance:** Translation happens asynchronously in pipeline, cached aggressively

---

## Implementation Details

### Language Detection (Inference Layer)

Language detection occurs during the **Inference Layer** before pipeline selection:

```
Input → Language Detection → Pipeline Selection → Enrichment
```

**Language Detection Step:**
- Detects primary language (`language_primary`) and secondary languages (`language_secondary[]`)
- Uses plugin-provided language detection models (e.g., `langdetect`, `fastText`, LLM-based)
- Outputs language metadata passed to pipeline context
- Cached per content hash

**Example Language Metadata:**
```json
{
  "language_primary": "en",
  "language_secondary": ["ar", "fr"],
  "language_confidence": 0.92,
  "language_mixed": true
}
```

### Translation Storage Schema

Translations stored in dedicated `translations` map within knowledge object:

```json
{
  "id": "obj-abc123",
  "type": "text",
  "summary": "Building microservices with Go...",
  "language_primary": "en",
  "translations": {
    "fr": {
      "summary": "Construire des microservices avec Go...",
      "tags": [
        { "label": "Architecture" }
      ],
      "sections": [
        { "title": "Introduction", "text": "..." }
      ]
    },
    "ar": {
      "summary": "بناء خدمات صغيرة باستخدام Go...",
      "tags": [
        { "label": "هندسة معمارية" }
      ]
    }
  }
}
```

**Translatable Fields:**
- `summary`
- `sections[].title`
- `sections[].text`
- `tags[].label`
- `decisions[].text`
- `tasks[].text`

**Non-Translatable Fields:**
- `id`, `content_hash`, `source_url`, `timestamps`
- `mentions[]` (entity slugs are language-neutral)
- `edges[]` (graph structure is language-neutral)

### User Configuration

```yaml
i18n:
  enabled: true
  preferredLanguages: ["fr", "en", "ar"]  # Priority order
  autoTranslate: true                      # Auto-translate during ingestion
  translateTags: true                      # Translate tag labels
  translateSections: true                  # Translate section text
  skipOnAnalyzeDefault: false              # Skip translation by default (override with --translate)
```

### Plugin Responsibilities

**Language Detection Plugin:**
- Implements `LanguageDetector` interface
- Returns language codes and confidence scores
- Example plugins: `langdetect`, `fasttext-langdetect`, `llm-langdetect`

**Translation Plugin:**
- Implements `Translator` interface
- Accepts source language, target language(s), field content
- Returns translations with metadata
- Example plugins: `openai-translator`, `deepl-translator`, `local-mt-translator`

**Plugin SDK Interface:**
```go
type LanguageDetector interface {
    Detect(ctx context.Context, text string) (LanguageResult, error)
}

type LanguageResult struct {
    Primary    string   // ISO 639-1 code
    Secondary  []string // Additional detected languages
    Confidence float64
    Mixed      bool
}

type Translator interface {
    Translate(ctx context.Context, req TranslationRequest) (TranslationResponse, error)
}

type TranslationRequest struct {
    SourceLang   string
    TargetLangs  []string
    Fields       map[string]string // field_name -> content
}

type TranslationResponse struct {
    Translations map[string]map[string]string // target_lang -> (field_name -> translated_content)
}
```

### Pipeline Integration

Translation step added to enrichment pipelines when `i18n.enabled=true`:

```
text.long Pipeline:
  1. ExtractMetadata
  2. DetectLanguage       ← Language detection
  3. GenerateSummary
  4. ExtractSections
  5. ExtractMentions
  6. ExtractTags
  7. TranslateFields      ← Translation step (optional, cached)
  8. AssembleObject
```

**Translation Step Behavior:**
- Reads `preferredLanguages` from config
- Checks translation cache (keyed by `content_hash + target_lang`)
- Calls translation plugin for missing translations
- Stores translations in `translations` map
- Marks object with `translations_available: ["fr", "ar"]`

### Registry Federation

**Sync Protocol:**
- Translations are part of knowledge object schema
- Synced like any other field during registry pull/push
- No special handling required
- Local registry can override translations (local takes precedence)

**Entity Translation:**
Entities have localized labels:

```json
{
  "slug": "ui.best-practice",
  "canonical_name": "UI Best Practice",
  "labels": {
    "en": "UI Best Practice",
    "fr": "Bonne pratique UI",
    "ar": "أفضل ممارسات واجهة المستخدم"
  }
}
```

### Search and Query Integration

**Query Language:**
Users can query in any language, system translates query for cross-language search:

```bash
# Query in French, matches English content
ctxt search "architecture microservices"

# Query in Arabic, matches English content
ctxt search "هندسة معمارية"
```

**Implementation:**
- Query term translated to `preferredLanguages` before embedding
- Semantic search (ADR-022) uses multilingual embedding models (e.g., `multilingual-e5`)
- Keyword search matches across original + all translations

**Tag Filtering:**
Tag labels translated for display, but slug remains language-neutral:

```bash
# User sees "Architecture" in French UI
ctxt list --tag architecture  # Slug is language-neutral
```

### UI Localization

**UI Element Translation:**
- CLI/TUI/Web UI elements stored in localization files (not in dPKMS)
- Standard i18n approach (e.g., `en.json`, `fr.json`, `ar.json`)
- Separate from knowledge content translation

---

## Consequences

### Positive

- **Original Content Preserved:** Translations never modify source content
- **Federation-Ready:** Translations sync cleanly across registries
- **Plugin-Driven:** Multiple translation providers supported
- **Cached:** Translations reused across queries and syncs
- **Search Across Languages:** Users can query in any language, find results in any language
- **Incremental:** Users can enable/disable translation per profile or globally

### Negative

- **Storage Overhead:** Translations increase storage size (mitigated by optional translation)
- **API Costs:** Translation APIs may be expensive (mitigated by caching and optional translation)
- **Partial Translations:** Not all fields may be translated (handled gracefully by falling back to original)
- **Translation Quality:** Depends on plugin provider quality

### Neutral

- **Language Detection Accuracy:** Depends on plugin quality, but detectable language metadata helps users validate

---

## Compliance

**Must Have for v1.0:**
- [ ] Language detection in Inference Layer
- [ ] Translation storage schema in knowledge object
- [ ] User configuration for i18n preferences
- [ ] Plugin SDK interfaces for `LanguageDetector` and `Translator`
- [ ] Translation step in pipelines (optional)
- [ ] Registry sync includes translations

**Should Have for v1.0:**
- [ ] Default language detection plugin (`langdetect`)
- [ ] Default translation plugin (`openai-translator`)
- [ ] Multilingual embedding model support (ADR-022)
- [ ] CLI flag `--translate` for explicit translation

**May Have for v2.0:**
- [ ] Local translation models (offline translation)
- [ ] Translation quality scoring
- [ ] User translation corrections (override plugin translations)

---

## Notes

**Design References:**
- `docs/design.md:634-683` (i18n-and-l10n.md)
- `docs/architecture.md:115-116` (Language-Native principle)
- `docs/architecture.md:134-135` (Polyglot principle)

**Related User Stories:**
- Users capturing mixed-language notes (Arabic/English/French)
- Researchers querying in their native language across multilingual knowledge
- Federated registries sharing knowledge across language boundaries

**Open Questions:**
- Should entity labels be translated automatically or manually curated?
- Should translation be triggered on-demand via CLI flag or automatically in background?
- Should we support translation chains (e.g., ar → en → fr)?

**Future Considerations:**
- OCR and ASR pipelines may use language-specific models (e.g., Arabic OCR, French transcription)
- Multilingual embeddings may benefit from language hints during vectorization
- Translation memory (reuse translations across similar content)
