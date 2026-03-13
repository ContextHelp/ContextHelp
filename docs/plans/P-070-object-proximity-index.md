# Design: Object Proximity Index

**Date:** 2026-02-18
**Status:** Draft
**Related:** ADR-016 (Just-In-Time Surfacing), ranking-and-reranking.md, US-0022 (Composition)

---

## Overview

Implement a precomputed **Object Proximity Index** that measures closeness between knowledge objects across multiple dimensions (semantic, temporal, entity, origin, behavioral). Unlike query-time relevance scoring, proximity is computed once and stored, enabling faster context assembly, citation prioritization, serendipitous discovery, and import quality checks.

---

## Problem Statement

### Current State

Proximity exists but is computed on-demand during search/ranking:

```
relevance_score =
  entity_match_weight * entity_match_score +
  tag_overlap_weight * tag_overlap_score +
  recency_weight * recency_score +
  graph_proximity_weight * graph_distance_score
```

**Limitations:**
- Expensive to compute for large result sets
- No persistent object-to-object relationships
- Same weights for all object types
- No origin/author proximity consideration
- Can't query "objects similar to X" without search

### Goals

1. **Composition Quality:** Select most relevant neighbors for context assembly
2. **Citation Prioritization:** Rank sources by proximity in `[ref:ID]` citations
3. **Serendipitous Discovery:** Enable "similar objects" without query formulation
4. **Import Quality:** Detect clusters and orphans during bulk imports
5. **Cluster Detection:** Identify groups of related objects automatically

### Non-Goals

- Real-time proximity for live collaboration
- Cross-user proximity (single-user scope only)
- Proximity between objects in different registries

---

## Proximity Factors

### Factor Definitions

| Factor | Description | Data Source | Range |
|--------|-------------|-------------|-------|
| **Semantic** | Content similarity via embeddings | `embeddings` table | 0.0 - 1.0 |
| **Temporal** | Submission time closeness | `created_at` field | 0.0 - 1.0 |
| **Entity** | Shared mentions/entities | `mentions` field + graph edges | 0.0 - 1.0 |
| **Origin** | Same source, author, or pipeline | `source`, `pipeline`, metadata | 0.0 - 1.0 |
| **Behavioral** | Co-accessed patterns | Access logs (future) | 0.0 - 1.0 |

### Factor Computation

#### 1. Semantic Proximity

Uses existing embeddings for cosine similarity:

```go
func SemanticProximity(a, b *KnowledgeObject) float64 {
    embA, err := store.Embeddings().Get(a.ID)
    embB, err := store.Embeddings().Get(b.ID)
    if err != nil {
        return 0.0 // No embedding = no semantic proximity
    }
    return CosineSimilarity(embA.Vector, embB.Vector)
}

func CosineSimilarity(a, b []float32) float64 {
    dot := 0.0
    normA, normB := 0.0, 0.0
    for i := range a {
        dot += float64(a[i]) * float64(b[i])
        normA += float64(a[i]) * float64(a[i])
        normB += float64(b[i]) * float64(b[i])
    }
    if normA == 0 || normB == 0 {
        return 0.0
    }
    return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
```

#### 2. Temporal Proximity

Exponential decay based on time difference:

```go
func TemporalProximity(a, b *KnowledgeObject) float64 {
    diff := math.Abs(a.CreatedAt.Sub(b.CreatedAt).Hours())
    
    // Decay factors by time window
    switch {
    case diff < 24:         // Same day
        return 1.0
    case diff < 24 * 7:     // Same week
        return 0.8 * math.Exp(-diff/(24*7))
    case diff < 24 * 30:    // Same month
        return 0.5 * math.Exp(-diff/(24*30))
    case diff < 24 * 365:   // Same year
        return 0.2 * math.Exp(-diff/(24*365))
    default:
        return 0.05 // Minimal proximity for old items
    }
}
```

**Visualization:**
```
Temporal Proximity
1.0 ┤■■■■■
0.8 ┤■■■■■■■■■■
0.6 ┤■■■■■■■
0.4 ┤■■■■
0.2 ┤■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■
0.0 ┴────────────────────────────────────
     1d   1w    1m    3m    6m    1y    2y
```

#### 3. Entity Proximity

Jaccard similarity of shared entities + graph distance:

```go
func EntityProximity(a, b *KnowledgeObject) float64 {
    // Extract entity slugs
    entitiesA := make(map[string]bool)
    for _, m := range a.Mentions {
        entitiesA[m.Slug] = true
    }
    
    entitiesB := make(map[string]bool)
    for _, m := range b.Mentions {
        entitiesB[m.Slug] = true
    }
    
    // Jaccard similarity
    intersection := 0
    for e := range entitiesA {
        if entitiesB[e] {
            intersection++
        }
    }
    union := len(entitiesA) + len(entitiesB) - intersection
    
    if union == 0 {
        return 0.0
    }
    
    jaccard := float64(intersection) / float64(union)
    
    // Bonus for graph-connected entities (optional, expensive)
    // graphBonus := graphDistanceBonus(entitiesA, entitiesB)
    
    return jaccard
}
```

**Entity overlap examples:**
```
Object A: [@person.alice, @project.checkout, @team.engineering]
Object B: [@person.alice, @project.checkout, @team.product]

Intersection: 2 (@person.alice, @project.checkout)
Union: 4
Jaccard: 0.5
```

#### 4. Origin Proximity

Same source/author/pipeline clustering:

```go
func OriginProximity(a, b *KnowledgeObject) float64 {
    score := 0.0
    
    // Same source (exact match)
    if a.Source == b.Source && a.Source != "" {
        score += 0.4
    }
    
    // Same pipeline
    if a.Pipeline == b.Pipeline && a.Pipeline != "" {
        score += 0.2
    }
    
    // Same author (from metadata)
    authorA, _ := a.Metadata["author"].(string)
    authorB, _ := b.Metadata["author"].(string)
    if authorA != "" && authorA == authorB {
        score += 0.3
    }
    
    // Same URL domain (for URLs)
    if a.Type == "url" && b.Type == "url" {
        domainA := extractDomain(a.RawContent)
        domainB := extractDomain(b.RawContent)
        if domainA == domainB {
            score += 0.1
        }
    }
    
    return math.Min(score, 1.0)
}
```

#### 5. Behavioral Proximity (Future)

Co-accessed patterns from interaction logs:

```go
func BehavioralProximity(a, b *KnowledgeObject) float64 {
    // TODO: Requires access log implementation
    // - Objects viewed in same session
    // - Objects edited together
    // - Objects cited together in compositions
    return 0.0
}
```

---

## Type-Specific Weights

Different object types prioritize different proximity factors:

```go
var ProximityWeightsByType = map[string]ProximityFactors{
    "conversation": {
        Semantic:   0.30,
        Temporal:   0.30,  // Conversations cluster by time
        Entity:     0.25,
        Origin:     0.15,
        Behavioral: 0.0,
    },
    "document": {
        Semantic:   0.50,  // Documents cluster by content
        Temporal:   0.10,
        Entity:     0.25,
        Origin:     0.15,
        Behavioral: 0.0,
    },
    "decision": {
        Semantic:   0.25,
        Temporal:   0.20,  // Decisions have temporal context
        Entity:     0.35,  // Decisions involve stakeholders
        Origin:     0.20,
        Behavioral: 0.0,
    },
    "url": {
        Semantic:   0.40,
        Temporal:   0.10,
        Entity:     0.20,
        Origin:     0.30,  // URLs cluster by domain/source
        Behavioral: 0.0,
    },
    "image": {
        Semantic:   0.45,  // Visual similarity (future: image embeddings)
        Temporal:   0.15,
        Entity:     0.25,
        Origin:     0.15,
        Behavioral: 0.0,
    },
    "email": {
        Semantic:   0.30,
        Temporal:   0.25,  // Emails cluster by thread/date
        Entity:     0.25,
        Origin:     0.20,  // Same sender
        Behavioral: 0.0,
    },
    "default": {
        Semantic:   0.40,
        Temporal:   0.20,
        Entity:     0.25,
        Origin:     0.15,
        Behavioral: 0.0,
    },
}

type ProximityFactors struct {
    Semantic   float64
    Temporal   float64
    Entity     float64
    Origin     float64
    Behavioral float64
}

func GetWeights(typeA, typeB string) ProximityFactors {
    // Use weights from the more specific type
    // If types differ, use default
    if typeA == typeB {
        if w, ok := ProximityWeightsByType[typeA]; ok {
            return w
        }
    }
    return ProximityWeightsByType["default"]
}
```

---

## Aggregated Proximity Score

```go
type ProximityScore struct {
    ObjectA    string
    ObjectB    string
    Score      float64
    Factors    ProximityFactors
    Weights    ProximityFactors
    ComputedAt time.Time
}

func ComputeProximity(a, b *KnowledgeObject) *ProximityScore {
    weights := GetWeights(a.Type, b.Type)
    
    factors := ProximityFactors{
        Semantic:   SemanticProximity(a, b),
        Temporal:   TemporalProximity(a, b),
        Entity:     EntityProximity(a, b),
        Origin:     OriginProximity(a, b),
        Behavioral: BehavioralProximity(a, b),
    }
    
    score := weights.Semantic*factors.Semantic +
        weights.Temporal*factors.Temporal +
        weights.Entity*factors.Entity +
        weights.Origin*factors.Origin +
        weights.Behavioral*factors.Behavioral
    
    return &ProximityScore{
        ObjectA:    a.ID,
        ObjectB:    b.ID,
        Score:      score,
        Factors:    factors,
        Weights:    weights,
        ComputedAt: time.Now(),
    }
}
```

---

## Storage Schema

### SQLite

```sql
-- Object proximity scores (symmetric, store only object_a < object_b)
CREATE TABLE object_proximity (
    object_a TEXT NOT NULL,
    object_b TEXT NOT NULL,
    score REAL NOT NULL,
    semantic REAL NOT NULL DEFAULT 0,
    temporal REAL NOT NULL DEFAULT 0,
    entity REAL NOT NULL DEFAULT 0,
    origin REAL NOT NULL DEFAULT 0,
    behavioral REAL NOT NULL DEFAULT 0,
    computed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    PRIMARY KEY (object_a, object_b),
    CHECK (object_a < object_b)  -- Enforce symmetry
);

-- Fast lookup of nearest neighbors
CREATE INDEX idx_proximity_neighbors ON object_proximity(object_a, score DESC);

-- Filter by score threshold
CREATE INDEX idx_proximity_score ON object_proximity(score DESC);

-- Staleness detection
CREATE INDEX idx_proximity_computed ON object_proximity(computed_at);
```

### PostgreSQL

```sql
CREATE TABLE object_proximity (
    object_a UUID NOT NULL REFERENCES knowledge_objects(id),
    object_b UUID NOT NULL REFERENCES knowledge_objects(id),
    score REAL NOT NULL,
    semantic REAL NOT NULL DEFAULT 0,
    temporal REAL NOT NULL DEFAULT 0,
    entity REAL NOT NULL DEFAULT 0,
    origin REAL NOT NULL DEFAULT 0,
    behavioral REAL NOT NULL DEFAULT 0,
    computed_at TIMESTAMP NOT NULL DEFAULT NOW(),
    
    PRIMARY KEY (object_a, object_b),
    CHECK (object_a < object_b)
);

-- Partial index for high-proximity pairs only (reduce storage)
CREATE INDEX idx_proximity_high ON object_proximity(object_a, score DESC)
    WHERE score >= 0.3;

-- GIN index for entity-based lookups (optional)
-- CREATE INDEX idx_proximity_entity ON object_proximity USING GIN(entity_entities);
```

### Storage Estimates

| Objects | Pairs (score ≥ 0.3) | Storage |
|---------|---------------------|---------|
| 1,000 | ~50,000 | 5 MB |
| 10,000 | ~500,000 | 50 MB |
| 100,000 | ~5,000,000 | 500 MB |
| 1,000,000 | ~50,000,000 | 5 GB |

*Assumes 10% of pairs exceed 0.3 threshold (sparse matrix).*

---

## Computation Strategy

### Trigger-Based Computation

```go
type ProximityIndexer struct {
    store       storage.StorageDriver
    queue       *jobs.Queue
    config      ProximityConfig
}

type ProximityConfig struct {
    // Minimum score to store (sparse matrix)
    MinScoreThreshold float64 `yaml:"min_score_threshold" default:"0.2"`
    
    // Max neighbors per object (limit storage)
    MaxNeighbors int `yaml:"max_neighbors" default:"100"`
    
    // Staleness TTL (recompute after)
    StalenessTTL time.Duration `yaml:"staleness_ttl" default:"720h"` // 30 days
    
    // Batch size for background computation
    BatchSize int `yaml:"batch_size" default:"100"`
    
    // Comparison window (only compare to recent objects for new items)
    ComparisonWindow time.Duration `yaml:"comparison_window" default:"2160h"` // 90 days
}
```

### On New Object Creation

```go
func (idx *ProximityIndexer) OnObjectCreated(ctx context.Context, obj *KnowledgeObject) error {
    // Find candidate objects to compare against
    candidates := idx.findCandidates(ctx, obj)
    
    // Compute proximity in batches
    for _, batch := range chunk(candidates, idx.config.BatchSize) {
        scores := make([]*ProximityScore, 0, len(batch))
        
        for _, candidate := range batch {
            score := ComputeProximity(obj, candidate)
            if score.Score >= idx.config.MinScoreThreshold {
                scores = append(scores, score)
            }
        }
        
        // Store scores
        if err := idx.store.Proximity().PutBatch(ctx, scores); err != nil {
            return err
        }
    }
    
    return nil
}

func (idx *ProximityIndexer) findCandidates(ctx context.Context, obj *KnowledgeObject) []*KnowledgeObject {
    // Strategy 1: Objects with shared entities
    entityCandidates := idx.findBySharedEntities(ctx, obj)
    
    // Strategy 2: Objects from same source/origin
    originCandidates := idx.findByOrigin(ctx, obj)
    
    // Strategy 3: Recent objects (temporal window)
    recentCandidates := idx.findByTimeWindow(ctx, obj)
    
    // Dedupe and combine
    return dedupeObjects(entityCandidates, originCandidates, recentCandidates)
}
```

### On Entity Update

```go
func (idx *ProximityIndexer) OnEntityUpdated(ctx context.Context, entitySlug string) error {
    // Find all objects mentioning this entity
    objects, err := idx.store.Objects().FindByMention(ctx, entitySlug)
    if err != nil {
        return err
    }
    
    // Recompute proximity between all pairs
    for i, objA := range objects {
        for _, objB := range objects[i+1:] {
            score := ComputeProximity(objA, objB)
            if score.Score >= idx.config.MinScoreThreshold {
                idx.store.Proximity().Put(ctx, score)
            }
        }
    }
    
    return nil
}
```

### Background Refresh Job

```go
func (idx *ProximityIndexer) RefreshStale(ctx context.Context) error {
    // Find objects with stale proximity scores
    cutoff := time.Now().Add(-idx.config.StalenessTTL)
    staleIDs, err := idx.store.Proximity().FindStale(ctx, cutoff, 1000)
    if err != nil {
        return err
    }
    
    for _, id := range staleIDs {
        obj, err := idx.store.Objects().Get(ctx, id)
        if err != nil {
            continue
        }
        
        // Recompute all proximity for this object
        idx.recomputeForObject(ctx, obj)
    }
    
    return nil
}
```

---

## Query Interface

### Storage Interface

```go
type ProximityStore interface {
    // Get neighbors for an object (sorted by score desc)
    GetNeighbors(ctx context.Context, objectID string, limit int) ([]*ProximityScore, error)
    
    // Get neighbors above threshold
    GetNeighborsAbove(ctx context.Context, objectID string, threshold float64) ([]*ProximityScore, error)
    
    // Get proximity between two specific objects
    Get(ctx context.Context, objectA, objectB string) (*ProximityScore, error)
    
    // Store proximity score
    Put(ctx context.Context, score *ProximityScore) error
    
    // Batch store
    PutBatch(ctx context.Context, scores []*ProximityScore) error
    
    // Delete all proximity for an object
    Delete(ctx context.Context, objectID string) error
    
    // Find objects with stale proximity
    FindStale(ctx context.Context, cutoff time.Time, limit int) ([]string, error)
    
    // Statistics
    Stats(ctx context.Context) (*ProximityStats, error)
}

type ProximityStats struct {
    TotalPairs      int64
    AvgScore        float64
    MaxScore        float64
    HighProximity   int64   // score >= 0.7
    MediumProximity int64   // score 0.4-0.7
    LowProximity    int64   // score 0.2-0.4
}
```

### Service Layer

```go
func (s *Service) GetSimilarObjects(ctx context.Context, objectID string, limit int) ([]*KnowledgeObject, error) {
    scores, err := s.Store.Proximity().GetNeighbors(ctx, objectID, limit)
    if err != nil {
        return nil, err
    }
    
    objects := make([]*KnowledgeObject, 0, len(scores))
    for _, score := range scores {
        // Get the other object (not the query object)
        otherID := score.ObjectB
        if score.ObjectB == objectID {
            otherID = score.ObjectA
        }
        
        obj, err := s.Store.Objects().Get(ctx, otherID)
        if err != nil {
            continue
        }
        
        // Attach proximity metadata
        obj.Metadata["proximity_score"] = score.Score
        obj.Metadata["proximity_factors"] = score.Factors
        
        objects = append(objects, obj)
    }
    
    return objects, nil
}

func (s *Service) GetProximity(ctx context.Context, objectA, objectB string) (*ProximityScore, error) {
    // Ensure consistent ordering
    if objectA > objectB {
        objectA, objectB = objectB, objectA
    }
    return s.Store.Proximity().Get(ctx, objectA, objectB)
}

func (s *Service) FindClusters(ctx context.Context, threshold float64) ([][]string, error) {
    // Find connected components where edge weight >= threshold
    return s.graphClustering(ctx, threshold)
}
```

---

## Integration Points

### 1. Composition Engine (US-0022)

```go
func (g *BriefGenerator) selectContextObjects(ctx context.Context, 
    primaryObjects []*KnowledgeObject, maxNeighbors int) []*KnowledgeObject {
    
    // Find high-proximity neighbors for context
    contextMap := make(map[string]*ProximityScore)
    
    for _, obj := range primaryObjects {
        neighbors, _ := g.proximityStore.GetNeighborsAbove(ctx, obj.ID, 0.5)
        for _, score := range neighbors {
            otherID := score.ObjectB
            if score.ObjectB == obj.ID {
                otherID = score.ObjectA
            }
            
            // Keep highest score if object is neighbor of multiple primaries
            if existing, ok := contextMap[otherID]; !ok || score.Score > existing.Score {
                contextMap[otherID] = score
            }
        }
    }
    
    // Sort by score and take top N
    sorted := sortByScore(contextMap)
    result := make([]*KnowledgeObject, 0, maxNeighbors)
    for i := 0; i < len(sorted) && i < maxNeighbors; i++ {
        obj, _ := g.objectStore.Get(ctx, sorted[i])
        result = append(result, obj)
    }
    
    return result
}
```

### 2. Citation Prioritization (Inline [ref:ID])

```go
func (g *BriefGenerator) buildCitationPrompt(ctx context.Context, 
    objects []*KnowledgeObject, primaryID string) string {
    
    // Sort objects by proximity to primary
    scored := make([]struct {
        obj   *KnowledgeObject
        score float64
    }, len(objects))
    
    for i, obj := range objects {
        if obj.ID == primaryID {
            scored[i] = struct {
                obj   *KnowledgeObject
                score float64
            }{obj, 1.0}
            continue
        }
        
        prox, _ := g.proximityStore.Get(ctx, primaryID, obj.ID)
        if prox != nil {
            scored[i] = struct {
                obj   *KnowledgeObject
                score float64
            }{obj, prox.Score}
        } else {
            scored[i] = struct {
                obj   *KnowledgeObject
                score float64
            }{obj, 0.0}
        }
    }
    
    sort.Slice(scored, func(i, j int) bool {
        return scored[i].score > scored[j].score
    })
    
    // Build prompt with proximity-ranked sources
    // Higher proximity = more likely to be cited
    // ...
}
```

### 3. Search Enhancement

```go
func (s *SearchEngine) Search(ctx context.Context, query string, opts SearchOptions) ([]*KnowledgeObject, error) {
    // Standard multi-source search
    results := s.multiSourceSearch(ctx, query)
    
    // Proximity boost: if query matches object A, boost objects near A
    if opts.ProximityBoost && len(results) > 0 && len(results) < 50 {
        boosted := make(map[string]float64)
        
        for _, obj := range results[:10] {  // Boost neighbors of top 10
            neighbors, _ := s.proximityStore.GetNeighbors(ctx, obj.ID, 5)
            for _, score := range neighbors {
                otherID := score.ObjectB
                if score.ObjectB == obj.ID {
                    otherID = score.ObjectA
                }
                
                // Add proximity boost to existing score
                boosted[otherID] += score.Score * 0.2  // 20% weight
            }
        }
        
        // Merge boosted scores
        results = s.mergeProximityBoost(results, boosted)
    }
    
    return results, nil
}
```

### 4. Import Quality Checks

```go
func (i *Importer) detectClusters(ctx context.Context, objects []*KnowledgeObject) []ImportCluster {
    // Compute pairwise proximity for imported objects
    scores := make([]*ProximityScore, 0)
    for j, objA := range objects {
        for _, objB := range objects[j+1:] {
            score := ComputeProximity(objA, objB)
            if score.Score >= 0.7 {
                scores = append(scores, score)
            }
        }
    }
    
    // Find connected components (clusters)
    clusters := i.findConnectedComponents(objects, scores)
    
    return clusters
}

func (i *Importer) detectOrphans(ctx context.Context, objects []*KnowledgeObject) []string {
    orphans := make([]string, 0)
    
    for _, obj := range objects {
        // Check if object has any high-proximity neighbors in existing KB
        neighbors, _ := i.proximityStore.GetNeighborsAbove(ctx, obj.ID, 0.3)
        if len(neighbors) == 0 {
            orphans = append(orphans, obj.ID)
        }
    }
    
    return orphans
}
```

---

## CLI Commands

```bash
# Show objects similar to a given object
ctxt similar o-abc123 --limit 10

# Show proximity score between two objects
ctxt proximity o-abc123 o-def456

# Find clusters of related objects
ctxt clusters --threshold 0.5

# Find orphan objects (no high-proximity neighbors)
ctxt orphans --threshold 0.3

# Refresh proximity index
ctxt proximity refresh --stale

# Show proximity statistics
ctxt proximity stats

# Rebuild entire proximity index
ctxt proximity rebuild
```

### Output Examples

```bash
$ ctxt similar o-abc123 --limit 5

Objects similar to o-abc123 (Decision: Use PostgreSQL for primary datastore)

Score   ID          Type        Summary
0.87    o-def456    decision    Migrate from MongoDB to PostgreSQL
0.82    o-ghi789    note        Database performance analysis
0.75    o-jkl012    email       Re: Infrastructure planning
0.71    o-mno345    document    Architecture review notes
0.68    o-pqr678    task        Evaluate database options

Factors for o-def456:
  Semantic: 0.45  Temporal: 0.92  Entity: 0.67  Origin: 0.40

$ ctxt clusters --threshold 0.6

Cluster 1 (5 objects) - "Checkout redesign project"
  o-abc123  decision   Use PostgreSQL for primary datastore
  o-def456  decision   Migrate from MongoDB to PostgreSQL
  o-ghi789  note       Database performance analysis
  o-jkl012  email      Re: Infrastructure planning
  o-mno345  document   Architecture review notes

Cluster 2 (3 objects) - "Mobile app redesign"
  o-xxx111  note       Mobile UX review
  o-yyy222  decision   Adopt React Native
  o-zzz333  task       Mobile app wireframes

$ ctxt orphans --threshold 0.3

Orphan objects (no high-proximity neighbors):
  o-orp001  note       Random thought about cats
  o-orp002  url        https://weather.com
  
Tip: Consider tagging or merging orphan objects.
```

---

## API Endpoints

```
GET  /objects/{id}/similar?limit=10&threshold=0.5
GET  /objects/{id}/proximity/{other_id}
GET  /clusters?threshold=0.6
GET  /orphans?threshold=0.3
POST /proximity/refresh
GET  /proximity/stats
```

### Response Format

```json
GET /objects/o-abc123/similar?limit=5

{
  "query_object": "o-abc123",
  "similar": [
    {
      "object_id": "o-def456",
      "type": "decision",
      "summary": "Migrate from MongoDB to PostgreSQL",
      "proximity_score": 0.87,
      "factors": {
        "semantic": 0.45,
        "temporal": 0.92,
        "entity": 0.67,
        "origin": 0.40
      }
    }
  ]
}
```

---

## Configuration

```yaml
proximity:
  enabled: true
  
  computation:
    min_score_threshold: 0.2    # Don't store pairs below this
    max_neighbors: 100          # Max neighbors per object
    comparison_window: 2160h    # 90 days - only compare to recent
    batch_size: 100
    
  staleness:
    ttl: 720h                   # 30 days - recompute after
    refresh_batch: 1000         # Objects per refresh cycle
    
  weights:
    # Override default type weights
    conversation:
      semantic: 0.30
      temporal: 0.30
      entity: 0.25
      origin: 0.15
    document:
      semantic: 0.50
      temporal: 0.10
      entity: 0.25
      origin: 0.15
      
  integration:
    composition_context_limit: 20   # Max neighbors for composition
    search_boost_weight: 0.2        # Weight for search proximity boost
    import_cluster_threshold: 0.7   # Threshold for cluster detection
```

---

## Testing Strategy

### Unit Tests

```go
func TestSemanticProximity(t *testing.T) {
    tests := []struct {
        name     string
        contentA string
        contentB string
        minScore float64
        maxScore float64
    }{
        {"identical", "hello world", "hello world", 0.99, 1.0},
        {"similar", "database migration", "migrating the database", 0.7, 0.95},
        {"unrelated", "database migration", "cooking recipes", 0.0, 0.3},
    }
    // ...
}

func TestTemporalProximity(t *testing.T) {
    now := time.Now()
    tests := []struct {
        name    string
        timeA   time.Time
        timeB   time.Time
        expect  float64
    }{
        {"same_day", now, now.Add(12 * time.Hour), 0.8},
        {"same_week", now, now.Add(5 * 24 * time.Hour), 0.5},
        {"different_year", now, now.Add(-400 * 24 * time.Hour), 0.05},
    }
    // ...
}

func TestEntityProximity(t *testing.T) {
    tests := []struct {
        name       string
        mentionsA  []string
        mentionsB  []string
        expect     float64
    }{
        {"full_overlap", []string{"alice", "bob"}, []string{"alice", "bob"}, 1.0},
        {"partial", []string{"alice", "bob"}, []string{"alice", "carol"}, 0.33},
        {"no_overlap", []string{"alice"}, []string{"bob"}, 0.0},
    }
    // ...
}

func TestAggregatedProximity(t *testing.T) {
    // Test weighted combination
    // Test type-specific weights
    // Test threshold filtering
}
```

### Integration Tests

```go
func TestProximityIndexing(t *testing.T) {
    // Create objects with known relationships
    // Trigger indexing
    // Verify proximity scores stored correctly
}

func TestNeighborRetrieval(t *testing.T) {
    // Create object cluster
    // Query neighbors
    // Verify order and threshold
}

func TestCompositionIntegration(t *testing.T) {
    // Create objects for composition
    // Generate brief
    // Verify high-proximity objects included in context
}

func TestSearchBoost(t *testing.T) {
    // Create related objects
    // Search for one
    // Verify neighbors boosted in results
}
```

### Performance Tests

```go
func BenchmarkProximityComputation(b *testing.B) {
    // Benchmark single pair computation
}

func BenchmarkNeighborQuery(b *testing.B) {
    // Benchmark neighbor retrieval at various scales
}

func BenchmarkBatchIndexing(b *testing.B) {
    // Benchmark indexing 1000 new objects
}
```

---

## Edge Cases

| Case | Handling |
|------|----------|
| Object without embedding | Semantic factor = 0, other factors still computed |
| Object without entities | Entity factor = 0, still indexable |
| Self-proximity | Not stored (object_a < object_b constraint) |
| Circular proximity | Not applicable (undirected graph) |
| Deleted object | Proximity scores cascade deleted |
| Type mismatch | Uses default weights |
| Same object created twice | Deduped via content hash |
| Very old objects | Low temporal proximity, may not meet threshold |

---

## Migration Plan

### Phase 1: Schema + Interface (Day 1)

1. Add `object_proximity` table migration
2. Implement `ProximityStore` interface
3. Add proximity computation functions
4. Unit tests for all factors

### Phase 2: Indexing Pipeline (Day 2)

1. Add `ProximityIndexer` service
2. Hook into object creation events
3. Hook into entity update events
4. Background refresh job

### Phase 3: Query Interface (Day 2)

1. Implement `GetSimilarObjects`
2. Implement `GetProximity`
3. Implement `FindClusters`
4. CLI commands

### Phase 4: Integration (Day 3)

1. Composition engine integration
2. Citation prioritization
3. Search boost (optional)
4. Import quality checks

### Phase 5: Testing + Polish (Day 3)

1. Integration tests
2. Performance tests
3. Documentation updates
4. API endpoints

---

## Open Questions

1. **Behavioral factor:** Should we implement access log tracking now or defer?
   - **Recommendation:** Defer to Phase 2. Storage overhead for logs, need to define session semantics.

2. **Graph distance bonus:** Should entity proximity include graph traversal (e.g., entity A → entity B → entity C)?
   - **Recommendation:** Defer. Computationally expensive, unclear value.

3. **Real-time updates:** Should proximity update immediately on edit, or batch?
   - **Recommendation:** Batch via background job. Immediate updates too expensive for frequent edits.

4. **Cross-type proximity:** Should we compute proximity between different types (e.g., decision ↔ email)?
   - **Recommendation:** Yes, use default weights. Valuable for context assembly.

---

## Success Metrics

| Metric | Target |
|--------|--------|
| Composition context relevance | 80% of included objects rated relevant |
| Citation accuracy | 90% of citations trace to highest-proximity source |
| Orphan detection | 95% of true orphans flagged |
| Query latency (neighbors) | <10ms for 100 neighbors |
| Storage overhead | <10% of total DB size |
| Index freshness | 95% of scores < 30 days old |

---

## References

- [ADR-016: Just-In-Time Surfacing](../decisions/ADR-016-just-in-time-surfacing.md)
- [ranking-and-reranking.md](../dpkms/ranking-and-reranking.md)
- [embeddings.md](../dpkms/embeddings.md)
- [knowledge-graph.md](../dpkms/knowledge-graph.md)
- [US-0022: Generate Brief from Objects](../stories/composition/US-0022-generate-brief-from-objects.md)
- [2026-02-18-inline-citations-plan.md](./2026-02-18-inline-citations-plan.md)
