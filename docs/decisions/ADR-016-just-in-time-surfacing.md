# ADR-016 – Just-In-Time Surfacing and Proactive Knowledge Resurfacing

> **Status:** Accepted
> **Date:** 2026-01-26
> **Author:** @jadb
> **Applies to:** ctxt
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

Knowledge systems face a fundamental paradox: the more you capture, the harder it becomes to remember what you know. Users experience:

**Knowledge Burial:**
- Valuable insights captured but never revisited
- Notes become "write-only" archives
- Context from weeks/months ago forgotten
- Connections between past and current work missed

**Reactive-Only Retrieval:**
- Users must remember to search for relevant knowledge
- Search requires formulating precise queries
- No system assistance in connecting current work to past insights
- Serendipitous discovery relies on chance

**Context Gaps:**
- Starting new projects without awareness of related prior work
- Repeating research or decisions already documented
- Missing opportunities to reuse existing analysis
- Lost leverage from accumulated knowledge

**Noise vs Signal:**
- Aggressive resurfacing creates notification fatigue
- Too quiet = knowledge stays buried
- One-size-fits-all timing misses context windows
- No personalization for relevance thresholds

**Constraints:**
- Must remain privacy-preserving (local-first, no tracking)
- Must respect user attention (no spam, high precision)
- Must be context-aware (profile-scoped, time-bounded)
- Must be explainable (user understands why something surfaced)
- Must support opt-out (users control resurfacing behavior)
- Must not require manual rule creation

**Affected Subsystems:**
- Query engine (relevance scoring)
- Graph traversal (entity/mention-based discovery)
- Focus profiles (context-aware filtering)
- Notification system (delivery mechanisms)
- Ranking algorithm (recency + relevance balance)
- CLI/TUI interfaces (surfacing display)

**Goals:**
- Surface relevant knowledge just-in-time for current work
- Reduce cognitive load of remembering to search
- Enable serendipitous discovery without noise
- Make captured knowledge evergreen through revisiting
- Maintain user trust through explainability and control

---

## Decision

**`ctxt` will implement a Just-In-Time Surfacing Engine that proactively identifies and surfaces relevant knowledge based on current context, activity patterns, entity mentions, time windows, and focus profiles, without requiring explicit user queries.**

The surfacing engine operates through:

1. **Trigger-Based Discovery:**
   ```yaml
   triggers:
     entity_mention:
       # When working with entities in current session
       - watching: active_entities
       - match_strategy: exact | related | transitive
       - lookback_window: 30d

     tag_pattern:
       # When content matches tag patterns
       - watching: profile_boost_tags
       - threshold: 0.7
       - lookback_window: 90d

     time_based:
       # Periodic review opportunities
       - pattern: daily | weekly | monthly
       - time_window: morning | evening
       - max_items: 5

     project_context:
       # When profile changes or work context shifts
       - on_profile_switch: true
       - related_to: profile_entities
       - max_items: 10
   ```

2. **Relevance Scoring Model:**
   ```
   relevance_score =
     entity_match_weight * entity_match_score +
     tag_overlap_weight * tag_overlap_score +
     recency_weight * recency_score +
     graph_proximity_weight * graph_distance_score +
     past_interaction_weight * interaction_score +
     profile_boost_weight * profile_relevance_score
   ```

3. **Surfacing Modes:**

   **Active Mode (High Frequency):**
   - Triggered by explicit work context (profile switch, entity mentions)
   - Surfaces 3-10 items immediately
   - High precision threshold (score > 0.8)

   **Ambient Mode (Background):**
   - Periodic evaluation (hourly/daily based on activity)
   - Surfaces 1-3 items at optimal times
   - Lower threshold (score > 0.6) for serendipity

   **Review Mode (Scheduled):**
   - Weekly/monthly knowledge review prompts
   - Surfaces stale but valuable items (not visited in 30d+)
   - Encourages evergreen maintenance

4. **Surfacing Channels:**
   - CLI: `ctxt surfaced` command
   - TUI: Dedicated surfacing panel
   - Notifications: OS notifications (opt-in)
   - Session start: On `ctxt` invocation (if items waiting)
   - Profile switch: Context-relevant items on profile activation

5. **User Control Mechanisms:**
   ```yaml
   surfacing:
     enabled: true
     mode: active | ambient | review | off

     thresholds:
       active_mode_score: 0.8
       ambient_mode_score: 0.6
       review_mode_score: 0.5

     timing:
       active_triggers: [profile_switch, entity_mention]
       ambient_interval: hourly | daily | weekly
       review_schedule: weekly
       quiet_hours: [22:00, 08:00]

     limits:
       max_active_items: 10
       max_ambient_items: 3
       max_review_items: 20
       min_interval_between_surfaces: 300s

     feedback:
       track_interactions: true  # for learning
       allow_dismiss: true
       allow_snooze: true
       snooze_duration: 1d | 1w | 1m
   ```

6. **Explainability:**
   Each surfaced item includes reasoning:
   ```json
   {
     "object_id": "uuid",
     "surfaced_at": "timestamp",
     "reason": {
       "primary": "entity_match",
       "entity": "@api.stripe",
       "explanation": "You're currently working with @api.stripe and this note documents integration patterns from 3 weeks ago.",
       "score": 0.85,
       "factors": {
         "entity_match": 0.9,
         "recency": 0.7,
         "profile_relevance": 0.95
       }
     }
   }
   ```

7. **Privacy Guarantees:**
   - All scoring happens locally
   - No external tracking or analytics
   - Interaction data stays on device
   - User can disable feedback tracking
   - Export includes surfacing history (optional)

---

## Rationale

### Alternatives Considered

#### 1. **Manual Tagging for Review (Rejected)**
Require users to tag items for future review (#review, #follow-up).

**Rejected because:**
- High friction at capture time
- Users forget to tag consistently
- No intelligence about when to surface
- Doesn't leverage context or graph connections
- Shifts burden entirely to user

#### 2. **Daily Digest Email (Rejected)**
Send daily email with random or recent items.

**Rejected because:**
- Not context-aware
- Adds email noise
- Fixed timing doesn't match work patterns
- No connection to current activity
- External dependency (email server)

#### 3. **Search Query Suggestions (Rejected)**
Suggest queries based on recent activity.

**Rejected because:**
- Still requires user to execute search
- Doesn't surface actual content
- Adds friction to discovery
- Users ignore suggestions (banner blindness)

#### 4. **Aggressive Notifications for All Matches (Rejected)**
Surface all potentially relevant items immediately.

**Rejected because:**
- Creates notification fatigue
- Trains users to ignore surfacing
- No precision control
- Overwhelming volume

#### 5. **ML-Based Attention Prediction (Rejected)**
Use ML to predict optimal surfacing times based on behavior.

**Rejected because:**
- Complexity and unpredictability
- Privacy concerns (behavior tracking)
- Requires training data
- Not deterministic or explainable
- Overkill for local-first system

### Benefits of Chosen Approach

**Proactive Discovery:**
- System brings knowledge to user, not vice versa
- Reduces cognitive load of remembering to search
- Enables serendipitous connections

**Context-Aware:**
- Leverages entities, profiles, graph structure
- Timing matches actual work patterns
- Respects user's current focus

**User-Controlled:**
- Multiple modes for different preferences
- Granular threshold controls
- Easy opt-out at any level
- Feedback improves over time

**Explainable:**
- Every surfacing includes reasoning
- User understands relevance factors
- Builds trust through transparency

**Privacy-Preserving:**
- Fully local computation
- No external dependencies
- Optional interaction tracking

**Extensible:**
- Plugins can add surfacing triggers
- Custom scoring factors possible
- New channels easily added

### Drawbacks / Risks

**Complexity:**
- Scoring algorithm has many parameters
- Profile integration adds dependencies
- Multiple modes increase configuration surface

**Tuning Difficulty:**
- Finding right thresholds per user
- Balancing precision vs recall
- Avoiding notification fatigue

**Computational Cost:**
- Periodic background evaluation
- Graph traversal for related items
- Scoring all candidates can be expensive

**False Positives:**
- Irrelevant surfacing trains users to ignore
- Hard to recover from early bad impressions
- Threshold tuning takes time

---

## Consequences

### Positive

**Evergreen Knowledge:**
- Captured insights revisited automatically
- Knowledge compounds through reconnection
- Reduces "write-only" archive problem

**Reduced Cognitive Load:**
- System remembers for user
- No need to formulate search queries
- Context shifts trigger relevant knowledge

**Serendipitous Discovery:**
- Connections emerge without explicit search
- Graph structure enables lateral discovery
- Time-shifted insights resurface naturally

**Increased Value:**
- ROI on capture effort improves
- Past work informs present decisions
- Knowledge reuse becomes frictionless

### Negative

**Implementation Complexity:**
- Scoring algorithm requires tuning
- Background evaluation adds daemon complexity
- Interaction tracking adds storage overhead

**User Experience Risk:**
- Poor tuning creates notification fatigue
- Irrelevant surfacing undermines trust
- Learning curve for configuration

**Performance Impact:**
- Background scoring consumes CPU
- Graph traversal can be expensive
- Scales with knowledge base size

**Privacy Perception:**
- Users may be wary of "watching" behavior
- Interaction tracking requires clear communication
- Opt-out must be obvious and easy

### Neutral / Considerations

**Interaction Tracking:**
- Improves relevance over time
- Must be optional and transparent
- Stored locally, never shared
- Can be disabled per profile

**Threshold Evolution:**
- Defaults work for most users
- Power users can fine-tune
- Plugins can suggest adjustments
- Future: adaptive thresholds per user

**Surfacing Fatigue:**
- Monitor dismissal rates
- Auto-adjust if dismissals spike
- Provide feedback UI for tuning
- Respect quiet hours

**Cross-Profile Surfacing:**
- Should items from other profiles surface?
- Configurable per profile
- Default: only current profile
- Override for serendipity

---

## Implementation Notes

### Core Components

**Surfacing Engine (`ctxt/surfacing/`):**
- Trigger evaluation loop
- Relevance scoring
- Candidate filtering
- Timing logic
- Interaction tracking

**Scoring Algorithm:**
```go
type SurfacingCandidate struct {
    ObjectID   string
    Score      float64
    Factors    map[string]float64
    Reason     SurfacingReason
    SurfacedAt time.Time
}

type SurfacingReason struct {
    Primary     string // entity_match, tag_overlap, time_window
    Entity      string // which entity triggered
    Explanation string // human-readable
}

func ScoreCandidate(
    obj Object,
    currentContext Context,
    profile Profile,
    weights ScoringWeights,
) SurfacingCandidate
```

**Trigger Evaluators:**
```go
type Trigger interface {
    Name() string
    Evaluate(ctx Context) []ObjectID
    Frequency() time.Duration
}

// Built-in triggers
type EntityMentionTrigger struct { ... }
type TagPatternTrigger struct { ... }
type TimeBasedTrigger struct { ... }
type ProfileSwitchTrigger struct { ... }
```

**CLI Commands:**
```bash
# View surfaced items
ctxt surfaced
ctxt surfaced --mode active
ctxt surfaced --explain

# Interact with surfacing
ctxt surfaced dismiss <id>
ctxt surfaced snooze <id> --duration 1w
ctxt surfaced open <id>

# Configure surfacing
ctxt surfacing config
ctxt surfacing tune --threshold 0.75
ctxt surfacing disable
ctxt surfacing enable --mode ambient
```

**Storage Schema:**

Add to objects table:
```sql
-- Surfacing metadata (optional)
surfacing_metadata: {
  "last_surfaced_at": "timestamp",
  "surface_count": int,
  "last_interaction": "timestamp",
  "interaction_type": "open" | "dismiss" | "snooze",
  "snooze_until": "timestamp",
  "manual_boost": float  // user can manually boost/suppress
}
```

Surfacing history table:
```sql
CREATE TABLE surfacing_history (
    id UUID PRIMARY KEY,
    object_id UUID NOT NULL,
    surfaced_at TIMESTAMP NOT NULL,
    reason_type TEXT NOT NULL,
    reason_entity TEXT,
    reason_explanation TEXT,
    score FLOAT NOT NULL,
    factors JSON,
    profile TEXT,
    interaction_type TEXT,  -- open, dismiss, snooze, none
    interaction_at TIMESTAMP,
    INDEX(object_id),
    INDEX(surfaced_at),
    INDEX(profile)
);
```

### Integration Points

**With Query Engine:**
1. Surfacing engine queries dPKMS for candidates
2. Filters based on profile scope
3. Retrieves graph neighbors for scoring
4. Returns scored candidates to `ctxt`

**With Focus Profiles:**
1. Profile switch triggers active surfacing
2. Profile boost/suppress tags affect scoring
3. Profile entities influence entity_match scoring
4. Profile limits cap surfaced items

**With Graph System:**
1. Entity mentions in current session tracked
2. Graph traversal finds related objects
3. Backlinks inform proximity scoring
4. Transitive relationships enable lateral discovery

**With Notification System:**
1. Surfacing engine pushes to notification queue
2. Notifications respect profile and global limits
3. User can configure per-channel delivery
4. Plugins can add custom notification handlers

### Migration Strategy

**Phase 1: Basic Surfacing (Skeleton 7)**
- Entity mention triggers
- Simple scoring (entity + recency)
- CLI command only
- No interaction tracking

**Phase 2: Context-Aware (Skeleton 7+)**
- Profile integration
- Tag pattern triggers
- Time-based review mode
- Basic interaction tracking

**Phase 3: Advanced Features (Skeleton 8+)**
- Adaptive scoring weights
- Ambient mode
- Multi-channel delivery
- Feedback-based learning

**Backward Compatibility:**
- Surfacing disabled by default (opt-in)
- No schema changes to existing objects
- History table optional
- Works without profile system

### Testing Requirements

**Unit Tests:**
- Scoring algorithm correctness
- Trigger evaluation logic
- Threshold filtering
- Reason generation

**Integration Tests:**
- Profile-scoped surfacing
- Entity mention detection
- Graph-based related items
- Interaction tracking

**User Acceptance Tests:**
- Surfacing relevance (manual validation)
- Notification timing appropriateness
- Dismissal and snooze workflows
- Configuration usability

**Performance Tests:**
- Scoring 10k candidates in <1s
- Background evaluation overhead <5% CPU
- Graph traversal performance
- Query optimization for candidates

### Performance Optimization

**Caching:**
- Cache active profile entities
- Cache recent candidate scores (TTL 5m)
- Precompute graph proximity for frequent entities

**Lazy Evaluation:**
- Only evaluate triggers when activity detected
- Skip scoring if no matches above threshold
- Batch candidate retrieval

**Incremental Updates:**
- Track entity mentions in session
- Only rescore on context change
- Avoid full rescoring on every check

**Indexing:**
- Index objects by entity mentions
- Index by last_surfaced_at
- Composite index on (profile, surfaced_at)

---

## References

- **architecture.md:269-273** – Just-In-Time Surfacing specification
- **architecture.md:290-293** – Personal Relevance Model
- **ROADMAP.md** – Skeleton 7: Profiles + Just-in-Time Context
- ADR-015 – Focus Profiles (profile integration)
- ADR-013 – Knowledge Graph and Mentions (entity-based surfacing)
- ADR-003 – Separate Read/Write Paths (surfacing is read-only)

**Related Documents:**
- `ctxt/surfacing/` – Surfacing engine implementation (to be created)
- `ctxt/notifications/` – Notification delivery system (to be created)
- `ctxt/profiles/` – Profile integration (ADR-015)

---
