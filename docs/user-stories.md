You are correct. In the effort to categorize the stories into functional groups (like "Agents" or "Registries"), I inadvertently compressed several specific configuration interactions back into high-level summaries.

To ensure **zero loss of fidelity** from our previous analysis, here is the corrected, fully expanded master list. It integrates all 14 configuration stories explicitly alongside the rest of the system functionality.

### **Installation & System Lifecycle**
1. As a Developer I want to install the CLI binary via a package manager or shell script
2. As a User I want to generate shell completion scripts (Bash, Zsh, Fish) for faster command entry
3. As a User I want to upgrade the engine version without losing my local configuration or database
4. As a User I want to verify the cryptographic signature of the binary to ensure security
5. As a Support Engineer I want to check the Engine and Registry Protocol versions to ensure compatibility (`ch version`)
6. As an Operator I want to run a health check command to verify all subsystems are operational

### **Configuration (Core & Management)**
1. As a User I want to load configuration from layered sources (Defaults, System, User, Env Vars)
2. As a System Admin I want to validate the configuration file syntax before restarting the service
3. As a Security Engineer I want to inject secrets (API tokens) via environment variables
4. As a Performance Engineer I want to configure the background queue worker count and retry limits
5. As a User I want to toggle anonymous telemetry and privacy settings
6. As a User I want to identify the absolute path of the currently active configuration file
7. As a User I want to load configuration from layered YAML sources (Defaults, System, User, Env Vars)
8. As a System Administrator I want to validate configuration files for syntax and schema errors via the CLI
9. As a User I want to identify the absolute path of the currently active configuration file (`ch config path`)
10. As a User I want to quickly edit the active configuration file using my default text editor (`ch config edit`)
11. As a Security Engineer I want to inject sensitive secrets (tokens) using environment variables
12. As an Operator I want to configure server network bindings (Public vs Localhost) and ports
13. As an Operator I want to toggle anonymous telemetry and privacy settings via configuration
14. As a Performance Engineer I want to configure background queue worker counts via configuration
15. As a Performance Engineer I want to configure retry logic and limits for failed jobs via configuration

### **Ingestion & Pipelines**
16. As a ContextHelp User I want to ingest raw content (URLs, text) via the Command Line Interface
17. As a Researcher I want to ingest local files directly from my filesystem
18. As a Developer I want to ingest a specific Git repository URL (`url.repo`)
19. As a Podcaster I want to ingest audio files to generate transcripts and summaries (`audio.transcript`)
20. As a User I want to ingest video files to extract visual context and speech (`video.analysis`)
21. As a User I want to use Optical Character Recognition (OCR) to extract text from images (`image.ocr`)
22. As a User I want to provide semantic hints (hashtags) during ingestion to guide AI enrichment
23. As a User I want to force a specific pipeline to execute on my input (overriding auto-detection)
24. As a User I want to set default pipelines for specific content types in my configuration
25. As a Data Scientist I want to pass specific parameters (thresholds, temperature) to pipelines via configuration
26. As a Debugger I want to ingest content in "Raw Mode" to skip AI enrichment

### **Enrichment & Knowledge Graph**
27. As a User I want the engine to automatically generate a title and summary for ingested content
28. As a User I want content to be automatically tagged based on my subscribed Taxonomy Registries
29. As a User I want to see the "Decision Rationale" for why the AI applied specific tags
30. As a User I want to store the "Provenance" (source URL, time, author) of every bookmark
31. As a User I want the engine to split long content into semantically relevant "Sections"

### **Search, Retrieval & Weighting**
32. As a User I want to list bookmarks with advanced filtering (tags, time, type, pipeline)
33. As a User I want to sort search results by "Weight" to prioritize content based on my specific context
34. As a User I want to sort search results by "Recency" or "View Count"
35. As a User I want to search for bookmarks that match a specific "Hint" I provided earlier
36. As a User I want to view the full details (YAML/JSON) of a single bookmark
37. As a User I want to see a visual indication of which Registry contributed to a bookmark's tags

### **Data Management & Storage**
38. As a User I want to manually edit bookmark metadata, tags, and decisions via the CLI
39. As a User I want to delete bookmarks individually by ID or in bulk by filter tags
40. As an Operations Engineer I want to configure the storage backend (JSON, SQLite, or Postgres)
41. As an Operations Engineer I want to migrate existing data between storage backends safely
42. As a User I want to export my knowledge graph to standard formats (JSON/YAML) for backup
43. As a User I want concurrency-safe storage to allow multiple agents to read/write simultaneously

### **Registry Management**
44. As a ContextHelp User I want to manage registry subscriptions (add, remove, list) via the CLI
45. As a User I want to temporarily disable a Registry without deleting it (`ch registry disable`)
46. As a User I want to view metadata (maintainer, version, description) for a remote Registry
47. As a User I want to subscribe to public Taxonomy Registries to align my vocabulary
48. As a User I want to subscribe to "Weights Registries" to adjust how the engine scores content relevance
49. As a User I want to load a Registry from a local file path for offline usage
50. As a Registry Maintainer I want to validate my Registry definition file against the protocol schema

### **Agents & Context Profiles**
51. As an Agent Developer I want to define Agent Profiles in the config file (Registries, Weights, Tags)
52. As an Agent Developer I want to manage Agent Profiles programmatically via the CLI (`ch agent add/remove`)
53. As a User I want to query data masquerading as a specific Agent (`--agent`) to see their worldview
54. As a Security Architect I want to enforce a "Deny by Default" policy for Agent tag access
55. As an Agent Developer I want to assign specific pipelines to an Agent Profile to restrict capabilities

### **API & Integration**
56. As a System Integrator I want to run a local REST API server to query context from web apps
57. As a High-Performance Developer I want to access the engine via gRPC for low-latency agent lookup
58. As a Scripter I want CLI commands to output pure JSON (`--json`) for piping into tools like `jq`
59. As an API Consumer I want to request a specific Agent's context window via an API parameter

### **Localization (I18N)**
60. As a User I want to define my preferred language order for content consumption
61. As a User I want summaries and tags to be automatically translated into my preferred language
62. As a User I want to filter search results to show only content in its original source language
63. As a User I want to manually override the language detection during ingestion (`--lang`)
64. As a Plugin Developer I want to extend the schema to store translation maps without breaking the core engine
