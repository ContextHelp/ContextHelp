package search

import (
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustParse(t *testing.T, input string) Node {
	t.Helper()
	node, err := Parse(input)
	if err != nil {
		t.Fatalf("parse %q: %v", input, err)
	}
	return node
}

func TestCompileEq(t *testing.T) {
	sql, args, err := Compile(mustParse(t, "type==article"))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if sql != "type = ?" {
		t.Errorf("sql: got %q", sql)
	}
	if len(args) != 1 || args[0] != "article" {
		t.Errorf("args: got %v", args)
	}
}

func TestCompileNeq(t *testing.T) {
	sql, _, err := Compile(mustParse(t, "type!=draft"))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if sql != "type != ?" {
		t.Errorf("sql: got %q", sql)
	}
}

func TestCompileGte(t *testing.T) {
	sql, _, err := Compile(mustParse(t, "created_at>=2025-01-01"))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if sql != "created_at >= ?" {
		t.Errorf("sql: got %q", sql)
	}
}

func TestCompileIn(t *testing.T) {
	sql, args, err := Compile(mustParse(t, "tag=in=(a,b)"))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(sql, "json_each") {
		t.Errorf("sql should contain json_each: got %q", sql)
	}
	if !strings.Contains(sql, "IN") {
		t.Errorf("sql should contain IN: got %q", sql)
	}
	if len(args) != 2 {
		t.Errorf("args: got %v", args)
	}
}

func TestCompileAnd(t *testing.T) {
	sql, _, err := Compile(mustParse(t, "type==article;tag==ui"))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(sql, "AND") {
		t.Errorf("sql should contain AND: got %q", sql)
	}
}

func TestCompileOr(t *testing.T) {
	sql, args, err := Compile(mustParse(t, "type==article,type==note"))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(sql, "OR") {
		t.Errorf("sql should contain OR: got %q", sql)
	}
	if len(args) != 2 {
		t.Errorf("args: got %v", args)
	}
}

func TestCompileMention(t *testing.T) {
	sql, args, err := Compile(mustParse(t, "mention==@ui.best-practice"))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(sql, "edges") {
		t.Errorf("sql should reference edges: got %q", sql)
	}
	if len(args) != 1 || args[0] != "@ui.best-practice" {
		t.Errorf("args: got %v", args)
	}
}

func TestCompileNested(t *testing.T) {
	sql, _, err := Compile(mustParse(t, "(type==article,type==note);source==cli"))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(sql, "AND") {
		t.Errorf("sql should contain AND: got %q", sql)
	}
	if !strings.Contains(sql, "OR") {
		t.Errorf("sql should contain OR: got %q", sql)
	}
}

func TestCompileUnknownField(t *testing.T) {
	_, _, err := Compile(mustParse(t, "bogus==value"))
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("error: got %q", err)
	}
}

// --- New coverage-gap tests below ---

func TestCompileSimilarEq(t *testing.T) {
	sql, args, err := Compile(mustParse(t, "similar==keyword"))
	require.NoError(t, err)
	assert.Contains(t, sql, "objects_fts")
	assert.Contains(t, sql, "MATCH")
	assert.Equal(t, []any{"keyword"}, args)
}

func TestCompileSimilarNonEqError(t *testing.T) {
	node := ComparisonNode{Field: "similar", Operator: OpNeq, Value: "keyword"}
	_, _, err := Compile(node)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "similar field only supports == operator")
}

func TestSqlOperatorMapping(t *testing.T) {
	tests := []struct {
		op      Operator
		wantSQL string
	}{
		{OpEq, "="},
		{OpNeq, "!="},
		{OpGt, ">"},
		{OpGte, ">="},
		{OpLt, "<"},
		{OpLte, "<="},
		{OpIn, "IN"},
		{OpOut, "NOT IN"},
	}
	for _, tt := range tests {
		t.Run(tt.op.String(), func(t *testing.T) {
			got, err := sqlOperator(tt.op)
			require.NoError(t, err)
			assert.Equal(t, tt.wantSQL, got)
		})
	}
}

func TestSqlOperatorUnknown(t *testing.T) {
	_, err := sqlOperator(Operator(99))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown operator")
}

func TestCompileDirectIn(t *testing.T) {
	sql, args, err := Compile(mustParse(t, "type=in=(article,note)"))
	require.NoError(t, err)
	assert.Equal(t, "type IN (?,?)", sql)
	assert.Equal(t, []any{"article", "note"}, args)
}

func TestCompileDirectOut(t *testing.T) {
	sql, args, err := Compile(mustParse(t, "type=out=(article)"))
	require.NoError(t, err)
	assert.Equal(t, "type NOT IN (?)", sql)
	assert.Equal(t, []any{"article"}, args)
}

func TestCompileDirectInNonSliceError(t *testing.T) {
	// Manually construct a ComparisonNode with OpIn but a string value instead
	// of []string to exercise the error path.
	node := ComparisonNode{Field: "type", Operator: OpIn, Value: "not-a-slice"}
	_, _, err := Compile(node)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IN/OUT requires []string value")
}

func TestCompileTagEq(t *testing.T) {
	sql, args, err := Compile(mustParse(t, "tag==performance"))
	require.NoError(t, err)
	assert.Contains(t, sql, "EXISTS")
	assert.Contains(t, sql, "json_each")
	assert.Contains(t, sql, "label")
	assert.Equal(t, []any{"performance"}, args)
}

func TestCompileTagNeq(t *testing.T) {
	sql, args, err := Compile(mustParse(t, "tag!=performance"))
	require.NoError(t, err)
	assert.Contains(t, sql, "NOT EXISTS")
	assert.Contains(t, sql, "json_each")
	assert.Equal(t, []any{"performance"}, args)
}

func TestCompileTagIn(t *testing.T) {
	sql, args, err := Compile(mustParse(t, "tag=in=(a,b)"))
	require.NoError(t, err)
	assert.Contains(t, sql, "EXISTS")
	assert.Contains(t, sql, "json_each")
	assert.Contains(t, sql, "IN (?,?)")
	assert.Equal(t, []any{"a", "b"}, args)
}

func TestCompileTagUnsupportedOperator(t *testing.T) {
	node := ComparisonNode{Field: "tag", Operator: OpGt, Value: "perf"}
	_, _, err := Compile(node)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported operator")
}

func TestCompileMentionEq(t *testing.T) {
	sql, args, err := Compile(mustParse(t, "mention==@entity.slug"))
	require.NoError(t, err)
	assert.Contains(t, sql, "edges")
	assert.Contains(t, sql, "mentions")
	assert.Contains(t, sql, "to_id = ?")
	assert.Equal(t, []any{"@entity.slug"}, args)
}

func TestCompileMentionNonEqError(t *testing.T) {
	node := ComparisonNode{Field: "mention", Operator: OpNeq, Value: "@entity.slug"}
	_, _, err := Compile(node)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mention field only supports == operator")
}

func TestCompileRelatedEq(t *testing.T) {
	sql, args, err := Compile(mustParse(t, "related==@arch.decision"))
	require.NoError(t, err)
	assert.Contains(t, sql, "edges")
	assert.Contains(t, sql, "to_id")
	assert.Equal(t, []any{"@arch.decision"}, args)
}

func TestCompileRelatedNonEqError(t *testing.T) {
	node := ComparisonNode{Field: "related", Operator: OpNeq, Value: "@arch.decision"}
	_, _, err := Compile(node)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "related field only supports == operator")
}

func TestCompileUnknownNodeType(t *testing.T) {
	// Passing nil exercises the default branch in compileNode.
	_, _, err := Compile(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown node type")
}

// --- Postgres dialect tests ---

func TestCompileForPostgresEq(t *testing.T) {
	sql, args, err := CompileFor(DialectPostgres, mustParse(t, "type==article"))
	require.NoError(t, err)
	assert.Equal(t, "type = $1", sql)
	assert.Equal(t, []any{"article"}, args)
}

func TestCompileForPostgresIn(t *testing.T) {
	sql, args, err := CompileFor(DialectPostgres, mustParse(t, "type=in=(article,note)"))
	require.NoError(t, err)
	assert.Equal(t, "type IN ($1,$2)", sql)
	assert.Equal(t, []any{"article", "note"}, args)
}

func TestCompileForPostgresOut(t *testing.T) {
	sql, args, err := CompileFor(DialectPostgres, mustParse(t, "type=out=(draft)"))
	require.NoError(t, err)
	assert.Equal(t, "type NOT IN ($1)", sql)
	assert.Equal(t, []any{"draft"}, args)
}

func TestCompileForPostgresTagEq(t *testing.T) {
	sql, args, err := CompileFor(DialectPostgres, mustParse(t, "tag==performance"))
	require.NoError(t, err)
	assert.Contains(t, sql, "jsonb_array_elements")
	assert.NotContains(t, sql, "json_each")
	assert.Contains(t, sql, "$1")
	assert.Equal(t, []any{"performance"}, args)
}

func TestCompileForPostgresTagNeq(t *testing.T) {
	sql, args, err := CompileFor(DialectPostgres, mustParse(t, "tag!=performance"))
	require.NoError(t, err)
	assert.Contains(t, sql, "NOT EXISTS")
	assert.Contains(t, sql, "jsonb_array_elements")
	assert.NotContains(t, sql, "json_each")
	assert.Equal(t, []any{"performance"}, args)
}

func TestCompileForPostgresTagIn(t *testing.T) {
	sql, args, err := CompileFor(DialectPostgres, mustParse(t, "tag=in=(a,b)"))
	require.NoError(t, err)
	assert.Contains(t, sql, "jsonb_array_elements")
	assert.Contains(t, sql, "IN ($1,$2)")
	assert.Equal(t, []any{"a", "b"}, args)
}

func TestCompileForPostgresMention(t *testing.T) {
	sql, args, err := CompileFor(DialectPostgres, mustParse(t, "mention==@ns.slug"))
	require.NoError(t, err)
	assert.Contains(t, sql, "edges")
	assert.Contains(t, sql, "$1")
	assert.NotContains(t, sql, "?")
	assert.Equal(t, []any{"@ns.slug"}, args)
}

func TestCompileForPostgresRelated(t *testing.T) {
	sql, args, err := CompileFor(DialectPostgres, mustParse(t, "related==@ns.slug"))
	require.NoError(t, err)
	assert.Contains(t, sql, "edges")
	assert.Contains(t, sql, "$1")
	assert.NotContains(t, sql, "?")
	assert.Equal(t, []any{"@ns.slug"}, args)
}

func TestCompileForPostgresSimilar(t *testing.T) {
	sql, args, err := CompileFor(DialectPostgres, mustParse(t, "similar==keyword"))
	require.NoError(t, err)
	assert.Contains(t, sql, "fts @@ websearch_to_tsquery($1, $2)")
	assert.NotContains(t, sql, "?")
	assert.Equal(t, []any{PostgresFTSRegconfig, "keyword"}, args)
}

func TestCompileForPostgresAnd(t *testing.T) {
	sql, args, err := CompileFor(DialectPostgres, mustParse(t, "type==article;tag==ui"))
	require.NoError(t, err)
	assert.Contains(t, sql, "AND")
	assert.Contains(t, sql, "$1")
	assert.Contains(t, sql, "$2")
	assert.NotContains(t, sql, "?")
	assert.Equal(t, []any{"article", "ui"}, args)
}

func TestRebindPostgres(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"type = ?", "type = $1"},
		{"a = ? AND b = ?", "a = $1 AND b = $2"},
		{"no placeholders", "no placeholders"},
		{"? AND ? AND ?", "$1 AND $2 AND $3"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := rebindPostgres(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDialectFor(t *testing.T) {
	// SQLite driver (no SQLDialect method) defaults to DialectSQLite.
	sqlite := storageutil.NewTestDriver(t)
	assert.Equal(t, DialectSQLite, DialectFor(sqlite))

	// A wrapper that adds SQLDialect() → "postgres" triggers DialectPostgres.
	pg := &postgresDialectWrapper{StorageDriver: sqlite}
	assert.Equal(t, DialectPostgres, DialectFor(pg))

	// A wrapper returning an unrecognised dialect string defaults to SQLite.
	unknown := &unknownDialectWrapper{StorageDriver: sqlite}
	assert.Equal(t, DialectSQLite, DialectFor(unknown))
}

// postgresDialectWrapper wraps any StorageDriver and claims the postgres dialect.
type postgresDialectWrapper struct {
	storage.StorageDriver
}

func (w *postgresDialectWrapper) SQLDialect() string { return "postgres" }

// unknownDialectWrapper wraps any StorageDriver and claims an unrecognised dialect.
type unknownDialectWrapper struct {
	storage.StorageDriver
}

func (w *unknownDialectWrapper) SQLDialect() string { return "mysql" }
