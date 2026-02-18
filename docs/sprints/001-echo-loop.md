# 🏁 Skeleton 1 Goal: The "Echo" Loop

## Package Focus

**Primary Package:** dPKMS (70%) + ctxt (30%)

This skeleton proves the end-to-end flow: ctxt captures input and defines a pipeline, dPKMS executes it. The majority of work is in dPKMS (job queue, worker, storage), with ctxt providing the minimal "echo" pipeline definition and CLI commands.

**Package Breakdown:**
- **dPKMS:** Job queue, worker service, storage persistence, schema implementation
- **ctxt:** Pipeline definition (text.echo), CLI commands (analyze, list, open), job monitoring

---

By the end of this skeleton, a developer should be able to run `ctxt analyze --text "Hello World"`, have it processed by a dummy pipeline, and immediately see it appear in `ctxt list`.

This sprint establishes the **first traces of semantic structure** needed for later introduction of **mentions**, **entities**, and the **knowledge graph**—without implementing any of those systems yet. The seeds planted here ensure that future sprints can layer semantic identity on top of the ingestion and knowledge object foundation.

---

### 1. dPKMS Infrastructure Team (The Backbone)

**Focus:** App Initialization & Job State Management.
They build the machinery to move jobs around (dPKMS job queue).

**New Consideration:**
Even though mentions/entities aren't implemented yet, **the knowledge_objects schema must already include an empty `mentions: []` field**, ensuring forward compatibility.

* **Task 1.1: The Bootloader (`cmd/dpkms`)**
  - Initialize `dpkms/cmd/dpkms/main.go`.
  - Load config (even if empty).
  - Connect to SQLite (using the schema from Skeleton 0).
  - **Ensure the knowledge_objects table includes a `mentions` column (JSON array, empty by default).**

* **Task 1.2: JobStore Implementation (dPKMS)**
  - Implement `EnqueueJob()` (SQL INSERT) in dPKMS job queue.
  - Implement `AcquireNextJob()` (SQL SELECT ... FOR UPDATE / locking logic).
  - Implement `UpdateJobStatus()` (Pending → Running → Completed).

* **Task 1.3: `ctxt job` CLI (ctxt wrapper around dPKMS)**
  - Implement `ctxt job list` so we can debug if pipeline execution is working.

---

### 2. ctxt Ingestion Team (The Writer)

**Focus:** ctxt pipeline definitions & dPKMS worker integration.
**Constraint:** Do **not** integrate OpenAI/LLMs yet—dummy pipeline only.

**New Consideration:**
This sprint introduces the **first placeholder for mention extraction**. The pipeline should output `mentions: []` explicitly, demonstrating the shape of future pipeline output.

* **Task 2.1: The Worker Service (`dpkms serve`) — dPKMS**
  - Create the background loop in dPKMS that polls `JobStore.AcquireNextJob()`.
  - Handle graceful shutdown (don't kill a job mid-process).

* **Task 2.2: The "Echo" Pipeline (`text.echo`) — ctxt**
  - Define the `text.echo` pipeline recipe in ctxt.
  - Take input text `"ABC"`, sleep 1s, output a KnowledgeObject titled `"Processed ABC"`.
  - **Include `mentions: []` in the pipeline output to establish the contract.**
  - This proves the ctxt → dPKMS pipeline interface works.

* **Task 2.3: Knowledge Object Writer — dPKMS**
  - Implement dPKMS logic that takes the Pipeline result and commits it to the `knowledge_objects` table.
  - **Write empty mentions (`[]`) even if none extracted.**
    (Future sprints will replace this with real mention extraction.)

---

### 3. dPKMS Search Team (The Reader)

**Focus:** Basic Read Operations in dPKMS storage.
**Constraint:** Do not build the RSQL parser yet. Stick to raw SQL filters.

**New Consideration:**
Search output must display the empty mentions field so that later sprints can build on the shape.

* **Task 3.1: KnowledgeObjectStore Implementation — dPKMS**
  - Implement `ListKnowledgeObjects()` (Basic SQL SELECT) in dPKMS.
  - Map SQL rows back to the `KnowledgeObject` struct defined in Skeleton 0.
  - **Ensure the Mentions array is always deserialized.**

* **Task 3.2: `ctxt list` CLI — ctxt**
  - Wire up the ctxt CLI command to call dPKMS storage and print a simple table of results.
  - **Optionally show a "mentions: n" count** to validate wiring.

* **Task 3.3: `ctxt open <id>` — ctxt**
  - Retrieve a single record by UUID from dPKMS.
  - Output as JSON.
  - Confirm empty `mentions: []` round-trips correctly.

---

### 4. dPKMS Registry Team (The Connector)

**Focus:** Local File Loading in dPKMS.
**Constraint:** Do not build the HTTP client yet. Focus on internal logic of "What is a Registry?"

**New Consideration:**
Registries will later include **entity definitions**, but Skeleton 1 should only prepare the folder/module boundaries—not implement entities yet.

* **Task 4.1: The Local Registry Loader — dPKMS**
  - Create a `dpkms/pkg/registry` module.
  - Implement loading a `taxonomy.json` file from disk.
  - Validate JSON against registry schema.
  - **Prepare module structure to later add `entities.json` without disrupting layout.**

* **Task 4.2: The "Enricher" Stub — ctxt/dPKMS integration**
  - Create the function `EnrichTags(tags []string)` in ctxt.
  - For now, only confirm that tags exist in local JSON (loaded from dPKMS registry).
  - *Integration Point:* Provide this function to ctxt Ingestion Team so the "Echo Pipeline" can try validating a fake tag.

---

### 🔗 The Integration Check (The Demo)

**End-to-End User Flow:**

1. **dPKMS Team** provides the `dpkms` binary. **ctxt Team** provides the `ctxt` binary.
2. **User** runs `ctxt analyze --text "Skeleton 1 Test"` → **ctxt** enqueues Job #1 to dPKMS.
3. **User** runs `dpkms serve` → **dPKMS worker** processes Job #1.
4. **dPKMS Worker** runs ctxt-defined "Echo Pipeline".
5. **ctxt Pipeline** calls **dPKMS Registry** to validate a fake tag.
6. **dPKMS Worker** writes a KnowledgeObject to DB **with `mentions: []`**.
7. **User** runs `ctxt list` → KnowledgeObject appears (retrieved from dPKMS storage).
8. **User** runs `ctxt open <id>` → sees `{ "mentions": [] }`.

---

**Package-Specific Validation:**

**dPKMS Validation:**
- ✅ SQLite WAL mode enabled
- ✅ Job queue accepts jobs (Pending → Running → Completed state transitions)
- ✅ Worker polls and acquires jobs correctly
- ✅ Knowledge object storage persists with `mentions: []` field
- ✅ Entity backlinks table exists (even if empty)
- ✅ Job step tracing captures pipeline execution

**ctxt Validation:**
- ✅ `ctxt analyze` successfully enqueues jobs to dPKMS
- ✅ Echo pipeline definition executes via dPKMS runtime
- ✅ `ctxt list` retrieves results from dPKMS storage
- ✅ `ctxt open` displays structured JSON output
- ✅ `ctxt job list` shows job status
- ✅ Pipeline output includes `mentions: []` contract

**Cross-Package Validation:**
- ✅ ctxt → dPKMS job enqueueing works
- ✅ dPKMS executes ctxt-defined pipeline steps
- ✅ ctxt reads from dPKMS storage
- ✅ Configuration loaded correctly by both binaries

This confirms ContextHelp is ready for Skeleton 2, where tags and hints stabilize, and Skeleton 3+, where **mentions become meaningful**.

---

### Risks to Watch For

* **Database Locking:** Since dPKMS Worker writes and ctxt CLI reads, SQLite locking issues may appear immediately. WAL mode must be enabled in dPKMS.
* **Config Path Hell:** Teams might hardcode paths (`./data.db` vs `~/.config/contexthelp/data.db`). Agree on defaults early.
* **Schema Drift Risk:** If `mentions` is not added now, future migrations become painful. Add the field to dPKMS schema now even if unused.
* **Pipeline Contract Drift:** Without including `mentions: []` today in ctxt pipeline output, later skeletons will break backward compatibility.
* **Package Boundary Confusion:** Clear interfaces between ctxt (pipeline definitions) and dPKMS (pipeline execution) must be established.
---

## See Also

**Package Boundaries:**
- [CROSS-PACKAGE-CONTRACTS.md](CROSS-PACKAGE-CONTRACTS.md) - dPKMS ↔ ctxt integration points
- [../branding.md](../branding.md) - Naming conventions (dPKMS vs ctxt vs ContextHelp)
- [../dpkms-or-ctxt.md](../dpkms-or-ctxt.md) - Package placement guide

**Configuration:**
- [CONFIGURATION-STRUCTURE.md](CONFIGURATION-STRUCTURE.md) - Config file organization
- [../ctxt/configuration.md](../ctxt/configuration.md) - Focus profiles & preferences

**Architecture:**
- [../architecture.md](../architecture.md) - System architecture overview
- [../../ROADMAP.md](../../ROADMAP.md) - Living skeleton roadmap
- [README.md](README.md) - Sprint documentation index
