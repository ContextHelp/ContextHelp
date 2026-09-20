package contentmonitor_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contentmonitor "github.com/ideacrafterslabs/ctxt-plugin-content-monitor"
)

// ─── compile-time interface check ────────────────────────────────────────────

var _ pluginapi.Plugin = (*contentmonitor.Plugin)(nil)

// ─── helpers ─────────────────────────────────────────────────────────────────

type stubBus struct {
	events []pluginapi.Event
}

func (b *stubBus) Publish(_ context.Context, e pluginapi.Event) error {
	b.events = append(b.events, e)
	return nil
}
func (b *stubBus) Subscribe(_ string, _ func(context.Context, pluginapi.Event) error) {}
func (b *stubBus) Close() error                                                       { return nil }

// ─── unit tests ───────────────────────────────────────────────────────────────

func TestPlugin_Name(t *testing.T) {
	p := contentmonitor.New()
	assert.Equal(t, "content-monitor", p.Name())
}

func TestPlugin_Version(t *testing.T) {
	p := contentmonitor.New()
	assert.Equal(t, "1.0.0", p.Version())
}

func TestPlugin_PipelineSteps_Empty(t *testing.T) {
	p := contentmonitor.New()
	assert.Empty(t, p.PipelineSteps())
}

func TestPlugin_Close_Uninitialised(t *testing.T) {
	p := contentmonitor.New()
	require.NoError(t, p.Close(context.Background()))
}

func TestPlugin_FirstFetchEmitsSeenEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>original</html>"))
	}))
	defer srv.Close()

	bus := &stubBus{}
	p := contentmonitor.New()
	err := p.Init(context.Background(), map[string]interface{}{
		"urls":     []interface{}{srv.URL},
		"interval": "1h",
	}, pluginapi.Deps{Bus: bus})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	require.NoError(t, p.Close(context.Background()))

	seenEvents := 0
	for _, e := range bus.events {
		if e.Type == "ctxt.plugin.content-monitor.seen" {
			seenEvents++
		}
	}
	assert.Equal(t, 1, seenEvents, "first fetch should emit exactly one 'seen' event")
}

func TestPlugin_ContentChangeEmitsChangeEvent(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := callCount.Add(1)
		w.WriteHeader(http.StatusOK)
		if n == 1 {
			_, _ = w.Write([]byte("version one content"))
		} else {
			_, _ = w.Write([]byte("version two content — different"))
		}
	}))
	defer srv.Close()

	bus := &stubBus{}
	p := contentmonitor.New()
	err := p.Init(context.Background(), map[string]interface{}{
		"urls":     []interface{}{srv.URL},
		"interval": "40ms",
	}, pluginapi.Deps{Bus: bus})
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)
	require.NoError(t, p.Close(context.Background()))

	changeEvents := 0
	for _, e := range bus.events {
		if e.Type == "ctxt.plugin.content-monitor.change" {
			changeEvents++
		}
	}
	assert.GreaterOrEqual(t, changeEvents, 1, "content change should emit at least one change event")
}

func TestPlugin_NoChangeDoesNotEmit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("stable content"))
	}))
	defer srv.Close()

	bus := &stubBus{}
	p := contentmonitor.New()
	err := p.Init(context.Background(), map[string]interface{}{
		"urls":     []interface{}{srv.URL},
		"interval": "40ms",
	}, pluginapi.Deps{Bus: bus})
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)
	require.NoError(t, p.Close(context.Background()))

	changeEvents := 0
	for _, e := range bus.events {
		if e.Type == "ctxt.plugin.content-monitor.change" {
			changeEvents++
		}
	}
	assert.Equal(t, 0, changeEvents, "stable content must not emit change events")
}

func TestPlugin_HTTPErrorEmitsErrorEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	bus := &stubBus{}
	p := contentmonitor.New()
	err := p.Init(context.Background(), map[string]interface{}{
		"urls":     []interface{}{srv.URL},
		"interval": "1h",
	}, pluginapi.Deps{Bus: bus})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	require.NoError(t, p.Close(context.Background()))

	errEvents := 0
	for _, e := range bus.events {
		if e.Type == "ctxt.plugin.content-monitor.error" {
			errEvents++
		}
	}
	assert.Equal(t, 1, errEvents, "HTTP 500 should produce an error event")
}

func TestPlugin_TargetLabel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("page"))
	}))
	defer srv.Close()

	bus := &stubBus{}
	p := contentmonitor.New()
	err := p.Init(context.Background(), map[string]interface{}{
		"targets": []interface{}{
			map[string]interface{}{"url": srv.URL, "label": "My Page"},
		},
		"interval": "1h",
	}, pluginapi.Deps{Bus: bus})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	require.NoError(t, p.Close(context.Background()))

	assert.NotEmpty(t, bus.events, "should have at least one event")
}

func TestPlugin_SeenEvent_EmitsGraphNode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("some page content"))
	}))
	defer srv.Close()

	bus := &stubBus{}
	p := contentmonitor.New()
	err := p.Init(context.Background(), map[string]interface{}{
		"targets":  []interface{}{map[string]interface{}{"url": srv.URL}},
		"interval": "1h",
	}, pluginapi.Deps{Bus: bus})
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, p.Close(context.Background()))

	var ko pluginapi.KnowledgeObject
	for _, e := range bus.events {
		if e.Type == "ctxt.plugin.content-monitor.seen" {
			require.NoError(t, json.Unmarshal(e.Data, &ko))
			break
		}
	}
	require.NotNil(t, ko.Graph, "Graph must be populated when content is non-empty")
	found := false
	for _, n := range ko.Graph.Nodes {
		if n.NodeType == pluginapi.NodeTypeSummary && n.Content != "" {
			found = true
			break
		}
	}
	assert.True(t, found, "summary graph node with content expected in seen event")
}
