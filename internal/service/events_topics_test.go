package service

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/events"
)

// recordingBus is a synchronous events.Bus that keeps every published
// event type in publish order.
type recordingBus struct {
	mu    sync.Mutex
	types []string
}

func (b *recordingBus) Publish(_ context.Context, e events.Event) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.types = append(b.types, e.Type)
	return nil
}

func (b *recordingBus) Subscribe(string, events.Handler) {}

func (b *recordingBus) Close() error { return nil }

func (b *recordingBus) take() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.types
	b.types = nil
	return out
}

// Every event type the service publishes on svc.Bus is a kit topic, and
// one enqueue publishes exactly one job-enqueued event.
func TestServicePublishesKitTopics(t *testing.T) {
	svc := newTestService(t)
	rec := &recordingBus{}
	svc.Bus = rec
	ctx := context.Background()

	steps := []struct {
		name string
		run  func(t *testing.T)
		want []bus.Topic
	}{
		{"analyze raw", func(t *testing.T) {
			_, err := svc.Analyze(ctx, AnalyzeRequest{Content: "raw body", Type: "text", Raw: true})
			require.NoError(t, err)
		}, []bus.Topic{events.TopicObjectRawStored}},
		{"analyze", func(t *testing.T) {
			_, err := svc.Analyze(ctx, AnalyzeRequest{Content: "queued body", Type: "text"})
			require.NoError(t, err)
		}, []bus.Topic{events.TopicJobEnqueued}},
		{"enqueue", func(t *testing.T) {
			_, err := svc.Enqueue(ctx, AnalyzeRequest{Content: "enqueued body", Type: "text"})
			require.NoError(t, err)
		}, []bus.Topic{events.TopicJobEnqueued}},
		{"inbox capture and triage", func(t *testing.T) {
			obj, err := svc.CaptureToInbox(ctx, InboxCaptureRequest{Content: "inbox body", Type: "text"})
			require.NoError(t, err)
			_, err = svc.TriageInbox(ctx, obj.ID, TriageRequest{Pipeline: "text.short"})
			require.NoError(t, err)
		}, []bus.Topic{events.TopicInboxCaptured, events.TopicInboxTriaged}},
		{"update and delete", func(t *testing.T) {
			obj, err := svc.CaptureToInbox(ctx, InboxCaptureRequest{Content: "mutable body", Type: "text"})
			require.NoError(t, err)
			_ = rec.take()
			require.NoError(t, svc.UpdateObject(ctx, obj))
			require.NoError(t, svc.DeleteObject(ctx, obj.ID))
		}, []bus.Topic{events.TopicObjectUpdated, events.TopicObjectDeleted}},
	}
	for _, st := range steps {
		t.Run(st.name, func(t *testing.T) {
			_ = rec.take()
			st.run(t)
			got := rec.take()
			want := make([]string, len(st.want))
			for i, w := range st.want {
				want[i] = string(w)
			}
			require.Equal(t, want, got)
			for _, typ := range got {
				require.NoError(t, bus.ValidateTopic(bus.Topic(typ)))
			}
		})
	}
}
