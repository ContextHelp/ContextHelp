# ADR-040 – HEMA: Long-Context Conversation Coherence Architecture

> **Status:** Proposed
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** ctxt, dPKMS
> **Supersedes:** —
> **Superseded by:** —
>
> **Reference:** [HEMA: A Hippocampus-Inspired Extended Memory Architecture for Long-Context AI Conversations](https://arxiv.org/abs/2504.16754) (arXiv:2504.16754, Apr 2025, author: Kwangseob Ahn)

---

## Context

ContextHelp is designed to support knowledge workers through capture, enrichment, search, and composition. The system includes support for "chat-like iterations" and learns from "conversation history" (knowledge-workers persona).

However, as the system scales operationally, two new use cases emerge that challenge current architecture:

### Use Case 1: Enterprise Chatbot Channels
- Deploy agents as Slack bots, Teams apps, or Discord bots
- Multi-turn conversations in public/private channels
- Conversations can span dozens or hundreds of turns
- Users expect coherence and context continuity across turns

### Use Case 2: Direct Messaging Integration
- Support Telegram, WhatsApp, or Signal bots
- Long-lived sessions with individual users
- Conversations may span weeks (if persistent storage enabled)
- Users expect memory of previous conversations

### Use Case 3: Iterative Search Refinement
- Knowledge workers doing multi-turn search refinement
- Each turn builds on previous context
- Long refinement sessions (20-100 turns typical)
- System should understand cumulative intent

**Current Challenge:**

Standard approaches to long-context conversations face fundamental trade-offs:

```
Approach 1: Expand Context Window
- ✅ Simple (include all prior turns)
- ❌ Expensive (quadratic token cost)
- ❌ Loses older information (beyond window size)
- ❌ Model performance degrades with very long contexts
- ❌ Information retrieval becomes harder (needle-in-haystack)

Approach 2: Summarization
- ✅ Reduces token count
- ✅ Compresses history
- ❌ Loses detail and nuance
- ❌ Summarization errors accumulate
- ❌ Can't recover granular facts from summary

Approach 3: Current dPKMS Approach (Storage + Multi-Strategy Search)
- ✅ Stores all turn data durably
- ✅ Can retrieve any previous turn
- ❌ No mechanism for maintaining narrative coherence
- ❌ Each turn requires fresh search (re-discovers context)
- ❌ Conversation loses "flow" over many turns
```

**The Real Problem:**

Beyond token count, there's a coherence problem: over 300+ turns, even with full context available, models struggle to:
1. Remember the primary narrative thread (what was the original question?)
2. Track decisions made earlier (what did we decide about X?)
3. Understand why we're asking a new question (how does this relate to prior context?)
4. Maintain a consistent "mental model" of the conversation

This is separate from information retrieval. It's about maintaining story coherence.

---

## The Problem HEMA Solves: Hippocampus-Inspired Dual Memory

### HEMA's Innovation

HEMA (Hippocampus-Inspired Extended Memory Architecture) addresses long-context coherence by separating memory into two complementary systems, inspired by neuroscience:

```
Human Hippocampus Model (Neuroscience):
- Hippocampus: Episodic memory (what happened, when)
  → Stores specific events, details, facts
  → Retrievable but slowly indexed
  → Good for "what happened in turn 45?"

- Neocortex: Semantic memory (general knowledge, narrative)
  → Stores consolidated patterns, narratives
  → Fast indexed access
  → Good for "what are we trying to accomplish?"

HEMA Architecture (AI Implementation):
- Vector Memory: Episodic store (HEMA's hippocampus)
  → Chunk embeddings from conversation turns
  → Cosine similarity retrieval
  → Retrievable: "find relevant previous turn"

- Compact Memory: Narrative summary (HEMA's neocortex)
  → One-sentence rolling summary of conversation arc
  → Updated after each turn
  → Fast access: "what's this conversation about?"
```

### How HEMA Works

**Initialization:**
```
Turn 1: User asks complex question
  → Compact Memory: "User asked about X" (one sentence)
  → Vector Memory: [turn_1_embedding]
```

**Subsequent Turns:**
```
Turn 2-N: Conversational exchanges
  For each turn:
    1. Embed new turn into vector
    2. Retrieve relevant prior turns via similarity search
    3. Update compact memory (one-sentence summary evolves)
    4. Generate response using:
       - Compact memory (narrative context)
       - Retrieved vector memories (relevant details)
       - Current turn (immediate context)

Prompt to model:
  "Conversation so far: [compact_memory]
   Related prior context: [vector_retrieved_chunks]
   Current turn: [user_message]
   Your response:"
```

### Performance Results (HEMA Paper)

Evaluated on 300+ turn conversations:

| Metric | Baseline (Full Context) | HEMA | Improvement |
|--------|------------------------|------|-------------|
| Factual recall accuracy | 41% | 87% | **+46 points** |
| Human-rated coherence | 2.7/5.0 | 4.3/5.0 | **+1.6 points** |
| Prompt length maintained | — | <3,500 tokens | **Efficient** |
| Vector retrieval precision@5 | — | ≥0.80 | **High precision** |
| Vector retrieval recall@50 | — | ≥0.74 | **Good recall** |

**Key insight:** HEMA maintains coherence on very long conversations while reducing prompt size (enabling cheaper inference and avoiding context window limits).

---

## Problem Analysis: Does ContextHelp Have This Problem?

### Current Long-Context Scenarios

**Scenario 1: Slack Bot Conversation**
```
User: "What are recent architecture decisions?"
Bot: [searches, composes brief]

User: "Who made that decision?"
Bot: [searches again for who made decision X]

User: "What were their concerns?"
Bot: [searches for concerns from person X]

User: "How does this relate to the mobile platform strategy?"
Bot: [searches for mobile strategy]
...50 turns later...

User: "So actually, can we go back to the first thing we talked about?"
Bot: ??? [lost the thread; doesn't remember original question without looking it up]
```

**Current ContextHelp Approach:**
- Each query is independent (searches knowledge base fresh)
- No narrative memory of conversation arc
- User must re-specify context if asking about earlier topics
- No mechanism to say "remember, we were discussing X, now let's relate it to Y"

**Problem:** System has data but lacks narrative coherence.

---

### Scenario 2: WhatsApp Bot Long Session
```
Week 1: User asks about Q1 goals
  → Bot provides brief

Week 2: User asks about Q2 priorities
  → Bot provides brief
  → But: Does bot understand these relate to Q1?
  → Does bot remember "we decided Q1 focus was X"?

Month 1: User asks "are we still on track?"
  → Bot searches recent data
  → But: Can't access "what did we commit to at the beginning?"
  → Must search explicitly for "Q1 goals"
```

**Problem:** Stateless conversations lose cumulative understanding.

---

### Scenario 3: Iterative Search Refinement
```
Turn 1: "Show me recent decisions about payments"
  → Results: D1, D2, D3

Turn 2: "Filter to only high-impact ones"
  → Results: D1, D3
  → But system doesn't remember "we were looking at payment decisions"
  → Must re-query with payment constraint

Turn 3: "Who's most affected?"
  → System searches for "affected by D1, D3"
  → But doesn't maintain thread that we're discussing payment decisions

Turn 10: User confused
  → "Wait, which decision was about the deprecation?"
  → System must search ("which payment decision mentioned deprecation?")
  → User lost narrative context
```

**Problem:** Each query is isolated; no conversation thread maintained.

---

## Why Current Approach Isn't Sufficient

### What dPKMS Provides
```
✅ Durable storage (all turns persisted)
✅ Multi-strategy search (can find relevant chunks)
✅ Knowledge graph (understands relationships)
✅ Ranking (can prioritize relevant results)
```

### What dPKMS Doesn't Provide
```
❌ Narrative thread (what's the conversation about?)
❌ Coherence over 300+ turns (model loses track)
❌ Implicit memory (user expects bot to remember without re-specifying)
❌ Conversation arc tracking (beginning → middle → where we are now)
```

**Example of the Gap:**

```
dPKMS Approach:
Turn 1: "Show me architecture decisions"
  Query: search(type==decision, topic==architecture)
  Result: [D1, D2, D3]

Turn 50: "What was that first thing we talked about?"
  Query: ??? What does "that first thing" mean?
  System: Has to guess or ask for clarification

HEMA Approach:
Turn 1: "Show me architecture decisions"
  → Compact Memory: "Discussing architecture decisions"
  → Query: search(type==decision, topic==architecture)
  → Result: [D1, D2, D3]

Turn 50: "What was that first thing we talked about?"
  → Compact Memory: [Retrieved: "We were discussing architecture decisions"]
  → Realizes "that first thing" = D1
  → Result: [D1 details]
```

---

## Applicability Analysis: Where HEMA Fits

### Where HEMA Directly Applies

**1. Slack/Teams/Discord Bots**
- Conversations in channels are public, visible, stored
- Each bot interaction is one turn in a thread
- 50-200 turns per channel over time is normal
- Users expect bot to remember thread context

```
Example Slack channel:
  Bot: "Hi, I can help with architecture questions"
  User: "What were our decision criteria?"
  Bot: [with HEMA: understands we're discussing decisions]
  User: [5 turns later] "How does this compare to what we decided?"
  Bot: [with HEMA: maintains coherence to earlier criteria]
  vs
  Bot: [without HEMA: must search for "comparison to decisions" explicitly]
```

**Fit:** ⭐⭐⭐⭐⭐ Excellent. Long-lived threads, explicit narrative evolution.

---

**2. DM Bots (Telegram, WhatsApp, Signal)**
- Private 1-on-1 conversations
- Can span hours, days, weeks if persistent
- Users treat as ongoing relationship ("my knowledge assistant")
- Expect memory across sessions

```
Example WhatsApp conversation with knowledge bot:
  Day 1: "What are the top 3 priorities?"
  Day 3: "How are we doing on priority #1?"
  Week 1: "Remember we discussed priorities? How's the implementation?"

  Without HEMA: Bot must search for "priorities" explicitly each time
  With HEMA: Bot maintains running narrative: "We've been tracking implementation of 3 priorities"
```

**Fit:** ⭐⭐⭐⭐⭐ Excellent. Long sessions, implicit memory expectations.

---

**3. Iterative Search Refinement (Knowledge Workers)**
- Human doing multi-turn search refinement
- Current flow: Ask → get results → refine → ask → refine → ...
- With HEMA: System maintains refinement narrative

```
Current flow (without HEMA):
  Turn 1: "Show decisions from Q4"
    Search: query_all_decisions(quarter==Q4)
    Result: 15 decisions

  Turn 2: "Filter to only ones affecting engineering"
    Search: query_decisions(quarter==Q4, affected_team==engineering)
    User thinks: "Good, filtered"

  Turn 3: "What's the status of the database one?"
    Search: query_decisions(quarter==Q4, affected_team==engineering, topic==database)
    Implicit assumption: System understands we're still in Q4+engineering context

  Turn 4: "Wait, which one was about the API deprecation?"
    System: "Which one?" (ambiguous reference to "the" one)
    User must re-clarify: "From the database decisions"

Refined flow (with HEMA):
  Turn 1-3: Same as above

  Turn 4: "Wait, which one was about the API deprecation?"
    Compact Memory: [Maintains thread of Q4 + engineering + database context]
    System: Understands "the API deprecation" = specific database decision
    Result: Shows the right one immediately
```

**Fit:** ⭐⭐⭐⭐ Good. Medium-length conversational refinement (20-100 turns typical).

---

### Where HEMA Partially Applies

**4. Agents with Multi-Turn Reasoning**
- If agents ask questions, get results, then ask follow-ups
- Each agent-system interaction is one turn
- Over time, agent may need narrative memory

```
Agent reasoning loop:
  Agent: "What's the status of payments system?"
  System: [returns status]

  Agent: "Who's responsible for this?"
  System: [searches, but doesn't remember "payments system" context automatically]

  Agent: "What's the timeline?"
  System: [same issue]
```

**Without HEMA:** Agent must include full context in each query
**With HEMA:** System maintains conversational thread; agent can use implicit references

**Fit:** ⭐⭐⭐ Medium. Useful if agents do multi-turn reasoning, but ADR-037 (event-driven coordination) may be more important for agent-to-agent scenarios.

---

### Where HEMA Doesn't Apply

**5. One-Shot Queries**
- "Show me recent decisions"
- "Search for X"
- No follow-up conversation

**Fit:** ❌ None. HEMA adds no value for single-turn interactions.

**6. Concurrent Channels/Sessions**
- User has multiple simultaneous conversations
- Each maintains separate narrative
- System must track which conversation is which

**Potential issue:** HEMA maintains one narrative thread. If user asks question in Channel A, then switches to Channel B, system needs to switch compact memory context.

**Fit:** ⚠️ Requires careful multi-session handling (Discussed below in "Implementation Notes").

---

## Decision: HEMA Has Applicability for Future Phases

### Immediate Applicability (Phases 2-3)

**For Slack/Teams Bot Integration:**
- Users expect conversational continuity
- 50-200 turn threads are normal
- Coherence loss is real problem without HEMA

**Recommendation:** Include HEMA-like approach in Phase 3 when building enterprise chatbot layer.

**For Iterative Search (Knowledge Workers):**
- Current 20-50 turn refinements work OK
- HEMA would improve UX but not critical
- Could be Phase 3-4 enhancement

**Recommendation:** Nice-to-have for Phase 3; prioritize after core agent coordination (ADR-037).

### Timeline

- **Phase 2 (Q2-Q3 2026):** Focus on agent coordination (ADR-037)
- **Phase 3 (Q3-Q4 2026+):** Consider adding HEMA-like layer for:
  - Slack/Teams bot memory
  - WhatsApp/Telegram bot memory
  - Iterative refinement coherence
- **Phase 4 (2027+):** Optimize and scale

---

## Proposed HEMA Integration with dPKMS + ctxt

### Architecture

```
ctxt (Agent Brain / Bot Interface):
├─ Conversation Manager
│  ├─ Compact Memory (narrative summary)
│  ├─ Vector Memory (episodic chunks)
│  └─ Turn History (durable store)
│
└─ Multi-Channel Support
   ├─ Slack bot
   ├─ Teams bot
   ├─ Telegram bot
   ├─ WhatsApp bot
   └─ Direct search refinement

dPKMS (Substrate):
├─ Persistent conversation storage
├─ Vector embeddings (conversation chunks)
├─ Knowledge graph (entities mentioned in conversation)
└─ Query execution
```

### Integration Points

**1. Conversation State**

```yaml
conversation:
  id: unique_id
  channel: slack|telegram|whatsapp|search
  user_id: user
  created_at: timestamp

  # HEMA-specific
  compact_memory: "One-sentence summary of conversation arc"
  compact_memory_updated_at: timestamp
  compact_memory_version: int  # For versioning/rollback

  # Vector memory integration
  turns:
    - turn_number: 1
      timestamp: ...
      user_message: ...
      bot_response: ...
      embedding: [...]  # Store embedding for retrieval
      entities_mentioned: [...]  # From knowledge graph

  # Links to dPKMS
  referenced_objects: [id1, id2, id3]  # Objects discussed
  referenced_entities: [@entity.slug1, @entity.slug2]  # Mentioned entities
```

**2. Compact Memory Update Algorithm**

```
After each turn:
  1. Extract key information from turn (intent, decisions, topics)
  2. Update compact memory to reflect:
     - Original question/goal
     - Key decisions made so far
     - Current refinement scope
     - Open questions

  Example evolution:
    Turn 1: "Show architecture decisions"
      → Compact: "Exploring architecture decisions"

    Turn 5: "Focus on database decisions"
      → Compact: "Exploring architecture decisions, narrowed to database"

    Turn 15: "How do these compare to old system?"
      → Compact: "Comparing database architecture decisions (new vs old system)"

    Turn 30: "Let's look at implementation plan"
      → Compact: "Reviewed database architecture decisions, now examining implementation plan"
```

**3. Vector Memory Retrieval**

```
When processing turn N:
  1. User asks question Q
  2. Search vector memory: cosine_similarity(Q, all_prior_turns)
  3. Retrieve top-k most relevant prior turns
  4. Combine: compact_memory + vector_results + current_turn
  5. Feed to LLM/reasoning engine

  Example:
    Turn 50: User: "Wait, what was that first concern?"
    → Vector search finds Turn 2 (most similar to "concern")
    → Compact memory: "We've been discussing database decisions"
    → System: "Concern from turn 2: [retrieved concern]"
```

**4. Multi-Session Handling**

```
If user has concurrent conversations:
  conversation_a:
    compact_memory: "Discussing mobile app decisions"

  conversation_b:
    compact_memory: "Reviewing Q1 planning"

When user switches channels:
  1. Identify which conversation context
  2. Load appropriate compact_memory
  3. Use conversation-specific vector memory
  4. Process turn in correct context
```

---

## Implementation Roadmap

### Phase 1: Architecture (Now - Q1 2026)

**Deliverables:**
1. ADR-040 (this document)
2. Design document: `docs/ctxt/conversation-coherence.md`
3. Data schema for compact memory + vector memory
4. Integration points with dPKMS identified

**Not Implementation Yet:** Just design and specification.

---

### Phase 2: Foundation (Q2-Q3 2026)

**Not Priority:** Defer in favor of agent coordination (ADR-037).

However, if building early Slack bot integration, consider:
1. Implement basic conversation state storage
2. Add turn history persistence
3. Wire up vector embedding of turns
4. Optional: Simple compact memory update

---

### Phase 3: Full Implementation (Q3-Q4 2026+)

**Core Components:**

1. **Compact Memory Engine**
   ```go
   type CompactMemoryEngine struct {
       model LLM  // or small local model
   }

   func (e *CompactMemoryEngine) Update(
       currentMemory string,
       newTurn ConversationTurn,
       context Context,
   ) (string, error) {
       // Prompt: "Given conversation so far: [memory]
       //          And new turn: [turn]
       //          Update summary to capture current state"
       // Returns: One-sentence updated summary
   }
   ```

2. **Vector Memory Retriever**
   ```go
   type VectorMemoryRetriever struct {
       embedder Embedder
       store    VectorStore
   }

   func (r *VectorMemoryRetriever) Retrieve(
       query string,
       conversationID string,
       topK int,
   ) ([]ConversationTurn, error) {
       // Embed query
       // Search conversation-specific turns
       // Return most relevant prior turns
   }
   ```

3. **Conversation Manager**
   ```go
   type ConversationManager struct {
       compactMemory CompactMemoryEngine
       vectorMemory  VectorMemoryRetriever
       storage       ConversationStore
   }

   func (m *ConversationManager) ProcessTurn(
       conversationID string,
       userMessage string,
       context Context,
   ) (botResponse string, err error) {
       // 1. Load conversation state
       // 2. Retrieve relevant prior turns
       // 3. Prepare prompt with compact + vectors + current turn
       // 4. Generate response
       // 5. Update compact memory
       // 6. Store turn + embedding
   }
   ```

---

## Comparison: HEMA vs Current Approach

| Capability | Current (Multi-Strategy Search) | With HEMA | Benefit |
|-----------|------|------|---------|
| **Turn 1-5 coherence** | ✅ Good (full context available) | ✅ Good | No difference |
| **Turn 50 coherence** | ⚠️ Degraded (model loses thread) | ✅ Better (narrative maintained) | Improved |
| **Turn 100+ coherence** | ❌ Poor (very hard to maintain) | ✅ Much better | Major improvement |
| **Implicit references** | ❌ Must re-specify context | ✅ Understood from narrative | Better UX |
| **Token efficiency** | ❌ Grows unbounded | ✅ Compact memory + vectors | Cost savings |
| **Scalability to 300+ turns** | ❌ Breaks (context limit) | ✅ Handles well | Operational advantage |
| **Implementation complexity** | Low (already have search) | Medium (add memory layer) | Worth cost |
| **Operational overhead** | Low | Medium (maintain embeddings) | Acceptable for bots |

---

## Risks & Mitigations

### Risk 1: Compact Memory Degradation

**Problem:** Over time, one-sentence summary loses important details.

**Example:**
```
Turn 1-20: Discussing database decisions
Turn 21: "Actually, let's revisit API design"
Turn 22: "How does this affect our caching strategy?"

Compact memory might become:
"Discussing database and API design decisions"
(but loses specific database decisions discussed earlier)

Turn 50: User: "Remember we decided on sharding?"
System: "Which sharding decision? (doesn't remember specifically)"
```

**Mitigation:**
1. Include version history: keep 3-5 prior compact memories
2. Use timestamped segments (compact memory per phase of conversation)
3. Implement compact memory "refresh" every N turns (re-summarize from full history)
4. Allow users to explicitly set compact memory: `"Remember, we're discussing payment architecture"`

---

### Risk 2: Vector Retrieval Hallucination

**Problem:** Cosine similarity might retrieve misleading prior turns.

**Example:**
```
Turn 1: "What's our payment processing status?"
Turn 50: "Show me database performance metrics"
Turn 51: System retrieves Turn 1 (both mention "status" vs "performance")
        But they're about completely different topics
```

**Mitigation:**
1. Use hybrid retrieval: semantic + metadata filters (same conversation phase)
2. Set similarity threshold (only retrieve if confidence > 0.7)
3. Validate retrieved turns with semantic classification
4. Allow user feedback: "That's not relevant to what I'm asking"

---

### Risk 3: Multi-Session Confusion

**Problem:** Compact memory for Channel A gets mixed with Channel B.

**Mitigation:**
1. Enforce strict conversation isolation (each has own compact memory)
2. Clear context switching: when user moves channels, explicitly acknowledge
3. Store conversation metadata (channel, user, time range)
4. Test multi-session scenarios thoroughly

---

### Risk 4: Increased Latency

**Problem:** Updating compact memory on every turn adds latency.

**Example:**
```
Without HEMA: Process turn = query + respond (~200ms)
With HEMA:   Process turn = query + update_compact + respond (~400ms)
             (2× slower)
```

**Mitigation:**
1. Run compact memory update asynchronously
2. Cache compact memory (update only every 5 turns)
3. Use small, fast LLM for updates (not full-size model)
4. Profile and optimize in Phase 3

---

## Related ADRs

- **ADR-037:** Agent-Aware Adaptive Memory (multi-agent coordination; HEMA is for conversation coherence)
- **ADR-018:** Safe Agent Execution (proposal/approval workflow; conversation could trigger proposals)
- **ADR-016:** Just-In-Time Surfacing (similar goal of right-time information, different mechanism)
- **ADR-015:** Focus Profiles (conversation narratives are profile-aware)

---

## Decision Summary

**What:** HEMA (Hippocampus-Inspired Extended Memory Architecture) provides dual-memory system for long-context conversation coherence.

**Problem Solved:** Over 300+ turns, conversations lose coherence despite full context available. Users expect bot to remember conversation narrative without re-specifying context.

**Applicability:**
- ✅ **High:** Slack/Teams/Discord bots (50-200 turn channels)
- ✅ **High:** WhatsApp/Telegram bots (long-lived 1-on-1)
- ✅ **Medium:** Iterative search refinement (20-100 turns)
- ⚠️ **Medium:** Agent multi-turn reasoning (if agents need stateful conversations)

**Use Cases That Require It:**
1. Enterprise chatbot in Slack channel (user expects thread coherence)
2. Personal WhatsApp bot (user expects "my assistant remembers me")
3. Long iterative search (refinement conversation should maintain flow)

**Recommendation:**
- **Now (Phases 1-2):** Document but don't implement. Focus on agent coordination (ADR-037).
- **Phase 3 (Q3-Q4 2026+):** Implement HEMA layer for:
  - Slack bot integration (if prioritized)
  - WhatsApp/Telegram bot integration (if prioritized)
  - Enhanced search refinement UX
- **Later:** Optimization and scaling

**Expected Benefits (When Implemented):**
- 46 percentage point improvement in factual recall (per HEMA paper)
- 1.6 point improvement in human-rated coherence
- Efficient token usage (compact memory < 3500 tokens vs unbounded full context)
- Better UX for multi-channel bots
- Support for 300+ turn conversations

---

## References

- [HEMA: A Hippocampus-Inspired Extended Memory Architecture for Long-Context AI Conversations](https://arxiv.org/abs/2504.16754) (arXiv:2504.16754, Apr 2025, Kwangseob Ahn)
- Related: ADR-037 (Agent-Aware Adaptive Memory)
- Related: ADR-039 (MLPMemory Evaluated)
- Related: ADR-038 (SuperMemory Evaluated)

---

**Status:** This ADR proposes including HEMA-inspired conversation coherence in Phase 3 architecture when building enterprise chatbot and messaging integrations. Implementation deferred pending priorities for bot/messaging roadmap.
