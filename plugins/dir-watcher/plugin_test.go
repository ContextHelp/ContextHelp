package dirwatcher_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dirwatcher "github.com/ideacrafterslabs/ctxt-plugin-dir-watcher"
)

// ─── compile-time interface check ────────────────────────────────────────────

var _ pluginapi.Plugin = (*dirwatcher.Plugin)(nil)

// ─── helpers ─────────────────────────────────────────────────────────────────

type stubBus struct {
	events []pluginapi.Event
}

func (b *stubBus) Publish(_ context.Context, e pluginapi.Event) error {
	b.events = append(b.events, e)
	return nil
}
func (b *stubBus) Subscribe(_ string, _ func(context.Context, pluginapi.Event) error) {}
func (b *stubBus) Close() error                                                        { return nil }

func newDropDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

// ─── unit tests ───────────────────────────────────────────────────────────────

func TestPlugin_Name(t *testing.T) {
	p := dirwatcher.New()
	assert.Equal(t, "dir-watcher", p.Name())
}

func TestPlugin_Version(t *testing.T) {
	p := dirwatcher.New()
	assert.Equal(t, "1.0.0", p.Version())
}

func TestPlugin_PipelineSteps_Empty(t *testing.T) {
	p := dirwatcher.New()
	assert.Empty(t, p.PipelineSteps())
}

func TestPlugin_Close_Uninitialised(t *testing.T) {
	p := dirwatcher.New()
	require.NoError(t, p.Close(context.Background()))
}

func TestPlugin_Init_RequiresDir(t *testing.T) {
	p := dirwatcher.New()
	err := p.Init(context.Background(), map[string]interface{}{}, pluginapi.Deps{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dir")
}

func TestPlugin_IngestsDroppedFile(t *testing.T) {
	dir := newDropDir(t)

	// Write a file before Init so it's picked up on first scan.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello world"), 0o644))

	bus := &stubBus{}
	p := dirwatcher.New()
	err := p.Init(context.Background(), map[string]interface{}{
		"dir":      dir,
		"interval": "1h",
	}, pluginapi.Deps{Bus: bus})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	require.NoError(t, p.Close(context.Background()))

	fileEvents := 0
	for _, e := range bus.events {
		if e.Type == "ctxt.plugin.dir-watcher.file" {
			fileEvents++
		}
	}
	assert.Equal(t, 1, fileEvents)
}

func TestPlugin_ExtensionFilter(t *testing.T) {
	dir := newDropDir(t)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "keep.md"), []byte("keep"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "skip.exe"), []byte("skip"), 0o644))

	bus := &stubBus{}
	p := dirwatcher.New()
	err := p.Init(context.Background(), map[string]interface{}{
		"dir":        dir,
		"interval":   "1h",
		"extensions": []interface{}{".md"},
	}, pluginapi.Deps{Bus: bus})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	require.NoError(t, p.Close(context.Background()))

	fileEvents := 0
	for _, e := range bus.events {
		if e.Type == "ctxt.plugin.dir-watcher.file" {
			fileEvents++
		}
	}
	assert.Equal(t, 1, fileEvents, "only .md files should be ingested")
}

func TestPlugin_DeduplicatesFiles(t *testing.T) {
	dir := newDropDir(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "once.txt"), []byte("dedup test"), 0o644))

	bus := &stubBus{}
	p := dirwatcher.New()
	err := p.Init(context.Background(), map[string]interface{}{
		"dir":      dir,
		"interval": "30ms",
	}, pluginapi.Deps{Bus: bus})
	require.NoError(t, err)

	time.Sleep(150 * time.Millisecond)
	require.NoError(t, p.Close(context.Background()))

	fileEvents := 0
	for _, e := range bus.events {
		if e.Type == "ctxt.plugin.dir-watcher.file" {
			fileEvents++
		}
	}
	assert.Equal(t, 1, fileEvents, "same file must not emit more than one event")
}

func TestPlugin_NewFilePickedUpAfterStart(t *testing.T) {
	dir := newDropDir(t)

	bus := &stubBus{}
	p := dirwatcher.New()
	err := p.Init(context.Background(), map[string]interface{}{
		"dir":      dir,
		"interval": "30ms",
	}, pluginapi.Deps{Bus: bus})
	require.NoError(t, err)

	// Drop a file after the first scan.
	time.Sleep(60 * time.Millisecond)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "late.txt"), []byte("late arrival"), 0o644))

	time.Sleep(100 * time.Millisecond)
	require.NoError(t, p.Close(context.Background()))

	fileEvents := 0
	for _, e := range bus.events {
		if e.Type == "ctxt.plugin.dir-watcher.file" {
			fileEvents++
		}
	}
	assert.Equal(t, 1, fileEvents, "late-dropped file must be picked up on subsequent poll")
}
