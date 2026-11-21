package servicetest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
)

// binaryCase is binary content a text column cannot hold: NULs, bytes that
// are not valid UTF-8, or both.
type binaryCase struct {
	name string
	data []byte
}

func binaryCases() []binaryCase {
	unit := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0xff, 0xfe, 0x80, 0xc3, 0x28, 0x00}
	var png []byte
	for len(png) < 700 {
		png = append(png, unit...)
	}
	return []binaryCase{
		{"png-like, NUL and invalid UTF-8", png},
		{"valid UTF-8 with NUL", []byte("Valid text\x00with a NUL, long enough to be sectioned.\n## Heading\nbody\x00more")},
		{"invalid UTF-8 only", []byte("caf\xe9 \xff\xfe lorem ipsum dolor sit amet")},
	}
}

func textClean(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0) && !strings.ContainsRune(s, utf8.RuneError)
}

func metaClean(v any) bool {
	switch x := v.(type) {
	case string:
		return textClean(x)
	case map[string]any:
		for k, e := range x {
			if !textClean(k) || !metaClean(e) {
				return false
			}
		}
	case []any:
		for _, e := range x {
			if !metaClean(e) {
				return false
			}
		}
	}
	return true
}

// requireBinaryStoredAsBlob reads id straight from the driver (the stored
// row, no resolution) and fails unless the row keeps a ref, every text
// the row projects holds no binary, and the blob is exactly want.
func requireBinaryStoredAsBlob(t *testing.T, drv storage.StorageDriver, id string, want []byte) {
	t.Helper()
	ctx := sharing.WithViewer(context.Background(), sharing.Unrestricted)
	obj, err := drv.Objects().Get(ctx, id)
	if err != nil || obj == nil {
		t.Fatalf("get %s: %v", id, err)
	}
	if !blobref.Is(obj.RawContent) {
		t.Fatalf("%s: raw_content is not a blob ref: %q", id, obj.RawContent)
	}
	if !textClean(obj.TextContent) {
		t.Errorf("%s: text_content holds binary: %q", id, obj.TextContent)
	}
	for _, s := range obj.Sections {
		if !textClean(s.Title) || !textClean(s.Content) || !metaClean(s.Metadata) {
			t.Errorf("%s: section holds binary: %q", id, s.Content)
		}
	}
	if obj.Graph != nil {
		for _, n := range obj.Graph.Nodes {
			if !textClean(n.Label) || !textClean(n.Content) {
				t.Errorf("%s: graph node %s holds binary: %q", id, n.ID, n.Content)
			}
		}
	}
	if !metaClean(obj.Metadata) {
		t.Errorf("%s: metadata holds binary: %v", id, obj.Metadata)
	}
	got, err := blob.Content(ctx, drv.Blobs(), obj.RawContent)
	if err != nil {
		t.Fatalf("%s: resolve blob: %v", id, err)
	}
	if got != string(want) {
		t.Fatalf("%s: blob = %x, want %x", id, got, want)
	}
}

// binaryFixture is a service and worker over drv with the built-in
// pipelines and no blob threshold: only binary is externalized.
type binaryFixture struct {
	t    *testing.T
	drv  storage.StorageDriver
	svc  *service.Service
	pool *jobs.WorkerPool
}

func newBinaryFixture(t *testing.T, drv storage.StorageDriver) *binaryFixture {
	t.Helper()
	q := jobs.NewQueue(drv.Jobs())
	pipes := builtins.Registry()
	svc := service.New(drv, q, pipes, search.NewEngine(drv), "", nil, config.Config{})
	cfg := config.JobsConfig{PollInterval: 50 * time.Millisecond, StaleTimeout: 30 * time.Minute, MaxRetries: 3, MaxHops: 5}
	return &binaryFixture{t: t, drv: drv, svc: svc, pool: jobs.NewWorkerPool(q, pipes, drv, 1, nil, cfg)}
}

// work runs the worker until job id is settled and returns the job.
func (f *binaryFixture) work(id string) *storage.Job {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(sharing.WithViewer(context.Background(), sharing.Unrestricted), 20*time.Second)
	defer cancel()
	go func() {
		for ctx.Err() == nil {
			if j, _ := f.svc.GetJob(ctx, id); j != nil && (j.Status == storage.JobCompleted || j.Status == storage.JobFailed) {
				cancel()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	if err := f.pool.Start(ctx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		f.t.Fatalf("worker: %v", err)
	}
	j, err := f.svc.GetJob(sharing.WithViewer(context.Background(), sharing.Unrestricted), id)
	if err != nil {
		f.t.Fatalf("get job: %v", err)
	}
	if j.Status != storage.JobCompleted {
		f.t.Fatalf("job %s = %s: %s", id, j.Status, j.Error)
	}
	return j
}

// RunBinaryContent runs binary content (NUL, invalid UTF-8) through every
// path that writes an object, on drv: raw analyze, the pipeline worker,
// and the update paths (UpdateObject, reprocess, reanalyze, federation
// accept). Whatever the path, the row keeps a blob ref, no text the row
// projects holds a byte of it, and the blob is the exact bytes; the same
// bytes through the raw path and the worker are one object.
func RunBinaryContent(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := sharing.WithViewer(context.Background(), sharing.Unrestricted)

	for i, bc := range binaryCases() {
		bc := bc
		src := "capture-" + string(rune('a'+i))
		t.Run(bc.name, func(t *testing.T) {
			f := newBinaryFixture(t, drv)
			content := string(bc.data)

			// Raw path.
			rawID, err := f.svc.Analyze(ctx, service.AnalyzeRequest{Content: content, Type: "text", Source: src, Raw: true})
			if err != nil {
				t.Fatalf("analyze raw: %v", err)
			}
			requireBinaryStoredAsBlob(t, drv, rawID, bc.data)

			// The same bytes through the pipeline land on the raw object:
			// the worker's dedup hash is over the bytes, as analyze's is.
			jobID, err := f.svc.Analyze(ctx, service.AnalyzeRequest{Content: content, Type: "text", Source: src, Force: true})
			if err != nil {
				t.Fatalf("analyze: %v", err)
			}
			if job := f.work(jobID); job.ResultID != rawID {
				t.Errorf("worker stored the raw bytes as %s, want the raw object %s", job.ResultID, rawID)
			}

			// A pipeline-first object of other bytes: no binary in its projection.
			other := append(append([]byte{}, bc.data...), 0x00, 0xfe, 'z')
			jobID, err = f.svc.Analyze(ctx, service.AnalyzeRequest{Content: string(other), Type: "text", Source: src})
			if err != nil {
				t.Fatalf("analyze other: %v", err)
			}
			pipeID := f.work(jobID).ResultID
			requireBinaryStoredAsBlob(t, drv, pipeID, other)

			// Update paths re-read the object resolved and write it back.
			t.Run("UpdateObject", func(t *testing.T) {
				obj, err := f.svc.GetObject(ctx, pipeID)
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				obj.Summaries = []string{"edited"}
				if err := f.svc.UpdateObject(ctx, obj); err != nil {
					t.Fatalf("UpdateObject: %v", err)
				}
				requireBinaryStoredAsBlob(t, drv, pipeID, other)
			})

			t.Run("reprocess", func(t *testing.T) {
				r := &jobs.Reprocessor{Objects: f.svc, LLM: providers.NewStubLLMProvider()}
				for _, step := range jobs.ReprocessSteps {
					payload, _ := json.Marshal(jobs.ReprocessPayload{ObjectID: pipeID, Step: step})
					if _, err := r.Handle(ctx, &storage.Job{ID: "rp-" + step, Payload: string(payload)}); err != nil {
						t.Fatalf("reprocess %s: %v", step, err)
					}
					requireBinaryStoredAsBlob(t, drv, pipeID, other)
				}
			})

			t.Run("reanalyze", func(t *testing.T) {
				stored, err := drv.Objects().Get(ctx, pipeID)
				if err != nil {
					t.Fatalf("get stored: %v", err)
				}
				stored.Pipeline = "text.long@v0"
				if err := drv.Objects().Update(ctx, stored); err != nil {
					t.Fatalf("stamp pipeline: %v", err)
				}
				if _, _, err := f.svc.ReanalyzeObject(ctx, pipeID); err != nil {
					t.Fatalf("ReanalyzeObject: %v", err)
				}
				requireBinaryStoredAsBlob(t, drv, pipeID, other)
			})

			t.Run("federation accept", func(t *testing.T) {
				// A peer pushes the content inline.
				fed := append(append([]byte{}, bc.data...), 0x00, 0xfd, 'f')
				id := fmt.Sprintf("fed-%d", i)
				now := time.Now().UTC().Truncate(time.Second)
				n, err := f.svc.FederationAccept(ctx, []storage.KnowledgeObject{{
					ID: id, Type: "text", RawContent: string(fed), Source: "peer://x",
					ContentHash: "fed-hash-" + id, CreatedAt: now, UpdatedAt: now,
				}}, nil, nil)
				if err != nil || n != 1 {
					t.Fatalf("FederationAccept = %d, %v", n, err)
				}
				requireBinaryStoredAsBlob(t, drv, id, fed)
			})
		})
	}

	t.Run("inbox refuses binary", func(t *testing.T) {
		svc := newBinaryFixture(t, drv).svc
		_, err := svc.CaptureToInbox(ctx, service.InboxCaptureRequest{Content: "a\x00b", Type: "text"})
		if err == nil {
			t.Fatal("CaptureToInbox stored NUL content")
		}
		_, err = svc.CaptureToInbox(ctx, service.InboxCaptureRequest{Content: "a\xffb", Type: "text"})
		if err == nil {
			t.Fatal("CaptureToInbox stored invalid UTF-8")
		}
	})
}
