package search

import (
	"strings"
	"testing"
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
