package tui_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/tui"
)

// Write opens never overlap: the TUI's own writes would otherwise contend
// for the database lock it holds and be refused.
func TestRealAdapter_SerializesWriteOpens(t *testing.T) {
	var open, peak int32
	errOpen := errors.New("open refused")
	a := tui.NewRealAdapter(nil, func() (*service.Service, func(), error) {
		n := atomic.AddInt32(&open, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt32(&open, -1)
		return nil, nil, errOpen
	})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := a.RetryJob(context.Background(), "j"); !errors.Is(err, errOpen) {
				t.Errorf("RetryJob = %v", err)
			}
		}()
	}
	wg.Wait()
	if peak != 1 {
		t.Errorf("%d write opens overlapped, want 1 at a time", peak)
	}
}
