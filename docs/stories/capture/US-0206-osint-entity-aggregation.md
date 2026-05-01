---
status: shipped
---

# US-0206: OSINT Entity Aggregation

**System Types:** ctxt, dpkms (self-hosted)
**Personas:** [Researchers & OSINT Analysts](../../personas/researchers-osint.md), [Agents/LLMs/Tools](../../personas/agents-llms-tools.md)

---

## User Goal

As a researcher, I want to aggregate all captured artifacts about an entity across platforms into a unified profile view so that I have a comprehensive picture without manually piecing together information from X, LinkedIn, GitHub, Wikipedia, and other sources.

---

## Context

Investigations and research frequently revolve around entities -- people, companies, projects -- that leave traces across many platforms. A single person might have an X profile, a LinkedIn page, several GitHub repositories, arXiv publications, and Wikipedia mentions. Without aggregation, a researcher must manually visit each source, mentally correlate identities, and keep a running tally of what is known. This is slow, error-prone, and produces results that are immediately stale.

The OSINT entity aggregation pipeline solves this by creating a canonical entity profile that pulls together every captured artifact referencing that entity. When `ctxt osint profile @person.jane-doe` is invoked, the system traverses the knowledge graph outward from the entity node, collecting all linked KnowledgeObjects -- regardless of which platform they originated from -- and composing them into a unified view with timeline, platform presence, key topics, and relationship map.

The `osint.aggregate` enrichment pipeline runs automatically whenever new artifacts are captured for known entities, keeping profiles fresh without manual intervention. For active investigations, `ctxt osint build` triggers cross-platform capture using configured source adapters, and `ctxt osint diff` surfaces changes detected since a given point in time, enabling researchers to track evolving situations.

---

## Acceptance Criteria

- [ ] `ctxt osint profile @entity.slug` returns a unified profile view aggregated from all captured sources
- [ ] `ctxt osint build @entity.slug --sources x,linkedin,github` triggers cross-platform entity resolution and capture
- [ ] System automatically links artifacts from different platforms to the same canonical entity using heuristics
- [ ] Heuristics include: same name + matching bio keywords + linked URLs = same person (configurable thresholds)
- [ ] Profile view includes: timeline of activity, platform presence summary, key topics, and relationship map
- [ ] `ctxt osint diff @entity.slug --since 7d` returns changes detected in the specified time window
- [ ] Enrichment pipeline runs automatically after new artifacts are captured for known entities
- [ ] Graph traversal follows entity -> all backlinks -> all captured objects -> unified view
- [ ] Profile output supports JSON, Markdown, and table formats via `--format` flag
- [ ] Build command returns a job ID and runs asynchronously; profile is updated when complete
- [ ] System handles entities with no captured artifacts gracefully (empty profile with guidance)
- [ ] Rate limiting is respected per source platform during build operations

---

## Implementation Notes

### CLI Interface

```bash
# View aggregated profile for an entity
ctxt osint profile @person.jane-doe
# Shows: X profile, LinkedIn profile, GitHub repos, arXiv papers, Wikipedia mentions
# All linked to the same canonical entity

# View profile in specific format
ctxt osint profile @person.jane-doe --format json
ctxt osint profile @person.jane-doe --format markdown

# Trigger active capture across configured sources
ctxt osint build @person.jane-doe --sources x,linkedin,github
# Returns immediately with job ID
{
  "job_id": "j-osint-build-9a2f1c",
  "entity": "@person.jane-doe",
  "sources": ["x", "linkedin", "github"],
  "status": "pending",
  "estimated_completion": "2026-02-18T15:45:00Z"
}

# Show changes detected in the last 7 days
ctxt osint diff @person.jane-doe --since 7d
# Returns:
{
  "entity": "@person.jane-doe",
  "period": "2026-02-11T00:00:00Z/2026-02-18T00:00:00Z",
  "changes": [
    {
      "source": "x",
      "type": "new_artifact",
      "object_id": "o-x-tweet-4f2a1b",
      "summary": "New post about machine learning research",
      "captured_at": "2026-02-15T09:12:33Z"
    },
    {
      "source": "linkedin",
      "type": "profile_update",
      "object_id": "o-li-profile-8c3d2e",
      "summary": "Job title changed from 'Senior Engineer' to 'Staff Engineer'",
      "captured_at": "2026-02-13T14:22:10Z"
    }
  ],
  "total_changes": 2
}

# List all known entities with OSINT profiles
ctxt osint list --type person
ctxt osint list --type organization --sort activity
```

### REST API

```
GET /api/v1/osint/profile/{entity_slug}
Accept: application/json

-> 200 OK
{
  "entity": "@person.jane-doe",
  "canonical_name": "Jane Doe",
  "platforms": {
    "x": {
      "handle": "@janedoe",
      "object_id": "o-x-profile-1a2b3c",
      "last_captured": "2026-02-17T08:00:00Z",
      "artifact_count": 47
    },
    "linkedin": {
      "url": "https://linkedin.com/in/jane-doe",
      "object_id": "o-li-profile-8c3d2e",
      "last_captured": "2026-02-16T12:00:00Z",
      "artifact_count": 3
    },
    "github": {
      "username": "jdoe",
      "object_id": "o-gh-profile-5e6f7g",
      "last_captured": "2026-02-15T10:00:00Z",
      "artifact_count": 12
    }
  },
  "timeline": [...],
  "key_topics": ["machine-learning", "distributed-systems", "go"],
  "relationships": [
    {"entity": "@organization.acme-corp", "type": "employed_by"},
    {"entity": "@person.john-smith", "type": "collaborates_with"}
  ],
  "total_artifacts": 62,
  "last_updated": "2026-02-17T08:00:00Z"
}

POST /api/v1/osint/build/{entity_slug}
Content-Type: application/json

{
  "sources": ["x", "linkedin", "github"],
  "depth": "standard"
}

-> 202 Accepted
{
  "job_id": "j-osint-build-9a2f1c",
  "entity": "@person.jane-doe",
  "status": "pending"
}

GET /api/v1/osint/diff/{entity_slug}?since=7d
Accept: application/json

-> 200 OK
{
  "entity": "@person.jane-doe",
  "changes": [...],
  "total_changes": 2
}
```

### Pipeline Steps

**osint.aggregate** (enrichment pipeline):
```
EntityCollector -> CrossPlatformMatcher -> ProfileComposer -> TimelineBuilder -> Tagger -> EmbeddingGenerator
```

Each step implements the `PipelineStep` interface:

```go
type EntityCollectorStep struct {
    graphStore  GraphStore
    objectStore ObjectStore
}

func (s *EntityCollectorStep) Name() string { return "entity_collector" }

func (s *EntityCollectorStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    entitySlug := draft.Metadata["target_entity"].(string)

    // Traverse graph: entity -> all backlinks -> all captured objects
    edges, err := s.graphStore.GetEdgesFrom(ctx, entitySlug)
    if err != nil {
        return nil, fmt.Errorf("entity_collector: graph traversal failed: %w", err)
    }

    artifacts := make([]ArtifactRef, 0, len(edges))
    for _, edge := range edges {
        obj, err := s.objectStore.Get(ctx, edge.TargetID)
        if err != nil {
            continue // Skip inaccessible objects, log warning
        }
        artifacts = append(artifacts, ArtifactRef{
            ObjectID:   obj.ID,
            Source:     obj.Source.Platform,
            CapturedAt: obj.Source.IngestedAt,
            ContentType: obj.ContentType,
        })
    }

    draft.Metadata["collected_artifacts"] = artifacts
    draft.Metadata["artifact_count"] = len(artifacts)

    return draft, nil
}
```

```go
type CrossPlatformMatcherStep struct {
    entityStore EntityStore
    config      MatcherConfig
}

func (s *CrossPlatformMatcherStep) Name() string { return "cross_platform_matcher" }

func (s *CrossPlatformMatcherStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    artifacts := draft.Metadata["collected_artifacts"].([]ArtifactRef)

    // Group artifacts by platform
    byPlatform := make(map[string][]ArtifactRef)
    for _, a := range artifacts {
        byPlatform[a.Source] = append(byPlatform[a.Source], a)
    }

    // Verify cross-platform links are still valid
    platforms := make(map[string]PlatformPresence)
    for platform, arts := range byPlatform {
        platforms[platform] = PlatformPresence{
            Platform:      platform,
            ArtifactCount: len(arts),
            LastCaptured:  latestCapture(arts),
        }
    }

    draft.Metadata["platform_presence"] = platforms
    return draft, nil
}
```

```go
type ProfileComposerStep struct {
    objectStore ObjectStore
}

func (s *ProfileComposerStep) Name() string { return "profile_composer" }

func (s *ProfileComposerStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    artifacts := draft.Metadata["collected_artifacts"].([]ArtifactRef)
    platforms := draft.Metadata["platform_presence"].(map[string]PlatformPresence)

    // Compose unified profile sections
    draft.Sections = append(draft.Sections, Section{
        Title:   "Platform Presence",
        Content: formatPlatformSummary(platforms),
    })

    // Extract key topics across all artifacts
    topics := extractTopicsFromArtifacts(ctx, s.objectStore, artifacts)
    draft.Sections = append(draft.Sections, Section{
        Title:   "Key Topics",
        Content: strings.Join(topics, ", "),
    })

    draft.Tags = topics
    return draft, nil
}
```

### Backend Processing

1. CLI or REST API receives the `osint profile`, `osint build`, or `osint diff` command
2. For `profile`: system resolves entity slug to canonical entity ID in the graph store
3. `EntityCollector` traverses all graph edges emanating from the entity node
4. For each edge, the linked KnowledgeObject is fetched from the object store
5. `CrossPlatformMatcher` groups artifacts by source platform and validates linkages
6. `ProfileComposer` merges platform-specific data into a unified profile with sections
7. `TimelineBuilder` sorts all artifacts chronologically and produces an activity timeline
8. `Tagger` assigns topic tags based on aggregate content analysis
9. `EmbeddingGenerator` produces embeddings for the composite profile text
10. For `build`: a job is created per source platform; each adapter fetches new content
11. For `diff`: system queries objects linked to the entity with `captured_at > since` threshold
12. Results are formatted according to the `--format` flag and returned

### Configuration

```yaml
# In configuration.yaml
pipelines:
  osint.aggregate:
    steps:
      - entity_collector
      - cross_platform_matcher
      - profile_composer
      - timeline_builder
      - tagger
      - embedding_generator
    trigger: on_entity_artifact  # runs when new artifact linked to known entity

osint:
  matching:
    nameThreshold: 0.85         # Jaro-Winkler similarity for name matching
    bioKeywordOverlap: 3        # Minimum shared keywords to consider match
    linkedURLWeight: 0.95       # Confidence weight for matching linked URLs
    confidenceLevels:
      high: 0.90                # Verified cross-platform link
      medium: 0.75              # Name + organization match
      low: 0.60                 # Name-only match

  sources:
    x:
      enabled: true
      rateLimit: 5/min
      adapter: x_profile_adapter
    linkedin:
      enabled: true
      rateLimit: 2/min
      adapter: linkedin_adapter
    github:
      enabled: true
      rateLimit: 10/min
      adapter: github_adapter
    arxiv:
      enabled: true
      rateLimit: 10/min
      adapter: arxiv_adapter
    wikipedia:
      enabled: true
      rateLimit: 10/min
      adapter: wikipedia_adapter

  profile:
    maxArtifactsInView: 100     # Limit artifacts shown in profile
    timelineGranularity: day    # day | week | month
    topicsLimit: 20             # Max key topics in profile
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt osint profile @person.jane-doe` returns aggregated profile from all captured sources
- [ ] CLI: Profile includes platform presence summary with artifact counts per platform
- [ ] CLI: Profile includes chronological timeline of captured activity
- [ ] CLI: Profile includes key topics extracted from aggregate content
- [ ] CLI: `ctxt osint profile @person.jane-doe --format json` sends `format=json` as a parameter and returns valid JSON
- [ ] CLI: `ctxt osint profile @person.jane-doe --format markdown` sends `format=markdown` as a parameter and returns formatted Markdown
- [ ] CLI: `ctxt osint build @person.jane-doe --sources x,github` sends `sources: ["x", "github"]` in the `POST /api/v1/osint/build` request payload and returns job ID within 1 second
- [ ] CLI: Build job transitions from `pending` to `completed` and profile reflects new artifacts
- [ ] CLI: `ctxt osint diff @person.jane-doe --since 7d` sends `since=7d` as a query parameter in the `GET /api/v1/osint/diff` request and returns only changes within the time window
- [ ] CLI: `ctxt osint list --type person` lists all entities with OSINT profiles
- [ ] Matching: Artifacts from different platforms linked to same entity via name + bio heuristics
- [ ] Matching: Linked URL in bio (e.g., X bio links to GitHub) produces high-confidence match
- [ ] Matching: Name-only matches flagged as low confidence and not auto-merged
- [ ] Graph: Entity -> backlinks -> objects traversal returns correct artifact set
- [ ] Graph: New artifact captured for known entity triggers `osint.aggregate` pipeline automatically
- [ ] Pipeline: EntityCollector gathers all artifacts across platforms for the target entity
- [ ] Pipeline: CrossPlatformMatcher correctly groups artifacts by source platform
- [ ] Pipeline: ProfileComposer generates unified sections from multi-platform data
- [ ] Pipeline: TimelineBuilder sorts artifacts chronologically and produces coherent timeline
- [ ] REST API: `POST /api/v1/osint/build/{slug}` request payload contains `sources` array and `depth` field
- [ ] REST API: `POST /api/v1/osint/build/{slug}` returns 202 with `job_id`, `entity`, and `status: pending`; job is persisted and retrievable
- [ ] REST API: `GET /api/v1/osint/profile/{slug}` returns 200 with full profile JSON including `platforms`, `timeline`, `key_topics`, and `total_artifacts`
- [ ] REST API: `GET /api/v1/osint/diff/{slug}?since=7d` returns 200 with `changes` array and `total_changes`
- [ ] Rate Limiting: Build operations respect per-platform rate limits
- [ ] Empty Profile: Entity with no captured artifacts returns empty profile with guidance message
- [ ] Resilience: Source adapter failure for one platform does not block other platforms in build
- [ ] Resilience: Network timeout during build is retried up to 3 times before marking source as failed

---

## Related Stories

- [US-0210](./US-0210-cross-platform-entity-resolution.md) -- Cross-platform entity resolution heuristics
- [US-0208](./US-0208-temporal-watch.md) -- Temporal watch for change detection on entity sources
- [US-0209](./US-0209-authenticated-web-fetch.md) -- Authenticated fetch for paywalled or private content
- [US-0009](../enrichment/US-0009-extract-entities-and-mentions.md) -- Entity extraction from captured content
- [US-0046](../enrichment/US-0046-extract-relationships-between-entities.md) -- Relationship extraction between entities
- [US-0052](../search/US-0052-graph-based-entity-search.md) -- Graph-based entity search
- [US-0024](../composition/US-0024-compose-with-graph-traversal.md) -- Graph traversal for composition

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Solo Developer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/solo-developer.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)

---

## E2E Tests

- `test/integration/us0206_osint_test.go::TestUS0206_MergedProfileFromMultipleSources`
- `test/integration/us0206_osint_test.go::TestUS0206_SingleSourceAggregation`
