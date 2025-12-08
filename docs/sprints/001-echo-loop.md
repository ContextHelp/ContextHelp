# 🏁 Sprint 1 Goal: The "Echo" Loop

By the end of this sprint, a developer should be able to run `ch analyze --text "Hello World"`, have it processed by a dummy pipeline, and immediately see it appear in `ch list`.

This sprint now also establishes the **first traces of semantic structure** needed for later introduction of **mentions**, **entities**, and the **knowledge graph**—without implementing any of those systems yet. The seeds planted here ensure that future sprints can layer semantic identity on top of the ingestion and bookmarking foundation.

---

### 1. Core/Infra Team (The Backbone)

**Focus:** App Initialization & Job State Management.
They aren't building the pipeline logic, just the machinery to move jobs around.

**New Consideration:**
Even though mentions/entities aren’t implemented yet, **the bookmark schema must already include an empty `mentions: []` field**, ensuring forward compatibility.

* **Task 1.1: The Bootloader (`cmd/ch`)**
  - Initialize `main.go`.
  - Load config (even if empty).
  - Connect to SQLite (using the schema from Sprint 0).
  - **Ensure the bookmarks table includes a `mentions` column (JSON array, empty by default).**

* **Task 1.2: JobStore Implementation**
  - Implement `EnqueueJob()` (SQL INSERT).
  - Implement `AcquireNextJob()` (SQL SELECT ... FOR UPDATE / locking logic).
  - Implement `UpdateJobStatus()` (Pending → Running → Completed).

* **Task 1.3: `ch jobs` CLI**
  - Implement `ch jobs list` so we can debug if the other teams' logic is working.

---

### 2. Ingestion/AI Team (The Writer)

**Focus:** The Worker Loop & The "Echo" Pipeline.
**Constraint:** Do **not** integrate OpenAI/LLMs yet—dummy pipeline only.

**New Consideration:**
This sprint introduces the **first placeholder for mention extraction**. The pipeline should output `mentions: []` explicitly, demonstrating the shape of future pipeline output.

* **Task 2.1: The Worker Service (`ch serve`)**
  - Create the background loop that polls `JobStore.AcquireNextJob()`.
  - Handle graceful shutdown (don’t kill a job mid-process).

* **Task 2.2: The "Echo" Pipeline (`text.echo`)**
  - Take input text `"ABC"`, sleep 1s, output a Bookmark titled `"Processed ABC"`.
  - **Include `mentions: []` in the pipeline output to establish the contract.**
  - This proves the pipeline interface and async behavior work.

* **Task 2.3: Bookmark Writer**
  - Implement logic that takes the Pipeline result and commits it to the `bookmarks` table.
  - **Write empty mentions (`[]`) even if none extracted.**
    (Future sprints will replace this with real mention extraction.)

---

### 3. Search/Retrieval Team (The Reader)

**Focus:** Basic Read Operations.
**Constraint:** Do not build the RSQL parser yet. Stick to raw SQL filters.

**New Consideration:**
Search output must display the empty mentions field so that later sprints can build on the shape.

* **Task 3.1: BookmarkStore Implementation**
  - Implement `ListBookmarks()` (Basic SQL SELECT).
  - Map SQL rows back to the `Bookmark` struct defined in Sprint 0.
  - **Ensure the Mentions array is always deserialized.**

* **Task 3.2: `ch list` CLI**
  - Wire up the CLI command to print a simple table of results.
  - **Optionally show a “mentions: n” count** to validate wiring.

* **Task 3.3: `ch show <id>`**
  - Retrieve a single record by UUID.
  - Output as JSON.
  - Confirm empty `mentions: []` round-trips correctly.

---

### 4. Registry/Ecosystem Team (The Connector)

**Focus:** Local File Loading.
**Constraint:** Do not build the HTTP client yet. Focus on internal logic of "What is a Registry?"

**New Consideration:**
Registries will later include **entity definitions**, but Sprint 1 should only prepare the folder/module boundaries—not implement entities yet.

* **Task 4.1: The Local Registry Loader**
  - Create a `pkg/registry` module.
  - Implement loading a `taxonomy.json` file from disk.
  - Validate JSON against registry schema.
  - **Prepare module structure to later add `entities.json` without disrupting layout.**

* **Task 4.2: The "Enricher" Stub**
  - Create the function `EnrichTags(tags []string)`.
  - For now, only confirm that tags exist in local JSON.
  - *Integration Point:* Provide this function to Team 2 so the "Echo Pipeline" can try validating a fake tag.

---

### 🔗 The Integration Check (The Demo)

If the sprint is successful, this sequence happens on Friday:

1. **Core Team** provides the binary `ch`.
2. **User** runs `ch analyze --text "Sprint 1 Test"` → **Core Team** writes Job #1 to DB.
3. **User** runs `ch serve` → **Ingestion Team** processes Job #1.
4. **Ingestion Worker** runs "Echo Pipeline".
5. **Ingestion Worker** calls **Registry Team** to validate a fake tag.
6. **Ingestion Worker** writes a Bookmark to DB **with `mentions: []`**.
7. **User** runs `ch list` → Bookmark appears.
8. **User** runs `ch show <id>` → sees `{ "mentions": [] }`.

This confirms the system is ready for Sprint 2, where tags and hints stabilize, and Sprint 3+, where **mentions become meaningful**.

---

### Risks to Watch For

* **Database Locking:** Since Team 2 (Worker) writes and Team 3 (CLI) reads, SQLite locking issues may appear immediately. WAL mode must be enabled.
* **Config Path Hell:** Teams might hardcode paths (`./data.db` vs `~/.config/ch/data.db`). Agree on defaults early.
* **Schema Drift Risk:** If `mentions` is not added now, future migrations become painful. Add the field now even if unused.
* **Pipeline Contract Drift:** Without including `mentions: []` today, later sprints will break backward compatibility.