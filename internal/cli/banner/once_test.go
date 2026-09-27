package banner

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

var inFlight = upgrade.Status{
	State: upgrade.StateInProgress, Bucket: upgrade.BucketReingestSelective,
	Done: 47, Total: 120, Progress: 47.0 / 120, EtaSeconds: 32,
}

// Once prints the first status that renders a line and nothing after it,
// however many responses carry the header.
func TestOncePrintsFirstStatusOnly(t *testing.T) {
	var buf bytes.Buffer
	o := NewOnce(&buf)
	o.Show(upgrade.Status{State: upgrade.StateIdle})
	o.Show(inFlight)
	o.Show(upgrade.Status{State: upgrade.StateFailed, Bucket: upgrade.BucketReindexAuto, LastError: "later"})
	o.Show(inFlight)

	got := buf.String()
	if strings.Count(got, "\n") != 1 || got != Format(inFlight)+"\n" {
		t.Fatalf("output = %q, want the in-flight banner once", got)
	}
}

// Responses arriving on several goroutines still print one line.
func TestOnceConcurrent(t *testing.T) {
	var buf bytes.Buffer
	o := NewOnce(&buf)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() { defer wg.Done(); o.Show(inFlight) }()
	}
	wg.Wait()
	if n := strings.Count(buf.String(), "\n"); n != 1 {
		t.Fatalf("printed %d lines, want 1", n)
	}
}
