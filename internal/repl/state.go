package repl

import (
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// SessionState holds mutable per-session context for the REPL.
// It is not goroutine-safe; access only from the read loop goroutine.
type SessionState struct {
	// LastResults is the ordered result set from the most recent find/list command.
	LastResults []*storage.KnowledgeObject
	// ActiveProfile is the focus profile active for this session.
	ActiveProfile string
	// LastQuery is the raw text of the most recent query.
	LastQuery string
}

// SetResults replaces LastResults with objs and clears any stale index references.
func (s *SessionState) SetResults(objs []*storage.KnowledgeObject) {
	s.LastResults = objs
}

// ResolveIndex returns the 1-based nth result from LastResults.
// Returns an error if n is out of range.
func (s *SessionState) ResolveIndex(n int) (*storage.KnowledgeObject, error) {
	if n < 1 || n > len(s.LastResults) {
		return nil, fmt.Errorf("no result %d (have %d)", n, len(s.LastResults))
	}
	return s.LastResults[n-1], nil
}
