package adapter_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/adapter/contacts/cardamum"
	"github.com/ideacrafterslabs/ctxt/internal/adapter/email/himalaya"
)

// TestRegistryRunnerEndToEnd registers two real adapters (cardamum +
// himalaya), exercises full Start→Stop lifecycle through the Runner,
// and verifies (a) one-platform-per-protocol holds across real
// adapters, (b) lifecycle events reach a bus subscriber for both.
//
// Phase 1 acceptance gates 1 (substrate exists), 2 (one-platform
// invariant), 3 (lifecycle events emit on bus), 5 (cardamum +
// himalaya migrated) — this test is the cross-cutting proof.
//
// Lives in `adapter_test` (external test package) so the import of
// the cardamum/himalaya backends doesn't pollute the adapter
// package's import graph (those packages import adapter; importing
// them back in adapter would close a cycle even in test files).
func TestRegistryRunnerEndToEnd(t *testing.T) {
	b := bus.New()
	defer func() { _ = b.Close(context.Background()) }()

	var got []string
	var mu sync.Mutex
	unsub := b.Subscribe("dpkms.adapter.lifecycle.*", func(_ context.Context, e bus.Event) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, string(e.Topic))
		return nil
	})
	defer unsub()

	registry := adapter.NewRegistry()
	if err := registry.Register(cardamum.New("default", cardamum.Config{})); err != nil {
		t.Fatalf("register cardamum: %v", err)
	}
	if err := registry.Register(himalaya.New(himalaya.Config{})); err != nil {
		t.Fatalf("register himalaya: %v", err)
	}

	// One-platform-per-protocol: a second contacts adapter rejects.
	dup := cardamum.New("other", cardamum.Config{})
	if err := registry.Register(dup); !errors.Is(err, adapter.ErrProtocolAlreadyRegistered) {
		t.Errorf("duplicate contacts registration: want ErrProtocolAlreadyRegistered, got %v", err)
	}

	runner := adapter.NewRunner(b)
	for _, proto := range registry.Protocols() {
		if err := runner.Start(context.Background(), registry.Get(proto)); err != nil {
			t.Errorf("Start %q: %v", proto, err)
		}
	}
	for _, proto := range registry.Protocols() {
		if err := runner.Stop(context.Background(), registry.Get(proto)); err != nil {
			t.Errorf("Stop %q: %v", proto, err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	// Two adapters, four events each (started, readied, drained, stopped) = 8.
	if len(got) != 8 {
		t.Errorf("want 8 lifecycle events across 2 adapters, got %d (%v)", len(got), got)
	}
}
