package steps

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestAudioExtractorSetsPath(t *testing.T) {
	step := NewAudioExtractor()
	draft := &storage.KnowledgeObject{
		Source: "/tmp/test.mp4",
	}
	ctx, scratch := pipeline.WithScratch(context.Background())
	got, err := step.Run(ctx, draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	defer scratch.Cleanup(got)
	if got.Metadata["audio_path"] == "" {
		t.Error("expected audio_path to be set")
	}
	if got.Metadata["audio_format"] == "" {
		t.Error("expected audio_format to be set")
	}
}

func TestAudioExtractorWritesToScratchNotBesideSource(t *testing.T) {
	srcDir := t.TempDir()
	ctx, scratch := pipeline.WithScratch(context.Background())
	draft := &storage.KnowledgeObject{Source: filepath.Join(srcDir, "talk.mp4")}
	got, err := NewAudioExtractor().Run(ctx, draft)
	if err != nil {
		t.Fatal(err)
	}
	audio, _ := got.Metadata["audio_path"].(string)
	if audio == "" || filepath.Dir(audio) == srcDir {
		t.Errorf("audio_path %q is empty or beside the source %s", audio, srcDir)
	}
	scratch.Cleanup(got)
	if _, ok := got.Metadata["audio_path"]; ok {
		t.Error("audio_path outlived its scratch dir")
	}
}

func TestAudioExtractorNeedsAScratchScope(t *testing.T) {
	_, err := NewAudioExtractor().Run(context.Background(), &storage.KnowledgeObject{Source: "/tmp/x.mp4"})
	if !errors.Is(err, pipeline.ErrNoScratch) {
		t.Fatalf("err = %v, want ErrNoScratch", err)
	}
}

func TestAudioExtractorName(t *testing.T) {
	step := NewAudioExtractor()
	if step.Name() != "audio_extractor" {
		t.Errorf("name: %q", step.Name())
	}
}
