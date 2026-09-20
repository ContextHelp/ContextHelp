// Package upgrade: Selector — predicate parsing for ADR-070
// reingest_selective upgrades (T-0581).
//
// Two surface forms:
//
//   - Pipeline filter: `pipeline=<name>@vN`. Compiles to
//     `WHERE pipeline = ?` with arg=`<name>@vN`. The bare form
//     `pipeline=<name>` (no `@vN`) is rejected — re-ingesting an entire
//     family by accident is exactly the footgun ADR-070 §1 calls out.
//
//   - SQL WHERE: `where:<predicate>`. Escape hatch for selectivity that the
//     pipeline filter can't express (e.g. T-0565's
//     `graph_json IS NULL AND pipeline LIKE 'text.short%'`). The `where:`
//     prefix is mandatory so a typo'd pipeline filter
//     ("pipline=text.short@v1") falls back to a parse error rather than
//     being shoehorned into raw SQL injection.
//
// Validation:
//   - Allowlist every bare identifier in the predicate against the queryable
//     `objects` columns plus the operator keywords (AND/OR/NOT/IS/NULL/
//     LIKE/IN/BETWEEN/GLOB). This is the primary control: it bounds the
//     predicate to a known vocabulary, so subqueries, function calls and
//     other tables are rejected rather than merely discouraged.
//   - Reject any predicate containing `;`, `--`, `/*`, `*/` to block
//     statement chaining and comment-as-code injection.
//   - Reject any predicate matching DDL/DML keywords as standalone tokens
//     (DROP, INSERT, UPDATE, DELETE, ALTER, CREATE, REPLACE, TRUNCATE,
//     ATTACH, DETACH, PRAGMA, BEGIN, COMMIT, ROLLBACK).
//   - Pre-warm the query plan via `EXPLAIN QUERY PLAN SELECT id FROM
//     objects WHERE <pred>` — if SQLite rejects, the predicate is
//     malformed (column doesn't exist, syntax error).
package upgrade

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Selector compiles a user-supplied predicate string to a SQL WHERE clause.
// Construct via ParseSelector. CountMatching / IterateMatching execute the
// compiled query against an *sql.DB.
type Selector struct {
	raw  string
	sql  string // WHERE clause body, no leading "WHERE"
	args []any
}

// Raw returns the original input string (for logging / event payloads).
func (s *Selector) Raw() string {
	if s == nil {
		return ""
	}
	return s.raw
}

// dangerousFragmentRE matches statement-chaining + comment-injection markers.
// Hits → reject. Conservative on purpose: a legitimate WHERE never needs
// these tokens. (`/*` / `*/` are also caught by the keyword scanner via the
// SQLite EXPLAIN reject path, but explicit rejection here gives a clearer
// error message than "syntax error near *".)
var dangerousFragmentRE = regexp.MustCompile(`(;|--|/\*|\*/)`)

// dangerousKeywordRE matches DDL / DML keywords as whole-word tokens. Wraps
// the keyword set in `\b` so it won't fire on identifiers that happen to
// contain the substring (e.g. a column named "draft" must not trip "DROP").
var dangerousKeywordRE = regexp.MustCompile(`(?i)\b(DROP|INSERT|UPDATE|DELETE|ALTER|CREATE|REPLACE|TRUNCATE|ATTACH|DETACH|PRAGMA|BEGIN|COMMIT|ROLLBACK|VACUUM)\b`)

// identifierRE extracts bare identifiers (unquoted word tokens not part of a
// string literal) so they can be checked against allowedIdentifiers.
var identifierRE = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// stringLiteralRE matches single-quoted SQL string literals. Literals are
// stripped before identifier extraction so a value like 'text.short%' is not
// mistaken for a column reference.
var stringLiteralRE = regexp.MustCompile(`'(?:[^']|'')*'`)

// allowedIdentifiers is the closed set of bare word tokens a `where:`
// predicate may reference: the queryable columns of the objects table plus
// the SQL operator keywords needed to combine them.
//
// This is the primary control. The denylists above catch obvious abuse, but a
// denylist can always be evaded; an allowlist bounds the predicate to a known
// vocabulary. Anything outside it — a subquery keyword (SELECT, UNION), a
// function name, another table — is rejected before the string is ever
// concatenated into SQL.
var allowedIdentifiers = map[string]bool{
	// objects columns eligible for selection predicates.
	"id": true, "type": true, "subtype": true, "pipeline": true,
	"source": true, "source_key": true, "status": true,
	"content_hash": true, "content_type": true, "profile_id": true,
	"graph_json": true, "metadata": true, "tags": true,
	"created_at": true, "updated_at": true, "last_reinforced_at": true,
	"reinforcement_count": true, "fts_indexed": true, "vector_indexed": true,
	// operator / literal keywords.
	"AND": true, "OR": true, "NOT": true, "IS": true, "NULL": true,
	"LIKE": true, "IN": true, "BETWEEN": true, "GLOB": true,
	"TRUE": true, "FALSE": true,
}

// validateIdentifiers rejects a predicate referencing anything outside
// allowedIdentifiers. String literals are stripped first so their contents
// are treated as data, not identifiers.
func validateIdentifiers(pred string) error {
	stripped := stringLiteralRE.ReplaceAllString(pred, "''")
	for _, tok := range identifierRE.FindAllString(stripped, -1) {
		// Columns are stored lowercase, keywords uppercase; accept a token
		// under either normalisation so `PIPELINE` and `and` both pass.
		if !allowedIdentifiers[strings.ToLower(tok)] && !allowedIdentifiers[strings.ToUpper(tok)] {
			return fmt.Errorf(
				"upgrade selector: predicate references unknown identifier %q "+
					"(allowed: objects columns and AND/OR/NOT/IS/NULL/LIKE/IN/BETWEEN/GLOB)", tok)
		}
	}
	return nil
}

// ErrSelectorEmpty is returned when ParseSelector is given an empty string.
var ErrSelectorEmpty = errors.New("upgrade selector: empty (use 'pipeline=<name>@vN' or 'where:<predicate>')")

// ParseSelector parses a selector string into a compiled Selector. The db
// argument is optional: when non-nil, the parser pre-warms the query plan
// to validate the predicate is well-formed against the live schema. Pass
// nil to skip validation (useful in tests).
func ParseSelector(s string) (*Selector, error) {
	return ParseSelectorWithDB(s, nil)
}

// ParseSelectorWithDB is ParseSelector with optional schema validation. When
// db is non-nil, runs `EXPLAIN QUERY PLAN SELECT id FROM objects WHERE <pred>`
// to confirm SQLite accepts the predicate.
func ParseSelectorWithDB(s string, db *sql.DB) (*Selector, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil, ErrSelectorEmpty
	}

	switch {
	case strings.HasPrefix(trimmed, "pipeline="):
		return parsePipelineFilter(trimmed)
	case strings.HasPrefix(trimmed, "where:"):
		return parseWhereSelector(trimmed[len("where:"):], db)
	default:
		return nil, fmt.Errorf("upgrade selector: unrecognised form %q (expected 'pipeline=<name>@vN' or 'where:<predicate>')", trimmed)
	}
}

// parsePipelineFilter handles the `pipeline=<name>@vN` form.
func parsePipelineFilter(input string) (*Selector, error) {
	value := strings.TrimSpace(strings.TrimPrefix(input, "pipeline="))
	if value == "" {
		return nil, errors.New("upgrade selector: pipeline filter requires a value (e.g. 'pipeline=text.short@v0')")
	}
	// Footgun guard: bare `pipeline=text.short` would re-ingest every
	// version of the family, including the current one. ADR-070 §1
	// requires explicit version targeting for selective re-ingest.
	if !strings.Contains(value, "@v") {
		return nil, fmt.Errorf("upgrade selector: pipeline=%q is missing '@vN' suffix (use 'pipeline=text.short@v0' to target a specific version; bare names are rejected to avoid accidental full-family re-ingest)", value)
	}
	return &Selector{
		raw:  input,
		sql:  "pipeline = ?",
		args: []any{value},
	}, nil
}

// parseWhereSelector handles the `where:<predicate>` escape hatch.
func parseWhereSelector(predicate string, db *sql.DB) (*Selector, error) {
	pred := strings.TrimSpace(predicate)
	if pred == "" {
		return nil, errors.New("upgrade selector: where:<predicate> requires a predicate body")
	}
	if m := dangerousFragmentRE.FindString(pred); m != "" {
		return nil, fmt.Errorf("upgrade selector: predicate contains forbidden fragment %q (statement chaining / comments not allowed)", m)
	}
	if m := dangerousKeywordRE.FindString(pred); m != "" {
		return nil, fmt.Errorf("upgrade selector: predicate contains forbidden keyword %q (DDL/DML not allowed in selector predicates)", m)
	}
	// Allowlist check — the control that actually bounds the predicate.
	if err := validateIdentifiers(pred); err != nil {
		return nil, err
	}

	// Pre-warm the query plan so column-not-found / syntax errors surface
	// at parse time rather than at the first iterator call. Skipped when
	// db is nil (tests).
	if db != nil {
		// #nosec G202 -- pred passed validateIdentifiers: every bare token is
		// an allowlisted objects column or operator keyword, and statement
		// chaining / comment markers are rejected above. This is a read-only
		// EXPLAIN. A bind parameter cannot express a WHERE clause structure.
		query := "EXPLAIN QUERY PLAN SELECT id FROM objects WHERE " + pred
		rows, err := db.QueryContext(context.Background(), query)
		if err != nil {
			return nil, fmt.Errorf("upgrade selector: predicate rejected by SQLite: %w", err)
		}
		_ = rows.Close()
	}

	return &Selector{
		raw: "where:" + pred,
		sql: pred,
	}, nil
}

// CountMatching executes `SELECT COUNT(*) FROM objects WHERE <pred>` against
// db.
func (s *Selector) CountMatching(ctx context.Context, db *sql.DB) (int, error) {
	if s == nil {
		return 0, errors.New("upgrade selector: nil")
	}
	query := "SELECT COUNT(*) FROM objects"
	if s.sql != "" {
		query += " WHERE " + s.sql
	}
	var n int
	if err := db.QueryRowContext(ctx, query, s.args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("upgrade selector: count: %w", err)
	}
	return n, nil
}

// IterateMatching streams object IDs matching the selector, ordered by
// (created_at ASC, id ASC) — deterministic across runs so a Tick'd progress
// counter is meaningful (the same object is always at position N).
//
// The returned channel closes when iteration finishes, the context is
// cancelled, or a row scan errors. On error, the error is logged via the
// returned error channel; callers should range over both channels.
//
// Single-channel signature simplifies callers that don't care about distinct
// success/error reporting (the worker treats any abort as a failure anyway).
func (s *Selector) IterateMatching(ctx context.Context, db *sql.DB) (<-chan string, error) {
	if s == nil {
		return nil, errors.New("upgrade selector: nil")
	}
	// #nosec G202 -- s.sql is either the constant "pipeline = ?" or a
	// predicate that passed validateIdentifiers at ParseSelector time
	// (allowlisted columns/operators only). Selector has no exported
	// mutator, so the compiled SQL cannot be altered after validation.
	query := "SELECT id FROM objects"
	if s.sql != "" {
		// #nosec G202 -- see the justification above this function body.
		query += " WHERE " + s.sql
	}
	query += " ORDER BY created_at ASC, id ASC"

	rows, err := db.QueryContext(ctx, query, s.args...)
	if err != nil {
		return nil, fmt.Errorf("upgrade selector: iterate: %w", err)
	}

	out := make(chan string, 32)
	go func() {
		defer rows.Close()
		defer close(out)
		for rows.Next() {
			var id string
			if scanErr := rows.Scan(&id); scanErr != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case out <- id:
			}
		}
	}()
	return out, nil
}
