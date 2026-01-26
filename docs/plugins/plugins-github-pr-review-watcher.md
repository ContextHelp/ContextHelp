# GitHub PR Review Watcher Plugin (Optional Core Plugin)

The **GitHub PR Review Watcher Plugin** monitors pull request reviews in configured repositories to extract and surface lessons, insights, code patterns, and technical opinions from collaborators. It enriches the knowledge graph with team conventions, review patterns, and evolving best practices.

This plugin is implemented entirely as an optional extension using the plugin architecture defined in [plugins.md](plugins.md) and [plugins-api.md](plugins-api.md), requiring no changes to core dPKMS or ctxt.

---

## Overview

The GitHub PR Review Watcher Plugin enables ContextHelp to:

- Monitor specified GitHub repositories for new PR review activity
- Extract actionable insights, patterns, and opinions from review comments
- Store insights as knowledge objects in dPKMS
- Identify recurring themes, code conventions, and team preferences
- Surface lessons learned from review feedback via notification plugin
- Build a knowledge base of team-specific practices
- Track collaborator expertise through entity relationships
- Detect controversial or frequently-debated topics

The plugin is designed for:

- Development teams learning from code review patterns
- Individual developers tracking feedback trends
- Tech leads understanding team conventions
- Documentation teams capturing implicit knowledge
- Engineering managers identifying training opportunities

---

## Goals

- Provide automated learning from PR review interactions entirely in plugin space
- Capture implicit team knowledge that lives in review comments
- Surface patterns and conventions before they're formally documented
- Build a searchable knowledge base of review insights
- Identify subject matter experts through entity graph relationships
- Detect emerging best practices and anti-patterns
- Enable proactive learning from collaborative feedback
- Demonstrate rich plugin ecosystem capabilities without core changes

---

## Architecture Overview

The plugin is composed of:

1. **Plugin Manifest** (`plugin.json`)
   - Declares capabilities, hooks, knowledge object types, permissions

2. **Webhook Listener / Polling Service**
   - Receives PR review events from configured repositories
   - Optional polling mode for repositories without webhook access
   - Runs as scheduled background task

3. **Review Content Analyzer** (Pipeline Steps)
   - Parses review comments, suggestions, and discussions
   - Extracts code patterns, conventions, and opinions
   - Categorizes insights by type (security, performance, style, etc.)

4. **Knowledge Object Creation** (via post_ingest hook)
   - Creates `pr-review-insight` knowledge objects
   - Attaches plugin metadata with categorization
   - Emits entity mentions for collaborators and topics

5. **Plugin-Owned Storage** (`state.json`)
   - Tracks repository sync state
   - Maintains webhook secrets
   - Stores processing metadata
   - Pattern frequency counters

6. **Notification Integration** (via Notification Plugin API)
   - Surfaces high-priority insights
   - Configurable quality thresholds

7. **CLI Integration** (via on_cli_start hook)
   - Injects pending insights into CLI output
   - Provides query commands for insight exploration

---

## Plugin Manifest (plugin.json)

```json
{
  "name": "github-pr-watcher",
  "version": "1.0.0",
  "description": "Monitors GitHub PR reviews to extract team conventions and insights",
  "entrypoint": "code.so",
  "hooks": [
    "post_ingest",
    "on_cli_start",
    "scheduled"
  ],
  "pipelines": [
    "github.pr_review.fetch",
    "github.pr_review.analyze",
    "github.pr_review.extract_insights"
  ],
  "object_types": [
    "pr-review-insight",
    "pr-review-pattern"
  ],
  "permissions": {
    "network": true,
    "filesystem": true
  },
  "requires_plugin_api": ">=1.0,<2.0",
  "dependencies": {
    "plugins": ["notifications"]
  }
}
```

---

## Storage Model

The plugin stores its state under:

```
~/.contexthelp/plugins/github-pr-watcher/
  plugin.json          # manifest
  state.json           # plugin-owned state
  cache/               # cached PR data
  logs/                # plugin logs
```

### state.json Structure

```json
{
  "state_version": "1.0",
  "repositories": [
    {
      "owner": "myorg",
      "repo": "backend-api",
      "enabled": true,
      "last_sync_timestamp": 1710000000,
      "last_pr_number": 1234,
      "webhook_secret_hash": "encrypted_value"
    }
  ],
  "patterns": [
    {
      "pattern_id": "uuid",
      "pattern_type": "code_convention",
      "description": "Team prefers async/await over .then() chains",
      "category": "code_style",
      "frequency": 23,
      "first_seen": 1700000000,
      "last_seen": 1710000000,
      "supporting_reviews": ["pr#1234", "pr#1456", "pr#1789"]
    }
  ],
  "collaborators": [
    {
      "username": "senior-dev",
      "entity_slug": "github.senior-dev",
      "review_count": 145,
      "categories": ["security", "performance", "database"],
      "insight_quality_avg": 0.87
    }
  ],
  "metadata": {
    "last_webhook_event": 1710000000,
    "total_reviews_processed": 1523,
    "insights_created": 342
  }
}
```

This requires **no changes** to core dPKMS storage.

---

## Configuration Model

Plugin configuration under `plugins.github-pr-watcher`:

```yaml
plugins:
  github-pr-watcher:
    enabled: true
    mode: webhook  # or "polling"
    polling_interval_seconds: 900  # 15 minutes

    repositories:
      - owner: myorg
        repo: backend-api
        branch_filter: ["main", "develop"]

      - owner: myorg
        repo: frontend-app
        branch_filter: ["main"]

    analysis:
      insight_categories:
        - security
        - performance
        - architecture
        - code_style
        - testing
        - documentation

      min_confidence: 0.7
      extract_code_patterns: true
      track_collaborator_expertise: true

    surfacing:
      auto_surface_threshold: 0.8  # Quality score
      notify_high_value: true
      aggregate_similar_insights: true
      max_insights_per_day: 10

    privacy:
      store_author_names: true
      store_code_snippets: true
      anonymize_after_days: 90
      redact_sensitive_patterns: true

    github:
      token: ${GITHUB_TOKEN}
      webhook_secret: ${GITHUB_WEBHOOK_SECRET}
```

---

## Knowledge Object Types

### pr-review-insight

Custom knowledge object type for extracted insights:

```json
{
  "id": "uuid",
  "type": "pr-review-insight",
  "url": "https://github.com/myorg/backend-api/pull/1234#discussion_r123456",
  "title": "Always use parameterized queries to prevent SQL injection",
  "body": "Reviewer feedback: This query is vulnerable to SQL injection...",
  "tags": ["security", "sql-injection", "input-validation"],
  "mentions": [
    "github.senior-dev",
    "pattern.parameterized-queries",
    "codebase.myorg/backend-api"
  ],
  "plugins": {
    "github-pr-watcher": {
      "pr_number": 1234,
      "pr_url": "https://github.com/myorg/backend-api/pull/1234",
      "reviewer": "senior-dev",
      "category": "security",
      "severity": "high",
      "quality_score": 0.92,
      "code_pattern_before": "SELECT * FROM users WHERE id = ${userId}",
      "code_pattern_after": "db.query('SELECT * FROM users WHERE id = ?', [userId])",
      "file_paths": ["src/controllers/user.ts"],
      "extracted_at": 1710000000,
      "surfaced": true
    }
  }
}
```

### pr-review-pattern

Knowledge object type for recurring patterns:

```json
{
  "id": "uuid",
  "type": "pr-review-pattern",
  "url": "internal://patterns/async-await-preference",
  "title": "Team prefers async/await over promise chains",
  "body": "Established convention detected across 23 reviews...",
  "tags": ["code-style", "javascript", "async", "team-convention"],
  "mentions": [
    "pattern.async-await",
    "codebase.myorg/backend-api",
    "codebase.myorg/frontend-app"
  ],
  "plugins": {
    "github-pr-watcher": {
      "pattern_type": "code_convention",
      "category": "code_style",
      "frequency": 23,
      "confidence": 0.95,
      "first_seen": 1700000000,
      "last_reinforced": 1710000000,
      "supporting_reviews": [
        "myorg/backend-api#1234",
        "myorg/backend-api#1456",
        "myorg/frontend-app#789"
      ]
    }
  }
}
```

---

## Plugin Pipelines

### Pipeline: github.pr_review.fetch

Fetches PR review data from GitHub API:

**Steps:**
1. `FetchPRReviews` - Retrieve PR reviews since last sync
2. `FilterRelevantReviews` - Apply branch and reviewer filters
3. `CacheReviewData` - Store in plugin cache

**Input:** Repository configuration
**Output:** Array of review data structures

### Pipeline: github.pr_review.analyze

Analyzes review content for insights:

**Steps:**
1. `ParseReviewComments` - Extract comment text and context
2. `ExtractCodePatterns` - Identify before/after code examples
3. `CategorizeInsight` - Classify by category (security, performance, etc.)
4. `ScoreQuality` - Evaluate actionability and specificity
5. `DetectPatterns` - Check for recurring themes

**Input:** Review data
**Output:** Structured insight candidates

### Pipeline: github.pr_review.extract_insights

Creates knowledge objects from analyzed reviews:

**Steps:**
1. `CreateInsightObject` - Generate pr-review-insight knowledge object
2. `ExtractMentions` - Emit reviewer, topic, and codebase mentions
3. `UpdatePatternFrequency` - Track recurring patterns in state.json
4. `UpdateCollaboratorProfile` - Track reviewer expertise
5. `TriggerNotification` - Notify via notification plugin if high-value

**Input:** Insight candidates
**Output:** Knowledge object IDs

---

## Hook Implementations

### scheduled (Webhook Polling / Sync)

```go
func ScheduledTask(name string) error {
    if name == "github-pr-review-sync" {
        repos := plugin.Config().Repositories
        for _, repo := range repos {
            // Enqueue fetch pipeline for each repository
            plugin.EnqueueJob("github.pr_review.fetch", map[string]interface{}{
                "owner": repo.Owner,
                "repo":  repo.Repo,
            })
        }
    }
    return nil
}
```

Scheduled every `polling_interval_seconds` (e.g., 15 minutes).

### post_ingest

```go
func PostIngest(obj KnowledgeObject) error {
    // If this is a newly created pr-review-insight, check if we should notify
    if obj.Type == "pr-review-insight" {
        metadata := obj.Plugins["github-pr-watcher"]
        qualityScore := metadata["quality_score"].(float64)

        if qualityScore >= plugin.Config().Surfacing.AutoSurfaceThreshold {
            // Call notification plugin
            notifications.Create("pr_insight", "info", map[string]interface{}{
                "category":      metadata["category"],
                "lesson":        obj.Title,
                "pr_url":        metadata["pr_url"],
                "reviewer":      metadata["reviewer"],
                "quality_score": qualityScore,
            })

            // Mark as surfaced in plugin state
            state := plugin.State()
            state["last_surfaced_insight"] = obj.ID
            plugin.WriteState(state)
        }
    }

    // Update pattern frequency if pattern detected
    if obj.Type == "pr-review-pattern" {
        // Pattern tracking logic
    }

    return nil
}
```

### on_cli_start

```go
func OnCLIStart(context CLIContext) ([]CLIInjection, error) {
    state := plugin.State()
    config := plugin.Config()

    // Check if there are new high-value insights since last shown
    newInsights := countNewInsights(state)

    if newInsights > 0 && config.Surfacing.NotifyHighValue {
        return []CLIInjection{
            {
                Position: "before_output",
                Text: fmt.Sprintf("\n💡 %d new code review insights available. Run: ctxt list type:pr-review-insight --sort quality --limit 5\n", newInsights),
            },
        }, nil
    }

    return nil, nil
}
```

---

## Insight Extraction Strategies

### Pattern Detection

The plugin identifies recurring patterns by:

1. **Lexical Analysis**: Detecting repeated phrases and terminology
2. **Code Diff Analysis**: Comparing suggested changes across reviews
3. **Semantic Clustering**: Grouping related review comments via embeddings
4. **Frequency Tracking**: Counting how often similar feedback appears

### Quality Scoring

Each insight receives a quality score (0.0-1.0) based on:

- **Actionability**: Can the lesson be applied to other code? (+0.3)
- **Specificity**: Is it concrete vs. generic advice? (+0.3)
- **Frequency**: How often does this pattern appear? (+0.2)
- **Reviewer Authority**: Track record of the reviewer (+0.1)
- **Discussion Depth**: Level of engagement and refinement (+0.1)

Insights scoring >= `auto_surface_threshold` (e.g., 0.8) are automatically surfaced.

### Deduplication

Similar insights are merged by:

- Semantic similarity comparison (embeddings)
- Topic clustering via mentions
- Time-window aggregation
- User feedback (manual bookmark/dismiss)

---

## Integration with dPKMS

Insights leverage dPKMS knowledge graph:

**Entity Relations:**
```
pr-review-insight
  ├─ mentioned_by → github.senior-dev (collaborator entity)
  ├─ mentioned_by → pattern.parameterized-queries (pattern entity)
  ├─ mentioned_by → codebase.myorg/backend-api (codebase entity)
  └─ relates_to → security.sql-injection (topic entity)
```

This enables:
- Querying insights by reviewer: `mention:github.senior-dev AND type:pr-review-insight`
- Finding patterns by topic: `mention:pattern.* AND type:pr-review-pattern`
- Discovering expertise: `ctxt graph backlinks github.senior-dev`
- Tracking pattern evolution over time

---

## Notification Integration

High-value insights trigger notifications via the Notification Plugin API:

### Notification Trigger

```go
notifications.Create("pr_insight", "info", map[string]interface{}{
    "category":      "security",
    "lesson":        "Always use parameterized queries to prevent SQL injection",
    "pr_url":        "https://github.com/myorg/backend-api/pull/1234",
    "reviewer":      "senior-dev",
    "quality_score": 0.92,
    "object_id":     "insight-uuid",
})
```

### Notification Configuration

```yaml
plugins:
  notifications:
    rules:
      - type_match: "pr_insight"
        levels: ["info"]
        channels: ["cli"]
```

User sees:

```
💡 New Code Review Insight (Quality: 0.92)
"Always use parameterized queries to prevent SQL injection"
  From: senior-dev in myorg/backend-api#1234
  View: ctxt open insight-uuid
```

---

## CLI Query Patterns

Users interact with insights through standard ctxt queries:

### List Recent Insights

```bash
ctxt list type:pr-review-insight --sort created --limit 10
```

### Filter by Category

```bash
ctxt list type:pr-review-insight tag:security --sort quality
```

### Find Reviewer Expertise

```bash
ctxt list type:pr-review-insight mention:github.senior-dev
```

### View Established Patterns

```bash
ctxt list type:pr-review-pattern --sort frequency
```

### Search by Topic

```bash
ctxt find "input validation" type:pr-review-insight
```

### Open Specific Insight

```bash
ctxt open <insight-id>
```

---

## Example Workflows

### 1. Detecting Security Pattern

**PR Review Event:**
```
Reviewer: "This is vulnerable to SQL injection. Use parameterized queries instead."
Code change: SELECT * FROM users WHERE id = ${userId}
          → db.query('SELECT * FROM users WHERE id = ?', [userId])
```

**Plugin Actions:**
1. Webhook received → enqueues `github.pr_review.fetch` pipeline
2. Pipeline `github.pr_review.analyze` runs:
   - Parses review comment
   - Extracts code patterns (before/after)
   - Categorizes as "security" with severity "high"
   - Scores quality at 0.91 (actionable, specific, high-authority reviewer)
3. Pipeline `github.pr_review.extract_insights` runs:
   - Creates pr-review-insight knowledge object
   - Emits mentions: `github.senior-dev`, `pattern.parameterized-queries`, `codebase.myorg/backend-api`
   - Checks pattern frequency in state.json (increment counter)
   - Triggers notification (quality 0.91 >= threshold 0.8)
4. `post_ingest` hook executes:
   - Calls notification plugin API
   - Updates plugin state with last_surfaced_insight
5. Next CLI invocation, `on_cli_start` hook shows:
   ```
   💡 New code review insight available (security)
   ```

**User Experience:**
```bash
$ ctxt list type:pr-review-insight --new

pr-review-insight | Quality: 0.91 | Security
"Always use parameterized queries to prevent SQL injection"
  From: senior-dev in myorg/backend-api#1234
  Pattern detected 2 times across reviews

  Example:
  - ❌ SELECT * FROM users WHERE id = ${userId}
  - ✅ db.query('SELECT * FROM users WHERE id = ?', [userId])

  Applied to: src/controllers/*.ts
  View: ctxt open <insight-id>
```

### 2. Identifying Team Convention

**Multiple Reviews:**
```
Review 1: "Let's use async/await here instead of .then()"
Review 2: "Can we convert this to async/await for consistency?"
Review 3: "Our team convention is to prefer async/await"
Review 4-23: Similar feedback
```

**Plugin Actions:**
1. As reviews are processed, plugin detects recurring theme
2. Pattern frequency counter increments in state.json
3. After reaching threshold (e.g., 5 occurrences), creates pr-review-pattern knowledge object
4. Pattern object includes:
   - Description: "Team prefers async/await over promise chains"
   - Frequency: 23 reviews
   - Supporting review links
   - Confidence score: 0.95

**User Experience:**
```bash
$ ctxt list type:pr-review-pattern --sort frequency

pr-review-pattern | Frequency: 23 | Code Style
"Team prefers async/await over .then() promise chains"
  Confidence: 0.95
  First seen: 3 months ago
  Last reinforced: 2 days ago

  This is a strong team convention. Consider applying to new code.
  Supporting reviews: myorg/backend-api#1234, #1456, #1789, ...
```

### 3. Finding Subject Matter Expert

**User Query:**
```bash
$ ctxt graph backlinks github.senior-dev
```

**dPKMS Response:**
```
Entity: github.senior-dev
  Type: collaborator
  Mentioned in: 34 knowledge objects

Insights by Category:
  - security: 12 insights (avg quality: 0.89)
  - performance: 8 insights (avg quality: 0.85)
  - database: 14 insights (avg quality: 0.91)

Top Insights:
  1. "Add composite index on (user_id, created_at) for faster queries"
     Quality: 0.94 | myorg/backend-api#567

  2. "Always use parameterized queries to prevent SQL injection"
     Quality: 0.92 | myorg/backend-api#1234
```

---

## Use Cases

### 1. Onboarding New Developers
New team members query insights to learn team conventions without reading every PR.

### 2. Proactive Learning
Developers browse high-quality insights before writing code in unfamiliar areas.

### 3. Documentation Generation
Tech writers query patterns to generate style guides and best practices docs.

### 4. Technical Debt Tracking
Identify frequently-mentioned issues that warrant systematic fixes.

### 5. Training Opportunities
Managers detect knowledge gaps where multiple developers make similar mistakes.

### 6. Architecture Evolution
Track how team opinions evolve by querying insights over time windows.

---

## Privacy & Security

### Privacy Considerations

- **Code Snippet Storage**: Configurable per repository via `store_code_snippets`
- **Author Attribution**: Can be anonymized after `anonymize_after_days` (e.g., 90 days)
- **Sensitive Data**: Automatic redaction of patterns matching secrets/credentials
- **Access Control**: Respects GitHub repository permissions via token scope

### Security Model

- **Webhook Secrets**: Stored encrypted in state.json, validated on receipt
- **Token Storage**: GitHub token from environment variable or OS keychain
- **Local-First**: All data stored locally in dPKMS, never sent to third parties
- **Audit Trail**: All plugin actions logged via `plugin.LogInfo()`

### Data Retention

```yaml
plugins:
  github-pr-watcher:
    privacy:
      anonymize_after_days: 90      # Replace author names with pseudonyms
      redact_code_after_days: 180   # Remove code snippets
      delete_insights_after_days: 365  # Purge old insights
```

---

## Plugin Dependencies

### Required Plugins

- **Notification Plugin**: For surfacing high-value insights

### Optional Integrations

- **Embeddings Plugin**: For semantic similarity and deduplication
- **Export Plugin**: For generating documentation from patterns

---

## Future Enhancements

### Phase 2 Features

- **Proactive Suggestions**: Surface relevant insights when `ctxt analyze <file>` is run
- **IDE Integration**: Real-time insight surfacing in VS Code via MCP protocol
- **Cross-Repository Patterns**: Detect patterns spanning multiple codebases
- **Review Quality Metrics**: Track review effectiveness and insight generation rates

### Phase 3 Features

- **AI Review Assistant**: Suggest improvements based on learned patterns before PR creation
- **Conflict Detection**: Flag code that contradicts established team patterns
- **Convention Enforcement**: Optional pre-commit checks for pattern adherence
- **Team Analytics**: Visualize review patterns, expertise distribution, trend analysis

---

## Summary

The GitHub PR Review Watcher Plugin is a fully self-contained module that:

- Works entirely with plugin storage, hooks, and pipelines
- Requires **no modifications** to core dPKMS or ctxt architecture
- Extracts actionable lessons from collaborative code reviews
- Builds a searchable knowledge base of team practices
- Surfaces insights via Notification Plugin integration
- Integrates with dPKMS knowledge graph via entities and mentions
- Respects privacy and security boundaries through configuration
- Provides rich query capabilities through standard ctxt commands
- Demonstrates complex plugin capabilities (pipelines, hooks, inter-plugin communication)

It shows how ContextHelp plugins can transform passive review history into active learning opportunities, capturing implicit team knowledge and making it searchable and actionable—all without touching core code.
