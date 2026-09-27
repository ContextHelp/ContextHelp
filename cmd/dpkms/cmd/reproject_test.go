package cmd

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// A stale signature at startup queues one re-projection job and says so;
// a matching one queues nothing.
func TestScheduleReproject_StartupOutput(t *testing.T) {
	ctx := context.Background()
	d, err := sqlite.New(filepath.Join(t.TempDir(), "d.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Init(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close(ctx) })
	q := jobs.NewQueue(d.Jobs())

	var out bytes.Buffer
	scheduleReproject(ctx, &out, q, &indexsig.VerifyResult{Match: true}, nil)
	scheduleReproject(ctx, &out, q, nil, nil)
	if out.Len() != 0 {
		t.Fatalf("matching signature printed %q", out.String())
	}

	scheduleReproject(ctx, &out, q, &indexsig.VerifyResult{OldHash: "", NewHash: "abc"}, nil)
	if !strings.Contains(out.String(), "FTS re-projection to "+jobs.ReprojectTarget()) {
		t.Fatalf("output %q does not announce the re-projection", out.String())
	}
	list, _, err := q.List(ctx, storage.JobFilter{Type: jobs.ReprojectJobType})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("%d re-projection jobs, want 1", len(list))
	}
}

func TestShortHash(t *testing.T) {
	for in, want := range map[string]string{
		"":                 "none",
		"abc":              "abc",
		"0123456789abcdef": "0123456789ab",
	} {
		if got := shortHash(in); got != want {
			t.Errorf("shortHash(%q) = %q, want %q", in, got, want)
		}
	}
}
