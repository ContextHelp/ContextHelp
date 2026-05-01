package repl

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
)

type mockAdapter struct {
	tags []string
}

func (m *mockAdapter) ListTags(_ context.Context) ([]string, error) {
	return m.tags, nil
}

func TestCompleter_EmptyLine(t *testing.T) {
	c := &Completer{state: &SessionState{}, service: &mockAdapter{}}
	got := c.Complete("")
	assert.ElementsMatch(t, knownCommands, got)
}

func TestCompleter_PartialCommand(t *testing.T) {
	c := &Completer{state: &SessionState{}, service: &mockAdapter{}}
	got := c.Complete("fi")
	assert.Contains(t, got, "find")
	// "feed" does not start with "fi", only "find" matches
	assert.NotContains(t, got, "feed")

	// "fe" prefix should match "feed" and "feed"
	got2 := c.Complete("fe")
	assert.Contains(t, got2, "feed")
}

func TestCompleter_OpenPrefix_WithResults(t *testing.T) {
	state := &SessionState{}
	state.SetResults([]*storage.KnowledgeObject{
		{ID: "abc-001"},
		{ID: "abc-002"},
		{ID: "abc-003"},
	})
	c := &Completer{state: state, service: &mockAdapter{}}
	got := c.Complete("show ")
	assert.Contains(t, got, "1")
	assert.Contains(t, got, "2")
	assert.Contains(t, got, "3")
	assert.Contains(t, got, "abc-001")
	assert.Contains(t, got, "abc-002")
}

func TestCompleter_OpenPrefix_NoResults(t *testing.T) {
	c := &Completer{state: &SessionState{}, service: &mockAdapter{}}
	got := c.Complete("show ")
	assert.Empty(t, got)
}

func TestCompleter_ListTypePrefix(t *testing.T) {
	c := &Completer{state: &SessionState{}, service: &mockAdapter{}}
	got := c.Complete("list --type ")
	assert.ElementsMatch(t, knownTypes, got)
}

func TestCompleter_MakePrefix(t *testing.T) {
	c := &Completer{state: &SessionState{}, service: &mockAdapter{}}
	got := c.Complete("make ")
	assert.ElementsMatch(t, knownArtifactTypes, got)
}

func TestCompleter_ListTagPrefix(t *testing.T) {
	c := &Completer{
		state:   &SessionState{},
		service: &mockAdapter{tags: []string{"auth", "checkout", "ux"}},
	}
	got := c.Complete("list --tag ")
	assert.Contains(t, got, "auth")
	assert.Contains(t, got, "checkout")
	assert.Contains(t, got, "ux")
}
