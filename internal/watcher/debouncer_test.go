package watcher

import (
	"sync"
	"testing"
	"time"
)

func TestDebouncer(t *testing.T) {
	const delay = 20 * time.Millisecond
	tests := []struct {
		name string
		run  func(d *debouncer)
		want map[string]int
	}{
		{"one event fires once", func(d *debouncer) { d.schedule("/a") }, map[string]int{"/a": 1}},
		{"burst on one path fires once", func(d *debouncer) {
			for range 5 {
				d.schedule("/a")
			}
		}, map[string]int{"/a": 1}},
		{"paths debounce independently", func(d *debouncer) {
			d.schedule("/a")
			d.schedule("/b")
		}, map[string]int{"/a": 1, "/b": 1}},
		{"cancel drops the pending fire", func(d *debouncer) {
			d.schedule("/a")
			d.schedule("/b")
			d.cancel("/a")
		}, map[string]int{"/b": 1}},
		{"cancel of an unscheduled path is a no-op", func(d *debouncer) { d.cancel("/a") }, map[string]int{}},
		{"stop drops every pending fire", func(d *debouncer) {
			d.schedule("/a")
			d.schedule("/b")
			d.stop()
		}, map[string]int{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			got := map[string]int{}
			d := newDebouncer(delay, func(p string) {
				mu.Lock()
				defer mu.Unlock()
				got[p]++
			})
			tt.run(d)
			time.Sleep(10 * delay)

			mu.Lock()
			defer mu.Unlock()
			if len(got) != len(tt.want) {
				t.Fatalf("fired %v, want %v", got, tt.want)
			}
			for p, n := range tt.want {
				if got[p] != n {
					t.Errorf("%s fired %d times, want %d (all: %v)", p, got[p], n, got)
				}
			}
		})
	}
}
