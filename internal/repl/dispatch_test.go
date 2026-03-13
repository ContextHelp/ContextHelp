package repl

import (
	"context"
	"errors"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captureExec struct {
	calls [][]string
	err   error
}

func (c *captureExec) exec(args []string) error {
	c.calls = append(c.calls, args)
	return c.err
}

func TestDispatch_EmptyLine(t *testing.T) {
	cap := &captureExec{}
	err := Dispatch(context.Background(), "", &SessionState{}, cap.exec)
	require.NoError(t, err)
	assert.Empty(t, cap.calls)
}

func TestDispatch_Comment(t *testing.T) {
	cap := &captureExec{}
	err := Dispatch(context.Background(), "# this is a comment", &SessionState{}, cap.exec)
	require.NoError(t, err)
	assert.Empty(t, cap.calls)
}

func TestDispatch_KnownCommand(t *testing.T) {
	cap := &captureExec{}
	err := Dispatch(context.Background(), "list --type url", &SessionState{}, cap.exec)
	require.NoError(t, err)
	require.Len(t, cap.calls, 1)
	assert.Equal(t, []string{"list", "--type", "url"}, cap.calls[0])
}

func TestDispatch_FindCommand_Explicit(t *testing.T) {
	cap := &captureExec{}
	err := Dispatch(context.Background(), "find auth flow", &SessionState{}, cap.exec)
	require.NoError(t, err)
	require.Len(t, cap.calls, 1)
	assert.Equal(t, []string{"find", "auth", "flow"}, cap.calls[0])
}

func TestDispatch_RSQLExpression(t *testing.T) {
	cap := &captureExec{}
	err := Dispatch(context.Background(), "type==url;tag==checkout", &SessionState{}, cap.exec)
	require.NoError(t, err)
	require.Len(t, cap.calls, 1)
	assert.Equal(t, []string{"list", "--q", "type==url;tag==checkout"}, cap.calls[0])
}

func TestDispatch_RSQL_InExpression(t *testing.T) {
	cap := &captureExec{}
	err := Dispatch(context.Background(), "id=in=(abc,def)", &SessionState{}, cap.exec)
	require.NoError(t, err)
	require.Len(t, cap.calls, 1)
	assert.Equal(t, []string{"list", "--q", "id=in=(abc,def)"}, cap.calls[0])
}

func TestDispatch_NLQFallback(t *testing.T) {
	cap := &captureExec{}
	err := Dispatch(context.Background(), "checkout flow UX", &SessionState{}, cap.exec)
	require.NoError(t, err)
	require.Len(t, cap.calls, 1)
	assert.Equal(t, []string{"find", "checkout", "flow", "UX"}, cap.calls[0])
}

func TestDispatch_Pipe(t *testing.T) {
	cap := &captureExec{}
	err := Dispatch(context.Background(), "auth flow | make brief", &SessionState{}, cap.exec)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(cap.calls), 1)
	assert.Equal(t, "find", cap.calls[0][0])
}

func TestDispatch_ExecError_Propagates(t *testing.T) {
	cap := &captureExec{err: errors.New("db error")}
	err := Dispatch(context.Background(), "find auth", &SessionState{}, cap.exec)
	assert.Error(t, err)
}

func TestDispatch_OpenShorthand_ResolvesIndex(t *testing.T) {
	state := &SessionState{}
	state.SetResults([]*storage.KnowledgeObject{
		{ID: "full-uuid-001"},
		{ID: "full-uuid-002"},
	})

	cap := &captureExec{}
	err := Dispatch(context.Background(), "open 2", state, cap.exec)
	require.NoError(t, err)
	require.Len(t, cap.calls, 1)
	assert.Equal(t, []string{"open", "full-uuid-002"}, cap.calls[0])
}

func TestDispatch_OpenShorthand_OutOfRange(t *testing.T) {
	state := &SessionState{}
	state.SetResults([]*storage.KnowledgeObject{{ID: "only"}})

	cap := &captureExec{}
	err := Dispatch(context.Background(), "open 5", state, cap.exec)
	assert.Error(t, err)
	assert.Empty(t, cap.calls)
}

func TestDispatch_OpenShorthand_NotNumeric(t *testing.T) {
	cap := &captureExec{}
	err := Dispatch(context.Background(), "open abc123", &SessionState{}, cap.exec)
	require.NoError(t, err)
	require.Len(t, cap.calls, 1)
	assert.Equal(t, []string{"open", "abc123"}, cap.calls[0])
}
