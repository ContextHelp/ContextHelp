# User Role Personas

**Version:** 0.1.0

Here are the detailed personas for roles 2 through 19.

### 2. Developer / High-Performance Developer
*   **Name:** **Devin**
*   **Context:** Building a local AI coding assistant that runs in the IDE. Speed is everything; he cannot afford the latency of HTTP calls.
*   **Motivation:** Needs to bypass the overhead of REST and communicate directly with the ContextHelp engine via Protobufs.
*   **Key Objective:** Integrate `contexthelp.proto` into his Go/Rust application to achieve sub-millisecond context retrieval via gRPC.

### 3. Support Engineer
*   **Name:** **Sarah**
*   **Context:** Field engineer helping enterprise clients deploy ContextHelp on-premise.
*   **Motivation:** Clients frequently mismatch configuration file versions with binary versions, causing startup crashes.
*   **Key Objective:** Use `ch version` and `ch config validate` to instantly diagnose compatibility issues between the installed binary and the client's config file.

### 4. Operator
*   **Name:** **Oliver**
*   **Context:** Manages the "always-on" background services on a team server.
*   **Motivation:** He needs to ensure the service is alive and listening on the correct ports without manually curling endpoints.
*   **Key Objective:** reliable health checks and verifying that the service is binding to the correct network interfaces (localhost vs public).

### 5. System Administrator
*   **Name:** **Alex**
*   **Context:** Responsible for the standard operating environment (SOE) for a team of 50 developers.
*   **Motivation:** He deploys the global `/etc/contexthelp/config.yaml`. He hates it when a syntax error in a config file takes down the service during a deployment.
*   **Key Objective:** Validate configuration syntax via CI/CD pipelines before pushing changes to production machines.

### 6. Security Engineer
*   **Name:** **Sam**
*   **Context:** Audits internal tools for credential leaks.
*   **Motivation:** She strictly forbids storing API keys or auth tokens in static text files (YAML/JSON) that might get checked into Git.
*   **Key Objective:** Inject all registry tokens and sensitive paths via `CH_REGISTRY_TOKEN_*` environment variables so no secrets exist on disk.

### 7. Performance Engineer
*   **Name:** **Pat**
*   **Context:** Tuning the ingestion engine to process terabytes of documentation overnight.
*   **Motivation:** The default settings choke when processing 10,000 PDFs at once.
*   **Key Objective:** Fine-tune `queue.workers` and `queue.retry_limits` to maximize CPU utilization without crashing the memory backend.

### 8. Researcher
*   **Name:** **Dr. Riya**
*   **Context:** Academic gathering thousands of PDFs, local notes, and raw text files for a thesis.
*   **Motivation:** Her data is messy and offline. She doesn't use APIs; she uses folders on her hard drive.
*   **Key Objective:** Ingest an entire directory of local files (`--file`) and maintain the "Provenance" so she knows exactly which paper a generated insight came from.

### 9. Podcaster
*   **Name:** **Paul**
*   **Context:** Content creator who records hours of audio interviews.
*   **Motivation:** He needs to find specific quotes or topics discussed in past episodes without re-listening to them.
*   **Key Objective:** Drop an `.mp3` into the `audio.transcript` pipeline and get a searchable, summarized text bookmark in return.

### 10. Data Scientist
*   **Name:** **Dana**
*   **Context:** Optimizing the quality of the embeddings and extraction logic.
*   **Motivation:** The default generic settings are too "safe." She wants to increase the temperature on the LLM summary step and lower the OCR confidence threshold.
*   **Key Objective:** Inject specific `params` (thresholds, model types) into the pipelines via configuration to improve data quality.

### 11. Debugger
*   **Name:** **Dave**
*   **Context:** Investigating why a specific URL is crashing the ingestion pipeline.
*   **Motivation:** The AI enrichment is masking the underlying parsing error. He needs to see the data "naked."
*   **Key Objective:** Run ingestion in "Raw Mode" to bypass AI enrichment and inspect the exact HTML/text the engine is seeing.

### 12. Operations Engineer
*   **Name:** **Ophelia**
*   **Context:** Managing the backend infrastructure as the team's knowledge graph grows from 10MB to 10GB.
*   **Motivation:** JSON files are locking up under concurrent writes. She needs to move to a real database.
*   **Key Objective:** Migrate the storage backend from `json` to `sqlite` (or Postgres) without losing any existing bookmarks.

### 13. Registry Maintainer
*   **Name:** **Rex**
*   **Context:** A senior architect at a large firm defining the "Company Standard" vocabulary.
*   **Motivation:** He wants to ensure that when developers tag something "API", it aligns with the official taxonomy, not their personal definition.
*   **Key Objective:** Publish and validate a "Taxonomy Registry" that 500 other users can subscribe to for semantic alignment.

### 14. Agent Developer
*   **Name:** **Aiden**
*   **Context:** Building specialized AI agents (e.g., a "Legal Bot" and a "Coding Bot").
*   **Motivation:** His Legal Bot shouldn't know about Java code, and his Coding Bot shouldn't see sensitive legal contracts.
*   **Key Objective:** Define strict `Agent Profiles` that scope visibility to specific tags (e.g., `include: ["legal.*"]`) and block everything else.

### 15. Security Architect
*   **Name:** **Arjun**
*   **Context:** Designing the overall policy for how AI interacts with corporate data.
*   **Motivation:** He operates on a "Zero Trust" model. Agents should have no access unless explicitly granted.
*   **Key Objective:** Enforce a "Deny by Default" policy on all Agent Profiles, ensuring that a misconfigured agent sees nothing rather than everything.

### 16. System Integrator
*   **Name:** **Ian**
*   **Context:** Frontend developer building a web dashboard for the company's knowledge base.
*   **Motivation:** He needs a standard interface to query data from a React application.
*   **Key Objective:** Spin up the `ch serve` REST API so his web app can hit `GET /bookmarks` via standard HTTP requests.

### 17. Scripter
*   **Name:** **Scott**
*   **Context:** A DevOps enthusiast who lives in the terminal.
*   **Motivation:** He wants to automate knowledge collection via cron jobs and pipe results into other CLI tools.
*   **Key Objective:** Execute commands like `ch list --json | jq .` to build complex automation workflows without writing a full application.

### 18. API Consumer
*   **Name:** **Alice**
*   **Context:** Building a mobile app that consumes ContextHelp data.
*   **Motivation:** Her mobile UI is small; she needs specific data shapes (e.g., just the summary, not the full text) and specific languages.
*   **Key Objective:** Send parameters via the API to negotiate the exact content language and formatting required for the mobile view.

### 19. Plugin Developer
*   **Name:** **Paige**
*   **Context:** Open-source contributor wanting to add French language support.
*   **Motivation:** The core team isn't prioritizing I18N, so she wants to build it herself as an extension.
*   **Key Objective:** Extend the bookmark schema to store `translations` map data without forking or breaking the core ContextHelp engine code.
