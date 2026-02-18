package search

import "testing"

func TestComparisonNode(t *testing.T) {
	n := ComparisonNode{Field: "type", Operator: OpEq, Value: "article"}
	if n.Field != "type" {
		t.Errorf("Field: got %q", n.Field)
	}
	if n.Operator != OpEq {
		t.Errorf("Operator: got %v", n.Operator)
	}
	if n.Value.(string) != "article" {
		t.Errorf("Value: got %v", n.Value)
	}
}

func TestAndNode(t *testing.T) {
	n := AndNode{Children: []Node{
		ComparisonNode{Field: "a", Operator: OpEq, Value: "1"},
		ComparisonNode{Field: "b", Operator: OpEq, Value: "2"},
	}}
	if len(n.Children) != 2 {
		t.Errorf("Children: got %d", len(n.Children))
	}
}

func TestOrNode(t *testing.T) {
	n := OrNode{Children: []Node{
		ComparisonNode{Field: "a", Operator: OpEq, Value: "1"},
		ComparisonNode{Field: "b", Operator: OpEq, Value: "2"},
	}}
	if len(n.Children) != 2 {
		t.Errorf("Children: got %d", len(n.Children))
	}
}

func TestOperatorString(t *testing.T) {
	tests := []struct {
		op   Operator
		want string
	}{
		{OpEq, "=="},
		{OpNeq, "!="},
		{OpGt, ">"},
		{OpGte, ">="},
		{OpLt, "<"},
		{OpLte, "<="},
		{OpIn, "=in="},
		{OpOut, "=out="},
	}
	for _, tt := range tests {
		if got := tt.op.String(); got != tt.want {
			t.Errorf("%d.String(): got %q, want %q", tt.op, got, tt.want)
		}
	}
}
