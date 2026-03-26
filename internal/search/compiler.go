package search

import (
	"fmt"
	"strings"
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
func Compile(node Node) (string, []any, error) {
	return compileNode(node)
}

func compileNode(node Node) (string, []any, error) {
	switch n := node.(type) {
	case ComparisonNode:
		return compileComparison(n)
	case AndNode:
		return compileLogical(n.Children, "AND")
	case OrNode:
		return compileLogical(n.Children, "OR")
	default:
		return "", nil, fmt.Errorf("unknown node type: %T", node)
	}
}

func compileComparison(n ComparisonNode) (string, []any, error) {
	// Tag field: JSON subquery.
	if n.Field == "tag" {
		return compileTag(n)
	}

	// Mention field: edge JOIN query.
	if n.Field == "mention" {
		return compileMention(n)
	}

	// Related field: objects sharing mention targets.
	if n.Field == "related" {
		return compileRelated(n)
	}

	// Similar field: FTS5 MATCH.
	if n.Field == "similar" {
		return compileSimilar(n)
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

func compileTag(n ComparisonNode) (string, []any, error) {
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
			"EXISTS (SELECT 1 FROM json_each(tags) WHERE json_each.value->>'label' IN (%s))",
			strings.Join(placeholders, ","),
		), args, nil
	}

	if n.Operator == OpEq {
		return "EXISTS (SELECT 1 FROM json_each(tags) WHERE json_each.value->>'label' = ?)",
			[]any{n.Value}, nil
	}

	if n.Operator == OpNeq {
		return "NOT EXISTS (SELECT 1 FROM json_each(tags) WHERE json_each.value->>'label' = ?)",
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

func compileSimilar(n ComparisonNode) (string, []any, error) {
	if n.Operator != OpEq {
		return "", nil, fmt.Errorf("similar field only supports == operator")
	}
	return "id IN (SELECT id FROM objects_fts WHERE objects_fts MATCH ?)",
		[]any{n.Value}, nil
}

func compileLogical(children []Node, op string) (string, []any, error) {
	parts := make([]string, len(children))
	var allArgs []any

	for i, child := range children {
		sql, args, err := compileNode(child)
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
