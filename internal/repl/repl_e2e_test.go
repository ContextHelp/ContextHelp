package repl

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type e2eCapture struct {
	calls   [][]string
	state   *SessionState
	results []*storage.KnowledgeObject
}

func (c *e2eCapture) exec(args []string) error {
	c.calls = append(c.calls, args)
	// Simulate find populating state.
	if len(args) > 0 && args[0] == "find" && len(c.results) > 0 {
		c.state.SetResults(c.results)
	}
	return nil
}

func TestREPL_E2E_ScriptedCommands(t *testing.T) {
	state := &SessionState{}
	results := []*storage.KnowledgeObject{
		{ID: "obj-aaa-001"},
		{ID: "obj-bbb-002"},
	}

	cap := &e2eCapture{state: state, results: results}
	ctx := context.Background()

	// NLQ fallback -> find
	require.NoError(t, Dispatch(ctx, "checkout flow UX", state, cap.exec))
	require.Len(t, cap.calls, 1)
	assert.Equal(t, []string{"find", "checkout", "flow", "UX"}, cap.calls[0])
	assert.Len(t, state.LastResults, 2)

	// open 1 -> resolved to first result
	require.NoError(t, Dispatch(ctx, "open 1", state, cap.exec))
	require.Len(t, cap.calls, 2)
	assert.Equal(t, []string{"open", "obj-aaa-001"}, cap.calls[1])

	// RSQL expression -> list --q
	require.NoError(t, Dispatch(ctx, "type==url;tag==checkout", state, cap.exec))
	require.Len(t, cap.calls, 3)
	assert.Equal(t, []string{"list", "--q", "type==url;tag==checkout"}, cap.calls[2])

	// Explicit cobra command -> direct dispatch
	require.NoError(t, Dispatch(ctx, "list --type pdf --limit 5", state, cap.exec))
	require.Len(t, cap.calls, 4)
	assert.Equal(t, []string{"list", "--type", "pdf", "--limit", "5"}, cap.calls[3])

	// Comment -> no dispatch
	require.NoError(t, Dispatch(ctx, "# just a comment", state, cap.exec))
	assert.Len(t, cap.calls, 4)

	// Empty line -> no dispatch
	require.NoError(t, Dispatch(ctx, "", state, cap.exec))
	assert.Len(t, cap.calls, 4)
}

func TestREPL_E2E_PipeFlow(t *testing.T) {
	state := &SessionState{}
	results := []*storage.KnowledgeObject{
		{ID: "pipe-obj-001"},
		{ID: "pipe-obj-002"},
	}

	cap := &e2eCapture{state: state, results: results}
	ctx := context.Background()

	require.NoError(t, Dispatch(ctx, "auth flow | make brief", state, cap.exec))

	require.GreaterOrEqual(t, len(cap.calls), 1)
	assert.Equal(t, "find", cap.calls[0][0])

	if len(cap.calls) >= 2 {
		assert.Equal(t, "make", cap.calls[1][0])
		assert.Equal(t, "brief", cap.calls[1][1])
		assert.Contains(t, cap.calls[1][2], "--q")
	}
}
