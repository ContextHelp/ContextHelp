package repl

import (
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ParsePipe splits line on the first unquoted " | " (space-pipe-space).
// Requiring surrounding spaces avoids false positives with RSQL value lists
// like type==url|pdf.
//
// Quoted sections (delimited by ") are skipped during scan.
// Returns (lhs, rhs, true) when a pipe is found; otherwise ("", "", false).
func ParsePipe(line string) (lhs, rhs string, isPipe bool) {
	inQuote := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if ch == '"' {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		if ch == '|' && i > 0 && i < len(line)-1 {
			if line[i-1] == ' ' && line[i+1] == ' ' {
				lhs = strings.TrimSpace(line[:i])
				rhs = strings.TrimSpace(line[i+1:])
				return lhs, rhs, true
			}
		}
	}
	return "", "", false
}

// BuildPipeCommand constructs a make command targeting the given result objects.
// Returns empty string if objs is nil or empty.
func BuildPipeCommand(objs []*storage.KnowledgeObject, artifactType string) string {
	if len(objs) == 0 {
		return ""
	}
	ids := make([]string, len(objs))
	for i, obj := range objs {
		ids[i] = obj.ID
	}
	return fmt.Sprintf(`make %s --q "id=in=(%s)"`, artifactType, strings.Join(ids, ","))
}
