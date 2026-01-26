# ADR-018 – Safe Agent Execution Model (Propose → Dry-Run → Apply)

> **Status:** Accepted
> **Date:** 2026-01-26
> **Author:** @jadb
> **Applies to:** ctxt, dPKMS
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

AI agents integrated with knowledge systems create significant risk if allowed to execute actions without oversight. Users face:

**Silent Automation Risk:**
- Agents modify data without user awareness
- Unintended consequences discovered too late
- No opportunity to review before execution
- Trust erosion from unexpected changes

**Irreversible Actions:**
- Data deletion without confirmation
- External API calls that cannot be undone
- Registry modifications affecting shared knowledge
- Pipeline execution with side effects

**Scope Creep:**
- Agents exceed intended permissions
- Operations affect more data than expected
- Cross-profile or cross-boundary modifications
- Privilege escalation through composition

**Lack of Auditability:**
- No record of what agent intended to do
- No diff between proposed and actual state
- Cannot replay or debug agent decisions
- Unclear responsibility for changes

**User Control Loss:**
- Agent decides when to act
- No pause/review mechanism
- All-or-nothing execution model
- Cannot selectively apply parts of plan

**Constraints:**
- Must preserve user sovereignty (explicit approval required)
- Must be auditable (full trail of proposed vs applied)
- Must be reversible (rollback mechanism)
- Must support dry-run (preview without execution)
- Must enforce scope boundaries (profile, permissions)
- Must work with both local and remote agents
- Must not break existing pipeline system

**Affected Subsystems:**
- Agent runtime (execution boundary)
- Job system (action queuing)
- Storage layer (transaction boundaries)
- Permission system (capability enforcement)
- Audit logging (action tracking)
- CLI/API (approval workflows)
- Pipeline system (agent-triggered pipelines)

**Goals:**
- Prevent silent agent modifications
- Enable user review before execution
- Provide clear diff of proposed changes
- Support reversibility and rollback
- Maintain audit trail for debugging
- Enforce principle of least privilege
- Build user trust through transparency

---

## Decision

**ContextHelp will implement a three-phase Safe Agent Execution Model (Propose → Dry-Run → Apply) that requires explicit user approval before any agent-initiated actions are executed, provides clear previews of intended changes, enforces scope boundaries, and maintains full audit trails with rollback capability.**

The execution model operates through:

1. **Three-Phase Workflow:**

   **Phase 1: Propose**
   ```
   Agent analyzes context and generates action plan:
   - What: Detailed description of intended actions
   - Why: Reasoning and justification
   - Scope: Which objects/entities will be affected
   - Risks: Potential side effects or concerns
   - Reversibility: How to undo changes
   ```

   **Phase 2: Dry-Run**
   ```
   System simulates execution without committing:
   - Execute all steps in sandbox
   - Generate diff of proposed state changes
   - Identify any scope violations or errors
   - Calculate resource costs (API calls, time)
   - Present preview to user
   ```

   **Phase 3: Apply**
   ```
   User approves, system commits changes:
   - Execute actions in transaction
   - Record audit log entry
   - Return execution results
   - Enable rollback if needed
   ```

2. **Proposal Schema:**
   ```yaml
   proposal:
     id: uuid
     agent: agent-name
     created_at: timestamp
     status: proposed | approved | rejected | applied | rolled_back

     actions:
       - type: create_object | update_object | delete_object | run_pipeline | external_call
         target: object-id | entity-slug | url
         operation: specific operation details
         reasoning: why this action is needed
         reversible: true | false
         estimated_cost:
           api_calls: int
           time_seconds: int

     scope:
       profile: profile-name
       max_objects: int
       allowed_operations: [operation-types]
       forbidden_patterns: [patterns]

     risks:
       - description: potential issue
         severity: low | medium | high
         mitigation: how to address

     diff_preview:
       objects_created: int
       objects_updated: int
       objects_deleted: int
       external_calls: int
       changes: [change-details]
   ```

3. **Dry-Run Sandbox:**

   **Isolation Mechanisms:**
   - In-memory transaction buffer
   - Copy-on-write object store
   - Mock external API calls
   - Capability enforcement
   - Resource limits (time, memory, API calls)

   **Diff Generation:**
   ```json
   {
     "object_id": "uuid",
     "operation": "update",
     "before": {
       "tags": ["old-tag"],
       "summary": "old summary"
     },
     "after": {
       "tags": ["old-tag", "new-tag"],
       "summary": "updated summary"
     },
     "fields_changed": ["tags", "summary"]
   }
   ```

4. **User Approval Workflow:**

   **CLI Interaction:**
   ```bash
   # Agent proposes actions
   $ ctxt agent run analyze-project

   [Proposal #abc-123]
   Agent: analyze-project
   Actions: 5 (3 updates, 2 creates)
   Scope: Profile "engineer", max 10 objects
   Estimated: 2 API calls, ~5 seconds

   Actions:
   1. Update object [def-456]: Add tags [architecture, decision]
   2. Update object [ghi-789]: Extract decision "Use PostgreSQL"
   3. Create object: Technical brief about project architecture
   4. Update object [jkl-012]: Add mention @component.database
   5. Create object: Task "Review database migration plan"

   Risks:
   - External API call to OpenAI for brief generation (moderate)

   [Preview] Show detailed diff? (y/n): y

   [Diff Preview]
   Object [def-456]:
   + tags: ["architecture", "decision"]
   + summary: "Evaluated database options for project..."

   Object [ghi-789]:
   + decisions: [{
       text: "Use PostgreSQL for primary database",
       status: "made",
       rationale: "Better JSON support and performance..."
     }]

   [Action Required]
   (a) Approve and apply
   (r) Reject proposal
   (e) Edit scope and re-propose
   (d) Apply with dry-run only
   Choice: a

   [Executing...]
   ✓ Applied proposal #abc-123
   ✓ 3 objects updated, 2 objects created
   ✓ Rollback available via: ctxt agent rollback abc-123
   ```

   **Programmatic Approval:**
   ```go
   proposal := agent.Propose(ctx, task)
   dryRun := proposal.DryRun()

   // Review changes
   for _, change := range dryRun.Changes {
       log.Info(change.Summary())
   }

   // Approve
   if userApproves(dryRun) {
       result := proposal.Apply()
   }
   ```

5. **Scope Enforcement:**

   **Profile-Based Limits:**
   ```yaml
   agent:
     name: analyze-project
     profile: engineer
     permissions:
       max_objects_affected: 50
       allowed_operations:
         - create_object
         - update_object
         - run_pipeline
       forbidden_operations:
         - delete_object
         - modify_entities
         - external_write_calls
       scope_filters:
         entity_patterns: ["@api.*", "@component.*"]
         tag_patterns: ["technical", "architecture"]
   ```

   **Capability Grants:**
   ```yaml
   agent_capabilities:
     - name: read_objects
       scope: profile-scoped
     - name: create_objects
       scope: profile-scoped
       requires_approval: true
     - name: update_objects
       scope: profile-scoped
       requires_approval: true
     - name: external_api_calls
       scope: explicit-urls
       requires_approval: true
       budget:
         max_calls: 10
         max_cost_usd: 1.00
   ```

6. **Audit Trail:**

   **Proposal Log:**
   ```sql
   CREATE TABLE agent_proposals (
       id UUID PRIMARY KEY,
       agent_name TEXT NOT NULL,
       profile TEXT,
       created_at TIMESTAMP NOT NULL,
       status TEXT NOT NULL,
       action_count INT,
       actions_json JSON,
       scope_json JSON,
       risks_json JSON,
       approved_at TIMESTAMP,
       applied_at TIMESTAMP,
       rolled_back_at TIMESTAMP,
       user_approval TEXT,  -- user identifier
       INDEX(agent_name),
       INDEX(status),
       INDEX(created_at)
   );
   ```

   **Execution Log:**
   ```sql
   CREATE TABLE agent_executions (
       id UUID PRIMARY KEY,
       proposal_id UUID NOT NULL,
       action_type TEXT NOT NULL,
       target_id UUID,
       target_type TEXT,
       executed_at TIMESTAMP NOT NULL,
       success BOOLEAN,
       error TEXT,
       before_state JSON,
       after_state JSON,
       reversible BOOLEAN,
       FOREIGN KEY(proposal_id) REFERENCES agent_proposals(id),
       INDEX(proposal_id),
       INDEX(executed_at)
   );
   ```

7. **Rollback Mechanism:**

   **Rollback Strategies:**

   **Reversible Operations:**
   - Object creation → delete created object
   - Object update → restore previous state
   - Tag addition → remove added tags
   - Mention addition → remove mentions

   **Non-Reversible Operations:**
   - External API calls (logged but not reversible)
   - Deleted objects (soft delete required)
   - Pipeline executions with side effects (manual intervention)

   **Rollback Command:**
   ```bash
   # List rollback candidates
   ctxt agent proposals --status applied --rollback-available

   # Preview rollback
   ctxt agent rollback abc-123 --dry-run

   # Execute rollback
   ctxt agent rollback abc-123

   # Verify rollback
   ctxt agent proposals show abc-123
   ```

8. **Security Boundaries:**

   **Sandbox Constraints:**
   - No filesystem writes outside workspace
   - No network calls without explicit permission
   - No access to objects outside profile scope
   - No privilege escalation
   - Resource limits enforced (CPU, memory, time)

   **Permission Escalation Prevention:**
   - Agents cannot modify their own permissions
   - Cannot create new agents with higher privileges
   - Cannot bypass approval workflows
   - Cannot access other profiles without grant

---

## Rationale

### Alternatives Considered

#### 1. **Automatic Execution with Logging (Rejected)**
Let agents execute freely, log all actions for later review.

**Rejected because:**
- No prevention of unintended changes
- User discovers problems after the fact
- Irreversible actions already executed
- Violates sovereignty principle

#### 2. **Permissions-Only Model (Rejected)**
Grant fine-grained permissions, trust agent to operate within bounds.

**Rejected because:**
- No visibility into agent intentions
- No review opportunity before execution
- Errors discovered after changes applied
- Users struggle with permission configuration

#### 3. **Manual Confirmation Per Action (Rejected)**
Prompt user to approve every individual action.

**Rejected because:**
- Extremely high friction
- Interrupts agent workflows constantly
- Users develop "approval fatigue"
- Poor UX for multi-action plans

#### 4. **Time-Delayed Execution (Rejected)**
Queue actions, wait N minutes before auto-applying.

**Rejected because:**
- Arbitrary delay doesn't help review
- Auto-application still removes control
- No structured diff preview
- Doesn't address scope enforcement

#### 5. **Trust Scores for Agents (Rejected)**
Build reputation system, trusted agents auto-apply.

**Rejected because:**
- Trust not substitute for transparency
- Reputation doesn't prevent mistakes
- Complex to implement and maintain
- Users still want control

### Benefits of Chosen Approach

**User Sovereignty:**
- Explicit approval required
- Clear visibility into intentions
- Full control over execution
- Can reject or modify proposals

**Transparency:**
- Detailed action plans
- Diff previews before changes
- Risk assessment included
- Reasoning explained

**Safety:**
- Dry-run catches errors
- Scope enforcement prevents overreach
- Reversibility enables recovery
- Audit trail for debugging

**Trust Building:**
- Users understand what agents do
- Predictable approval workflow
- No surprises or silent changes
- Accountability through logging

**Flexibility:**
- Can approve all, some, or none
- Edit scope and re-propose
- Test with dry-run only
- Rollback if needed

### Drawbacks / Risks

**Friction:**
- Approval step adds latency
- May feel tedious for trusted agents
- Could slow down workflows
- Users might "click through" without review

**Complexity:**
- Three-phase workflow to implement
- Dry-run sandbox requires isolation
- Rollback logic is sophisticated
- Audit storage overhead

**Reversibility Limits:**
- Some operations cannot be undone
- External API calls irreversible
- Deleted data requires soft-delete
- Users expect full rollback

**Performance Overhead:**
- Dry-run doubles execution work
- Sandbox isolation adds cost
- Audit logging increases I/O
- Diff generation computationally expensive

---

## Consequences

### Positive

**Increased Trust:**
- Users confident agents won't surprise them
- Clear accountability for changes
- Transparency builds confidence
- Sovereignty preserved

**Error Prevention:**
- Dry-run catches mistakes before commit
- Scope violations detected early
- Users can correct agent misunderstandings
- No silent data corruption

**Auditability:**
- Full trail of proposals and executions
- Can replay decisions
- Debug agent behavior
- Compliance-friendly

**Reversibility:**
- Rollback enables experimentation
- Mistakes can be undone
- Safe to try risky operations
- Lower stakes for automation

### Negative

**Implementation Burden:**
- Sandbox isolation complex
- Dry-run requires duplication
- Rollback logic for each operation type
- Audit storage grows unbounded

**User Friction:**
- Approval step adds latency
- May disrupt flow state
- Learning curve for workflow
- Potential approval fatigue

**Performance Impact:**
- Dry-run effectively doubles work
- Audit logging adds I/O overhead
- Rollback state storage grows
- Proposal storage accumulates

**Partial Rollback Challenges:**
- Not all operations reversible
- External side effects permanent
- Users may expect full rollback
- Documentation needed for limitations

### Neutral / Considerations

**Approval Fatigue:**
- Monitor rejection rates
- Consider trusted agent lists
- Provide batch approval for similar actions
- Auto-approve for low-risk operations (opt-in)

**Rollback Expiry:**
- Store rollback state for how long?
- Purge old proposals periodically
- Trade-off: auditability vs storage
- Configurable retention policy

**Cross-Agent Coordination:**
- Multiple agents proposing simultaneously?
- Conflict resolution between proposals
- Ordering dependencies
- Transactional guarantees

**Performance Optimization:**
- Cache dry-run results
- Lazy diff generation
- Async proposal evaluation
- Batch similar operations

---

## Implementation Notes

### Core Components

**Agent Execution Framework (`ctxt/agents/`):**
```go
type SafeAgent interface {
    Name() string
    Propose(ctx Context, task Task) (*Proposal, error)
}

type Proposal struct {
    ID       string
    Agent    string
    Actions  []Action
    Scope    Scope
    Risks    []Risk
    Status   ProposalStatus
}

func (p *Proposal) DryRun() (*DryRunResult, error)
func (p *Proposal) Apply() (*ExecutionResult, error)
func (p *Proposal) Rollback() error
```

**Dry-Run Sandbox (`dPKMS/sandbox/`):**
```go
type Sandbox struct {
    store    *InMemoryStore
    limits   ResourceLimits
    mocks    ExternalCallMocks
}

func NewSandbox(baseStore Store) *Sandbox
func (s *Sandbox) Execute(actions []Action) (*DryRunResult, error)
func (s *Sandbox) GenerateDiff() []Change
```

**Approval Manager (`ctxt/approval/`):**
```go
type ApprovalManager struct {
    proposals ProposalStore
    audit     AuditLogger
}

func (am *ApprovalManager) RequestApproval(p *Proposal) (*ApprovalRequest, error)
func (am *ApprovalManager) WaitForApproval(reqID string) (*ApprovalDecision, error)
func (am *ApprovalManager) RecordExecution(p *Proposal, result *ExecutionResult) error
```

**CLI Commands:**
```bash
# Agent execution
ctxt agent run <agent-name> [--auto-approve] [--dry-run-only]

# Proposal management
ctxt agent proposals list [--status proposed|approved|applied]
ctxt agent proposals show <proposal-id>
ctxt agent proposals approve <proposal-id>
ctxt agent proposals reject <proposal-id>

# Rollback
ctxt agent rollback <proposal-id> [--dry-run]
ctxt agent proposals history

# Configuration
ctxt agent permissions <agent-name>
ctxt agent configure <agent-name>
```

**Configuration Schema:**
```yaml
agents:
  analyze-project:
    profile: engineer
    permissions:
      max_objects_affected: 50
      allowed_operations:
        - create_object
        - update_object
        - run_pipeline
      forbidden_operations:
        - delete_object
      scope_filters:
        entity_patterns: ["@api.*"]
    approval:
      required: true
      auto_approve_low_risk: false
      timeout_seconds: 300
    capabilities:
      read_objects: true
      create_objects: true
      update_objects: true
      external_api_calls:
        enabled: true
        budget:
          max_calls: 10
          max_cost_usd: 1.00
```

### Integration Points

**With Job System:**
1. Agent creates proposal → logged as job
2. Approval triggers job execution
3. Execution results recorded in job
4. Rollback as compensating job

**With Profile System:**
1. Agent inherits active profile scope
2. Profile limits enforced on proposals
3. Profile filters applied to dry-run
4. Profile permissions checked at execution

**With Audit System:**
1. All proposals logged
2. All executions logged
3. All rollbacks logged
4. User decisions recorded

**With Storage Layer:**
1. Proposals stored in dPKMS
2. Execution log in dPKMS
3. Rollback state in dPKMS
4. Sandbox uses copy-on-write

### Migration Strategy

**Phase 1: Proposal Framework (Skeleton 8)**
- Proposal schema and storage
- Basic CLI approval workflow
- Simple action types (create, update)
- Audit logging

**Phase 2: Dry-Run Sandbox (Skeleton 8)**
- In-memory sandbox implementation
- Diff generation
- Resource limits
- Mock external calls

**Phase 3: Rollback Support (Skeleton 8)**
- Reversible operation tracking
- Rollback command
- State restoration
- Non-reversible warnings

**Phase 4: Advanced Features (Skeleton 9+)**
- Batch approval
- Trusted agent lists
- Partial approval (select actions)
- Cross-agent coordination

**Backward Compatibility:**
- Non-agent pipelines unaffected
- Manual operations bypass approval
- Existing job system continues to work
- Agents can opt-out of safe mode (with warnings)

### Testing Requirements

**Unit Tests:**
- Proposal generation
- Dry-run sandbox isolation
- Diff generation accuracy
- Rollback correctness
- Scope enforcement

**Integration Tests:**
- End-to-end approval workflow
- Multi-action proposals
- Rollback of complex proposals
- Permission violations
- Timeout handling

**Security Tests:**
- Sandbox escape attempts
- Permission escalation prevention
- Cross-profile access attempts
- Resource limit enforcement

**Performance Tests:**
- Dry-run overhead measurement
- Large proposal handling (100+ actions)
- Concurrent proposals
- Audit log scalability

### Performance Optimization

**Dry-Run Caching:**
- Cache results for identical proposals
- TTL-based invalidation
- Hash-based deduplication

**Lazy Diff Generation:**
- Only compute diffs if user requests
- Progressive disclosure (summary first)
- Async diff computation

**Audit Log Rotation:**
- Archive old proposals
- Configurable retention
- Compressed storage for old logs

**Batch Operations:**
- Group similar actions
- Single transaction for batch
- Parallel dry-run evaluation

---

## References

- **architecture.md:280-283** – Safe Agent Behavior specification
- **dpkms/security.md** – Security model and boundaries
- **ROADMAP.md** – Skeleton 8: Trust That Travels
- ADR-003 – Separate Read/Write Paths (agents use write path)
- ADR-007 – Transactional Outbox (proposals as jobs)
- ADR-015 – Focus Profiles (profile-scoped execution)

**Related Documents:**
- `ctxt/agents/` – Agent execution framework (to be created)
- `dPKMS/sandbox/` – Dry-run sandbox (to be created)
- `ctxt/approval/` – Approval workflows (to be created)
- `dPKMS/audit/` – Audit logging (to be created)

---
