// Package podexec executes allowlisted Stage 1 ingestion pipeline jobs inside
// ephemeral pods via internal/podrun, returning KnowledgeObject drafts to the
// host WorkerPool for Stage 2 storage and queue settlement.
package podexec

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/podrun"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

var requiredLeaves = []string{"pod create", "pod upload", "pod exec", "pod destroy"}

// Options configures an ephemeral pod job Executor.
type Options struct {
	Provider       string
	Image          string
	DpkmsBin       string
	MaxInflight    int
	MaxPodsPerHour int
	MaxPodDuration time.Duration
	MaxHops        int
	Warn           io.Writer
	Now            func() time.Time
	NameFunc       func(jobID string) string
}

// Executor runs Stage 1 ingest pipelines inside ephemeral pods with bounded
// concurrency, hourly pod budget ceiling, and guaranteed teardown.
type Executor struct {
	pod            *podrun.Client
	provider       string
	image          string
	dpkmsBin       string
	maxPodDuration time.Duration
	maxHops        int
	warn           io.Writer
	sem            chan struct{}
	budget         *hourlyBudget
	nameFunc       func(jobID string) string

	probeMu sync.Mutex
	probed  bool
}

// NewExecutor constructs an ephemeral pod executor backed by podClient.
func NewExecutor(podClient *podrun.Client, opts Options) *Executor {
	maxInflight := opts.MaxInflight
	if maxInflight <= 0 {
		maxInflight = config.DefaultJobsPodMaxInflight
	}
	dpkmsBin := opts.DpkmsBin
	if dpkmsBin == "" {
		dpkmsBin = config.DefaultDaemonPodDpkmsBin
	}
	maxDur := opts.MaxPodDuration
	if maxDur <= 0 {
		maxDur = config.DefaultJobsPodMaxPodDuration
	}
	maxHops := opts.MaxHops
	if maxHops <= 0 {
		maxHops = 5
	}
	warn := opts.Warn
	if warn == nil {
		warn = io.Discard
	}
	nameFn := opts.NameFunc
	if nameFn == nil {
		nameFn = ephemeralPodName
	}
	return &Executor{
		pod:            podClient,
		provider:       opts.Provider,
		image:          opts.Image,
		dpkmsBin:       dpkmsBin,
		maxPodDuration: maxDur,
		maxHops:        maxHops,
		warn:           warn,
		sem:            make(chan struct{}, maxInflight),
		budget:         newHourlyBudget(opts.MaxPodsPerHour, opts.Now),
		nameFunc:       nameFn,
	}
}

// Execute runs job's Stage 1 pipeline inside a fresh ephemeral pod:
// create -> upload request JSON -> exec `dpkms worker run-draft` -> defer destroy.
func (e *Executor) Execute(ctx context.Context, job *storage.Job) (*storage.KnowledgeObject, error) {
	if job == nil {
		return nil, &pipeline.PermanentError{Err: errors.New("podexec: nil job")}
	}
	if err := e.acquireSlot(ctx); err != nil {
		return nil, err
	}
	defer e.releaseSlot()

	if !e.budget.Allow() {
		return nil, fmt.Errorf("podexec: hourly pod ceiling (%d/hr) reached; deferring job %s", e.budget.limit, job.ID)
	}

	if err := e.ensureProbed(ctx); err != nil {
		return nil, err
	}

	jobCtx, cancel := context.WithTimeout(ctx, e.maxPodDuration)
	defer cancel()

	podName := e.nameFunc(job.ID)
	if _, err := e.pod.CreatePod(jobCtx, podrun.CreateOpts{
		Name:     podName,
		Provider: e.provider,
		Image:    e.image,
		Note:     "ctxt job " + shortID(job.ID),
	}); err != nil {
		return nil, fmt.Errorf("podexec create %s: %w", podName, err)
	}
	// Guarantee teardown on every exit path (success, error, timeout, cancel).
	defer e.destroyPod(ctx, podName)

	localReq, err := writeRequestFile(jobs.DraftJobRequest{Job: *job, MaxHops: e.maxHops})
	if err != nil {
		return nil, fmt.Errorf("podexec encode request: %w", err)
	}
	defer func() { _ = os.Remove(localReq) }()

	const remoteReq = "/tmp/ctxt-job-req.json"
	if err := e.pod.Upload(jobCtx, podName, localReq, remoteReq); err != nil {
		return nil, fmt.Errorf("podexec upload %s: %w", podName, err)
	}

	res, err := e.pod.Exec(jobCtx, podName, []string{e.dpkmsBin, "worker", "run-draft", "--input", remoteReq}, podrun.RunOpts{
		Timeout: e.maxPodDuration,
	})
	if res.ExitCode == 127 {
		return nil, &pipeline.PermanentError{
			Err: fmt.Errorf("podexec: %s not found in pod image (pod %s)", e.dpkmsBin, podName),
		}
	}
	if err != nil || res.ExitCode != 0 {
		detail := strings.TrimSpace(string(res.Stderr))
		if detail == "" && err != nil {
			detail = err.Error()
		}
		return nil, fmt.Errorf("podexec run-draft on %s failed (exit %d): %s", podName, res.ExitCode, detail)
	}

	return decodeDraftResult(podName, res.Stdout)
}

func (e *Executor) acquireSlot(ctx context.Context) error {
	select {
	case e.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Executor) releaseSlot() {
	<-e.sem
}

func (e *Executor) ensureProbed(ctx context.Context) error {
	e.probeMu.Lock()
	defer e.probeMu.Unlock()
	if e.probed {
		return nil
	}
	caps, err := e.pod.Probe(ctx)
	if err != nil {
		return err
	}
	if err := podrun.RequireLeaves(caps, requiredLeaves...); err != nil {
		return err
	}
	e.probed = true
	return nil
}

func (e *Executor) destroyPod(parent context.Context, podName string) {
	cleanCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), 30*time.Second)
	defer cancel()
	if err := e.pod.DestroyPod(cleanCtx, podName, "ctxt job cleanup"); err != nil {
		_, _ = fmt.Fprintf(e.warn, "warning: podexec destroy %s: %v\n", podName, err)
	}
}

func decodeDraftResult(podName string, raw []byte) (*storage.KnowledgeObject, error) {
	var out jobs.DraftJobResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("podexec %s returned invalid draft JSON: %w", podName, err)
	}
	if out.Error != "" {
		if out.Permanent {
			return nil, &pipeline.PermanentError{Err: errors.New(out.Error)}
		}
		return nil, errors.New(out.Error)
	}
	if out.Draft == nil {
		return nil, fmt.Errorf("podexec %s returned empty draft", podName)
	}
	return out.Draft, nil
}

func writeRequestFile(req jobs.DraftJobRequest) (string, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "ctxt-job-req-*.json")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func ephemeralPodName(jobID string) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("ctxt-job-%s-%s", shortID(jobID), hex.EncodeToString(b[:]))
}

func shortID(id string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(id) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
			if sb.Len() == 8 {
				break
			}
		}
	}
	if sb.Len() == 0 {
		return "job"
	}
	return sb.String()
}

// Router dispatches allowlisted job topics to Pod and all other jobs to Local.
type Router struct {
	local  jobs.PipelineExecutor
	pod    jobs.PipelineExecutor
	topics map[string]struct{}
}

// NewRouter returns a PipelineExecutor that sends jobs matching topics (after
// stripping any ":nofanout" suffix) to podExec and every other job to local.
func NewRouter(local, podExec jobs.PipelineExecutor, topics []string) *Router {
	set := make(map[string]struct{}, len(topics))
	for _, t := range topics {
		t = strings.TrimSpace(t)
		if t != "" {
			set[t] = struct{}{}
		}
	}
	return &Router{
		local:  local,
		pod:    podExec,
		topics: set,
	}
}

// Offloadable reports whether jobType matches the router's topic allowlist.
func (r *Router) Offloadable(jobType string) bool {
	base := strings.TrimSuffix(jobType, ":nofanout")
	_, ok := r.topics[base]
	return ok
}

// Execute routes job to the pod executor when its topic is allowlisted, else local.
func (r *Router) Execute(ctx context.Context, job *storage.Job) (*storage.KnowledgeObject, error) {
	if job != nil && r.pod != nil && r.Offloadable(job.Type) {
		return r.pod.Execute(ctx, job)
	}
	return r.local.Execute(ctx, job)
}

type hourlyBudget struct {
	mu    sync.Mutex
	limit int
	now   func() time.Time
	times []time.Time
}

func newHourlyBudget(limit int, now func() time.Time) *hourlyBudget {
	if now == nil {
		now = time.Now
	}
	return &hourlyBudget{limit: limit, now: now}
}

func (b *hourlyBudget) Allow() bool {
	if b.limit <= 0 {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	cutoff := now.Add(-time.Hour)
	keep := b.times[:0]
	for _, t := range b.times {
		if t.After(cutoff) {
			keep = append(keep, t)
		}
	}
	b.times = keep
	if len(b.times) >= b.limit {
		return false
	}
	b.times = append(b.times, now)
	return true
}
