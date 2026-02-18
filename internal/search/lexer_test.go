package search

import "testing"

func TestLexSimple(t *testing.T) {
	tokens, err := Lex("type==article")
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	// Expect: FIELD("type"), OP("=="), VALUE("article"), EOF
	expect := []struct {
		typ TokenType
		val string
	}{
		{TokenField, "type"},
		{TokenOp, "=="},
		{TokenValue, "article"},
		{TokenEOF, ""},
	}
	for i, e := range expect {
		if i >= len(tokens) {
			t.Fatalf("token %d: missing", i)
		}
		if tokens[i].Type != e.typ {
			t.Errorf("token %d type: got %d, want %d", i, tokens[i].Type, e.typ)
		}
		if e.val != "" && tokens[i].Value != e.val {
			t.Errorf("token %d value: got %q, want %q", i, tokens[i].Value, e.val)
		}
	}
}

func TestLexAnd(t *testing.T) {
	tokens, err := Lex("a==1;b==2")
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	// Find AND token.
	found := false
	for _, tok := range tokens {
		if tok.Type == TokenAnd {
			found = true
		}
	}
	if !found {
		t.Error("expected AND token")
	}
}

func TestLexOr(t *testing.T) {
	tokens, err := Lex("a==1,b==2")
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	found := false
	for _, tok := range tokens {
		if tok.Type == TokenOr {
			found = true
		}
	}
	if !found {
		t.Error("expected OR token")
	}
}

func TestLexIn(t *testing.T) {
	tokens, err := Lex("tag=in=(a,b,c)")
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	// Should have FIELD, OP(=in=), VALUES([a,b,c]), EOF
	var foundOp, foundValues bool
	for _, tok := range tokens {
		if tok.Type == TokenOp && tok.Value == "=in=" {
			foundOp = true
		}
		if tok.Type == TokenValues {
			foundValues = true
			if len(tok.Values) != 3 {
				t.Errorf("values count: got %d, want 3", len(tok.Values))
			}
		}
	}
	if !foundOp {
		t.Error("expected =in= operator")
	}
	if !foundValues {
		t.Error("expected values token")
	}
}

func TestLexGrouping(t *testing.T) {
	tokens, err := Lex("(a==1)")
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	if tokens[0].Type != TokenLParen {
		t.Errorf("first token: got %d, want LPAREN", tokens[0].Type)
	}
	// Find RPAREN.
	found := false
	for _, tok := range tokens {
		if tok.Type == TokenRParen {
			found = true
		}
	}
	if !found {
		t.Error("expected RPAREN token")
	}
}

func TestLexQuoted(t *testing.T) {
	tokens, err := Lex(`name=="hello world"`)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	var foundValue bool
	for _, tok := range tokens {
		if tok.Type == TokenValue && tok.Value == "hello world" {
			foundValue = true
		}
	}
	if !foundValue {
		t.Error("expected quoted value 'hello world'")
	}
}

func TestLexError(t *testing.T) {
	_, err := Lex("==")
	if err == nil {
		t.Error("expected error for standalone ==")
	}
}
