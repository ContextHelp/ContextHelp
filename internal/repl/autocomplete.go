package repl

import (
	"context"
	"fmt"
	"strings"
)

// knownCommands is the authoritative list of top-level ctxt sub-commands.
var knownCommands = []string{
	"find", "list", "open", "make", "analyze",
	"job", "config", "import", "feed", "entity",
	"profile", "registry",
}

// knownTypes are the valid --type values for list.
var knownTypes = []string{"url", "text", "pdf", "image", "audio", "video"}

// knownArtifactTypes are the valid artifact types for make.
var knownArtifactTypes = []string{"brief", "plan", "summary", "draft"}

// ServiceAdapter is the minimal interface Completer needs from *service.Service.
// Using an interface keeps autocomplete testable without a real DB.
type ServiceAdapter interface {
	ListTags(ctx context.Context) ([]string, error)
}

// Completer provides context-aware tab completion for the REPL.
type Completer struct {
	state   *SessionState
	service ServiceAdapter
}

// NewCompleter creates a Completer backed by state and service.
func NewCompleter(state *SessionState, svc ServiceAdapter) *Completer {
	return &Completer{state: state, service: svc}
}

// Complete returns candidates for the given line prefix.
// Called by liner from the same goroutine as the read loop — no locking needed.
func (c *Completer) Complete(line string) []string {
	switch {
	case strings.HasPrefix(line, "open "):
		return c.completeOpen()
	case strings.HasPrefix(line, "list --type "):
		return knownTypes
	case strings.HasPrefix(line, "list --tag "):
		return c.completeTags()
	case strings.HasPrefix(line, "make "):
		return knownArtifactTypes
	default:
		return c.completeCommand(line)
	}
}

func (c *Completer) completeOpen() []string {
	n := len(c.state.LastResults)
	if n == 0 {
		return nil
	}
	var out []string
	for i := 1; i <= n; i++ {
		out = append(out, fmt.Sprintf("%d", i))
	}
	for _, obj := range c.state.LastResults {
		out = append(out, obj.ID)
	}
	return out
}

func (c *Completer) completeTags() []string {
	tags, err := c.service.ListTags(context.Background())
	if err != nil {
		return nil
	}
	return tags
}

func (c *Completer) completeCommand(prefix string) []string {
	var out []string
	for _, cmd := range knownCommands {
		if strings.HasPrefix(cmd, prefix) {
			out = append(out, cmd)
		}
	}
	return out
}
