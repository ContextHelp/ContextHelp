package search

// Node is an element in an RSQL query AST.
type Node interface{ node() }

// ComparisonNode represents a field-operator-value comparison.
type ComparisonNode struct {
	Field    string
	Operator Operator
	Value    any // string or []string for IN/OUT
}

func (ComparisonNode) node() {}

// AndNode joins children with logical AND (RSQL ";").
type AndNode struct {
	Children []Node
}

func (AndNode) node() {}

// OrNode joins children with logical OR (RSQL ",").
type OrNode struct {
	Children []Node
}

func (OrNode) node() {}

// Operator represents an RSQL comparison operator.
type Operator int

const (
	OpEq  Operator = iota // ==
	OpNeq                 // !=
	OpGt                  // >
	OpGte                 // >=
	OpLt                  // <
	OpLte                 // <=
	OpIn                  // =in=
	OpOut                 // =out=
)

var operatorStrings = [...]string{
	OpEq:  "==",
	OpNeq: "!=",
	OpGt:  ">",
	OpGte: ">=",
	OpLt:  "<",
	OpLte: "<=",
	OpIn:  "=in=",
	OpOut: "=out=",
}

func (o Operator) String() string {
	if int(o) < len(operatorStrings) {
		return operatorStrings[o]
	}
	return "unknown"
}
