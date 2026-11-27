package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/safefetch"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// fakeLLM is the transport of an Ollama LLM client: it answers each
// prompt with a fixed answer, or fails every call (mode "fail"), and
// never touches the network.
type fakeLLM struct {
	mu      sync.Mutex
	mode    string       // "ok", "few" (one tag) or "fail"
	replies []fixedReply // prompts containing a substring get its reply
	calls   int
}

func (f *fakeLLM) setMode(m string) {
	f.mu.Lock()
	f.mode = m
	f.mu.Unlock()
}

type fixedReply struct{ substr, reply string }

// answer makes the LLM reply reply, without failing, to prompts containing
// substr: an answer the step may or may not be able to use, not an outage.
func (f *fakeLLM) answer(substr, reply string) {
	f.mu.Lock()
	f.replies = append(f.replies, fixedReply{substr, reply})
	f.mu.Unlock()
}

func (f *fakeLLM) RoundTrip(r *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Prompt string `json:"prompt"`
	}
	_ = json.Unmarshal(body, &req)
	f.mu.Lock()
	f.calls++
	mode, replies := f.mode, slices.Clone(f.replies)
	f.mu.Unlock()
	var out map[string]any
	switch {
	case slices.ContainsFunc(replies, func(r fixedReply) bool { return strings.Contains(req.Prompt, r.substr) }):
		i := slices.IndexFunc(replies, func(r fixedReply) bool { return strings.Contains(req.Prompt, r.substr) })
		out = map[string]any{"response": replies[i].reply}
	case mode == "fail":
		out = map[string]any{"error": "model unavailable"}
	case strings.Contains(req.Prompt, "one-sentence summary of the following text"):
		out = map[string]any{"response": "A fixed section summary."}
	case strings.Contains(req.Prompt, "concise tags"):
		if mode == "few" {
			out = map[string]any{"response": "llmtagone"}
		} else {
			out = map[string]any{"response": "llmtagone, llmtagtwo"}
		}
	case strings.Contains(req.Prompt, "Identify named entities"):
		out = map[string]any{"response": "@person.ada"}
	case strings.Contains(req.Prompt, "knowledge extraction assistant"):
		out = map[string]any{"response": `{"summary":"Fixed summary.","topics":["fixedtopic"],"decisions":["Use SQLite"],"open_questions":[],"artifacts":[]}`}
	default:
		out = map[string]any{"response": "{}"}
	}
	b, _ := json.Marshal(out)
	return &http.Response{
		StatusCode: 200, Status: "200 OK", Body: io.NopCloser(bytes.NewReader(b)),
		Header: http.Header{"Content-Type": []string{"application/json"}}, Request: r,
	}, nil
}

// realEnv is a service over the built-in pipelines with real steps,
// fake providers and a page server counting its requests.
type realEnv struct {
	svc         *Service
	driver      storage.StorageDriver
	reg         pipeline.Registry
	llm         *fakeLLM
	srv         *httptest.Server
	hits        *atomic.Int32
	page        atomic.Value // string: the body the page server serves
	ocr         *recordingOCR
	contentType atomic.Value // string: the page server's Content-Type
}

func newRealEnv(t *testing.T) *realEnv {
	t.Helper()
	e := &realEnv{llm: &fakeLLM{mode: "ok"}, hits: new(atomic.Int32), ocr: &recordingOCR{}}
	e.page.Store("<html><body><p>default page</p></body></html>")
	e.contentType.Store("text/html; charset=utf-8")
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		e.hits.Add(1)
		w.Header().Set("Content-Type", e.contentType.Load().(string))
		_, _ = io.WriteString(w, e.page.Load().(string))
	}))
	t.Cleanup(e.srv.Close)

	stub := config.ProviderBackendConfig{Backend: config.BackendStub}
	cfg := config.ProvidersConfig{
		Video: stub, Document: config.ProviderBackendConfig{Backend: "golib"}, OCR: stub, Transcription: stub,
		Vision: stub, Diarization: stub, Frames: stub, Summarization: stub,
		LLM: config.ProviderBackendConfig{Backend: "ollama", Model: "fake-model", Endpoint: "http://llm.invalid:11434"},
	}
	refuse := func(name string) (string, error) { return "", &providers.ToolNotInstalledError{Tool: name} }
	f := providers.NewFactory(cfg, nil).
		WithHTTPClient(&http.Client{Transport: e.llm}).
		WithContext(providers.WithToolLookup(context.Background(), refuse))
	reg, err := builtins.ConfiguredRegistryWithOpts(builtins.BuildOpts{
		Factory:    f,
		OCR:        e.ocr,
		FetchGuard: safefetch.New(safefetch.Options{Policy: safefetch.Policy{AllowPrivate: true}}),
	})
	require.NoError(t, err)
	e.reg = reg
	e.driver = storageutil.NewTestDriver(t)
	e.svc = New(e.driver, jobs.NewQueue(e.driver.Jobs()), reg, search.NewEngine(e.driver), "", nil)
	e.svc.Cfg.Providers = cfg
	return e
}

// recordingOCR is a fake OCR engine that answers a fixed text and
// remembers the bytes it was given.
type recordingOCR struct {
	mu   sync.Mutex
	seen []byte
}

func (*recordingOCR) Name() string { return "fake" }
func (o *recordingOCR) Extract(_ context.Context, img []byte, _ string) (*providers.OCRResult, error) {
	o.mu.Lock()
	o.seen = append([]byte(nil), img...)
	o.mu.Unlock()
	return &providers.OCRResult{Text: "fixed ocr text of a scanned page, long enough for the tagger to ask the language model about it and for it to answer", Confidence: 0.9}, nil
}

// ingest runs the pipeline over payload as the ingest worker's first
// stage does and stores the draft, with its mention edges.
func (e *realEnv) ingest(t *testing.T, pipelineName, source, payload string) *storage.KnowledgeObject {
	t.Helper()
	draft, err := jobs.RunDraft(ownerCtx(), e.reg, &storage.Job{Pipeline: pipelineName, Payload: payload, Source: source}, 5)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)
	draft.CreatedAt, draft.UpdatedAt = now, now
	require.NoError(t, e.driver.Objects().Create(ownerCtx(), draft))
	require.NoError(t, e.svc.PersistMentionEdges(ownerCtx(), draft))
	return draft
}

func (e *realEnv) get(t *testing.T, id string) *storage.KnowledgeObject {
	return regrGet(t, e.driver, id)
}

// ingestJob is ingest for a payload that may be base64 binary.
func (e *realEnv) ingestJob(t *testing.T, pipelineName, source, payload string, b64 bool) *storage.KnowledgeObject {
	t.Helper()
	job := &storage.Job{
		Pipeline: pipelineName, Payload: payload, Source: source,
		UserHints: []string{"inv-user"}, UserMentions: []string{"@inv.user"}, UserNote: "my note",
		UserData: map[string]any{"k": "v"},
	}
	if b64 {
		job.PayloadEncoding = storage.PayloadBase64
	}
	draft, err := jobs.RunDraft(ownerCtx(), e.reg, job, 5)
	require.NoError(t, err, "first ingest of %s", pipelineName)
	now := time.Now().UTC().Truncate(time.Second)
	draft.CreatedAt, draft.UpdatedAt = now, now
	require.NoError(t, e.driver.Objects().Create(ownerCtx(), draft))
	require.NoError(t, e.svc.PersistMentionEdges(ownerCtx(), draft))
	return draft
}
