package search

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/search/ftsq"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
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

// columnAliases map query fields to the object columns they compare
// against when the two names differ: a share policy filters by
// `profile` (ADR-080 §2) and the column is profile_id.
var columnAliases = map[string]string{
	"profile": "profile_id",
}

// metadataPrefix introduces a comparison against one key of the object's
// metadata: `metadata.<key>`, nested keys dotted (`metadata.a.b`). The
// key is bound as a JSON path argument, never spliced into the SQL.
const metadataPrefix = "metadata."

// metadataKeyPattern bounds a metadata key to path components the two
// JSON path syntaxes read alike.
var metadataKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$`)

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
	return CompileScoped(d, node, nil)
}

// CompileScoped compiles node for dialect d like CompileFor and, when
// scope is set, compiles the cross-object operators (mention, related)
// with the scope's predicate on both sides of their edge subqueries
// (ADR-080 §7): a restricted viewer never reaches an object through an
// edge from one it may not see. The scope's own placeholders join the
// predicate's and are numbered with them.
func CompileScoped(d Dialect, node Node, scope *storage.Scope) (string, []any, error) {
	sql, args, err := CompileUnnumbered(d, node, scope)
	if err != nil {
		return "", nil, err
	}
	if d == DialectPostgres {
		sql = rebindPostgres(sql)
	}
	return sql, args, nil
}

// CompileUnnumbered compiles node for dialect d with ? placeholders
// whatever the dialect: the form a caller splices into a larger
// statement and numbers itself (the drivers' scoped reads). scope is
// as for CompileScoped.
func CompileUnnumbered(d Dialect, node Node, scope *storage.Scope) (string, []any, error) {
	c := compiler{dialect: d, scope: scope}
	return c.node(node)
}

// compiler carries one compilation's dialect and scope.
type compiler struct {
	dialect Dialect
	scope   *storage.Scope
}

// visibleObjects is " AND col IN (SELECT id FROM objects WHERE scope)"
// with its arguments, or empty with no scope.
func (c compiler) visibleObjects(col string) (string, []any) {
	if c.scope == nil {
		return "", nil
	}
	return " AND " + col + " IN (SELECT id FROM objects WHERE " + c.scope.SQL + ")", append([]any(nil), c.scope.Args...)
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

func (c compiler) node(node Node) (string, []any, error) {
	switch n := node.(type) {
	case ComparisonNode:
		return c.comparison(n)
	case AndNode:
		return c.logical(n.Children, "AND")
	case OrNode:
		return c.logical(n.Children, "OR")
	default:
		return "", nil, fmt.Errorf("unknown node type: %T", node)
	}
}

func (c compiler) comparison(n ComparisonNode) (string, []any, error) {
	d := c.dialect
	// Tag field: JSON subquery.
	if n.Field == "tag" {
		return compileTag(d, n)
	}

	// Mention field: edge JOIN query.
	if n.Field == "mention" {
		return c.mention(n)
	}

	// Related field: objects sharing mention targets.
	if n.Field == "related" {
		return c.related(n)
	}

	// Similar field: FTS5 MATCH (SQLite only).
	if n.Field == "similar" {
		return compileSimilar(d, n)
	}

	// Class field: JSON array membership over objects.classes.
	if n.Field == "class" {
		return compileClass(d, n)
	}

	// Metadata keys: a JSON path into objects.metadata.
	if strings.HasPrefix(n.Field, metadataPrefix) {
		return compileMetadata(d, n)
	}

	// Direct columns, by their own name or an alias.
	if column, ok := columnAliases[n.Field]; ok {
		return compileColumn(column, n)
	}
	if directColumns[n.Field] || dateColumns[n.Field] {
		return compileColumn(n.Field, n)
	}

	return "", nil, fmt.Errorf("unknown field: %q", n.Field)
}

// compileColumn compares one SQL expression (a column name, or a JSON
// path expression whose own arguments come first in pathArgs) with the
// node's value under its operator.
func compileColumn(expr string, n ComparisonNode, pathArgs ...any) (string, []any, error) {
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
		args := append([]any(nil), pathArgs...)
		for i, v := range vals {
			placeholders[i] = "?"
			args = append(args, v)
		}
		not := ""
		if n.Operator == OpOut {
			not = "NOT "
		}
		return fmt.Sprintf("%s %sIN (%s)", expr, not, strings.Join(placeholders, ",")), args, nil
	}

	return fmt.Sprintf("%s %s ?", expr, sqlOp), append(append([]any(nil), pathArgs...), n.Value), nil
}

// compileClass emits a membership check over the classes JSON array:
// == and =in= hold when any class matches, != and =out= when none does
// (a policy's exclude_classes and a principal's deny_classes compile to
// the latter, ADR-080 §2). SQLite uses json_each; Postgres
// jsonb_array_elements_text.
func compileClass(d Dialect, n ComparisonNode) (string, []any, error) {
	jsonFunc, member := "json_each(classes)", "json_each.value"
	if d == DialectPostgres {
		jsonFunc, member = "jsonb_array_elements_text(classes) AS c", "c"
	}
	var vals []string
	switch n.Operator {
	case OpEq, OpNeq:
		v, ok := n.Value.(string)
		if !ok {
			return "", nil, fmt.Errorf("class ==/!= requires a string value")
		}
		vals = []string{v}
	case OpIn, OpOut:
		v, ok := n.Value.([]string)
		if !ok {
			return "", nil, fmt.Errorf("class IN/OUT requires []string value")
		}
		vals = v
	default:
		return "", nil, fmt.Errorf("unsupported operator %v for class field", n.Operator)
	}
	if len(vals) == 0 {
		return "", nil, fmt.Errorf("class %v requires at least one class", n.Operator)
	}
	placeholders := make([]string, len(vals))
	args := make([]any, len(vals))
	for i, v := range vals {
		placeholders[i] = "?"
		args[i] = v
	}
	not := ""
	if n.Operator == OpNeq || n.Operator == OpOut {
		not = "NOT "
	}
	return fmt.Sprintf("%sEXISTS (SELECT 1 FROM %s WHERE %s IN (%s))",
		not, jsonFunc, member, strings.Join(placeholders, ",")), args, nil
}

// compileMetadata compares one metadata key, read as text, with the
// node's value. The key travels as a bound JSON path: `$.a.b` for
// SQLite's json_extract, `{a,b}` for Postgres's #>> operator.
func compileMetadata(d Dialect, n ComparisonNode) (string, []any, error) {
	key := strings.TrimPrefix(n.Field, metadataPrefix)
	if !metadataKeyPattern.MatchString(key) {
		return "", nil, fmt.Errorf("metadata key %q: want dotted path components of [A-Za-z0-9_-]", key)
	}
	if d == DialectPostgres {
		return compileColumn("(metadata #>> ?::text[])", n, "{"+strings.ReplaceAll(key, ".", ",")+"}")
	}
	return compileColumn("json_extract(metadata, ?)", n, "$."+key)
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

// mention keeps the objects that mention the entity. Under a scope the
// edge's object side is kept to visible objects too.
func (c compiler) mention(n ComparisonNode) (string, []any, error) {
	if n.Operator != OpEq {
		return "", nil, fmt.Errorf("mention field only supports == operator")
	}
	vis, vargs := c.visibleObjects("from_id")
	return "id IN (SELECT from_id FROM edges WHERE from_type = 'object' AND to_type = 'entity' AND edge_type = 'mentions' AND to_id = ?" + vis + ")",
		append([]any{n.Value}, vargs...), nil
}

// related returns objects that share at least one mention target with any
// object that mentions the given entity slug. For each object e1 that mentions
// the entity, it finds all other objects e2 that share any common mention
// target (to_id) with e1. The entity slug itself is excluded from the join
// condition so e2 ranges over ALL shared targets, not just the seed entity.
// Under a scope both e1 and e2 are kept to visible objects: a hidden
// object never bridges two visible ones.
func (c compiler) related(n ComparisonNode) (string, []any, error) {
	if n.Operator != OpEq {
		return "", nil, fmt.Errorf("related field only supports == operator")
	}
	e1, args1 := c.visibleObjects("e1.from_id")
	e2, args2 := c.visibleObjects("e2.from_id")
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
		  AND e2.edge_type = 'mentions'` + e1 + e2 + `
	)`
	return sql, append(append([]any{n.Value}, args1...), args2...), nil
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

func (c compiler) logical(children []Node, op string) (string, []any, error) {
	parts := make([]string, len(children))
	var allArgs []any

	for i, child := range children {
		sql, args, err := c.node(child)
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
