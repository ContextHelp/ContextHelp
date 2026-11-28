package rederive

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/embeddingtest"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/github"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/safefetch"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// loopbackLLM is an endpoint llm classification judges local; the fake
// transport answers it, so nothing leaves the process.
const loopbackLLM = "http://127.0.0.1:11434"

// fakeLLM is the transport of an Ollama client: it answers fixed text
// and counts the calls. It never touches the network.
type fakeLLM struct {
	calls atomic.Int32
	// status, when not 0, answers every call with it and a JSON body that
	// has no error field (a proxy, an overloaded server); empty answers 200
	// with an empty reply.
	status atomic.Int32
	empty  atomic.Bool
}

func (f *fakeLLM) RoundTrip(r *http.Request) (*http.Response, error) {
	f.calls.Add(1)
	if st := int(f.status.Load()); st != 0 {
		return &http.Response{
			StatusCode: st, Status: http.StatusText(st), Request: r,
			Body:   io.NopCloser(strings.NewReader(`{"message":"service unavailable"}`)),
			Header: http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	}
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Prompt string `json:"prompt"`
	}
	_ = json.Unmarshal(body, &req)
	resp := "{}"
	switch {
	case f.empty.Load():
		resp = ""
	case strings.Contains(req.Prompt, "concise tags"):
		resp = "llmtagone, llmtagtwo"
	case strings.Contains(req.Prompt, "one-sentence summary"):
		resp = "A fixed summary."
	}
	b, _ := json.Marshal(map[string]any{"response": resp})
	return &http.Response{
		StatusCode: 200, Status: "200 OK", Body: io.NopCloser(bytes.NewReader(b)),
		Header: http.Header{"Content-Type": []string{"application/json"}}, Request: r,
	}, nil
}

// realEnv is a service over the built-in pipelines with their real steps,
// a fake model server and a page server that counts its requests.
type realEnv struct {
	t      *testing.T
	driver storage.StorageDriver
	svc    *service.Service
	reg    pipeline.Registry
	llm    *fakeLLM
	srv    *httptest.Server
	hits   atomic.Int32
	mu     sync.Mutex
	page   string
	status int // the page server's status; 0 is 200
	// handler, when set, answers the page server instead of page.
	handler http.HandlerFunc
	gh      *fakeGitHub
	// pipelines are the pipeline overrides the registry is built with;
	// llm the LLM backend (useLLM sets it).
	pipelines config.PipelinesConfig
	llmCfg    config.ProviderBackendConfig
	// embModels are the embedding models the registry is built with; a
	// model named in badModels has a provider that cannot be built.
	embModels embeddingtest.Models
	badModels map[string]bool
	// embedFails makes the built models' embed calls fail.
	embedFails atomic.Bool
}

// modelResolver builds a fake provider for a model, or fails for a bad one.
type modelResolver struct {
	embeddings.ProviderResolver
	bad  map[string]bool
	fail *atomic.Bool
}

type fixedDim struct {
	dim  int
	fail *atomic.Bool
}

func (p fixedDim) Embed(context.Context, string) ([]float32, error) {
	if p.fail != nil && p.fail.Load() {
		return nil, fmt.Errorf("embedding backend refused the call")
	}
	v := make([]float32, p.dim)
	for i := range v {
		v[i] = float32(i + 1)
	}
	return v, nil
}
func (p fixedDim) Dimensions() int { return p.dim }
func (p fixedDim) Name() string    { return "ollama" }

// Endpoint is what locality reads: a loopback address is local.
func (p fixedDim) Endpoint() string { return loopbackLLM }

func (r modelResolver) ForModel(_ context.Context, m registry.Model) (providers.EmbeddingProvider, error) {
	if r.bad[m.ModelID] {
		return nil, fmt.Errorf("model %s: no such backend", m.ModelID)
	}
	return fixedDim{dim: m.Dimension, fail: r.fail}, nil
}

func ctxSystem() context.Context {
	return sharing.WithViewer(context.Background(), sharing.Unrestricted)
}

func newRealEnv(t *testing.T) *realEnv {
	t.Helper()
	e := &realEnv{t: t, llm: &fakeLLM{}, page: samePage("")}
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.hits.Add(1)
		e.mu.Lock()
		page, status, h := e.page, e.status, e.handler
		e.mu.Unlock()
		if h != nil {
			h(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = io.WriteString(w, page)
	}))
	t.Cleanup(e.srv.Close)
	e.gh = newFakeGitHub(t)
	e.driver = storageutil.NewTestDriver(t)
	e.useLLM(config.ProviderBackendConfig{Backend: "ollama", Model: "fake-model", Endpoint: loopbackLLM})
	return e
}

func (e *realEnv) setPage(html string) {
	e.mu.Lock()
	e.page, e.status, e.handler = html, 0, nil
	e.mu.Unlock()
}

func (e *realEnv) respondWith(h http.HandlerFunc) {
	e.mu.Lock()
	e.handler = h
	e.mu.Unlock()
}

func (e *realEnv) setStatus(s int) {
	e.mu.Lock()
	e.status = s
	e.mu.Unlock()
}

// useLLM rebuilds the registry and the service over the same store with
// the given LLM backend: what a changed provider configuration does at
// the next start.
func (e *realEnv) useLLM(llm config.ProviderBackendConfig) {
	e.t.Helper()
	e.llmCfg = llm
	stub := config.ProviderBackendConfig{Backend: config.BackendStub}
	cfg := config.ProvidersConfig{
		Video: stub, Document: config.ProviderBackendConfig{Backend: "golib"}, OCR: stub, Transcription: stub,
		Vision: stub, Diarization: stub, Frames: stub, Summarization: stub, LLM: llm,
	}
	refuse := func(name string) (string, error) { return "", &providers.ToolNotInstalledError{Tool: name} }
	f := providers.NewFactory(cfg, nil).
		WithHTTPClient(&http.Client{Transport: e.llm}).
		WithContext(providers.WithToolLookup(context.Background(), refuse))
	reg, err := builtins.ConfiguredRegistryWithPipelineOverrides(builtins.BuildOpts{
		Factory:    f,
		Models:     e.modelSource(),
		Resolver:   e.resolver(),
		GitHub:     github.NewClient(github.WithBaseURL(e.gh.srv.URL), github.WithMaxWait(0), github.WithMaxRetries(0)),
		FetchGuard: safefetch.New(safefetch.Options{Policy: safefetch.Policy{AllowPrivate: true}}),
	}, cfg, e.pipelines)
	require.NoError(e.t, err)
	e.reg = reg
	e.svc = service.New(e.driver, jobs.NewQueue(e.driver.Jobs()), reg, search.NewEngine(e.driver), "", nil)
	e.svc.Cfg.Providers = cfg
}

// ingest runs the pipeline as the ingest worker does and stores the
// draft, signed with the pipeline's current signature.
func (e *realEnv) ingest(pipelineName, source, payload string) *storage.KnowledgeObject {
	e.t.Helper()
	draft, err := jobs.RunDraft(ctxSystem(), e.reg, &storage.Job{Pipeline: pipelineName, Payload: payload, Source: source}, 5)
	require.NoError(e.t, err)
	now := time.Now().UTC().Truncate(time.Second)
	draft.CreatedAt, draft.UpdatedAt = now, now
	require.NoError(e.t, e.driver.Objects().Create(ctxSystem(), draft))
	return draft
}

func (e *realEnv) get(id string) *storage.KnowledgeObject {
	e.t.Helper()
	got, err := e.driver.Objects().Get(ctxSystem(), id)
	require.NoError(e.t, err)
	require.NotNil(e.t, got)
	return got
}

// snapshot is the stored object as JSON, to compare byte for byte.
func (e *realEnv) snapshot(id string) string {
	b, err := json.Marshal(e.get(id))
	require.NoError(e.t, err)
	return string(b)
}

func (e *realEnv) sigOf(id string) string {
	return sigOfRow(e.t, e.driver, id)
}

// bump makes step of pipeline name one version newer in the registry, as a
// release that changed the step would, and records the new signature's
// entries the way the daemon does at start. It returns the new signature.
func (e *realEnv) bump(name, step string) string {
	e.t.Helper()
	p, err := e.reg.Get(name)
	require.NoError(e.t, err)
	next := *p
	var refs []pipeline.StepRef
	var models []string
	found := false
	for _, en := range p.Entries {
		if i := strings.LastIndex(en, "@"); i >= 0 && !strings.Contains(en, "=") {
			v := 0
			for _, c := range en[i+1:] {
				v = v*10 + int(c-'0')
			}
			if en[:i] == step {
				v++
				found = true
			}
			refs = append(refs, pipeline.StepRef{Name: en[:i], Version: v})
		} else {
			models = append(models, en)
		}
	}
	require.True(e.t, found, "step %q is not an entry of %s: %v", step, name, p.Entries)
	next.Entries = pipeline.EntriesOf(refs, models...)
	next.Signature = pipeline.SignatureOf(name, refs, models...)
	e.reg.Upsert(name, &next)
	_, err = RecordSignatures(context.Background(), e.store(), e.reg)
	require.NoError(e.t, err)
	return next.Signature
}

func (e *realEnv) store() Store {
	s, ok := e.driver.Objects().(Store)
	require.True(e.t, ok)
	return s
}

// record is the daemon's start-up recording.
func (e *realEnv) record() {
	e.t.Helper()
	_, err := RecordSignatures(context.Background(), e.store(), e.reg)
	require.NoError(e.t, err)
}

func (e *realEnv) job() *Job {
	return &Job{
		Store: e.store(), Svc: e.svc, Registry: e.reg, Enabled: true,
		Wait:  func(context.Context, time.Duration) error { return nil },
		Scope: func(ctx context.Context) context.Context { return sharing.WithViewer(ctx, sharing.Unrestricted) },
	}
}

func (e *realEnv) run() (Result, error) {
	return e.job().Run(context.Background(), "job-1")
}

func (e *realEnv) skips(target string) map[string]string {
	e.t.Helper()
	got, err := skipRows(e.t, e.driver, target), error(nil)
	require.NoError(e.t, err)
	out := map[string]string{}
	for _, k := range got {
		out[k.ObjectID] = k.Reason
	}
	return out
}

// samePage is the page server's default page, with extra text added: the
// same content, as a refresh sees it after a small change.
func samePage(extra string) string {
	return "<html><body><p>" + words("page", 0, 200) + " " + extra + "</p></body></html>"
}

func (e *realEnv) modelSource() embeddings.ModelSource {
	if len(e.embModels) == 0 {
		return nil
	}
	return e.embModels
}

func (e *realEnv) resolver() embeddings.ProviderResolver {
	if len(e.embModels) == 0 {
		return nil
	}
	return modelResolver{bad: e.badModels, fail: &e.embedFails}
}
