package events

import (
	"context"
	"sync"
)

// Handler handles an event.
type Handler func(ctx context.Context, e Event) error

// Bus defines the interface for publishing and subscribing to events.
type Bus interface {
	Publish(ctx context.Context, e Event) error
	Subscribe(eventType string, handler Handler)
	Close() error
}

// LocalBus is an in-memory implementation of Bus.
type LocalBus struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
}

// NewLocalBus creates a new LocalBus.
func NewLocalBus() *LocalBus {
	return &LocalBus{
		handlers: make(map[string][]Handler),
	}
}

// Publish executes all handlers registered for the event's type asynchronously.
func (b *LocalBus) Publish(ctx context.Context, e Event) error {
	b.mu.RLock()
	handlers := b.handlers[e.Type]
	catchAll := b.handlers["*"]
	b.mu.RUnlock()

	for _, h := range handlers {
		go func(handler Handler) {
			_ = handler(context.Background(), e)
		}(h)
	}
	for _, h := range catchAll {
		go func(handler Handler) {
			_ = handler(context.Background(), e)
		}(h)
	}

	return nil
}

// Subscribe registers a handler for a specific event type.
func (b *LocalBus) Subscribe(eventType string, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

// Close closes the bus.
func (b *LocalBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = make(map[string][]Handler)
	return nil
}
