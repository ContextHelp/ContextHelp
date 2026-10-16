package grpc

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// A handler called on a context that carries no viewer reads nothing:
// every RPC that returns rows fails with the scoped driver's refusal,
// and the same call under the owner's viewer answers.
func TestHandlersFailClosedWithoutViewer(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	svc := service.New(driver, jobs.NewQueue(driver.Jobs()), builtins.Registry(), search.NewEngine(driver), "", nil)
	owner := sharing.WithViewer(context.Background(), sharing.Unrestricted)
	now := time.Now().UTC().Truncate(time.Second)
	obj := &storage.KnowledgeObject{
		ID: "scope-owner-note", Type: "note", Status: storage.ObjectStatusActive,
		RawContent: "scope canary body", ContentHash: "sha256:scope-owner-note", CreatedAt: now, UpdatedAt: now,
	}
	if err := driver.Objects().Create(owner, obj); err != nil {
		t.Fatalf("seed object: %v", err)
	}
	job := &storage.Job{ID: "scope-owner-job", Type: "analyze", Status: storage.JobCompleted, Pipeline: "text.short",
		ResultID: obj.ID, CreatedAt: now, UpdatedAt: now, CompletedAt: &now}
	if err := driver.Jobs().Create(owner, job); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	queries, jobsH := newQueryHandler(svc), newJobHandler(svc)
	calls := map[string]func(ctx context.Context) error{
		"QueryService/ListObjects": func(ctx context.Context) error {
			_, err := queries.ListObjects(ctx, &pb.ListObjectsRequest{Limit: 10})
			return err
		},
		"QueryService/GetObject": func(ctx context.Context) error {
			_, err := queries.GetObject(ctx, &pb.GetObjectRequest{Id: obj.ID})
			return err
		},
		"QueryService/Search": func(ctx context.Context) error {
			_, err := queries.Search(ctx, &pb.SearchRequest{Query: "type==note"})
			return err
		},
		"JobService/GetJob": func(ctx context.Context) error {
			_, err := jobsH.GetJob(ctx, &pb.GetJobRequest{Id: job.ID})
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call(context.Background())
			if err == nil {
				t.Fatal("no viewer: call succeeded, want the scoped driver's refusal")
			}
			if status.Code(err) == codes.NotFound || !strings.Contains(err.Error(), storage.ErrRestrictedViewer.Error()) {
				t.Fatalf("no viewer: err = %v, want a refusal naming ErrRestrictedViewer", err)
			}
			if err := call(owner); err != nil {
				t.Fatalf("owner: %v", err)
			}
		})
	}
}
