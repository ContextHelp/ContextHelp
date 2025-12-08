# gRPC API

This document specifies the **ContextHelp gRPC API**, the canonical typed interface for high-performance ingestion, retrieval, entity-aware mentions, and authorized plugin-driven extensions.

While the REST API is optimized for human and web use, the gRPC API is optimized for:

- UIs with real-time interactions
- local or remote agents
- plugins invoking the engine programmatically
- streaming pipelines
- bulk ingestion
- multi-step retrieval workflows
- structured, strongly typed integration

gRPC provides:

- high throughput
- low latency
- bi-directional streaming
- strong typing and schema evolution
- language bindings for all major ecosystems

Plugins MAY define additional RPC services under their own namespaces without modifying core.

---

## Overview

### Services

The gRPC surface area is divided into logical services:

| Service | Responsibility |
|--------|----------------|
| `AnalyzeService` | Ingestion via job-based pipelines |
| `JobService` | Query, retry, manage job lifecycle |
| `BookmarkService` | Search, list, update, delete bookmarks |
| `RegistryService` | Discover registries and capabilities |
| `EntityService` | Resolve entities, list mentions, navigate backlinks |
| `SuggestionService` | Tag and hint suggestions (optional) |
| **`PluginService` (reserved)** | Plugin-defined RPCs mounted dynamically |

Plugins MAY register additional RPC services under the namespace:

```
contexthelp.plugins.<pluginName>.v1
```

These services do not require any modification to core API files.

```mermaid
graph LR
  A[Clients<br/>UIs, Agents, Plugins] --> B[gRPC Endpoint]
  B --> C[AnalyzeService]
  B --> D[JobService]
  B --> E[BookmarkService]
  B --> F[RegistryService]
  B --> G[EntityService]
  B --> H[SuggestionService]
  B --> P[Plugin Services<br/>Dynamic, per-plugin]

  E --> I[(Local Storage)]
  F --> J[(Registries)]
  G --> J
  G --> I
  P --> I
```

### Protobuf Package

The recommended namespace:

```protobuf
package contexthelp.v1;
```

Future versions will increment the package (for example, `contexthelp.v2`).

Plugin-defined APIs MUST use:

```protobuf
package contexthelp.plugins.<pluginName>.v1;
```

---

## Common Messages

Shared across services.

### Timestamp

- Uses `google.protobuf.Timestamp`.

### KeyValue

```protobuf
message KeyValue {
  string key = 1;
  string value = 2;
}
```

### Error (For Streaming RPCs)

```protobuf
message Error {
  string code = 1;
  string message = 2;
  map<string, string> details = 3;
}
```

---

## Job-Related Messages

### AnalyzeRequest

```protobuf
message AnalyzeRequest {
  string type = 1;                     // text, url, image, audio, video, other
  string content = 2;                  // raw text, URL, base64 data, etc.
  repeated string hints = 3;           // user hints (#tags, freeform cues)
  string pipeline = 4;                 // optional explicit pipeline
  string language = 5;                 // optional declared input language
  bool skip_translation = 6;           // override i18n plugins for this call

  // Canonical entity IDs or slugs already known to the caller.
  repeated string mentions = 7;

  // Plugins may attach metadata to influence plugin-defined pipelines.
  map<string, string> plugin_metadata = 8;
}
```

### AnalyzeResponse

```protobuf
message AnalyzeResponse {
  string job_id = 1;
  string status = 2;                   // pending, running, completed, failed
}
```

### Job

```protobuf
message Job {
  string id = 1;
  string type = 2;                           // analyze, plugin:<pluginName>:*
  string status = 3;
  google.protobuf.Timestamp created_at = 4;
  google.protobuf.Timestamp updated_at = 5;

  AnalyzeRequest input = 6;

  string pipeline = 7;
  repeated JobStep steps = 8;

  string result_bookmark_id = 9;
  string error = 10;

  // Plugins may attach additional state (e.g., price_monitor, rss_feed)
  map<string, string> plugin_metadata = 11;
}
```

### JobStep

```protobuf
message JobStep {
  string name = 1;
  string status = 2;
  google.protobuf.Timestamp started_at = 3;
  google.protobuf.Timestamp finished_at = 4;
  string error = 5;
}
```

---

## Bookmark and Mention Messages

### Bookmark

```protobuf
message Bookmark {
  string id = 1;

  google.protobuf.Timestamp created_at = 2;
  google.protobuf.Timestamp last_matched_at = 3;

  string type = 4;                           // text, url, feed, plugin-defined
  string subtype = 5;

  string title = 6;
  string summary = 7;

  string raw = 8;

  string language = 9;

  repeated Tag tags = 10;
  repeated string hints = 11;

  string pipeline = 12;
  string source = 13;

  uint32 views = 14;
  uint32 matches = 15;

  map<string, Translation> translations = 16;

  repeated Section sections = 17;
  repeated Decision decisions = 18;

  repeated RegistryReference registry_refs = 19;

  repeated string mentions = 20;
  repeated EntityRef entities = 21;

  // Namespaced plugin fields:
  // bookmark.plugins.<pluginName>.* becomes:
  map<string, string> plugin_metadata = 22;
}
```

### Tag

```protobuf
message Tag {
  string label = 1;
  double weight = 2;
  string polarity = 3;
  repeated string influenced_by = 4;
}
```

### Section

```protobuf
message Section {
  string name = 1;
  string summary = 2;
}
```

### Decision

```protobuf
message Decision {
  string section = 1;
  string rating = 2;
  string reason = 3;
  string improvement = 4;
}
```

### Translation

```protobuf
message Translation {
  string summary = 1;
  map<string, string> fields = 2;
}
```

### RegistryReference

```protobuf
message RegistryReference {
  string registry_id = 1;
  string tag = 2;
}
```

### EntityRef

```protobuf
message EntityRef {
  string id = 1;
  string registry_id = 2;
  string source = 3;
}
```

---

## Entity Messages

```protobuf
message Entity {
  string id = 1;
  string title = 2;
  string description = 3;

  repeated string aliases = 4;

  map<string, string> translations = 5;

  string source = 6;
  map<string, string> metadata = 7;

  uint32 version = 8;
}
```

### EntityBacklink

```protobuf
message EntityBacklink {
  string entity_id = 1;
  string bookmark_id = 2;
  string bookmark_title = 3;
  string bookmark_summary = 4;
}
```

---

## Query Messages (RSQL-Based)

### SearchRequest

```protobuf
message SearchRequest {
  string q = 1;

  repeated string tag = 2;
  string type = 3;
  string pipeline = 4;

  string after = 5;
  string before = 6;

  string registry = 7;

  string orig_lang = 8;
  string prefer_lang = 9;
  string translate = 10;

  uint32 limit = 11;
  uint32 offset = 12;

  repeated string sort = 13;
  string dir = 14;

  repeated string mention = 15;

  bool mentions_only = 16;

  // Plugins may extend filtering via namespaced keys.
  map<string, string> plugin_filters = 17;
}
```

---

## Registry Messages

```protobuf
message Registry {
  string id = 1;
  string name = 2;
  string description = 3;

  repeated string type = 4;
  string url = 5;

  bool sync_supported = 6;
  string sync_mode = 7;

  string auth_type = 8;
  bool auth_configured = 9;

  map<string, string> capabilities = 10;
}
```

---

## Suggestion Messages (Optional)

### TagSuggestionRequest

```protobuf
message TagSuggestionRequest {
  string q = 1;
  uint32 limit = 2;
}
```

### TagSuggestion

```protobuf
message TagSuggestion {
  string label = 1;
  string source = 2;
  double confidence = 3;
}
```

### TagSuggestionResponse

```protobuf
message TagSuggestionResponse {
  repeated TagSuggestion suggestions = 1;
}
```

---

## Services

### AnalyzeService

```protobuf
service AnalyzeService {
  rpc Analyze(AnalyzeRequest) returns (AnalyzeResponse);
}
```

### JobService

```protobuf
service JobService {
  rpc ListJobs(ListJobsRequest) returns (ListJobsResponse);
  rpc GetJob(GetJobRequest) returns (Job);
  rpc RetryJob(RetryJobRequest) returns (Job);
}
```

### BookmarkService

```protobuf
service BookmarkService {
  rpc Search(SearchRequest) returns (SearchResponse);
  rpc Get(GetBookmarkRequest) returns (Bookmark);
  rpc Update(UpdateBookmarkRequest) returns (Bookmark);
  rpc Delete(DeleteBookmarkRequest) returns (DeleteBookmarkResponse);
}
```

### RegistryService

```protobuf
service RegistryService {
  rpc ListRegistries(ListRegistriesRequest) returns (ListRegistriesResponse);
  rpc GetRegistry(GetRegistryRequest) returns (Registry);
}
```

### EntityService

```protobuf
service EntityService {
  rpc ListEntities(ListEntitiesRequest) returns (ListEntitiesResponse);
  rpc GetEntity(GetEntityRequest) returns (Entity);
  rpc ResolveEntity(ResolveEntityRequest) returns (Entity);
  rpc ListEntityBacklinks(ListEntityBacklinksRequest) returns (ListEntityBacklinksResponse);
}
```

### SuggestionService (Optional)

```protobuf
service SuggestionService {
  rpc SuggestTags(TagSuggestionRequest) returns (TagSuggestionResponse);
}
```

---

## Plugin Services

Plugins may expose their own gRPC services without modifying core.

### Naming Convention

```
package contexthelp.plugins.<pluginName>.v1;
```

### Example

A price monitoring plugin may define:

```protobuf
service PriceMonitorService {
  rpc GetPriceHistory(GetPriceHistoryRequest) returns (GetPriceHistoryResponse);
  rpc GetAlerts(GetPriceAlertsRequest) returns (GetPriceAlertsResponse);
}
```

A notification plugin may define:

```protobuf
service NotificationService {
  rpc GetPending(GetNotificationRequest) returns (NotificationResponse);
  rpc MarkRead(NotificationAckRequest) returns (NotificationAckResponse);
}
```

Core dynamically mounts these services based on plugin registration.

No core modification is required.

---

## Mention Semantics

(Hints vs tags vs mentions — unchanged logic)

---

## Streaming Extensions

Same as before; plugins may also define streaming endpoints.

---

## Summary

This updated gRPC API now explicitly supports:

- **plugin-defined RPC services**
- **plugin-defined bookmark types**
- **plugin metadata on jobs and bookmarks**
- **plugin-defined filters**
- **plugin-influenced ingestion through metadata**
- **non-core notification & alert surfacing**
- **safe coexistence of core and plugin pipelines**

The core API remains stable, minimal, and deterministic, while enabling powerful extensions via plugins—without requiring core changes.