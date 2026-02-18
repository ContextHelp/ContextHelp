package search

import (
	"fmt"
	"strings"
	"unicode"
)

// TokenType represents the type of a lexer token.
type TokenType int

const (
	TokenField  TokenType = iota // field name
	TokenOp                      // operator (==, !=, etc.)
	TokenValue                   // single value
	TokenValues                  // value list for IN/OUT
	TokenAnd                     // ;
	TokenOr                      // ,
	TokenLParen                  // (
	TokenRParen                  // )
	TokenEOF
)

// Token is a single lexer token.
type Token struct {
	Type   TokenType
	Value  string
	Values []string // populated for TokenValues
	Pos    int
}

// Lexer tokenizes an RSQL query string.
type Lexer struct {
	input  string
	pos    int
	tokens []Token
}

// Lex tokenizes the input string into a token slice.
func Lex(input string) ([]Token, error) {
	l := &Lexer{input: input}
	if err := l.lex(); err != nil {
		return nil, err
	}
	return l.tokens, nil
}

func (l *Lexer) lex() error {
	for l.pos < len(l.input) {
		l.skipWhitespace()
		if l.pos >= len(l.input) {
			break
		}

		ch := l.input[l.pos]

		switch {
		case ch == ';':
			l.tokens = append(l.tokens, Token{Type: TokenAnd, Value: ";", Pos: l.pos})
			l.pos++
		case ch == ',':
			// Distinguish between OR separator and value list separator.
			// If the last token was an operator (=in= or =out=), this starts a value list.
			// Otherwise it's OR.
			l.tokens = append(l.tokens, Token{Type: TokenOr, Value: ",", Pos: l.pos})
			l.pos++
		case ch == '(':
			// Check if this is a value list after =in= or =out=.
			if l.lastTokenIsSetOp() {
				return l.lexValueList()
			}
			l.tokens = append(l.tokens, Token{Type: TokenLParen, Value: "(", Pos: l.pos})
			l.pos++
		case ch == ')':
			l.tokens = append(l.tokens, Token{Type: TokenRParen, Value: ")", Pos: l.pos})
			l.pos++
		case ch == '=' || ch == '!' || ch == '>' || ch == '<':
			return l.lexOperatorAndValue()
		case ch == '"' || ch == '\'':
			return l.lexQuotedValue()
		default:
			if isIdentStart(ch) {
				l.lexField()
			} else {
				return fmt.Errorf("unexpected character %q at position %d", string(ch), l.pos)
			}
		}
	}

	l.tokens = append(l.tokens, Token{Type: TokenEOF, Pos: l.pos})
	return nil
}

func (l *Lexer) lastTokenIsSetOp() bool {
	if len(l.tokens) == 0 {
		return false
	}
	last := l.tokens[len(l.tokens)-1]
	return last.Type == TokenOp && (last.Value == "=in=" || last.Value == "=out=")
}

func (l *Lexer) lexField() {
	start := l.pos
	for l.pos < len(l.input) && isIdentChar(l.input[l.pos]) {
		l.pos++
	}
	l.tokens = append(l.tokens, Token{Type: TokenField, Value: l.input[start:l.pos], Pos: start})
}

func (l *Lexer) lexOperatorAndValue() error {
	start := l.pos

	// Read operator.
	op, err := l.readOperator()
	if err != nil {
		return err
	}
	l.tokens = append(l.tokens, Token{Type: TokenOp, Value: op, Pos: start})

	// For =in= and =out=, value list comes in parens — handled by main loop.
	if op == "=in=" || op == "=out=" {
		return l.lex()
	}

	// Read value.
	l.skipWhitespace()
	if l.pos >= len(l.input) {
		return fmt.Errorf("expected value at position %d", l.pos)
	}

	if l.input[l.pos] == '"' || l.input[l.pos] == '\'' {
		return l.lexQuotedValue()
	}

	vStart := l.pos
	for l.pos < len(l.input) && !isSeparator(l.input[l.pos]) {
		l.pos++
	}
	if l.pos == vStart {
		return fmt.Errorf("expected value at position %d", l.pos)
	}
	l.tokens = append(l.tokens, Token{Type: TokenValue, Value: l.input[vStart:l.pos], Pos: vStart})
	return l.lex()
}

func (l *Lexer) readOperator() (string, error) {
	if l.pos >= len(l.input) {
		return "", fmt.Errorf("unexpected end of input at position %d", l.pos)
	}

	// Two-char operators: ==, !=, >=, <=
	if l.pos+1 < len(l.input) {
		two := l.input[l.pos : l.pos+2]
		switch two {
		case "==", "!=", ">=", "<=":
			l.pos += 2
			return two, nil
		}
	}

	// Single-char: >, <
	ch := l.input[l.pos]
	if ch == '>' || ch == '<' {
		l.pos++
		return string(ch), nil
	}

	// Named operators: =in=, =out=
	if ch == '=' {
		for _, named := range []string{"=in=", "=out="} {
			if strings.HasPrefix(l.input[l.pos:], named) {
				l.pos += len(named)
				return named, nil
			}
		}
		return "", fmt.Errorf("unexpected '=' at position %d", l.pos)
	}

	if ch == '!' {
		return "", fmt.Errorf("expected '!=' at position %d", l.pos)
	}

	return "", fmt.Errorf("unexpected operator character %q at position %d", string(ch), l.pos)
}

func (l *Lexer) lexQuotedValue() error {
	quote := l.input[l.pos]
	l.pos++ // skip opening quote
	start := l.pos
	for l.pos < len(l.input) && l.input[l.pos] != quote {
		l.pos++
	}
	if l.pos >= len(l.input) {
		return fmt.Errorf("unterminated string at position %d", start-1)
	}
	l.tokens = append(l.tokens, Token{Type: TokenValue, Value: l.input[start:l.pos], Pos: start})
	l.pos++ // skip closing quote
	return l.lex()
}

func (l *Lexer) lexValueList() error {
	l.pos++ // skip '('
	var values []string
	start := l.pos

	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == ')' {
			val := strings.TrimSpace(l.input[start:l.pos])
			if val != "" {
				values = append(values, val)
			}
			l.pos++ // skip ')'

			// Replace the last OR token if we accidentally emitted one.
			// Actually, the value list is consumed here directly.
			l.tokens = append(l.tokens, Token{Type: TokenValues, Values: values, Pos: start})
			return l.lex()
		}
		if ch == ',' {
			val := strings.TrimSpace(l.input[start:l.pos])
			if val != "" {
				values = append(values, val)
			}
			l.pos++
			start = l.pos
			continue
		}
		l.pos++
	}

	return fmt.Errorf("unterminated value list at position %d", start)
}

func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.input) && unicode.IsSpace(rune(l.input[l.pos])) {
		l.pos++
	}
}

func isIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_' || ch == '.' || ch == '@'
}

func isIdentChar(ch byte) bool {
	return isIdentStart(ch) || (ch >= '0' && ch <= '9') || ch == '-'
}

func isSeparator(ch byte) bool {
	return ch == ';' || ch == ',' || ch == ')' || ch == ' '
}
