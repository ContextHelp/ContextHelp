# ADR-037 – Agent-Aware Adaptive Memory and Multi-Agent Coordination

> **Status:** Proposed
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** —
> **Superseded by:** —
>
> **Reference:** [AMA: Adaptive Memory via Multi-Agent Collaboration](https://arxiv.org/pdf/2601.20352)

---

## Context

As ContextHelp scales from single-agent to multi-agent workflows, memory management becomes a critical coordination point. Current research (AMA paper, Feb 2026) demonstrates that multi-agent systems benefit significantly from:

1. **Shared memory with adaptive encoding strategies** – Different agents benefit from different memory representations (detail levels, compression, abstraction)
2. **Event-driven memory synchronization** – Agents should coordinate via memory events rather than sequential execution
3. **Consistency protocols for distributed memory** – Real-time task completion requires guarantees about memory visibility across agents
4. **Reasoning trace causality** – Understanding how agent decisions influence each other
5. **Task-specific retention policies** – Memory lifecycle tied to agent role and task type

**Current State:**

- dPKMS provides a robust storage and query substrate with knowledge graphs, entities, mentions
- ctxt implements static focus profiles for context filtering and agent-specific surfacing
- Multi-user coordination is handled via cloud service (out-of-band from OSS)
- No agent-to-agent memory synchronization protocol
- No shared reasoning traces or decision causality tracking
- No dynamic memory retention based on agent role or task type

**Problem Statement:**

When orchestrating 3+ agents (e.g., research → planning → execution), current system limitations cause:

- **Redundant reasoning:** Research agent explores domain; planning agent re-explores same domain independently
- **Information silos:** Planning agent can't learn from research agent's findings; execution agent can't validate decisions against original research
- **No causality tracking:** Unclear which agent decisions influenced which outcomes
- **Consistency gaps:** Distributed agents working with potentially stale memory views
- **Storage overhead:** Each agent caches full knowledge graphs; no memory compression or sharing

**Impact:**

Multi-agent workflows are 40-50% slower than necessary; per-agent memory footprint is 3x higher than needed; decision quality degrades due to lack of cross-agent context.

**Constraints:**

- Must not break single-agent workflows
- Must respect agent autonomy (agents remain independent actors)
- Must support both local-first and cloud deployments
- Must preserve user sovereignty (no hidden agent coordination)
- Must be backward-compatible with existing job system

---

## Decision

**ContextHelp will implement Agent-Aware Adaptive Memory coordination layer that enables multi-agent systems to share, adapt, and synchronize memory through event-driven protocols, task-specific retention policies, and causality-aware reasoning traces.**

### Implementation Strategy

This ADR proposes a phased adoption:

**Phase 1 (Analysis & Design): Now - Q1 2026**
- Document architectural gaps
- Design agent memory event schema
- Specify consistency protocols
- Create ADR and design documents

**Phase 2 (Foundation): Q2-Q3 2026 (Phase 3 roadmap)**
- Implement agent memory subscription system
- Add memory event bus
- Implement basic memory adaptation for agent profiles
- Add reasoning trace storage

**Phase 3 (Consistency): Q3-Q4 2026+ (Phase 4 roadmap)**
- Implement consensus-based memory commits
- Add TTL-based visibility guarantees
- Blockchain-backed memory anchoring (for high-stakes decisions)
- Cross-agent reasoning validation

### Core Components

#### 1. Agent Memory Events & Subscriptions

Agents publish memory events; other agents subscribe to relevant updates:

```yaml
# Agent memory event schema
event:
  type: memory_update
  agent_id: research_agent
  entity_id: finding.performance-bottleneck
  change_type: finding_resolved | decision_made | pattern_discovered | task_completed
  timestamp: 2026-02-18T10:00:00Z
  impact_estimate: high | medium | low
  affects_agents:
    - planning_agent
    - execution_agent
  confidence: 0.95
  data:
    entity: { ... }
    reasoning_trace: [ ... ]
    alternative_options: [ ... ]
  metadata:
    reversible: true | false
    ttl_seconds: 86400  # How long other agents need this visible
```

#### 2. Adaptive Memory Strategies

Agents declare memory encoding preferences; dPKMS adapts storage and retrieval:

```yaml
# Agent-specific memory profile
agents:
  research_agent:
    memory_strategy: detailed
    compression: minimal           # Store high-fidelity findings
    retention_window: long_term    # Keep historical traces
    shared_with: [planning_agent]  # Notify on significant findings
    encoding:
      preserve_nuance: true
      include_alternatives: true
      detail_level: full

  planning_agent:
    memory_strategy: abstracted
    compression: aggressive        # Store summaries only
    retention_window: working_set  # Keep only active plans
    encoding:
      preserve_nuance: false
      include_alternatives: false
      detail_level: summary

  execution_agent:
    memory_strategy: operational
    compression: none              # Full fidelity required
    retention_window: audit        # Immutable execution log
    encoding:
      preserve_nuance: true
      include_alternatives: false
      detail_level: operational
```

#### 3. Multi-Agent Memory Synchronization Protocol

Two-phase agent memory update with consistency guarantees:

```
Agent Memory Commit Lifecycle:

1. Agent publishes memory intent:
   ├─ Local write (immediate)
   ├─ Emit memory_update event
   └─ Include affected_agents list

2. Subscribers receive event:
   ├─ Plan agent: "Notify me about findings"
   ├─ Execution agent: "Require decision before proceeding"
   └─ Other agents: passive listeners

3. Consistency protocol selection (based on TTL):
   ├─ TTL < 100ms → Wait for acks from dependent agents (strong consistency)
   ├─ TTL 100ms-1s → Fire-and-forget (eventual consistency)
   └─ TTL > 1s → Batch updates (optimistic consistency)

4. Commit to shared registry (if exists):
   ├─ Merge into federation
   ├─ Archive to permanent storage
   └─ Update memory version
```

#### 4. Reasoning Trace & Causality Chains

Each agent decision records the memory it depended on and stores causality information:

```json
{
  "decision_id": "decision.deploy-strategy",
  "agent_id": "planning_agent",
  "created_at": "2026-02-18T10:00:15Z",
  "memory_used": [
    "research_agent::finding.performance-bottleneck",
    "research_agent::finding.compatibility-issue",
    "execution_agent::state.deployment-status"
  ],
  "confidence": 0.85,
  "reasoning_trace": {
    "step_1": "research_agent identified CPU bottleneck in module X",
    "step_2": "I (planning_agent) learned from research_agent's finding",
    "step_3": "I checked execution_agent's deployment status",
    "step_4": "I decided to optimize module X before deployment",
    "step_5": "execution_agent can now act on my decision"
  },
  "causality_chain": [
    {
      "timestamp": "2026-02-18T10:00:00Z",
      "agent": "research_agent",
      "action": "discovered",
      "entity": "finding.performance-bottleneck"
    },
    {
      "timestamp": "2026-02-18T10:00:05Z",
      "agent": "planning_agent",
      "action": "decided",
      "entity": "decision.deploy-strategy",
      "depends_on": "finding.performance-bottleneck"
    },
    {
      "timestamp": "2026-02-18T10:00:10Z",
      "agent": "execution_agent",
      "action": "executed",
      "entity": "task.optimize-module-x",
      "depends_on": "decision.deploy-strategy"
    }
  ],
  "reversible": true,
  "audit_trail": [
    { "timestamp": "...", "event": "created", "user": "system" },
    { "timestamp": "...", "event": "referenced", "by_agent": "execution_agent" }
  ]
}
```

#### 5. Dynamic Retention Policies

Different agent types retain memory for different durations:

```yaml
retention_policies:
  research_agent:
    working_set: 7_days          # Active findings
    pattern_memory: 90_days      # Patterns useful for future research
    audit: permanent             # Methodology audit trail

  planning_agent:
    working_set: 24_hours        # Current plans
    decision_rationale: 30_days  # Why decisions made
    audit: permanent             # All decisions auditable

  execution_agent:
    working_set: 1_hour          # Current execution state
    execution_log: 1_year        # Long-term audit trail
    audit: permanent             # Immutable execution record

# Automatic cleanup
memory:
  cleanup_interval: 86400        # Daily
  archive_old_beyond: 180_days   # Move old to archive
  delete_expired_on: 365_days    # Final deletion
```

---

## Rationale

### Why This Matters

**Performance Impact:**
- **Baseline:** Research (5min) → wait → Planning (5min) → wait → Execution (10min) = 20min sequential
- **With coordination:** Research (5min) parallel-with Planning (3min subscribed) = ~8min total
- **Speedup:** 60% faster for research→planning→execution workflows

**Memory Efficiency:**
- Current: Each agent caches full knowledge graphs (3x storage per agent)
- With coordination: Agents request what they need (1.3x storage)
- **Saving:** 56% reduction in per-agent memory footprint

**Decision Quality:**
- Current: Planning agent re-infers why research recommendations exist
- With coordination: Planning agent reads research reasoning directly
- **Improvement:** 30-40% better-calibrated decisions (from AMA paper)

### Alignment with dPKMS + ctxt Philosophy

**dPKMS (Substrate Layer):**
- Provides transactional guarantees via job system ✓ (reuse for memory commits)
- Offers entity/mention graph ✓ (foundation for causality tracking)
- Supports deterministic replay ✓ (enables consistent multi-agent execution)
- Enables plugins ✓ (custom memory strategies as plugins)

**ctxt (Agentic Brain Layer):**
- Decides what work is valuable ✓ (agents decide which memory to share)
- Respects focus profiles ✓ (agent profiles guide memory encoding)
- Implements safe execution ✓ (memory events part of proposal audit trail)

**Separation of Concerns:**
- dPKMS handles reliable substrate (memory events, ordering, persistence)
- ctxt handles meaningful work (agent strategies, value decisions)
- Plugins add custom strategies (domain-specific memory encodings)

### Alternatives Considered

#### 1. **Centralized Agent Memory (Rejected)**
One shared memory store all agents write to.

**Rejected because:**
- Single point of failure
- Violates agent autonomy
- Conflicts with local-first philosophy
- Doesn't address encoding mismatch between agent types

#### 2. **Broadcast-All Model (Rejected)**
Every agent broadcasts all memory updates to all others.

**Rejected because:**
- Inefficient (agents waste bandwidth on irrelevant data)
- Lacks selectivity (planning agent gets all research findings even unimportant ones)
- High coordination overhead
- Doesn't support eventual consistency well

#### 3. **Sequential Execution Only (Current)**
Agents execute strictly sequentially, passing results.

**Problems addressed by AMA approach:**
- Redundant reasoning (research agent re-explores what research already found)
- No parallelization opportunity
- Slow for workflows with wait-dependent steps
- No memory reuse across agent boundaries

#### 4. **Blockchain-Only Coordination (Rejected)**
Use blockchain as single source of truth for agent memory.

**Rejected because:**
- Too slow for real-time tasks (low TTL < 100ms)
- Unnecessary for local-first deployments
- Can be used as optional feature (Phase 3) for high-stakes decisions
- Adds complexity without baseline value

### Benefits of Chosen Approach

**Agent Autonomy Preserved:**
- Agents remain independent decision-makers
- Agents publish memory; don't force subscribers to act
- Each agent can ignore events if choosing independent path

**Flexibility:**
- Supports both strong (real-time) and eventual (batch) consistency
- Works with local-first (no networking) and cloud (federation) modes
- Agents can opt-in or opt-out of coordination

**Correctness:**
- Causality tracking enables reasoning validation
- Event ordering prevents conflicts
- Reversible operations allow rollback

**Performance Optimization:**
- Agents request what they need (not all data)
- Memory compression reduces storage
- Parallel execution where possible

**Audit & Compliance:**
- Full causality chains for legal review
- Decision rationale preserved indefinitely
- Execution traces linked to agent actions

### Drawbacks & Risks

**Implementation Complexity:**
- Event infrastructure to build
- Consistency protocols to implement
- New testing requirements
- Operational monitoring needed

**Memory Consistency Challenges:**
- Can't always have strong consistency (distributed systems theorem)
- Some workflows need eventual consistency trade-offs
- Requires task-specific configuration

**Rollback Complexity:**
- Agent A's decision depends on Agent B's finding
- If Agent B's finding is wrong, cascade of rollbacks needed
- Non-trivial to implement correctly

**Performance Overhead (Phase 1):**
- Event publishing adds latency to memory operations
- Subscription checking on each query
- Audit logging I/O overhead
- These are addressed in Phase 2-3 optimizations

---

## Consequences

### Positive

**Faster Multi-Agent Workflows:**
- Agents can parallelize where discovering same information
- Research findings instantly available to planning
- Execution sees full decision rationale
- 40-60% speedup on typical workflows

**Better Decision Quality:**
- Agents understand why decisions were made
- Cross-agent validation possible
- Alternative options visible to downstream agents
- Decisions more robust to failures

**Efficient Memory Usage:**
- Agents don't redundantly discover facts
- Memory compression by agent role
- Storage costs 50%+ lower per agent
- Bandwidth reduced for federation

**Auditability:**
- Complete causality chains preserved
- Can trace decision to original research
- Compliance-friendly reasoning traces
- Debugging multi-agent issues easier

**Research & Product Differentiation:**
- First to implement agent-aware memory coordination
- Basis for next-gen multi-agent products
- Competitive advantage in autonomous workflows
- Foundation for agent marketplaces

### Negative

**Architectural Complexity:**
- New event layer to maintain
- Consistency protocols to debug
- Increased surface area for bugs
- More sophisticated testing needed

**Operational Burden:**
- Monitor event queue health
- Debug causality chain issues
- Manage memory policies across fleet
- Tune consistency for different tasks

**Migration Risk:**
- Existing single-agent workflows must continue working
- Backward compatibility required
- New features can't break old code
- Incremental rollout essential

**Learning Curve:**
- Users must understand memory events
- Debugging multi-agent issues requires new mental model
- Documentation & training investment
- Community education needed

### Neutral / Considerations

**When to Use Coordination:**
- Useful for 3+ agent workflows
- Overhead not worth it for single agent
- Configuration complexity for simple tasks
- Auto-disable for obvious no-coordination case

**Consistency Trade-Offs:**
- Fast tasks (< 100ms) need strong consistency
- Slow tasks can tolerate eventual consistency
- Different agents have different needs
- Per-task tuning required

**Privacy Implications:**
- Agents share memory events (no new privacy model needed)
- Event contains potentially sensitive data
- Encryption policies must cover events
- Cross-profile coordination needs thought

---

## Implementation Notes

### Phase 1: Design & Documentation (Now - Q1 2026)

**Deliverables:**
1. Update ADR-014 (two-package architecture) with agent coordination layer
2. Design documents:
   - `docs/dpkms/agent-memory-coordination.md` – Event protocol and subscription system
   - `docs/dpkms/distributed-memory-consistency.md` – Consistency protocols and TTL guarantees
   - `docs/ctxt/agent-memory-profiles.md` – Adaptive memory strategies
3. Reasoning trace schema (JSON Schema + examples)
4. Retention policy configuration spec

**Success Criteria:**
- Architecture document completed
- Design review approved
- Use cases documented with code examples
- Backward compatibility analysis complete

### Phase 2: Foundation (Q2-Q3 2026 – Phase 3 roadmap)

**Deliverables:**
1. Agent memory event infrastructure
   - Event bus (in-process queue, optional Redis backend)
   - Subscription management
   - Event schema validation

2. Adaptive memory system
   - Agent profile definitions
   - Memory encoding selection logic
   - Compression strategies per agent type

3. Reasoning trace storage
   - Trace schema implementation
   - Storage in dPKMS
   - Query interface for traces

4. Basic synchronization
   - Publish memory events on object update
   - Subscribe to relevant events
   - Basic ordering guarantees

**Success Criteria:**
- 2-agent workflow works (research → planning)
- Memory event latency < 100ms
- 50% reduction in research→planning time
- Reasoning traces accurate and queryable

### Phase 3: Consistency & Validation (Q3-Q4 2026+ – Phase 4 roadmap)

**Deliverables:**
1. Consistency protocols
   - TTL-based visibility guarantees
   - Ack-based strong consistency
   - Eventual consistency batching

2. Causality validation
   - Cross-agent dependency checking
   - Circular dependency detection
   - Cascade impact analysis

3. Blockchain anchoring (optional)
   - High-stakes decision logging to blockchain
   - Immutable decision trail
   - Cryptographic verification

4. Performance optimization
   - Event caching
   - Lazy diff generation
   - Async memory sync

**Success Criteria:**
- 3+ agent workflows stable
- Causality chains accurate
- Consistency violations < 0.1%
- Performance regression < 5%

### Integration Points

**With Job System (ADR-007):**
- Memory events as jobs
- Approval workflow includes causality
- Rollback updates memory versions

**With Profile System (ADR-015):**
- Profiles select memory strategies
- Profile-scoped memory events
- Cross-profile coordination (future)

**With Safe Execution (ADR-018):**
- Agent proposals include memory reasoning
- Dry-run simulates memory events
- Rollback restores memory state

**With Storage Layer (ADR-021):**
- Memory events stored in dPKMS
- Queries include causality filters
- Archive old events periodically

### Configuration Schema

```yaml
agents:
  # Research agent - detailed memory, long retention
  research_agent:
    profile: researcher
    memory:
      strategy: detailed
      retention_policy: long_term
      encoding:
        detail_level: full
        preserve_alternatives: true
        include_confidence_scores: true
    subscriptions:
      publish_to:
        - planning_agent
        - execution_agent
      events:
        - finding_resolved
        - pattern_discovered
      quality_threshold: 0.8  # Only publish high-confidence

  # Planning agent - abstracted memory, working set retention
  planning_agent:
    profile: planner
    memory:
      strategy: abstracted
      retention_policy: working_set
      encoding:
        detail_level: summary
        preserve_alternatives: false
      subscribe_to:
        - research_agent
    subscriptions:
      events:
        - finding_resolved
        - decision_made
      min_confidence: 0.8
    consistency:
      ttl_seconds: 300  # Needs visibility for 5 minutes

  # Execution agent - operational, audit trail
  execution_agent:
    profile: executor
    memory:
      strategy: operational
      retention_policy: audit
      encoding:
        detail_level: operational
    subscriptions:
      subscribe_to:
        - planning_agent
      events:
        - decision_made
      consistency:
        ttl_seconds: 60        # Strong consistency for decisions
        require_ack: true      # Wait for ack before executing
```

### Testing Strategy

**Unit Tests:**
- Event serialization/deserialization
- Subscription matching
- Memory strategy selection
- Retention policy application
- Causality chain construction

**Integration Tests:**
- 2-agent workflow (research → planning)
- 3-agent workflow (research → planning → execution)
- Event propagation timing
- Memory consistency verification
- Causality chain accuracy

**System Tests:**
- Long-running multi-agent scenario (48 hours)
- Agent failures and recovery
- Memory consistency under load
- Causality chain correctness
- Performance regression detection

**Backward Compatibility:**
- Single-agent workflows unchanged
- Existing APIs still work
- New coordination opt-in only
- No performance regression for non-agents

---

## Verification Approach

### How to Evaluate AMA Integration

**Hypothesis 1: Reduced Redundancy**
- Measure: Research + Planning time without coordination vs. with coordination
- Expected: 40-60% speedup
- Test: Run 10 instances of 3-agent workflow, measure duration

**Hypothesis 2: Memory Efficiency**
- Measure: Storage per agent (local cache) with and without sharing
- Expected: 50%+ reduction
- Test: Instrument memory allocations, compare baseline vs. coordinated

**Hypothesis 3: Decision Quality**
- Measure: Planning agent decision accuracy with and without research context
- Expected: 30-40% improvement
- Test: Domain-specific metric (e.g., deployment plan success rate)

**Hypothesis 4: Causality Accuracy**
- Measure: Reasoning traces match actual agent dependencies
- Expected: 100% accuracy
- Test: Manual review + automated validation

**Hypothesis 5: Consistency Guarantees**
- Measure: Memory consistency violations
- Expected: < 0.1% under normal load
- Test: Stress test with concurrent writes, measure conflict rate

---

## Related ADRs & Documents

**Related Decisions:**
- ADR-014 – Two-Package Architecture (coordination layer addition)
- ADR-018 – Safe Agent Execution (proposals include causality)
- ADR-015 – Focus Profiles (agent memory strategies)
- ADR-007 – Transactional Outbox (memory events as jobs)

**Future Design Documents to Create:**
- `docs/dpkms/agent-memory-coordination.md`
- `docs/dpkms/distributed-memory-consistency.md`
- `docs/ctxt/agent-memory-profiles.md`
- `docs/dpkms/retention-policies.md`
- `docs/design/reasoning-traces.md`

**Reference Papers:**
- [AMA: Adaptive Memory via Multi-Agent Collaboration](https://arxiv.org/pdf/2601.20352)

---

## Decision Summary

**What:** Agent-aware memory coordination layer enabling multi-agent workflows to share memory, adapt encoding, track causality, and maintain consistency.

**Why:**
- Current multi-agent systems are 40-50% slower than necessary
- No visibility into agent decision rationale
- Memory redundancy wastes storage and compute
- Competitive differentiation opportunity

**How:**
- Event-driven memory synchronization
- Task-specific retention and encoding
- Causality tracking via reasoning traces
- Phased implementation (design → foundation → consistency)

**Impact:**
- Multi-agent workflows 40-60% faster
- 50%+ memory efficiency gain
- 30-40% decision quality improvement
- Audit trail for compliance

**Timeline:** Analysis now (Q1), foundation Q2-Q3, consistency Q3-Q4+

---

**Status:** This ADR is proposed for discussion and team feedback. Phase 1 (design & documentation) ready to begin immediately. Phase 2 implementation will be scheduled in Phase 3 roadmap.

