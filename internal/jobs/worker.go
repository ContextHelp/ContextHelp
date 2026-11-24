// Package jobs runs queued work: ingest jobs through their pipelines and
// task jobs through registered handlers.
package jobs

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	kitbus "hop.top/kit/go/runtime/bus"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	uri "hop.top/cite/scheme"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/mentions"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/pkg/taxonomy"
)

// parseUserMentions converts caller-asserted @namespace.slug strings
// to the URI form draft.Mentions expects. Empty / unparseable tokens are
// silently dropped — the user's free-form input shouldn't kill the job.
func parseUserMentions(raw []string) []uri.URI {
	if len(raw) == 0 {
		return nil
	}
	return mentions.ParseSlice(raw)
}

// parseUserHints converts caller-asserted hint strings into the
// Tag form draft.Tags expects, with Source:"user". Blank / whitespace-only
// hints are silently dropped — `--hint ""` shouldn't kill the job.
// The auto-tagger (pipeline/steps/tagger.go) merges with these rather
// than overwriting, preserving operator intent.
func parseUserHints(raw []string) []storage.Tag {
	if len(raw) == 0 {
		return nil
	}
	out := make([]storage.Tag, 0, len(raw))
	for _, h := range raw {
		label := strings.TrimSpace(h)
		if label == "" {
			continue
		}
		out = append(out, storage.Tag{
			Label:  label,
			Source: "user",
			Weight: 1.0,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// FanOutFunc is called after a successful ingest with the new object ID.
// It runs fan-out enrichment (cross-reference edges, audit log).
// Returning an error is logged but does not fail the ingest job.
type FanOutFunc func(ctx context.Context, objectID string) error

// WorkerPool runs pipeline jobs from the queue.
type WorkerPool struct {
	queue        *Queue
	pipelines    pipeline.Registry
	store        storage.StorageDriver
	bus          kitbus.Bus
	workers      int
	staleTimeout time.Duration
	pollInterval time.Duration
	drainTimeout time.Duration
	maxHops      int
	maxRetries   int
	fanOut       FanOutFunc
	executor     PipelineExecutor       // optional Stage 1 pipeline executor (nil = in-process)
	handlers     map[string]TaskHandler // task job type -> handler (tasks.go)
	inflight     sync.Map               // task job ID -> *taskClaim, running in this pool
	leaseTTL     time.Duration          // task job lease; renewed every leaseTTL/3
	recoverEvery time.Duration          // stale recovery interval
}

// NewWorkerPool creates a worker pool.
func NewWorkerPool(queue *Queue, pipelines pipeline.Registry, store storage.StorageDriver, workers int, bus kitbus.Bus, cfg config.JobsConfig) *WorkerPool {
	return &WorkerPool{
		queue:        queue,
		pipelines:    pipelines,
		store:        store,
		bus:          bus,
		workers:      workers,
		staleTimeout: cfg.StaleTimeout,
		pollInterval: cfg.PollInterval,
		drainTimeout: cfg.DrainTimeout,
		maxHops:      cfg.MaxHops,
		maxRetries:   cfg.MaxRetries,
		leaseTTL:     defaultTaskLeaseTTL,
		recoverEvery: time.Minute,
	}
}

// SetFanOut installs a post-ingest fan-out callback.
func (p *WorkerPool) SetFanOut(fn FanOutFunc) { p.fanOut = fn }

// staleScratchAge is how old a scratch dir must be before start-up
// removes it; a live run finishes long before.
const staleScratchAge = 24 * time.Hour

// SetExecutor installs an optional Stage 1 pipeline executor (for example, the
// opt-in pod job executor router). Passing nil restores the default in-process
// execution path.
func (p *WorkerPool) SetExecutor(exec PipelineExecutor) { p.executor = exec }

// Start runs the worker pool until the context is cancelled.
// On cancellation, in-flight jobs are given drainTimeout to finish before
// returning. Pass a drainTimeout of 0 to use the default (30s).
func (p *WorkerPool) Start(ctx context.Context) error {
	// Scratch files of a run that died before cleaning up.
	pipeline.SweepStaleScratch(staleScratchAge)
	drain := p.drainTimeout
	if drain == 0 {
		// Default must exceed any realistic server uptime. The previous
		// 30s default expired during normal operation, killing all
		// in-flight pipeline steps after 30s of uptime.
		drain = 365 * 24 * time.Hour
	}

	g, acquireCtx := errgroup.WithContext(ctx)

	// drainCtx: jobs started before shutdown get up to drain seconds to finish.
	// It is not tied to acquireCtx so cancelling acquireCtx does not abort them.
	// The timeout starts when Start is called; in practice jobs are short-lived
	// so the deadline is only reached on hangs.
	drainCtx, drainCancel := context.WithTimeout(context.Background(), drain)
	defer drainCancel()

	// Stale recovery goroutine — stops acquiring when ctx is cancelled.
	g.Go(func() error { return p.recoverStaleLoop(acquireCtx) })

	// Worker goroutines — stop acquiring on ctx cancel but finish in-flight
	// jobs using drainCtx (bounded by drain timeout).
	for i := 0; i < p.workers; i++ {
		g.Go(func() error { return p.workerLoop(acquireCtx, drainCtx) })
	}

	return g.Wait()
}

func (p *WorkerPool) workerLoop(acquireCtx, jobCtx context.Context) error {
	for {
		select {
		case <-acquireCtx.Done():
			return nil
		default:
			job, err := p.queue.AcquireNext(acquireCtx)
			if err != nil || job == nil {
				select {
				case <-acquireCtx.Done():
					return nil
				case <-time.After(p.pollInterval):
				}
				continue
			}
			if h, ok := p.handlers[job.Type]; ok {
				// Task jobs are long-running and resumable: they stop at
				// shutdown instead of holding the drain.
				p.runTask(acquireCtx, job, h)
				continue
			}
			// Use jobCtx so in-flight work is not cancelled immediately on
			// shutdown — serve.go cancels jobCtx after drain timeout.
			p.process(jobCtx, job)
		}
	}
}

// runSteps executes all steps in a pipeline on draft, skipping any step that
// returns ErrDelegate and failing on any other error.
func (p *WorkerPool) runSteps(ctx context.Context, pipe *pipeline.Pipeline, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	return pipeline.RunSteps(ctx, pipe, draft)
}

// jobObjectType is the object type an ingest job asks for, from its
// "ingest:<type>[:nofanout]" type. Blank means the pipeline decides:
// text and auto name no kind, and job kinds that are not object types
// ("ingest:feed_item", "ingest:github_release") are not seeded.
func jobObjectType(jobType string) string {
	rest, ok := strings.CutPrefix(jobType, "ingest:")
	if !ok {
		return ""
	}
	t, _, _ := strings.Cut(rest, ":")
	switch t {
	case taxonomy.TypeURL, taxonomy.TypeImage, taxonomy.TypeAudio, taxonomy.TypeVideo,
		taxonomy.TypeFeed, taxonomy.TypeDocument, taxonomy.TypeCode:
		return t
	}
	return ""
}

// seedType is the type a job's draft starts with: the job's own kind,
// else url for any url.* pipeline (everything it stores was fetched
// from a URL, whatever label the job or its source carries). Steps
// that set an explicit type, such as the repo mapper, still override.
func seedType(jobType, pipelineName string) string {
	if t := jobObjectType(jobType); t != "" {
		return t
	}
	if strings.HasPrefix(pipelineName, "url.") {
		return taxonomy.TypeURL
	}
	return ""
}

// processWithHops runs the initial pipeline and follows any hop requests
// (draft.Metadata["next_pipeline"]) up to maxHops total runs, then externalizes
// binary content to the blob store.
func (p *WorkerPool) processWithHops(ctx context.Context, job *storage.Job) (*storage.KnowledgeObject, error) {
	draft, err := runDraft(ctx, p.pipelines, p.store.Blobs(), job, p.maxHops)
	if err != nil {
		return nil, err
	}
	// Binary content cannot live in the row (a TEXT column), whether or
	// not the pipeline externalizes: it goes to the blob store here.
	return steps.ExternalizeBinary(ctx, p.store.Blobs(), draft)
}

// executeWithExecutor runs Stage 1 through the installed PipelineExecutor,
// keeping binary originals in the host blob store before execution and
// externalizing any remaining binary content afterward.
func (p *WorkerPool) executeWithExecutor(ctx context.Context, job *storage.Job) (*storage.KnowledgeObject, error) {
	var (
		orig        blob.Original
		origContent string
		hasOrig     bool
	)
	if content, err := jobContent(job); err == nil && blobref.NeedsBlob(content) {
		o, err := blob.PutOriginal(ctx, p.store.Blobs(), content, sniffContentType(content), originalFileName(job.Source))
		if err != nil {
			return nil, err
		}
		orig, origContent, hasOrig = o, content, true
	}
	draft, err := p.executor.Execute(ctx, job)
	if err != nil {
		return nil, err
	}
	if hasOrig {
		if err := blob.AttachOriginal(draft, orig, origContent); err != nil {
			return nil, err
		}
	}
	return steps.ExternalizeBinary(ctx, p.store.Blobs(), draft)
}

func sniffContentType(content string) string {
	head := content
	if len(head) > 512 { // all http.DetectContentType reads
		head = head[:512]
	}
	return strings.TrimSpace(strings.SplitN(http.DetectContentType([]byte(head)), ";", 2)[0])
}

func describeOriginal(content, source string) blob.Original {
	key := blob.OriginalKey(content)
	return blob.Original{
		Key:         key,
		SHA256:      key,
		ContentType: sniffContentType(content),
		FileName:    originalFileName(source),
		Size:        int64(len(content)),
	}
}

// keepOriginal stores content, the original media bytes, in blobs (when
// non-nil) and records it on draft (blob.AttachOriginal).
func keepOriginal(ctx context.Context, blobs storage.BlobStore, draft *storage.KnowledgeObject, content string) error {
	if blobs == nil {
		return blob.AttachOriginal(draft, describeOriginal(content, draft.Source), "")
	}
	o, err := blob.PutOriginal(ctx, blobs, content, sniffContentType(content), originalFileName(draft.Source))
	if err != nil {
		return err
	}
	return blob.AttachOriginal(draft, o, content)
}

// isDriveSource reports whether source starts with a Windows drive
// path (C:\dir, C:/dir), which url.Parse would take for a scheme.
func isDriveSource(source string) bool {
	if len(source) < 3 || source[1] != ':' || (source[2] != '\\' && source[2] != '/') {
		return false
	}
	c := source[0] | 0x20
	return c >= 'a' && c <= 'z'
}

// maxFileNameLen caps an original's file_name, in bytes.
const maxFileNameLen = 255

// originalFileName is the display name of an original: the last path
// segment of source, with control and format characters removed and the
// length capped. A source counts as a path only when it holds a
// separator, or is a file: URL, a URL with a host, or a watcher source
// (watch:<mode>:<relpath>); labels (cli, stdin, v1.2, user@example.com),
// other schemes (mailto:, ambient:) and sources ending in a separator
// give "". Source is never opened as a path.
func originalFileName(source string) string {
	path, ok := sourcePath(source)
	if !ok {
		return ""
	}
	path = strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) || unicode.Is(unicode.Cf, r) {
			return -1 // C0, DEL, C1, bidi overrides and other format characters
		}
		return r
	}, path)
	return lastSegment(path)
}

// sourcePath is the path part of source, and whether source names a path
// at all (see originalFileName for what counts).
func sourcePath(source string) (string, bool) {
	if rest, ok := strings.CutPrefix(source, "watch:"); ok {
		// the watcher's "watch:<mode>:<relpath>": a relative path, a
		// bare name included
		if _, rel, found := strings.Cut(rest, ":"); found {
			source = "/" + rel
		}
	}
	if !isDriveSource(source) {
		if u, err := url.Parse(source); err == nil && u.Scheme != "" {
			if u.Scheme != "file" && u.Host == "" {
				return "", false
			}
			source = u.Path
		}
	}
	return source, strings.ContainsAny(source, `/\`)
}

// lastSegment is the final path segment of path, capped at maxFileNameLen
// bytes without splitting a rune; "" for a path that ends in a separator
// or in "." or "..".
func lastSegment(path string) string {
	if strings.HasSuffix(path, "/") || strings.HasSuffix(path, `\`) {
		return ""
	}
	segs := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' })
	if len(segs) == 0 {
		return ""
	}
	name := segs[len(segs)-1]
	if name == "." || name == ".." {
		return ""
	}
	if len(name) > maxFileNameLen {
		name = name[:maxFileNameLen]
		for !utf8.ValidString(name) { // do not leave half a rune
			name = name[:len(name)-1]
		}
	}
	return name
}

// jobContent is the content job carries: its payload, decoded under the
// job's payload encoding.
func jobContent(job *storage.Job) (string, error) {
	switch job.PayloadEncoding {
	case "":
		return job.Payload, nil
	case storage.PayloadBase64:
	default:
		return "", fmt.Errorf("job payload encoding %q is not supported", job.PayloadEncoding)
	}
	b, err := base64.StdEncoding.DecodeString(job.Payload)
	if err != nil {
		return "", fmt.Errorf("job payload is not valid base64: %w", err)
	}
	return string(b), nil
}

func (p *WorkerPool) process(ctx context.Context, job *storage.Job) {
	start := time.Now()

	// A job queued before its pipeline was archived must not run it:
	// archived pipelines are retained but disabled.
	if p.pipelineArchived(ctx, job.Pipeline) {
		p.markFailed(ctx, job.ID, fmt.Sprintf("pipeline %q is archived", job.Pipeline))
		return
	}

	var draft *storage.KnowledgeObject
	var err error
	if p.executor != nil {
		draft, err = p.executeWithExecutor(ctx, job)
	} else {
		draft, err = p.processWithHops(ctx, job)
	}
	if err != nil {
		var perm *pipeline.PermanentError
		if errors.As(err, &perm) {
			slog.Debug("jobs: permanent error, skipping retry", "job", job.ID, "err", err)
			p.markFailed(ctx, job.ID, err.Error())
			return
		}
		p.retryOrFail(ctx, job.ID, job.Claim, err.Error())
		return
	}

	draft.UpdatedAt = time.Now()

	durationMs := time.Since(start).Milliseconds()

	// A source key names one source: a stored object under it takes this
	// submission as an update, whatever the fetched content hashes to.
	// It is checked before the content hash, so a submission that carries
	// a key never lands on another object by a hash match.
	if p.updateBySourceKey(ctx, job, draft, durationMs) {
		return
	}

	// Dedup stays inside the draft's profile (ADR-080 §4): the same
	// content under another profile becomes its own object.
	existing, err := p.store.Objects().GetByContentHash(ctx, draft.ContentHash, draft.ProfileID)
	if err == nil && existing != nil {
		p.reinforceExisting(ctx, job, draft, durationMs)
		return
	}

	if p.refuseNonText(ctx, job, draft, writeCreate) {
		return
	}

	if dupOf, drop := suppressedDuplicate(draft); drop {
		// duplicates.policy drop: the near-duplicate is not stored. The job
		// answers with the existing object's ID, as analyze does for an
		// exact duplicate.
		slog.Info("jobs: near-duplicate dropped, not stored", "job", job.ID, "duplicate_of", dupOf)
		p.markCompleted(ctx, job.ID, dupOf, durationMs)
		return
	}

	draft.ReinforcementCount = 1

	ingestCtx, ingestEv := p.announceIngested(ctx, draft, durationMs)
	if err := p.store.Objects().Create(ingestCtx, draft); err != nil {
		// Unique constraint race: another worker inserted the same hash
		// (in the same profile) concurrently.
		if errors.Is(err, storage.ErrAlreadyExists) {
			p.settleCreateRace(ctx, job, draft, durationMs)
			return
		}
		p.markFailed(ctx, job.ID, fmt.Sprintf("store: %s", err), draft.ID)
		return
	}
	// A delete or sweep of the same blob key may have deleted the blob
	// the externalize step put before this row landed: put it back. If
	// that fails, the retry runs the pipeline again, which puts it.
	if err := blob.Ensure(ctx, p.store.Blobs(), draft); err != nil {
		p.retryOrFail(ctx, job.ID, job.Claim, fmt.Sprintf("store: %s", err), draft.ID)
		return
	}
	p.putVectors(ctx, draft.ID, draft)

	// Write the mention edges (ADR-049): one per entity, named by
	// storage.MentionTarget as every mention-edge writer names it, each
	// carrying the entity_mention node of draft.Graph that holds its
	// mention and the field it came from. The store keeps an edge an
	// earlier run stored for the pair. Any edge write failure fails the
	// job; the object has already been persisted but no edges exist yet,
	// so the job is retryable and a subsequent run will re-create the
	// object (content-hash dedup) and re-attempt edge writes.
	if err := storage.EnsureMentionEntities(ctx, p.store.Entities(), draft.Mentions, time.Now().UTC()); err != nil {
		p.markFailed(ctx, job.ID, fmt.Sprintf("entity write: mentions: %s", err), draft.ID)
		return
	}
	edges := storage.MentionEdges(draft, draft.Mentions, false, time.Now())
	if _, err := p.store.Edges().CreateMissing(ctx, edges); err != nil {
		p.markFailed(ctx, job.ID, fmt.Sprintf("edge write: mentions: %s", err), draft.ID)
		return
	}

	durationMs = time.Since(start).Milliseconds()
	p.publishAfter(ctx, ingestEv)

	// Fan out per-item jobs if the pipeline staged items for enqueueing.
	// An item that could not be enqueued fails the container job: it
	// must not complete with items silently missing.
	if err := p.fanOutItems(ctx, draft); err != nil {
		p.markFailed(ctx, job.ID, err.Error(), draft.ID)
		return
	}
	p.markCompleted(ctx, job.ID, draft.ID, durationMs)

	// Post-ingest fan-out enrichment (bidirectional edges, audit log).
	if p.fanOut != nil && !strings.HasSuffix(job.Type, ":nofanout") {
		if err := p.fanOut(ctx, draft.ID); err != nil {
			slog.Warn("jobs: fan-out enrichment failed", "object", draft.ID, "err", err)
		}
	}
}

// reinforceExisting settles job as a content-hash reinforce of the object
// already stored under draft's hash: the count and the draft's tags and
// mentions are merged, and the caller's note and data replace the stored
// ones.
func (p *WorkerPool) reinforceExisting(ctx context.Context, job *storage.Job, draft *storage.KnowledgeObject, durationMs int64) {
	if p.refuseNonText(ctx, job, draft, writeReinforce) {
		return
	}
	existingID, err := p.store.Objects().Reinforce(ctx, draft.ContentHash, draft)
	if err != nil {
		// Reinforce errors are storage contention (SQLITE_BUSY) or a row
		// deleted since the lookup; both can succeed on a later attempt.
		p.retryOrFail(ctx, job.ID, job.Claim, fmt.Sprintf("reinforce: %s", err), existingID)
		return
	}
	p.putVectors(ctx, existingID, draft)
	// The caller's note and data on a re-submission replace the stored
	// ones: reinforcing alone would keep the first capture's.
	p.applySubmission(ctx, job, existingID)
	p.markCompleted(ctx, job.ID, existingID, durationMs)
}

// settleCreateRace settles job after its create lost to a concurrent
// insert of the same content hash or source key.
func (p *WorkerPool) settleCreateRace(ctx context.Context, job *storage.Job, draft *storage.KnowledgeObject, durationMs int64) {
	// Another worker stored this source key first: this is its update.
	if p.updateBySourceKey(ctx, job, draft, durationMs) {
		return
	}
	existingID, rerr := p.reinforceAfterRace(ctx, draft)
	if rerr != nil {
		p.markFailed(ctx, job.ID, fmt.Sprintf("reinforce after race: %s", rerr), draft.ID)
		return
	}
	p.putVectors(ctx, existingID, draft)
	p.markCompleted(ctx, job.ID, existingID, durationMs)
}

// submission is what the job's caller attached to the capture.
func submission(job *storage.Job) storageutil.Submission {
	return storageutil.Submission{
		Note:     job.UserNote,
		Data:     job.UserData,
		Hints:    parseUserHints(job.UserHints),
		Mentions: parseUserMentions(job.UserMentions),
	}
}

// writeKind is what a job is about to write, which decides the fields the
// text check covers.
type writeKind int

const (
	// writeCreate stores a new object: every draft field.
	writeCreate writeKind = iota
	// writeReinforce bumps a stored object and merges the draft's tags and
	// mentions.
	writeReinforce
	// writeSourceKeyUpdate writes only the submission onto the stored
	// object; the draft is not stored.
	writeSourceKeyUpdate
)

// Defense in depth: the store refuses NUL and invalid UTF-8 in text columns
// and JSON with a driver error, so a step that let either through fails the
// job before any write. A create checks every draft field the store
// writes; a reinforce the ones it writes; an update by source key none of
// the draft. Every write also checks the caller's submission (note, data,
// hints, mentions), which all of them store.
//
// refuseNonText fails job permanently, by field, when what kind is about
// to write holds NUL or invalid UTF-8, and reports whether it did.
func (p *WorkerPool) refuseNonText(ctx context.Context, job *storage.Job, draft *storage.KnowledgeObject, kind writeKind) bool {
	var field string
	switch kind {
	case writeReinforce:
		field = pipeline.ReinforceNonTextField(draft)
	case writeCreate:
		field = pipeline.NonTextField(draft, false, true)
	}
	if field == "" {
		sub := submission(job)
		field = pipeline.SubmissionNonTextField(sub.Note, sub.Data, sub.Hints, sub.Mentions)
	}
	if field == "" {
		return false
	}
	p.markFailed(ctx, job.ID, fmt.Sprintf("%s holds NUL or invalid UTF-8: refusing to store", field))
	return true
}

// updateBySourceKey settles job as an update of the object the submitting
// profile stored under key (a key in another profile is another source), when there is one, and reports whether it did. A failed lookup
// settles the job as a retry or failure and reports true.
func (p *WorkerPool) updateBySourceKey(ctx context.Context, job *storage.Job, draft *storage.KnowledgeObject, durationMs int64) bool {
	key := draft.SourceKey
	if key == "" {
		return false
	}
	stored, err := p.store.Objects().GetBySourceKey(ctx, key, draft.ProfileID)
	if err != nil {
		p.retryOrFail(ctx, job.ID, job.Claim, fmt.Sprintf("source key lookup: %s", err))
		return true
	}
	if stored == nil {
		return false
	}
	if p.refuseNonText(ctx, job, draft, writeSourceKeyUpdate) {
		return true
	}
	p.updateStored(ctx, job, stored, durationMs)
	return true
}

// updateStored settles job as an update of stored, the object its source
// key names: the caller's note, data, hints and mentions are applied, the
// object is reinforced, and the job answers with its ID. The stored
// content, its hash and the pipeline's own output are left as they are.
func (p *WorkerPool) updateStored(ctx context.Context, job *storage.Job, stored *storage.KnowledgeObject, durationMs int64) {
	fields := submission(job).Apply(stored)
	now := time.Now()
	stored.ReinforcementCount++
	stored.LastReinforcedAt = &now
	stored.UpdatedAt = now
	uctx, upEv := p.announceUpdated(ctx, stored, fields)
	if err := p.store.Objects().Update(uctx, stored); err != nil {
		p.retryOrFail(ctx, job.ID, job.Claim, fmt.Sprintf("update by source key: %s", err), stored.ID)
		return
	}
	p.writeSubmissionEdges(ctx, stored, job)
	p.publishAfter(ctx, upEv)
	p.markCompleted(ctx, job.ID, stored.ID, durationMs)
}

// applySubmission applies the job's note, data, hints and mentions to the
// stored object id after a content-hash reinforce. Failures are logged:
// the reinforce already succeeded and the job must not fail for it.
func (p *WorkerPool) applySubmission(ctx context.Context, job *storage.Job, id string) {
	sub := submission(job)
	if sub.Note == "" && len(sub.Data) == 0 {
		return
	}
	stored, err := p.store.Objects().Get(ctx, id)
	if err != nil || stored == nil {
		slog.Warn("jobs: apply submission: load object", "object", id, "err", err)
		return
	}
	fields := sub.Apply(stored)
	if len(fields) == 0 {
		return
	}
	stored.UpdatedAt = time.Now()
	uctx, upEv := p.announceUpdated(ctx, stored, fields)
	if err := p.store.Objects().Update(uctx, stored); err != nil {
		slog.Warn("jobs: apply submission: update object", "object", id, "err", err)
		return
	}
	p.publishAfter(ctx, upEv)
}

// writeSubmissionEdges writes the mention edges for the mentions the job
// carries onto stored. The store keeps an edge already there.
func (p *WorkerPool) writeSubmissionEdges(ctx context.Context, stored *storage.KnowledgeObject, job *storage.Job) {
	targets := parseUserMentions(job.UserMentions)
	if len(targets) == 0 {
		return
	}
	edges := storage.MentionEdges(stored, targets, false, time.Now())
	if _, err := p.store.Edges().CreateMissing(ctx, edges); err != nil {
		slog.Warn("jobs: update by source key: mention edges", "object", stored.ID, "err", err)
	}
}

// announceUpdated attaches the object.updated after event for the
// object store to write in the update's transaction.
func (p *WorkerPool) announceUpdated(ctx context.Context, obj *storage.KnowledgeObject, fields []string) (context.Context, *storage.OutboxEvent) {
	return events.Announce(ctx, events.TopicObjectUpdated, SourceWorkerPool, events.OriginJob, "", obj.ID, events.ObjectUpdatedPayload{
		ObjectID: obj.ID,
		Type:     obj.Type,
		Fields:   fields,
	})
}

// publishAfter sends the live copy of a committed after event.
func (p *WorkerPool) publishAfter(ctx context.Context, ev *storage.OutboxEvent) {
	if p.bus == nil || ev == nil {
		return
	}
	if err := events.Publish(ctx, p.bus, ev); err != nil {
		slog.Warn("jobs: failed to publish after event", "topic", ev.Topic, "err", err)
	}
}

// reinforceAfterRace reinforces the object another worker inserted with
// draft's content hash concurrently. That insert may not be visible yet (WAL
// delay), so a missing row is retried with short backoff before giving up.
func (p *WorkerPool) reinforceAfterRace(ctx context.Context, draft *storage.KnowledgeObject) (string, error) {
	backoffs := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond}
	var existingID string
	var rerr error
	for i, delay := range backoffs {
		existingID, rerr = p.store.Objects().Reinforce(ctx, draft.ContentHash, draft)
		if rerr == nil {
			break
		}
		if !errors.Is(rerr, sql.ErrNoRows) {
			break
		}
		slog.Debug("jobs: reinforce after race retry",
			"attempt", i+1,
			"delay", delay,
			"hash", draft.ContentHash,
		)
		// Don't sleep after the last attempt.
		if i >= len(backoffs)-1 {
			break
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			rerr = ctx.Err()
		case <-timer.C:
		}
		if ctx.Err() != nil {
			rerr = ctx.Err()
			break
		}
	}
	return existingID, rerr
}

// suppressedDuplicate reports whether the dedup step marked draft
// suppress_output (policy drop), with the existing object it duplicates.
// A mark without a duplicate_of has no object to answer with, so the draft
// is stored.
func suppressedDuplicate(draft *storage.KnowledgeObject) (duplicateOf string, suppressed bool) {
	suppressed, _ = draft.Metadata["suppress_output"].(bool)
	duplicateOf, _ = draft.Metadata["duplicate_of"].(string)
	return duplicateOf, suppressed && duplicateOf != ""
}

// putVectors persists the embedding step's per-model vectors for the stored
// object (a new object's ID, or the reinforced one's). Vectors are additive:
// a failed Put is logged and never fails the job; the object's missing rows
// are what the embedding migration backfill picks up.
func (p *WorkerPool) putVectors(ctx context.Context, objectID string, draft *storage.KnowledgeObject) {
	if len(draft.Vectors) == 0 {
		return
	}
	if err := p.store.Embeddings().Put(ctx, objectID, draft.Vectors); err != nil {
		slog.Warn("jobs: embeddings not stored; ingest continues", "object", objectID, "err", err)
	}
}

// pipelineArchived reports whether name is a stored pipeline that is
// archived. A pipeline in the registry (built-in) is never archived.
func (p *WorkerPool) pipelineArchived(ctx context.Context, name string) bool {
	if name == "" || p.store == nil {
		return false
	}
	if _, err := p.pipelines.Get(name); err == nil {
		return false
	}
	pl, err := p.store.Pipelines().Get(ctx, name)
	return err == nil && pl != nil && pl.Archived
}

// retryOrFail requeues a job after a transient error, failing it once
// its retries are exhausted.
func (p *WorkerPool) retryOrFail(ctx context.Context, jobID, claim, msg string, resultIDs ...string) {
	err := p.queue.Requeue(ctx, jobID, claim)
	switch {
	case err == nil:
	case errors.Is(err, storage.ErrClaimLost):
		// Cancelled, or requeued and claimed by another worker (lease
		// expired): the job is not this worker's to settle.
		slog.Warn("jobs: not requeueing a job this worker no longer holds", "job", jobID)
	default:
		p.markFailed(ctx, jobID, msg, resultIDs...)
	}
}

// markCompleted settles jobID as completed with resultID and publishes
// job.completed. A store that rejects the write leaves the job running
// until stale recovery re-runs it, so the rejection is logged.
func (p *WorkerPool) markCompleted(ctx context.Context, jobID, resultID string, durationMs int64) {
	if err := p.queue.Complete(ctx, jobID, resultID); err != nil {
		slog.Warn("jobs: mark completed", "job", jobID, "err", err)
	}
	p.emitCompleted(ctx, jobID, durationMs)
}

// markFailed settles jobID as failed with reason and publishes job.failed,
// logging a store that rejects the write.
func (p *WorkerPool) markFailed(ctx context.Context, jobID, reason string, objectID ...string) {
	if err := p.queue.Fail(ctx, jobID, reason); err != nil {
		slog.Warn("jobs: mark failed", "job", jobID, "err", err)
	}
	p.emitFailed(ctx, jobID, reason, objectID...)
}

// fanOutItems enqueues per-item jobs from Metadata["items_to_enqueue"].
// An item that cannot be enqueued is not dropped: the rest are still
// enqueued, and the error says how many were lost so the caller fails
// the job instead of completing it.
func (p *WorkerPool) fanOutItems(ctx context.Context, draft *storage.KnowledgeObject) error {
	if draft.Metadata == nil {
		return nil
	}
	rawItems, ok := draft.Metadata["items_to_enqueue"]
	if !ok {
		return nil
	}
	items, ok := rawItems.([]map[string]any)
	if !ok || len(items) == 0 {
		return nil
	}
	var failed int
	var firstErr error
	now := time.Now().Truncate(time.Second)
	for _, item := range items {
		content, _ := item["content"].(string)
		link, _ := item["link"].(string)
		if content == "" {
			content = link
		}
		if content == "" {
			continue
		}
		source, _ := item["source"].(string)
		itemPipeline, _ := item["pipeline"].(string)
		if itemPipeline == "" {
			itemPipeline = "feed.ingest"
		}
		payload, encoding := Payload(content)
		job := &storage.Job{
			ID:              uuid.New().String(),
			Type:            "ingest:feed_item",
			Status:          storage.JobPending,
			Payload:         payload,
			PayloadEncoding: encoding,
			Pipeline:        itemPipeline,
			Source:          source,
			MaxRetries:      p.maxRetries,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := p.queue.Enqueue(ctx, job); err != nil {
			slog.Warn("jobs: fanOut enqueue failed", "source", source, "err", err)
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if p.bus != nil {
			_ = p.bus.Publish(ctx, kitbus.NewEvent(TopicJobEnqueued, SourceWorkerPoolFanOut, job))
		}
	}
	if failed > 0 {
		return fmt.Errorf("fan-out: %d of %d item jobs not enqueued: %w", failed, len(items), firstErr)
	}
	return nil
}

func (p *WorkerPool) emitCompleted(ctx context.Context, jobID string, durationMs int64) {
	if p.bus == nil {
		return
	}
	ev := kitbus.NewEvent(TopicJobCompleted, SourceWorkerPool, events.JobCompletedPayload{
		JobID:       jobID,
		ObjectCount: 1,
		DurationMs:  durationMs,
	})
	if pubErr := p.bus.Publish(ctx, ev); pubErr != nil {
		slog.Warn("jobs: failed to publish job.completed event", "job", jobID, "err", pubErr)
	}
}

func (p *WorkerPool) emitFailed(ctx context.Context, jobID, reason string, objectID ...string) {
	if p.bus == nil {
		return
	}
	payload := events.JobFailedPayload{
		JobID: jobID,
		Error: reason,
	}
	if len(objectID) > 0 {
		payload.ObjectID = objectID[0]
	}
	ev := kitbus.NewEvent(TopicJobFailed, SourceWorkerPool, payload)
	if pubErr := p.bus.Publish(ctx, ev); pubErr != nil {
		slog.Warn("jobs: failed to publish job.failed event", "job", jobID, "err", pubErr)
	}
}

// announceIngested attaches the object.ingested after event for the
// object store to write in the create's transaction.
func (p *WorkerPool) announceIngested(ctx context.Context, draft *storage.KnowledgeObject, durationMs int64) (context.Context, *storage.OutboxEvent) {
	tags := make([]string, len(draft.Tags))
	for i, t := range draft.Tags {
		tags[i] = t.Label
	}
	return events.Announce(ctx, TopicObjectIngested, SourceWorkerPool, events.OriginJob, "", draft.ID, events.ObjectIngestedPayload{
		ObjectID:   draft.ID,
		Type:       draft.Type,
		Source:     draft.Source,
		SourceKey:  draft.SourceKey,
		Pipeline:   draft.Pipeline,
		Tags:       tags,
		ProfileID:  draft.ProfileID,
		DurationMs: durationMs,
	})
}

func (p *WorkerPool) recoverStaleLoop(ctx context.Context) error {
	ticker := time.NewTicker(p.recoverEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := p.queue.RecoverStale(ctx, int64(p.staleTimeout.Seconds())); err != nil {
				slog.Warn("jobs: recover stale", "err", err)
			}
		}
	}
}
