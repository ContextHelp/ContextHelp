package storage

import "strings"

// likeEscaper escapes LIKE metacharacters with a backslash; queries that
// use its output declare ESCAPE '\'.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// ContainsPattern returns a LIKE pattern, for use with ESCAPE '\', that
// matches any value containing q lower-cased. The caller compares it
// against a lower-cased column.
func ContainsPattern(q string) string {
	return "%" + likeEscaper.Replace(strings.ToLower(q)) + "%"
}
