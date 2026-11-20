package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// seenBus checks, at publish time, that the outbox row of an object
// event is already committed: the live event follows the row.
type seenBus struct {
	recordingBus
	ob   storage.OutboxStore
	mu2  sync.Mutex
	miss []string
}

func (b *seenBus) Publish(ctx context.Context, e bus.Event) error {
	if events.OutboxTopics[e.Topic] {
		head, err := b.ob.Head(context.Background())
		b.mu2.Lock()
		if err != nil || head == 0 {
			b.miss = append(b.miss, string(e.Topic))
		}
		b.mu2.Unlock()
	}
	return b.recordingBus.Publish(ctx, e)
}

func rows(t *testing.T, svc *Service) []storage.OutboxRow {
	t.Helper()
	r, err := svc.Outbox().Since(context.Background(), 0, 0)
	require.NoError(t, err)
	return r
}

// Every object mutation writes its after event to the outbox in the
// mutation's transaction; a failed mutation writes none and publishes
// none; the live publish follows the committed row.
func TestObjectMutationsWriteOutbox(t *testing.T) {
	svc := newTestService(t)
	sb := &seenBus{ob: svc.Outbox()}
	svc.Bus = sb
	ctx := ownerCtx()
	require.NotNil(t, svc.Outbox())

	// analyze raw -> raw_stored
	id, err := svc.Analyze(ctx, AnalyzeRequest{Content: "raw body", Type: "text", Raw: true})
	require.NoError(t, err)
	got := rows(t, svc)
	require.Len(t, got, 1)
	assert.Equal(t, string(events.TopicObjectRawStored), got[0].Topic)
	assert.Equal(t, id, got[0].ObjectID)
	assert.Equal(t, events.OriginAPI, got[0].Origin)
	assert.Equal(t, "service.analyze", got[0].Source)

	// update, delete
	obj, err := svc.GetObject(ctx, id)
	require.NoError(t, err)
	require.NoError(t, svc.UpdateObject(ctx, obj))
	require.NoError(t, svc.DeleteObject(ctx, id))
	got = rows(t, svc)
	require.Len(t, got, 3)
	assert.Equal(t, string(events.TopicObjectUpdated), got[1].Topic)
	assert.Equal(t, string(events.TopicObjectDeleted), got[2].Topic)
	var del events.ObjectDeletedPayload
	require.NoError(t, json.Unmarshal(got[2].Payload, &del))
	assert.Equal(t, id, del.ObjectID)
	assert.Equal(t, id, got[2].ObjectID)
	assert.Less(t, got[0].Seq, got[1].Seq)
	assert.Less(t, got[1].Seq, got[2].Seq)

	// failures write and publish nothing
	published := len(sb.take())
	require.Error(t, svc.DeleteObject(ctx, "missing"))
	require.Error(t, svc.UpdateObject(ctx, &storage.KnowledgeObject{ID: "missing", Type: "note", CreatedAt: time.Now(), UpdatedAt: time.Now()}))
	assert.Len(t, rows(t, svc), 3, "failed mutations wrote rows")
	assert.Empty(t, sb.take(), "failed mutations published")
	assert.Equal(t, 3, published)
	assert.Empty(t, sb.miss, "an event was published before its row was committed")
}

// A raw submission under a stored source key updates that object and
// writes object.updated with the changed fields.
func TestRawSourceKeyUpdateWritesOutbox(t *testing.T) {
	svc := newTestService(t)
	ctx := ownerCtx()
	id, err := svc.Analyze(ctx, AnalyzeRequest{Content: "raw body", Type: "text", Raw: true, SourceKey: "sk-1", Source: "t"})
	require.NoError(t, err)
	id2, err := svc.Analyze(ctx, AnalyzeRequest{Content: "raw body", Type: "text", Raw: true, SourceKey: "sk-1", Source: "t", Note: "a note"})
	require.NoError(t, err)
	require.Equal(t, id, id2)
	got := rows(t, svc)
	require.Len(t, got, 2)
	assert.Equal(t, string(events.TopicObjectUpdated), got[1].Topic)
	var p events.ObjectUpdatedPayload
	require.NoError(t, json.Unmarshal(got[1].Payload, &p))
	assert.Equal(t, id, p.ObjectID)
	assert.NotEmpty(t, p.Fields)
}
