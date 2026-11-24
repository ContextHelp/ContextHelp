package steps

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestFrameSamplerWritesFramesToScratchDirNotBesideSource(t *testing.T) {
	// Stub video: 150s. One frame per 50s at most: 0, 50, 100.
	step := NewFrameSampler(WithFrameOptions(providers.FrameOptions{MaxFPS: 0.02}))
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "test.mp4")
	draft := &storage.KnowledgeObject{Source: src}
	ctx, scratch := pipeline.WithScratch(context.Background())
	got, err := step.Run(ctx, draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	defer scratch.Cleanup(got)
	if got.Metadata["frame_count"] != 3 {
		t.Errorf("frame_count: %v", got.Metadata["frame_count"])
	}
	paths, ok := got.Metadata["frame_paths"].([]string)
	if !ok || len(paths) != 3 {
		t.Fatalf("frame_paths: %v", got.Metadata["frame_paths"])
	}
	for _, p := range paths {
		if fi, err := os.Stat(p); err != nil || fi.Size() == 0 {
			t.Errorf("frame file %s: %v", p, err)
		}
		if filepath.Dir(p) == srcDir {
			t.Errorf("frame %s written beside the source", p)
		}
	}
	if entries, _ := os.ReadDir(srcDir); len(entries) != 0 {
		t.Errorf("source directory was written to: %v", entries)
	}
	ms, _ := got.Metadata["frame_ms"].([]int64)
	if len(ms) != 3 || ms[0] != 0 || ms[1] != 50000 {
		t.Errorf("frame_ms: %v", ms)
	}

	if _, ok := got.Metadata["scratch_dirs"]; ok {
		t.Error("scratch bookkeeping leaked into the draft metadata")
	}
	scratch.Cleanup(got)
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("frame %s survived cleanup", p)
		}
	}
	if _, ok := got.Metadata["frame_paths"]; ok {
		t.Error("frame_paths outlived the files")
	}
}

func TestFrameSamplerWorksOnReadOnlySource(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700) //nolint:errcheck // restore so TempDir cleanup works
	draft := &storage.KnowledgeObject{Source: filepath.Join(dir, "test.mp4")}
	ctx, scratch := pipeline.WithScratch(context.Background())
	got, err := NewFrameSampler(WithFrameOptions(providers.FrameOptions{MaxFPS: 0.02})).Run(ctx, draft)
	if err != nil {
		t.Fatalf("read-only source: %v", err)
	}
	scratch.Cleanup(got)
}

// brokenProvider yields a frame that violates the contract.
type brokenProvider struct{}

func (brokenProvider) Name() string { return "broken" }
func (brokenProvider) ExtractFrames(_ context.Context, _ providers.MediaSource, _ providers.FrameOptions, yield providers.FrameYield) error {
	return yield(providers.Frame{Index: 0, ContentType: "image/png"}) // no bytes
}

func TestFrameSamplerChecksProviderOutput(t *testing.T) {
	draft := &storage.KnowledgeObject{Source: "/tmp/x.mp4"}
	ctx, scratch := pipeline.WithScratch(context.Background())
	_, err := NewFrameSampler(WithFrameProvider(brokenProvider{})).Run(ctx, draft)
	scratch.Cleanup(draft)
	if !errors.Is(err, providers.ErrInvalidFrame) {
		t.Fatalf("err = %v, want ErrInvalidFrame", err)
	}
}

func TestFrameSamplerNeedsAScratchScope(t *testing.T) {
	_, err := NewFrameSampler().Run(context.Background(), &storage.KnowledgeObject{Source: "/tmp/x.mp4"})
	if !errors.Is(err, pipeline.ErrNoScratch) {
		t.Fatalf("err = %v, want ErrNoScratch", err)
	}
}

// unusableProvider fails with media no retry can fix.
type unusableProvider struct{}

func (unusableProvider) Name() string { return "unusable" }
func (unusableProvider) ExtractFrames(context.Context, providers.MediaSource, providers.FrameOptions, providers.FrameYield) error {
	return providers.ErrUnusableMedia
}

func TestFrameSamplerUnusableMediaIsPermanent(t *testing.T) {
	ctx, scratch := pipeline.WithScratch(context.Background())
	draft := &storage.KnowledgeObject{Source: "/tmp/x.mp4"}
	defer scratch.Cleanup(draft)
	_, err := NewFrameSampler(WithFrameProvider(unusableProvider{})).Run(ctx, draft)
	var perm *pipeline.PermanentError
	if !errors.As(err, &perm) {
		t.Fatalf("err = %v, want a permanent error so the job is not retried", err)
	}
}

func TestFrameSamplerRejectsInvalidOptions(t *testing.T) {
	step := NewFrameSampler(WithFrameOptions(providers.FrameOptions{}))
	ctx, scratch := pipeline.WithScratch(context.Background())
	draft := &storage.KnowledgeObject{Source: "/tmp/x.mp4"}
	defer scratch.Cleanup(draft)
	_, err := step.Run(ctx, draft)
	if !errors.Is(err, providers.ErrInvalidFrameOptions) {
		t.Fatalf("err = %v, want ErrInvalidFrameOptions", err)
	}
}

func TestFrameSamplerName(t *testing.T) {
	step := NewFrameSampler()
	if step.Name() != "frame_sampler" {
		t.Errorf("name: %q", step.Name())
	}
}
