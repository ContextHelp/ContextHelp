package search

import (
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/search/ftsq"
)

// Known fields that map directly to columns.
var directColumns = map[string]bool{
	"type":     true,
	"subtype":  true,
	"pipeline": true,
	"source":   true,
}

var dateColumns = map[string]bool{
	"created_at": true,
	"updated_at": true,
}

// Compile converts an AST node into a SQL WHERE clause and arguments.
// It targets SQLite (? placeholders, json_each, FTS5 MATCH).
// Use CompileFor to target a specific dialect.
func Compile(node Node) (string, []any, error) {
	return CompileFor(DialectSQLite, node)
}

// CompileFor compiles node for the given SQL dialect.
// For DialectPostgres: uses $N placeholders, jsonb_array_elements for tag
// queries, and websearch_to_tsquery over the generated tsvector column for
// the similar== field.
func CompileFor(d Dialect, node Node) (string, []any, error) {
	sql, args, err := compileNode(d, node)
	if err != nil {
		return "", nil, err
	}
	if d == DialectPostgres {
		sql = rebindPostgres(sql)
	}
	return sql, args, nil
}

// rebindPostgres replaces sequential ? placeholders with $1, $2, ... as
// required by the lib/pq PostgreSQL driver.
func rebindPostgres(sql string) string {
	var b strings.Builder
	n := 1
	for i := 0; i < len(sql); i++ {
		if sql[i] == '?' {
			fmt.Fprintf(&b, "$%d", n)
			n++
		} else {
			b.WriteByte(sql[i])
		}
	}
	return b.String()
}

func compileNode(d Dialect, node Node) (string, []any, error) {
	switch n := node.(type) {
	case ComparisonNode:
		return compileComparison(d, n)
	case AndNode:
		return compileLogical(d, n.Children, "AND")
	case OrNode:
		return compileLogical(d, n.Children, "OR")
	default:
		return "", nil, fmt.Errorf("unknown node type: %T", node)
	}
}

func compileComparison(d Dialect, n ComparisonNode) (string, []any, error) {
	// Tag field: JSON subquery.
	if n.Field == "tag" {
		return compileTag(d, n)
	}

	// Mention field: edge JOIN query.
	if n.Field == "mention" {
		return compileMention(n)
	}

	// Related field: objects sharing mention targets.
	if n.Field == "related" {
		return compileRelated(n)
	}

	// Similar field: FTS5 MATCH (SQLite only).
	if n.Field == "similar" {
		return compileSimilar(d, n)
	}

	// Direct columns.
	if directColumns[n.Field] || dateColumns[n.Field] {
		return compileDirect(n)
	}

	return "", nil, fmt.Errorf("unknown field: %q", n.Field)
}

func compileDirect(n ComparisonNode) (string, []any, error) {
	sqlOp, err := sqlOperator(n.Operator)
	if err != nil {
		return "", nil, err
	}

	if n.Operator == OpIn || n.Operator == OpOut {
		vals, ok := n.Value.([]string)
		if !ok {
			return "", nil, fmt.Errorf("IN/OUT requires []string value")
		}
		placeholders := make([]string, len(vals))
		args := make([]any, len(vals))
		for i, v := range vals {
			placeholders[i] = "?"
			args[i] = v
		}
		not := ""
		if n.Operator == OpOut {
			not = "NOT "
		}
		return fmt.Sprintf("%s %sIN (%s)", n.Field, not, strings.Join(placeholders, ",")), args, nil
	}

	return fmt.Sprintf("%s %s ?", n.Field, sqlOp), []any{n.Value}, nil
}

// compileTag emits a JSON tag existence check.
// SQLite uses json_each; Postgres uses jsonb_array_elements.
func compileTag(d Dialect, n ComparisonNode) (string, []any, error) {
	jsonFunc := "json_each(tags)"
	rowAlias := "json_each.value->>'label'"
	if d == DialectPostgres {
		jsonFunc = "jsonb_array_elements(tags)"
		rowAlias = "value->>'label'"
	}

	if n.Operator == OpIn {
		vals, ok := n.Value.([]string)
		if !ok {
			return "", nil, fmt.Errorf("tag IN requires []string value")
		}
		placeholders := make([]string, len(vals))
		args := make([]any, len(vals))
		for i, v := range vals {
			placeholders[i] = "?"
			args[i] = v
		}
		return fmt.Sprintf(
			"EXISTS (SELECT 1 FROM %s WHERE %s IN (%s))",
			jsonFunc, rowAlias, strings.Join(placeholders, ","),
		), args, nil
	}

	if n.Operator == OpEq {
		return fmt.Sprintf("EXISTS (SELECT 1 FROM %s WHERE %s = ?)", jsonFunc, rowAlias),
			[]any{n.Value}, nil
	}

	if n.Operator == OpNeq {
		return fmt.Sprintf("NOT EXISTS (SELECT 1 FROM %s WHERE %s = ?)", jsonFunc, rowAlias),
			[]any{n.Value}, nil
	}

	return "", nil, fmt.Errorf("unsupported operator %v for tag field", n.Operator)
}

func compileMention(n ComparisonNode) (string, []any, error) {
	if n.Operator != OpEq {
		return "", nil, fmt.Errorf("mention field only supports == operator")
	}
	return "id IN (SELECT from_id FROM edges WHERE from_type = 'object' AND to_type = 'entity' AND edge_type = 'mentions' AND to_id = ?)",
		[]any{n.Value}, nil
}

// compileRelated returns objects that share at least one mention target with any
// object that mentions the given entity slug. For each object e1 that mentions
// the entity, it finds all other objects e2 that share any common mention
// target (to_id) with e1. The entity slug itself is excluded from the join
// condition so e2 ranges over ALL shared targets, not just the seed entity.
func compileRelated(n ComparisonNode) (string, []any, error) {
	if n.Operator != OpEq {
		return "", nil, fmt.Errorf("related field only supports == operator")
	}
	sql := `id IN (
		SELECT DISTINCT e2.from_id
		FROM edges e1
		JOIN edges e2 ON e1.to_id = e2.to_id
		WHERE e1.from_type = 'object'
		  AND e1.to_type = 'entity'
		  AND e1.to_id = ?
		  AND e1.edge_type = 'mentions'
		  AND e2.from_type = 'object'
		  AND e2.from_id != e1.from_id
		  AND e2.edge_type = 'mentions'
	)`
	return sql, []any{n.Value}, nil
}

// PostgresFTSRegconfig is the text-search configuration bound into every
// tsquery the compiler emits for Postgres. Aliased from the shared ftsq
// constant the Postgres driver also builds its generated tsvector column
// from, so compiled predicates and the column can never disagree.
const PostgresFTSRegconfig = ftsq.PostgresRegconfig

// compileSimilar emits a full-text predicate against the dialect's FTS
// index: FTS5 MATCH on SQLite, websearch_to_tsquery over the generated
// tsvector column on Postgres.
func compileSimilar(d Dialect, n ComparisonNode) (string, []any, error) {
	if n.Operator != OpEq {
		return "", nil, fmt.Errorf("similar field only supports == operator")
	}
	if d == DialectPostgres {
		return "id IN (SELECT id FROM objects WHERE fts @@ websearch_to_tsquery(?, ?))",
			[]any{PostgresFTSRegconfig, n.Value}, nil
	}
	return "id IN (SELECT id FROM objects_fts WHERE objects_fts MATCH ?)",
		[]any{n.Value}, nil
}

func compileLogical(d Dialect, children []Node, op string) (string, []any, error) {
	parts := make([]string, len(children))
	var allArgs []any

	for i, child := range children {
		sql, args, err := compileNode(d, child)
		if err != nil {
			return "", nil, err
		}
		parts[i] = "(" + sql + ")"
		allArgs = append(allArgs, args...)
	}

	return strings.Join(parts, " "+op+" "), allArgs, nil
}

func sqlOperator(op Operator) (string, error) {
	switch op {
	case OpEq:
		return "=", nil
	case OpNeq:
		return "!=", nil
	case OpGt:
		return ">", nil
	case OpGte:
		return ">=", nil
	case OpLt:
		return "<", nil
	case OpLte:
		return "<=", nil
	case OpIn:
		return "IN", nil
	case OpOut:
		return "NOT IN", nil
	default:
		return "", fmt.Errorf("unknown operator: %v", op)
	}
}
