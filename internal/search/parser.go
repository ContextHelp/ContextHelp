package search

import "fmt"

// Parse parses an RSQL query string into an AST.
func Parse(input string) (Node, error) {
	tokens, err := Lex(input)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}
	node, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().Type != TokenEOF {
		return nil, fmt.Errorf("unexpected token %q at position %d", p.peek().Value, p.peek().Pos)
	}
	return node, nil
}

type parser struct {
	tokens []Token
	pos    int
}

func (p *parser) peek() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *parser) next() Token {
	t := p.peek()
	p.pos++
	return t
}

// parseOr: and_expr ("," and_expr)*
func (p *parser) parseOr() (Node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}

	if p.peek().Type == TokenOr {
		children := []Node{left}
		for p.peek().Type == TokenOr {
			p.next() // consume ","
			right, err := p.parseAnd()
			if err != nil {
				return nil, err
			}
			children = append(children, right)
		}
		return OrNode{Children: children}, nil
	}

	return left, nil
}

// parseAnd: constraint (";" constraint)*
func (p *parser) parseAnd() (Node, error) {
	left, err := p.parseConstraint()
	if err != nil {
		return nil, err
	}

	if p.peek().Type == TokenAnd {
		children := []Node{left}
		for p.peek().Type == TokenAnd {
			p.next() // consume ";"
			right, err := p.parseConstraint()
			if err != nil {
				return nil, err
			}
			children = append(children, right)
		}
		return AndNode{Children: children}, nil
	}

	return left, nil
}

// parseConstraint: group | comparison
func (p *parser) parseConstraint() (Node, error) {
	if p.peek().Type == TokenLParen {
		return p.parseGroup()
	}
	return p.parseComparison()
}

// parseGroup: "(" query ")"
func (p *parser) parseGroup() (Node, error) {
	p.next() // consume "("
	node, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().Type != TokenRParen {
		return nil, fmt.Errorf("expected ')' at position %d", p.peek().Pos)
	}
	p.next() // consume ")"
	return node, nil
}

// parseComparison: field operator value
func (p *parser) parseComparison() (Node, error) {
	if p.peek().Type != TokenField {
		return nil, fmt.Errorf("expected field at position %d, got %q", p.peek().Pos, p.peek().Value)
	}
	field := p.next()

	if p.peek().Type != TokenOp {
		return nil, fmt.Errorf("expected operator at position %d", p.peek().Pos)
	}
	op := p.next()

	operator, err := parseOperator(op.Value)
	if err != nil {
		return nil, err
	}

	// For IN/OUT, value is a list.
	if operator == OpIn || operator == OpOut {
		if p.peek().Type != TokenValues {
			return nil, fmt.Errorf("expected value list at position %d", p.peek().Pos)
		}
		values := p.next()
		return ComparisonNode{
			Field:    field.Value,
			Operator: operator,
			Value:    values.Values,
		}, nil
	}

	if p.peek().Type != TokenValue {
		return nil, fmt.Errorf("expected value at position %d", p.peek().Pos)
	}
	value := p.next()
	return ComparisonNode{
		Field:    field.Value,
		Operator: operator,
		Value:    value.Value,
	}, nil
}

func parseOperator(s string) (Operator, error) {
	switch s {
	case "==":
		return OpEq, nil
	case "!=":
		return OpNeq, nil
	case ">":
		return OpGt, nil
	case ">=":
		return OpGte, nil
	case "<":
		return OpLt, nil
	case "<=":
		return OpLte, nil
	case "=in=":
		return OpIn, nil
	case "=out=":
		return OpOut, nil
	default:
		return 0, fmt.Errorf("unknown operator: %s", s)
	}
}
