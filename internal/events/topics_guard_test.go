package events_test

// Repo-wide guard for kit's topic convention. kit is the authority:
// a published topic is source.category.object.action, validated by
// bus.ValidateTopic (4 segments, [a-z0-9_], past-tense action), and
// bus.ParseTopic reads the first underscore in the object segment as an
// object_modifier split. The guard reads topic constants from source so
// a constant added in any file of the scanned packages is checked
// without registering it anywhere.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"hop.top/kit/go/runtime/bus"
)

// topicConst is one topic constant read from source.
type topicConst struct {
	pos   string // file:line
	name  string
	value string
}

// intendedModifiers lists topic constants whose object segment carries
// a deliberate kit object_modifier. Anything else with an underscore in
// the object segment is a mistake: kit would split it.
var intendedModifiers = map[string]string{}

// repoRoot returns the module root (two levels above internal/events).
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root %s has no go.mod: %v", root, err)
	}
	return root
}

// parseDir parses the non-test Go files of one directory.
func parseDir(t *testing.T, fset *token.FileSet, dir string) []*ast.File {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, f)
	}
	return files
}

// isBusTopicType reports whether expr is the selector bus.Topic.
func isBusTopicType(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "bus" && sel.Sel.Name == "Topic"
}

// isSubscriptionPattern reports whether s is an inbound wildcard
// pattern rather than a published topic.
func isSubscriptionPattern(s string) bool {
	return strings.ContainsAny(s, "*#")
}

// collectTopicConsts returns every topic constant declared in dir: each
// const or var typed bus.Topic, plus each untyped string const named
// Topic* (plugin modules cannot import kit's bus). Wildcard subscription
// patterns are skipped. A topic constant whose value is not a plain
// string literal is reported: the guard must be able to read it.
func collectTopicConsts(t *testing.T, dir string) []topicConst {
	t.Helper()
	fset := token.NewFileSet()
	var out []topicConst
	for _, f := range parseDir(t, fset, dir) {
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				typed := vs.Type != nil && isBusTopicType(vs.Type)
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					pos := fset.Position(name.Pos())
					at := filepath.Base(pos.Filename) + ":" + strconv.Itoa(pos.Line)
					lit, isLit := vs.Values[i].(*ast.BasicLit)
					named := vs.Type == nil && strings.HasPrefix(name.Name, "Topic")
					if !typed && !named {
						continue
					}
					if !isLit || lit.Kind != token.STRING {
						if typed {
							t.Errorf("%s %s: topic constant must be a plain string literal", at, name.Name)
						}
						continue
					}
					val, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("%s %s: unquote: %v", at, name.Name, err)
					}
					if isSubscriptionPattern(val) {
						continue
					}
					out = append(out, topicConst{pos: at, name: name.Name, value: val})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// assertKitTopic checks one constant against kit's grammar: ValidateTopic,
// a TopicOf/ParseTopic round-trip, and no unintended object modifier.
func assertKitTopic(t *testing.T, c topicConst) {
	t.Helper()
	topic := bus.Topic(c.value)
	if err := bus.ValidateTopic(topic); err != nil {
		t.Errorf("%s %s = %q: %v", c.pos, c.name, c.value, err)
		return
	}
	parsed, action, err := bus.ParseTopic(c.value)
	if err != nil {
		t.Errorf("%s %s = %q: ParseTopic: %v", c.pos, c.name, c.value, err)
		return
	}
	rebuilt := bus.TopicOf(parsed.SourceSeg(), parsed.CategorySeg(), parsed.ObjectSeg()).
		Mod(parsed.ModifierSeg()).Action(action)
	if rebuilt != topic {
		t.Errorf("%s %s = %q: builder round-trip gave %q", c.pos, c.name, c.value, rebuilt)
	}
	if mod := parsed.ModifierSeg(); mod != intendedModifiers[c.name] {
		t.Errorf("%s %s = %q: object %q carries modifier %q; kit reads the first underscore in the object segment as object_modifier",
			c.pos, c.name, c.value, parsed.ObjectSeg(), mod)
	}
}

// TestEveryEventsTopicFollowsKitGrammar checks every topic constant
// declared in internal/events.
func TestEveryEventsTopicFollowsKitGrammar(t *testing.T) {
	consts := collectTopicConsts(t, ".")
	// A parser regression must not pass as "nothing to check".
	if len(consts) < 20 {
		t.Fatalf("found %d topic constants in internal/events; the scan is broken", len(consts))
	}
	for _, c := range consts {
		t.Run(c.name, func(t *testing.T) { assertKitTopic(t, c) })
	}
}

// TestEveryPluginTopicFollowsKitGrammar checks the Topic* constants of
// every plugin under plugins/. Plugins are separate modules that cannot
// import internal/events, so each declares its own topics.
func TestEveryPluginTopicFollowsKitGrammar(t *testing.T) {
	pluginsDir := filepath.Join(repoRoot(t), "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		consts := collectTopicConsts(t, filepath.Join(pluginsDir, e.Name()))
		total += len(consts)
		for _, c := range consts {
			c.pos = e.Name() + "/" + c.pos
			t.Run(e.Name()+"/"+c.name, func(t *testing.T) { assertKitTopic(t, c) })
		}
	}
	if total == 0 {
		t.Fatal("found no plugin topic constants; the scan is broken")
	}
}
