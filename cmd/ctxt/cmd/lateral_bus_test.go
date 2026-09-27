package cmd

import (
	"context"
	"sync"
	"testing"

	"hop.top/kit/go/runtime/bus"

	lateralevents "github.com/ideacrafterslabs/ctxt/internal/lateral/events"
)

// The lateral daemon bus validates topics the way kit does by default:
// a conformant topic passes silently, a malformed one is reported.
func TestLateralBusValidatesTopics(t *testing.T) {
	var (
		mu       sync.Mutex
		reported []error
	)
	b := newLateralBus(func(err error) {
		mu.Lock()
		defer mu.Unlock()
		reported = append(reported, err)
	})
	defer func() { _ = b.Close(context.Background()) }()
	ctx := context.Background()

	if err := b.Publish(ctx, bus.NewEvent(lateralevents.ScanFailed, "test", nil)); err != nil {
		t.Fatalf("publish conformant topic: %v", err)
	}
	mu.Lock()
	if len(reported) != 0 {
		t.Fatalf("conformant topic %q reported: %v", lateralevents.ScanFailed, reported)
	}
	mu.Unlock()

	if err := b.Publish(ctx, bus.NewEvent(bus.Topic("ctxt.lateral.scan-failed"), "test", nil)); err != nil {
		t.Fatalf("warn mode must not reject a publish: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reported) != 1 {
		t.Fatalf("malformed topic reported %d times, want 1", len(reported))
	}
}
