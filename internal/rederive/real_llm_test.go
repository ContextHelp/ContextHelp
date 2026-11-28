package rederive

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// llmDownEnv is an env with n text.short objects ingested while the model
// answered, and a release that changes the tagger.
func llmDownEnv(t *testing.T, n int) (*realEnv, string) {
	e := newRealEnv(t)
	for i := 0; i < n; i++ {
		e.ingest("text.short", "", fmt.Sprintf("note number %d about nothing in particular, kept short", i))
	}
	e.record()
	sig := e.bump("text.short", "tagger")
	return e, sig
}

// A model server that answers 503 (an overloaded server, a proxy) is a
// failed call, not a blank reply: runs over several starts record nothing
// and skip nothing, and a larger store is not walked.
func TestReal_ModelAnswering503SkipsNothing(t *testing.T) {
	e, sig := llmDownEnv(t, 8)
	e.llm.status.Store(503)
	for run := 1; run <= 4; run++ {
		before := e.llm.calls.Load()
		res, err := e.run()
		require.NoError(t, err)
		require.Len(t, res.Stopped, 1, "run %d: %+v", run, res)
		// A failed call (HTTP 503), not a reply nobody could use: this is
		// what fails if the client takes a 503 for a blank reply.
		require.Equal(t, "llm", res.Stopped[0].Kind, "run %d", run)
		require.Contains(t, res.Stopped[0].Cause, "HTTP 503", "run %d", run)
		require.NotContains(t, res.Stopped[0].Cause, "unusable reply", "run %d", run)
		require.Zero(t, res.SkippedTotal(), "run %d: %+v", run, res)
		require.Empty(t, e.skips(sig))
		require.Less(t, e.llm.calls.Load()-before, int32(8*40), "bounded calls")
	}
	require.Equal(t, 8, staleObjects(t, e.store(), e.reg))
}

// A model that answers 200 with nothing to every object: the replies are
// unusable, nothing proves the model works, so they only count as tries and
// stop the run after five.
func TestReal_ModelAnsweringNothingStopsTheRunAndSkipsNothing(t *testing.T) {
	e, sig := llmDownEnv(t, 8)
	e.llm.empty.Store(true)
	for run := 1; run <= 4; run++ {
		res, err := e.run()
		require.NoError(t, err)
		require.Len(t, res.Stopped, 1, "run %d: %+v", run, res)
		require.Equal(t, failureStreak, res.Transient, "run %d", run)
		require.Empty(t, e.skips(sig), "run %d", run)
	}
}
