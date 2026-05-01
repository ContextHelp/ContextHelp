//go:build cursor_e2e

package cursor

import (
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cursor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCursor_ConcurrentAdvanceSafe: N goroutines call Advance with strictly
// increasing timestamps; final cursor must equal the max timestamp + matching
// object id. Failure modes: corrupt YAML, lost updates, partial writes.
func TestCursor_ConcurrentAdvanceSafe(t *testing.T) {
	env := newCursorEnv(t)
	const n = 50
	base := time.Now().UTC().Truncate(time.Second)

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ts := base.Add(time.Duration(i) * time.Second)
			_, err := env.mgr.Advance("feed", ts, "ko", cursor.QuerySnapshot{})
			if err != nil {
				t.Errorf("advance %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	c, err := env.mgr.Get("feed")
	require.NoError(t, err)
	expected := base.Add(time.Duration(n-1) * time.Second)
	assert.True(t, c.LastSeenAt.Equal(expected),
		"final last_seen_at: got %v, want %v", c.LastSeenAt, expected)
}
