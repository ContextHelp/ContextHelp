package search

import (
	"strings"
	"testing"

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

func TestCompileUnknownNodeType(t *testing.T) {
	// Passing nil exercises the default branch in compileNode.
	_, _, err := Compile(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown node type")
}
