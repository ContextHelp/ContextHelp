# US-0210: Cross-Platform Entity Resolution

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Researchers & OSINT Analysts](../../personas/researchers-osint.md), [Agents/LLMs/Tools](../../personas/agents-llms-tools.md)

---

## User Goal

As a researcher, I want the system to automatically detect when artifacts captured from different platforms refer to the same real-world entity (person, company, project) so that I get a unified view without manual linking.

---

## Context

Real-world entities exist across many digital platforms simultaneously: a person has a LinkedIn profile, an X account, a GitHub presence, and arXiv publications. Each platform assigns its own identifiers, naming conventions, and metadata schemas, making it non-obvious that `@janedoe` on X, `jane-doe` on LinkedIn, and `jdoe` on GitHub are all the same person. Without automatic resolution, each platform identity lives as a separate entity in the knowledge graph, fragmenting what should be a unified understanding.

Cross-platform entity resolution addresses this by running matching heuristics whenever a new artifact is captured. The system checks whether the newly extracted entity matches any existing entities in the graph using a layered scoring approach: verified links (a URL in an X bio pointing to a GitHub profile) produce high-confidence matches, name plus organization overlap produces medium confidence, and name-only similarity produces low confidence. Only high and medium-confidence matches are auto-linked; low-confidence candidates are surfaced for manual review via `ctxt entity candidates`.

The resolution process creates `same_as` edges in the knowledge graph, preserving all platform-specific slugs while unifying them under a canonical entity. When entities are merged, all backlinks from both entities are consolidated, ensuring that searches and graph traversals return the complete picture. This design supports incremental resolution -- as more artifacts are captured, confidence scores can increase, and previously uncertain matches may be automatically confirmed.

---

## Acceptance Criteria

- [ ] System auto-detects when a newly captured entity matches an existing entity from a different platform
- [ ] Matching heuristics include: name similarity, linked URLs, email domain, bio keyword overlap
- [ ] Confidence scoring: high (verified link, >= 0.90), medium (name + org match, >= 0.75), low (name-only, >= 0.60)
- [ ] High and medium confidence matches are auto-linked with `same_as` edges
- [ ] Low confidence matches are surfaced as candidates for manual review
- [ ] `ctxt entity candidates @entity.slug` shows potential matches with confidence scores
- [ ] `ctxt entity merge @slug1 @slug2` manually confirms and merges two entities
- [ ] `ctxt entity graph @entity.slug` shows the full entity graph with all platform presences
- [ ] Resolution creates `same_as` edges in the knowledge graph between entity slugs
- [ ] Merged entities consolidate all backlinks from both source entities
- [ ] Supports entity types: people, companies/organizations, projects/repositories, academic papers
- [ ] Enrichment pipeline runs after every capture to check new entities against the existing graph
- [ ] De-duplication: when entities are merged, searches return consolidated results

---

## Implementation Notes

### CLI Interface

```bash
# Show potential matches for an entity
ctxt entity candidates @person.jane-doe
# ->
# Candidate                       Confidence  Matched On
# @person.jane-doe-linkedin       0.92        name + org + bio URL
# @person.jdoe-github             0.78        name + linked URL
# @person.j-doe-arxiv             0.65        name + affiliation

# Manually merge two entities
ctxt entity merge @person.jane-doe @person.jane-doe-linkedin
# -> Merged: 2 entities unified under @person.jane-doe
# -> 47 backlinks consolidated
# -> same_as edge created: @person.jane-doe <-> @person.jane-doe-linkedin

# Merge multiple entities at once
ctxt entity merge @person.jane-doe @person.jdoe-github @person.j-doe-arxiv
# -> Merged: 3 entities unified under @person.jane-doe
# -> 89 backlinks consolidated

# Show entity graph with all platform presences
ctxt entity graph @person.jane-doe
# ->
# @person.jane-doe (canonical)
#   |-- same_as -> @person.jane-doe-linkedin
#   |-- same_as -> @person.jdoe-github
#   |-- same_as -> @person.j-doe-arxiv
#   |
#   |-- X profile (o-x-prof-1a2b3c)
#   |-- LinkedIn profile (o-li-prof-4d5e6f)
#   |-- 12 GitHub repos (o-gh-repo-*)
#   |-- 3 arXiv papers (o-arxiv-*)
#   |-- 2 Wikipedia mentions (o-wiki-*)

# Show entity graph in JSON format
ctxt entity graph @person.jane-doe --format json

# Reject a candidate match (prevent future auto-linking)
ctxt entity reject @person.jane-doe @person.jane-doe-different
# -> Rejected: @person.jane-doe and @person.jane-doe-different marked as distinct entities

# List all unresolved candidates above a threshold
ctxt entity candidates --unresolved --min-confidence 0.60
# ->
# Entity                    Candidate                     Confidence
# @person.jane-doe          @person.j-doe-arxiv           0.65
# @org.acme-corp            @org.acme-corporation         0.72
# @project.mobile-app       @project.mobile-app-v2        0.68

# Show resolution history for an entity
ctxt entity history @person.jane-doe
# ->
# Date                  Action    Target                       Confidence  Method
# 2026-02-15T10:00:00Z  merged    @person.jane-doe-linkedin    0.92        auto
# 2026-02-16T14:30:00Z  merged    @person.jdoe-github          0.78        manual
# 2026-02-17T09:15:00Z  candidate @person.j-doe-arxiv          0.65        auto (pending)
```

### REST API

```
# Get candidates for an entity
GET /api/v1/entities/{entity_slug}/candidates

-> 200 OK
{
  "entity": "@person.jane-doe",
  "candidates": [
    {
      "slug": "@person.jane-doe-linkedin",
      "confidence": 0.92,
      "matched_on": ["name", "organization", "bio_url"],
      "details": {
        "name_similarity": 0.95,
        "org_match": true,
        "bio_url_match": "https://github.com/jdoe",
        "platform": "linkedin"
      },
      "status": "auto_merged"
    },
    {
      "slug": "@person.jdoe-github",
      "confidence": 0.78,
      "matched_on": ["name", "linked_url"],
      "details": {
        "name_similarity": 0.72,
        "linked_url_match": "https://linkedin.com/in/jane-doe",
        "platform": "github"
      },
      "status": "auto_merged"
    },
    {
      "slug": "@person.j-doe-arxiv",
      "confidence": 0.65,
      "matched_on": ["name", "affiliation"],
      "details": {
        "name_similarity": 0.68,
        "affiliation_match": "Acme Corp",
        "platform": "arxiv"
      },
      "status": "pending_review"
    }
  ]
}

# Merge entities
POST /api/v1/entities/merge
Content-Type: application/json

{
  "canonical": "@person.jane-doe",
  "targets": ["@person.jane-doe-linkedin", "@person.jdoe-github"]
}

-> 200 OK
{
  "canonical": "@person.jane-doe",
  "merged": ["@person.jane-doe-linkedin", "@person.jdoe-github"],
  "backlinks_consolidated": 47,
  "same_as_edges_created": 2
}

# Reject a candidate match
POST /api/v1/entities/reject
Content-Type: application/json

{
  "entity_a": "@person.jane-doe",
  "entity_b": "@person.jane-doe-different"
}

-> 200 OK
{
  "status": "rejected",
  "entity_a": "@person.jane-doe",
  "entity_b": "@person.jane-doe-different",
  "note": "Marked as distinct entities; will not be auto-linked"
}

# Get entity graph
GET /api/v1/entities/{entity_slug}/graph

-> 200 OK
{
  "canonical": "@person.jane-doe",
  "same_as": [
    "@person.jane-doe-linkedin",
    "@person.jdoe-github"
  ],
  "platforms": {
    "x": {"artifact_count": 1, "last_captured": "2026-02-17T08:00:00Z"},
    "linkedin": {"artifact_count": 3, "last_captured": "2026-02-16T12:00:00Z"},
    "github": {"artifact_count": 12, "last_captured": "2026-02-15T10:00:00Z"},
    "arxiv": {"artifact_count": 3, "last_captured": "2026-02-14T06:00:00Z"},
    "wikipedia": {"artifact_count": 2, "last_captured": "2026-02-10T18:00:00Z"}
  },
  "total_artifacts": 21
}

# List unresolved candidates globally
GET /api/v1/entities/candidates?status=pending_review&min_confidence=0.60

-> 200 OK
{
  "candidates": [...],
  "total": 5
}

# Resolution history
GET /api/v1/entities/{entity_slug}/history

-> 200 OK
{
  "entity": "@person.jane-doe",
  "history": [
    {
      "action": "merged",
      "target": "@person.jane-doe-linkedin",
      "confidence": 0.92,
      "method": "auto",
      "timestamp": "2026-02-15T10:00:00Z"
    },
    ...
  ]
}
```

### Pipeline Steps

**enrich.entity_resolution** (enrichment pipeline):
```
EntityExtractor -> CandidateFinder -> SimilarityScorer -> ConfidenceFilter -> EdgeCreator
```

Each step implements the `PipelineStep` interface:

```go
type EntityExtractorStep struct {
    entityStore EntityStore
}

func (s *EntityExtractorStep) Name() string { return "entity_extractor" }

func (s *EntityExtractorStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    // Extract entities from the newly captured artifact
    mentions := draft.Mentions
    if len(mentions) == 0 {
        // No entities to resolve
        return draft, nil
    }

    // Collect entity metadata for matching
    entities := make([]EntityProfile, 0, len(mentions))
    for _, mention := range mentions {
        entity, err := s.entityStore.Get(ctx, mention)
        if err != nil {
            continue
        }
        entities = append(entities, EntityProfile{
            Slug:         entity.Slug,
            Type:         entity.Type,
            Name:         entity.Name,
            Platform:     entity.Platform,
            Bio:          entity.Bio,
            URLs:         entity.LinkedURLs,
            Organization: entity.Organization,
            Email:        entity.Email,
        })
    }

    draft.Metadata["extracted_entities"] = entities
    return draft, nil
}
```

```go
type CandidateFinderStep struct {
    entityStore  EntityStore
    graphStore   GraphStore
    rejectStore  RejectStore
}

func (s *CandidateFinderStep) Name() string { return "candidate_finder" }

func (s *CandidateFinderStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    entities := draft.Metadata["extracted_entities"].([]EntityProfile)
    allCandidates := make(map[string][]CandidateMatch)

    for _, entity := range entities {
        // Search for potential matches in the entity store
        candidates, err := s.entityStore.FindSimilar(ctx, FindSimilarQuery{
            Name:         entity.Name,
            Type:         entity.Type,
            Organization: entity.Organization,
            ExcludeSelf:  entity.Slug,
        })
        if err != nil {
            continue
        }

        // Filter out rejected pairs
        filtered := make([]CandidateMatch, 0, len(candidates))
        for _, c := range candidates {
            rejected, _ := s.rejectStore.IsRejected(ctx, entity.Slug, c.Slug)
            if !rejected {
                filtered = append(filtered, c)
            }
        }

        allCandidates[entity.Slug] = filtered
    }

    draft.Metadata["candidate_matches"] = allCandidates
    return draft, nil
}
```

```go
type SimilarityScorerStep struct {
    config ScorerConfig
}

func (s *SimilarityScorerStep) Name() string { return "similarity_scorer" }

func (s *SimilarityScorerStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    entities := draft.Metadata["extracted_entities"].([]EntityProfile)
    candidates := draft.Metadata["candidate_matches"].(map[string][]CandidateMatch)

    scoredCandidates := make(map[string][]ScoredCandidate)

    for _, entity := range entities {
        matches := candidates[entity.Slug]
        scored := make([]ScoredCandidate, 0, len(matches))

        for _, match := range matches {
            score := s.computeScore(entity, match)
            scored = append(scored, ScoredCandidate{
                Slug:       match.Slug,
                Confidence: score.Overall,
                MatchedOn:  score.MatchedFields,
                Details:    score.Details,
            })
        }

        // Sort by confidence descending
        sort.Slice(scored, func(i, j int) bool {
            return scored[i].Confidence > scored[j].Confidence
        })

        scoredCandidates[entity.Slug] = scored
    }

    draft.Metadata["scored_candidates"] = scoredCandidates
    return draft, nil
}

func (s *SimilarityScorerStep) computeScore(entity EntityProfile,
    candidate CandidateMatch) Score {

    score := Score{}
    matchedFields := []string{}

    // Name similarity (Jaro-Winkler)
    nameSim := jaroWinkler(entity.Name, candidate.Name)
    score.NameSimilarity = nameSim
    if nameSim >= s.config.NameThreshold {
        matchedFields = append(matchedFields, "name")
    }

    // Linked URL match (high signal)
    for _, url := range entity.URLs {
        for _, cURL := range candidate.URLs {
            if normalizeURL(url) == normalizeURL(cURL) {
                score.LinkedURLMatch = true
                matchedFields = append(matchedFields, "linked_url")
                break
            }
        }
    }

    // Bio URL match (X bio links to GitHub, etc.)
    if containsURL(entity.Bio, candidate.URLs) || containsURL(candidate.Bio, entity.URLs) {
        score.BioURLMatch = true
        matchedFields = append(matchedFields, "bio_url")
    }

    // Organization match
    if entity.Organization != "" && entity.Organization == candidate.Organization {
        score.OrgMatch = true
        matchedFields = append(matchedFields, "organization")
    }

    // Email domain match
    if entity.Email != "" && candidate.Email != "" {
        if extractDomain(entity.Email) == extractDomain(candidate.Email) {
            score.EmailDomainMatch = true
            matchedFields = append(matchedFields, "email_domain")
        }
    }

    // Bio keyword overlap
    entityKeywords := extractKeywords(entity.Bio)
    candidateKeywords := extractKeywords(candidate.Bio)
    overlap := keywordOverlap(entityKeywords, candidateKeywords)
    if overlap >= s.config.BioKeywordOverlap {
        score.BioKeywordOverlap = overlap
        matchedFields = append(matchedFields, "bio_keywords")
    }

    // Compute overall confidence
    score.Overall = s.weightedScore(score)
    score.MatchedFields = matchedFields
    return score
}
```

```go
type ConfidenceFilterStep struct {
    config FilterConfig
}

func (s *ConfidenceFilterStep) Name() string { return "confidence_filter" }

func (s *ConfidenceFilterStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    scoredCandidates := draft.Metadata["scored_candidates"].(map[string][]ScoredCandidate)

    autoMerge := make(map[string][]ScoredCandidate)
    pendingReview := make(map[string][]ScoredCandidate)

    for entitySlug, candidates := range scoredCandidates {
        for _, c := range candidates {
            switch {
            case c.Confidence >= s.config.HighThreshold:
                // Auto-merge: verified link or strong multi-signal match
                c.Status = "auto_merge"
                autoMerge[entitySlug] = append(autoMerge[entitySlug], c)
            case c.Confidence >= s.config.MediumThreshold:
                // Auto-merge: name + org match
                c.Status = "auto_merge"
                autoMerge[entitySlug] = append(autoMerge[entitySlug], c)
            case c.Confidence >= s.config.LowThreshold:
                // Surface for manual review
                c.Status = "pending_review"
                pendingReview[entitySlug] = append(pendingReview[entitySlug], c)
            }
            // Below low threshold: ignore
        }
    }

    draft.Metadata["auto_merge"] = autoMerge
    draft.Metadata["pending_review"] = pendingReview

    return draft, nil
}
```

```go
type EdgeCreatorStep struct {
    graphStore  GraphStore
    entityStore EntityStore
}

func (s *EdgeCreatorStep) Name() string { return "edge_creator" }

func (s *EdgeCreatorStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    autoMerge := draft.Metadata["auto_merge"].(map[string][]ScoredCandidate)

    totalEdges := 0
    totalBacklinks := 0

    for entitySlug, candidates := range autoMerge {
        for _, candidate := range candidates {
            // Create same_as edge
            err := s.graphStore.CreateEdge(ctx, entitySlug, "same_as", candidate.Slug)
            if err != nil {
                return nil, fmt.Errorf("edge_creator: failed to create same_as edge: %w", err)
            }
            totalEdges++

            // Consolidate backlinks: all references to candidate now also reference canonical
            backlinks, err := s.graphStore.GetEdgesTo(ctx, candidate.Slug)
            if err != nil {
                continue
            }
            for _, bl := range backlinks {
                s.graphStore.CreateEdge(ctx, bl.SourceID, "mentions", entitySlug)
                totalBacklinks++
            }

            // Record resolution in history
            s.entityStore.RecordResolution(ctx, ResolutionEvent{
                Canonical:  entitySlug,
                Target:     candidate.Slug,
                Confidence: candidate.Confidence,
                MatchedOn:  candidate.MatchedOn,
                Method:     "auto",
                Timestamp:  time.Now().UTC(),
            })
        }
    }

    draft.Metadata["same_as_edges_created"] = totalEdges
    draft.Metadata["backlinks_consolidated"] = totalBacklinks

    return draft, nil
}
```

### Backend Processing

1. New artifact is captured and entities are extracted (via US-0009 pipeline)
2. `enrich.entity_resolution` pipeline is triggered automatically for each extracted entity
3. `EntityExtractor` collects metadata (name, bio, URLs, org) for each new entity
4. `CandidateFinder` searches the entity store for potential matches, excluding rejected pairs
5. `SimilarityScorer` computes multi-signal confidence scores for each candidate pair
6. Scoring uses: Jaro-Winkler name similarity, linked URL matching, bio URL matching, org match, email domain, bio keyword overlap
7. `ConfidenceFilter` classifies candidates: auto-merge (>= 0.75), pending review (>= 0.60), ignore (< 0.60)
8. `EdgeCreator` creates `same_as` graph edges for auto-merge candidates
9. Backlinks from merged entities are consolidated under the canonical entity
10. Resolution events are recorded in history for audit trail
11. Pending review candidates are stored for surfacing via `ctxt entity candidates`
12. Manual merges via `ctxt entity merge` follow the same edge creation and backlink consolidation process
13. Manual rejections via `ctxt entity reject` create rejection records preventing future auto-linking

### Database Schema

```sql
CREATE TABLE entity_resolutions (
    id TEXT PRIMARY KEY,
    canonical_slug TEXT NOT NULL,
    target_slug TEXT NOT NULL,
    confidence REAL NOT NULL,
    matched_on TEXT NOT NULL,         -- JSON array of matched fields
    method TEXT NOT NULL,             -- 'auto' or 'manual'
    status TEXT NOT NULL DEFAULT 'merged',  -- 'merged', 'pending_review', 'rejected'
    created_at TEXT NOT NULL,
    UNIQUE(canonical_slug, target_slug)
);

CREATE INDEX idx_resolutions_canonical ON entity_resolutions(canonical_slug);
CREATE INDEX idx_resolutions_target ON entity_resolutions(target_slug);
CREATE INDEX idx_resolutions_status ON entity_resolutions(status);

CREATE TABLE entity_rejections (
    id TEXT PRIMARY KEY,
    entity_a TEXT NOT NULL,
    entity_b TEXT NOT NULL,
    rejected_at TEXT NOT NULL,
    UNIQUE(entity_a, entity_b)
);

CREATE INDEX idx_rejections_pair ON entity_rejections(entity_a, entity_b);
```

### Configuration

```yaml
# In configuration.yaml
pipelines:
  enrich.entity_resolution:
    steps:
      - entity_extractor
      - candidate_finder
      - similarity_scorer
      - confidence_filter
      - edge_creator
    trigger: on_entity_extracted     # Runs after entity extraction pipeline

entity_resolution:
  scoring:
    nameThreshold: 0.70              # Jaro-Winkler threshold for name match
    bioKeywordOverlap: 3             # Minimum shared keywords
    weights:
      nameSimilarity: 0.25
      linkedURL: 0.30                # Highest weight: verified cross-platform link
      bioURL: 0.20
      organization: 0.15
      emailDomain: 0.05
      bioKeywords: 0.05

  confidence:
    highThreshold: 0.90              # Verified link; auto-merge
    mediumThreshold: 0.75            # Name + org; auto-merge
    lowThreshold: 0.60               # Name-only; surface for review

  entityTypes:
    - person
    - organization
    - project
    - paper

  autoMerge:
    enabled: true                    # Set to false to require manual confirmation for all
    maxAutoMergePerRun: 5            # Safety limit: max auto-merges per pipeline run
    requireMultiSignal: true         # Auto-merge only if >= 2 matching signals

  deduplication:
    consolidateBacklinks: true       # Redirect all backlinks to canonical entity
    preserveOriginalSlugs: true      # Keep platform-specific slugs as aliases
    updateSearchIndex: true          # Re-index after merge for consistent search results
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt entity candidates @person.jane-doe` shows potential matches with confidence scores
- [ ] CLI: `ctxt entity merge @slug1 @slug2` merges entities and reports consolidated backlinks
- [ ] CLI: `ctxt entity merge @slug1 @slug2 @slug3` merges multiple entities in one command
- [ ] CLI: `ctxt entity graph @entity.slug` shows full entity graph with platform presences
- [ ] CLI: `ctxt entity graph @entity.slug --format json` returns valid JSON graph
- [ ] CLI: `ctxt entity reject @slug1 @slug2` prevents future auto-linking of the pair
- [ ] CLI: `ctxt entity candidates --unresolved` lists all pending review candidates
- [ ] CLI: `ctxt entity history @entity.slug` shows chronological resolution events
- [ ] Auto-Detection: New entity from X triggers resolution check against existing LinkedIn/GitHub entities
- [ ] Auto-Detection: Matching linked URL (X bio -> GitHub) produces high confidence (>= 0.90)
- [ ] Auto-Detection: Name + organization match produces medium confidence (>= 0.75)
- [ ] Auto-Detection: Name-only match produces low confidence (>= 0.60) and is not auto-merged
- [ ] Scoring: Jaro-Winkler name similarity is computed correctly for similar names
- [ ] Scoring: Linked URL match gets highest weight in confidence calculation
- [ ] Scoring: Bio keyword overlap of >= 3 keywords contributes to confidence
- [ ] Scoring: Multiple matching signals produce higher confidence than single signals
- [ ] Graph: `same_as` edges are created between merged entity slugs
- [ ] Graph: All backlinks from target entity are consolidated to canonical entity
- [ ] Graph: Searches for merged entity return consolidated results from all platforms
- [ ] Rejection: Rejected pairs are excluded from future candidate matching
- [ ] Pipeline: `enrich.entity_resolution` runs automatically after entity extraction
- [ ] Pipeline: Auto-merge respects `maxAutoMergePerRun` safety limit
- [ ] Pipeline: `requireMultiSignal: true` prevents auto-merge on single-signal matches
- [ ] REST API: `GET /api/v1/entities/{slug}/candidates` returns candidates with scores
- [ ] REST API: `POST /api/v1/entities/merge` merges entities and returns consolidation count
- [ ] REST API: `POST /api/v1/entities/reject` creates rejection record
- [ ] REST API: `GET /api/v1/entities/{slug}/graph` returns entity graph with platform data
- [ ] REST API: `GET /api/v1/entities/{slug}/history` returns resolution history
- [ ] De-duplication: After merge, `ctxt search` returns unified results (no duplicate entities)
- [ ] Resilience: Pipeline failure does not corrupt existing entity relationships
- [ ] Resilience: Concurrent resolution of same entity pair is handled without duplicate edges

---

## Related Stories

- [US-0206](./US-0206-osint-entity-aggregation.md) -- OSINT entity aggregation (consumes resolution results)
- [US-0208](./US-0208-temporal-watch.md) -- Temporal watch (entity watches depend on resolved entities)
- [US-0009](../enrichment/US-0009-extract-entities-and-mentions.md) -- Entity extraction (triggers resolution)
- [US-0046](../enrichment/US-0046-extract-relationships-between-entities.md) -- Relationship extraction between entities
- [US-0052](../search/US-0052-graph-based-entity-search.md) -- Graph-based entity search (uses same_as edges)
- [US-0024](../composition/US-0024-compose-with-graph-traversal.md) -- Graph traversal for composition
