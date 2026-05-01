package repl

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSession_New(t *testing.T) {
	cfg := &config.Config{}
	s := NewSession(nil, cfg)
	assert.NotNil(t, s)
	assert.NotNil(t, s.state)
}

// TestSession_DispatchIntegration exercises the dispatch path used by Run
// without requiring a real liner TTY.
func TestSession_DispatchIntegration(t *testing.T) {
	state := &SessionState{}
	var execCalls [][]string
	execFn := func(args []string) error {
		execCalls = append(execCalls, args)
		return nil
	}

	require.NoError(t, Dispatch(context.Background(), "checkout flow UX", state, execFn))
	require.Len(t, execCalls, 1)
	assert.Equal(t, []string{"find", "checkout", "flow", "UX"}, execCalls[0])

	execCalls = nil
	require.NoError(t, Dispatch(context.Background(), "type==url;tag==checkout", state, execFn))
	require.Len(t, execCalls, 1)
	assert.Equal(t, []string{"list", "--q", "type==url;tag==checkout"}, execCalls[0])

	// Populate state, then test open N.
	state.SetResults([]*storage.KnowledgeObject{{ID: "real-uuid-abc"}})
	execCalls = nil
	require.NoError(t, Dispatch(context.Background(), "show 1", state, execFn))
	require.Len(t, execCalls, 1)
	assert.Equal(t, []string{"show", "real-uuid-abc"}, execCalls[0])
}
