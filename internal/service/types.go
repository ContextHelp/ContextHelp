package service

import (
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/composition"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	uri "hop.top/cite/scheme"
)

// AnalyzeRequest represents a request to analyze content.
type AnalyzeRequest struct {
	Content string `json:"content"`
	// ContentEncoding says how Content maps to the captured bytes: ""
	// (UTF-8 text, as sent) or "base64" (standard encoding, for bytes
	// that are not valid UTF-8, such as a PNG or a PDF). The service
	// decodes it before any other use of Content, so dedup and the content
	// hash see the decoded bytes; any other value is refused.
	ContentEncoding string `json:"content_encoding,omitempty"`
	Type            string `json:"type"`
	Pipeline        string `json:"pipeline,omitempty"`
	Source          string `json:"source,omitempty"`
	KnownHash       string `json:"known_hash,omitempty"`   // pre-computed content hash; skips re-hashing
	SourceTitle     string `json:"source_title,omitempty"` // human-readable title of source (e.g. page title)
	AuthState       string `json:"auth_state,omitempty"`   // opaque auth state token (browser extension)
	Raw             bool   `json:"raw,omitempty"`          // skip AI enrichment; store object immediately in raw state
	NoFanout        bool   `json:"no_fanout,omitempty"`    // skip post-ingest fan-out enrichment
	SourceKey       string `json:"source_key,omitempty"`   // external dedup key (Slack ts, tweet ID, etc.)
	// ExpiresAt optionally schedules automatic expiration (UTC) for the
	// captured object. When ExpiryScope is empty the whole object is erased;
	// when non-empty only the described data is redacted in place.
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	ExpiryScope string     `json:"expiry_scope,omitempty"`
	Force       bool       `json:"force,omitempty"` // bypass duplicate detection
	// Mentions are caller-asserted @namespace.slug strings. They
	// are merged with auto-extracted mentions during pipeline execution and
	// become real mention edges + thin entity rows. Caller-supplied wins on
	// conflict — the merge step dedupes by slug.
	Mentions []string `json:"mentions,omitempty"`
	// Hints are caller-asserted hint strings (`ctxt capture --hint research`).
	// They become Tag{Source:"user"} entries on the persisted KnowledgeObject
	// after the auto-tagger merges. Stored on the Job as UserHints
	// for the worker to read at pipeline-start time.
	Hints []string `json:"hints,omitempty"`
	// Profile is the focus-profile slug the operator pinned at capture
	// time (`ctxt capture --profile founder`). Populates the
	// KnowledgeObject.ProfileID so the persisted object is partitioned
	// to that profile. Empty = global / no profile.
	Profile string `json:"profile,omitempty"`
	// IdempotencyKey identifies one logical submission (client-generated,
	// e.g. a UUID minted once per `ctxt analyze` invocation). The enqueue
	// surface dedupes on it: a replay carrying a key already present in
	// the jobs table returns the existing job id instead of minting a
	// second job. This makes retrying a submission whose response was
	// lost in transit safe. Empty = no dedupe (legacy callers). The raw
	// path (Raw: true) stores an object directly and does not consult
	// the key.
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	// Note is a free-form audit string the operator attached to the
	// capture (`ctxt capture --note "client kickoff 2026-Q2"`).
	// Populates KnowledgeObject.InboxNote so the note survives both
	// raw and pipeline paths. InboxNote is the canonical
	// "operator-attached" field on KnowledgeObject; reusing it here
	// keeps a single field name across capture, inbox, and pipeline.
	Note string `json:"note,omitempty"`
	// Data is caller-supplied structured JSON (`ctxt analyze --data`):
	// a video's title, channel and publish time, say. It is stored on the
	// object as metadata["data"] and is on the draft every pipeline step
	// receives. It must be a JSON object of at most MaxAnalyzeDataBytes
	// encoded.
	Data map[string]any `json:"data,omitempty"`
	// AmbientSource, Fingerprint, SessionID and Metadata are the optional
	// ambient-capture fields the enqueue client sends (ADR-066, ADR-067).
	// AmbientSource and Fingerprint land on the object as metadata keys
	// MetadataAmbientSourceKey and MetadataFingerprintKey; SessionID in
	// the objects.session_id column; Metadata is stored whole under the
	// object metadata key "client" and never merged into the keys the
	// server and pipeline steps own (see storageutil.ApplyAmbient).
	// Fingerprint dedup stays client-side.
	AmbientSource string         `json:"ambient_source,omitempty"`
	Fingerprint   string         `json:"fingerprint,omitempty"`
	SessionID     string         `json:"session_id,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// CreatePipelineRequest represents a request to create a custom pipeline.
type CreatePipelineRequest struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Steps       string                 `json:"steps"`
	Sandbox     *storage.SandboxConfig `json:"sandbox,omitempty"`
}

// CompositionResult is the structured output of ComposeWithCitations.
type CompositionResult = composition.Result

// InboxCaptureRequest carries data for a fast-path inbox capture.
type InboxCaptureRequest struct {
	Content   string
	Type      string
	Source    string
	InboxNote string
	// Hints are caller-asserted hint strings. On the inbox path
	// they land directly on KnowledgeObject.Tags with Source:"user" — no
	// pipeline runs at capture time, only at triage.
	Hints    []string
	Mentions []uri.URI
	// Profile is the focus-profile slug the operator pinned at capture
	// time. Populates KnowledgeObject.ProfileID so the inbox
	// item is partitioned to that profile from the start.
	Profile     string
	ExpiresAt   *time.Time
	ExpiryScope string
}

// InboxFilter specifies criteria for listing inbox items.
type InboxFilter struct {
	Limit  int
	Offset int
	Before time.Time
	After  time.Time
}

// TriageRequest carries parameters to promote an inbox object into the pipeline.
type TriageRequest struct {
	Pipeline string
	// Hints are caller-asserted hint strings; forwarded to
	// Job.UserHints when triage enqueues the pipeline job.
	Hints    []string
	Mentions []string
}

// InboxQueueFilter specifies criteria for the combined inbox queue view.
type InboxQueueFilter struct {
	Pending bool
	Failed  bool
	Raw     bool
	Limit   int
	Offset  int
}

// ScoreBreakdown holds per-signal scores explaining why a result ranked where it did.
type ScoreBreakdown struct {
	// FTS is the Reciprocal Rank Fusion contribution from the full-text search leg.
	FTS float64 `json:"fts"`
	// Vector is the RRF contribution from the vector similarity leg.
	Vector float64 `json:"vector"`
	// MentionBoost is the additive bonus from outbound mention count.
	MentionBoost float64 `json:"mention_boost"`
	// GraphRelevance is the additive bonus from inbound backlinks (direct + 2-hop).
	GraphRelevance float64 `json:"graph_relevance"`
	// WordOverlap is the additive bonus from projection-aware word overlap scoring.
	WordOverlap float64 `json:"word_overlap"`
	// TagsMatched is how many of the tags of the request's tagged filter
	// the result carries. Results order by it first (more first), then by
	// Total; it is not part of Total.
	TagsMatched int `json:"tags_matched,omitempty"`
	// Total is the sum of all signal contributions.
	Total float64 `json:"total"`
}

// HybridResult pairs a KnowledgeObject with its score breakdown.
// DocumentView is populated for all results; use it for display surfaces.
type HybridResult struct {
	Object       *storage.KnowledgeObject   `json:"object"`
	Breakdown    ScoreBreakdown             `json:"score_breakdown"`
	DocumentView storage.DocumentProjection `json:"document_view"`
}

// SearchDiagnostics captures information about candidates that surfaced from
// the FTS or vector legs but were dropped by the reranker's MinScore threshold.
// It lets callers distinguish "no docs matched any token" from
// "matches existed but all fell below threshold".
//
// All fields are populated on every search call (zero values are valid).
type SearchDiagnostics struct {
	// CandidateCount is the number of unique objects that surfaced from the
	// FTS or vector legs before reranker filtering.
	CandidateCount int `json:"candidate_count"`
	// BelowThresholdCount is the number of candidates dropped by the
	// reranker because their total score was strictly below Threshold.
	BelowThresholdCount int `json:"below_threshold_count"`
	// TopBelowThresholdScore is the highest total score among the dropped
	// candidates. Zero when BelowThresholdCount is zero.
	TopBelowThresholdScore float64 `json:"top_below_threshold_score,omitempty"`
	// Threshold is the MinScore threshold applied during this search call.
	Threshold float64 `json:"threshold"`
	// StalenessWarning is populated when one or more candidates in the
	// pre-filter set are stamped with a pipeline version older than the
	// registry's currently-installed version (ADR-070 §6). Soft
	// signal: search still returns the stale results, but the caller knows
	// to suggest `ctxt upgrade plan` to the operator.
	StalenessWarning *StalenessWarning `json:"staleness_warning,omitempty"`
	// Semantic reports the vector leg: whether it searched the default
	// embedding model's index and, when it could not, why (its Notice is
	// the one-line operator message). Nil for FTS-only searches.
	Semantic *retrieval.SemanticReport `json:"semantic,omitempty"`
}

// StalenessWarning summarises pipeline-version drift in a search result set.
// Populated by HybridSearch* when at least one candidate's `pipeline` field
// parses to an older version than the registry's currently-installed version
// for the same family (ADR-070 §6).
type StalenessWarning struct {
	Count  int    `json:"count"`
	Reason string `json:"reason"`
}

// HybridSearchResult is the wrapper envelope for HybridSearchExplain calls.
// It pairs the post-threshold result list with diagnostics covering candidates
// that surfaced from retrieval but were dropped by the reranker.
type HybridSearchResult struct {
	Results     []HybridResult    `json:"results"`
	Diagnostics SearchDiagnostics `json:"diagnostics"`
	// Trace lists every scored candidate and its stage. Nil unless the
	// caller used HybridSearchExplainFilteredWithTrace.
	Trace *SearchTrace `json:"trace,omitempty"`
}

// InboxQueueItem is a row in the combined inbox queue view.
// Represents either a job (pending/failed) or a raw object.
type InboxQueueItem struct {
	// ID is the job ID or object ID depending on Kind.
	ID string `json:"id"`
	// Kind is "job" or "object".
	Kind string `json:"kind"`
	// Status is job status ("pending", "running", "failed") or object status ("raw").
	Status string `json:"status"`
	// Type is the content type (e.g., "text", "url", "image").
	Type string `json:"type"`
	// Source is the content origin (URL, filename, etc).
	Source string `json:"source"`
	// Pipeline is the pipeline name if set.
	Pipeline string `json:"pipeline"`
	// CreatedAt is when the item was created.
	CreatedAt time.Time `json:"created_at"`
	// Error holds the failure message for failed jobs.
	Error string `json:"error,omitempty"`
}
