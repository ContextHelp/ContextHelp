package search

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSimpleComparison(t *testing.T) {
	node, err := Parse("type==article")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cmp, ok := node.(ComparisonNode)
	if !ok {
		t.Fatalf("expected ComparisonNode, got %T", node)
	}
	if cmp.Field != "type" {
		t.Errorf("field: got %q", cmp.Field)
	}
	if cmp.Operator != OpEq {
		t.Errorf("op: got %v", cmp.Operator)
	}
	if cmp.Value.(string) != "article" {
		t.Errorf("value: got %v", cmp.Value)
	}
}

func TestParseAnd(t *testing.T) {
	node, err := Parse("a==1;b==2")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	and, ok := node.(AndNode)
	if !ok {
		t.Fatalf("expected AndNode, got %T", node)
	}
	if len(and.Children) != 2 {
		t.Errorf("children: got %d", len(and.Children))
	}
}

func TestParseOr(t *testing.T) {
	node, err := Parse("a==1,b==2")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	or, ok := node.(OrNode)
	if !ok {
		t.Fatalf("expected OrNode, got %T", node)
	}
	if len(or.Children) != 2 {
		t.Errorf("children: got %d", len(or.Children))
	}
}

func TestParsePrecedence(t *testing.T) {
	// "," binds weaker than ";"
	// a==1,b==2;c==3 should be OR(a==1, AND(b==2, c==3))
	node, err := Parse("a==1,b==2;c==3")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	or, ok := node.(OrNode)
	if !ok {
		t.Fatalf("expected OrNode, got %T", node)
	}
	if len(or.Children) != 2 {
		t.Fatalf("or children: got %d", len(or.Children))
	}
	// First child: comparison a==1
	if _, ok := or.Children[0].(ComparisonNode); !ok {
		t.Errorf("first child: expected ComparisonNode, got %T", or.Children[0])
	}
	// Second child: AND(b==2, c==3)
	if _, ok := or.Children[1].(AndNode); !ok {
		t.Errorf("second child: expected AndNode, got %T", or.Children[1])
	}
}

func TestParseGrouping(t *testing.T) {
	// (a==1,b==2);c==3 should be AND(OR(a==1, b==2), c==3)
	node, err := Parse("(a==1,b==2);c==3")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	and, ok := node.(AndNode)
	if !ok {
		t.Fatalf("expected AndNode, got %T", node)
	}
	if len(and.Children) != 2 {
		t.Fatalf("and children: got %d", len(and.Children))
	}
	if _, ok := and.Children[0].(OrNode); !ok {
		t.Errorf("first child: expected OrNode, got %T", and.Children[0])
	}
}

func TestParseIn(t *testing.T) {
	node, err := Parse("tag=in=(a,b)")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cmp, ok := node.(ComparisonNode)
	if !ok {
		t.Fatalf("expected ComparisonNode, got %T", node)
	}
	if cmp.Operator != OpIn {
		t.Errorf("op: got %v", cmp.Operator)
	}
	vals, ok := cmp.Value.([]string)
	if !ok {
		t.Fatalf("value type: expected []string, got %T", cmp.Value)
	}
	if len(vals) != 2 {
		t.Errorf("values count: got %d", len(vals))
	}
}

func TestParseOut(t *testing.T) {
	node, err := Parse("tag=out=(a,b)")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cmp := node.(ComparisonNode)
	if cmp.Operator != OpOut {
		t.Errorf("op: got %v", cmp.Operator)
	}
}

func TestParseAllOperators(t *testing.T) {
	tests := []struct {
		input string
		op    Operator
	}{
		{"x==1", OpEq},
		{"x!=1", OpNeq},
		{"x>1", OpGt},
		{"x>=1", OpGte},
		{"x<1", OpLt},
		{"x<=1", OpLte},
		{"x=in=(1)", OpIn},
		{"x=out=(1)", OpOut},
	}
	for _, tt := range tests {
		node, err := Parse(tt.input)
		if err != nil {
			t.Errorf("%s: parse error: %v", tt.input, err)
			continue
		}
		cmp := node.(ComparisonNode)
		if cmp.Operator != tt.op {
			t.Errorf("%s: op got %v, want %v", tt.input, cmp.Operator, tt.op)
		}
	}
}

func TestParseError(t *testing.T) {
	_, err := Parse("==bad")
	if err == nil {
		t.Error("expected parse error")
	}
}

// --- New error-path tests below ---

func TestParseEmptyInput(t *testing.T) {
	_, err := Parse("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected field")
}

func TestParseUnterminatedQuotedString(t *testing.T) {
	_, err := Parse(`type=="hello`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unterminated string")
}

func TestParseMissingValueAfterOperator(t *testing.T) {
	_, err := Parse("type==")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected value")
}

func TestParseUnbalancedParentheses(t *testing.T) {
	_, err := Parse("(type==article")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected ')'")
}
