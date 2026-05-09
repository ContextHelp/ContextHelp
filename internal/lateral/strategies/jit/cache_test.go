package jit_test

import (
	"context"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
)

func TestMemoryProposalCache_GetEmpty(t *testing.T) {
	c := jit.NewMemoryProposalCache()
	got, ok := c.Get(context.Background(), "github.com", "repo_root")
	if ok {
		t.Fatalf("Get on empty cache: ok = true, want false")
	}
	if got != nil {
		t.Fatalf("Get on empty cache: value = %v, want nil", got)
	}
}

func TestMemoryProposalCache_PutThenGet(t *testing.T) {
	c := jit.NewMemoryProposalCache()
	ctx := context.Background()
	want := []string{"/issues", "/pulls", "/wiki"}

	c.Put(ctx, "github.com", "repo_root", want)

	got, ok := c.Get(ctx, "github.com", "repo_root")
	if !ok {
		t.Fatalf("Get after Put: ok = false, want true")
	}
	if len(got) != len(want) {
		t.Fatalf("Get after Put: len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Get after Put: [%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMemoryProposalCache_PutOverwrites(t *testing.T) {
	c := jit.NewMemoryProposalCache()
	ctx := context.Background()

	c.Put(ctx, "github.com", "repo_root", []string{"/old"})
	c.Put(ctx, "github.com", "repo_root", []string{"/new", "/newer"})

	got, ok := c.Get(ctx, "github.com", "repo_root")
	if !ok {
		t.Fatalf("Get after overwrite: ok = false, want true")
	}
	if len(got) != 2 || got[0] != "/new" || got[1] != "/newer" {
		t.Fatalf("Get after overwrite: %v, want [/new /newer]", got)
	}
}

func TestMemoryProposalCache_KeyIsolation(t *testing.T) {
	c := jit.NewMemoryProposalCache()
	ctx := context.Background()

	c.Put(ctx, "github.com", "repo_root", []string{"/gh-a"})
	c.Put(ctx, "github.com", "issue_list", []string{"/gh-b"})
	c.Put(ctx, "gitlab.com", "repo_root", []string{"/gl-a"})

	if got, _ := c.Get(ctx, "github.com", "repo_root"); len(got) != 1 || got[0] != "/gh-a" {
		t.Fatalf("github.com/repo_root = %v, want [/gh-a]", got)
	}
	if got, _ := c.Get(ctx, "github.com", "issue_list"); len(got) != 1 || got[0] != "/gh-b" {
		t.Fatalf("github.com/issue_list = %v, want [/gh-b]", got)
	}
	if got, _ := c.Get(ctx, "gitlab.com", "repo_root"); len(got) != 1 || got[0] != "/gl-a" {
		t.Fatalf("gitlab.com/repo_root = %v, want [/gl-a]", got)
	}
}

func TestMemoryProposalCache_Invalidate(t *testing.T) {
	c := jit.NewMemoryProposalCache()
	ctx := context.Background()

	c.Put(ctx, "github.com", "repo_root", []string{"/issues"})
	c.Invalidate(ctx, "github.com", "repo_root")

	if _, ok := c.Get(ctx, "github.com", "repo_root"); ok {
		t.Fatalf("Get after Invalidate: ok = true, want false")
	}
}

func TestMemoryProposalCache_InvalidateMissingNoop(t *testing.T) {
	c := jit.NewMemoryProposalCache()
	// Must not panic and must not mutate other entries.
	c.Invalidate(context.Background(), "absent.example", "nope")

	c.Put(context.Background(), "github.com", "repo_root", []string{"/x"})
	c.Invalidate(context.Background(), "absent.example", "still_nope")

	if _, ok := c.Get(context.Background(), "github.com", "repo_root"); !ok {
		t.Fatalf("unrelated entry got dropped by Invalidate of missing key")
	}
}

// TestMemoryProposalCache_ConcurrentReads is a light smoke test: one writer
// seeds a value, then many readers fan out. Designed to be a cheap sanity
// check under -race, not a stress benchmark.
func TestMemoryProposalCache_ConcurrentReads(t *testing.T) {
	c := jit.NewMemoryProposalCache()
	ctx := context.Background()
	c.Put(ctx, "github.com", "repo_root", []string{"/issues", "/pulls"})

	const readers = 8
	var wg sync.WaitGroup
	wg.Add(readers)
	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				got, ok := c.Get(ctx, "github.com", "repo_root")
				if !ok {
					t.Errorf("concurrent Get: ok = false, want true")
					return
				}
				if len(got) != 2 {
					t.Errorf("concurrent Get: len = %d, want 2", len(got))
					return
				}
			}
		}()
	}
	wg.Wait()
}
