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

// literalTopicAllowlist names publish sites outside this change's scope
// that still pass an inline literal. Each entry must still be a valid
// kit topic; the guard checks that too. Remove entries as they move to
// catalog constants.
var literalTopicAllowlist = map[string]bool{
	"internal/lateral/daemon/lifecycle.go": true,
}

// containsStringLit reports whether expr builds its value from a string
// literal anywhere (a literal, a conversion of one, a concatenation).
func containsStringLit(expr ast.Expr) (string, bool) {
	var found string
	ast.Inspect(expr, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && found == "" {
			found = lit.Value
		}
		return found == ""
	})
	return found, found != ""
}

// importPaths maps each import's local name to its path for one file.
func importPaths(f *ast.File) map[string]string {
	m := map[string]string{}
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		name := filepath.Base(p)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		m[name] = p
	}
	return m
}

const (
	eventsPkg    = "github.com/ideacrafterslabs/ctxt/internal/events"
	kitBusPkg    = "hop.top/kit/go/runtime/bus"
	pluginapiPkg = "github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// selectorPkg returns the import path and selected name of pkg.Name.
func selectorPkg(expr ast.Expr, imports map[string]string) (string, string) {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", ""
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", ""
	}
	return imports[id.Name], sel.Sel.Name
}

// topicArgs returns the expressions that name a published topic in n.
func topicArgs(n ast.Node, imports map[string]string) []ast.Expr {
	switch x := n.(type) {
	case *ast.CallExpr:
		return callTopicArgs(x, imports)
	case *ast.CompositeLit:
		return eventTypeArgs(x, imports)
	}
	return nil
}

// callTopicArgs handles events.NewEvent, bus.NewEvent, bus.Topic and the
// Publisher.Publish(ctx, topic, source, payload) shape used by the
// ambient and lateral publishers.
func callTopicArgs(x *ast.CallExpr, imports map[string]string) []ast.Expr {
	pkg, name := selectorPkg(x.Fun, imports)
	switch {
	case pkg == eventsPkg && name == "NewEvent" && len(x.Args) >= 2:
		return []ast.Expr{x.Args[1]}
	case pkg == kitBusPkg && (name == "NewEvent" || name == "Topic") && len(x.Args) >= 1:
		return []ast.Expr{x.Args[0]}
	}
	if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Publish" && len(x.Args) >= 3 {
		return []ast.Expr{x.Args[1]}
	}
	return nil
}

// eventTypeArgs handles events.Event{Type: ...} and
// pluginapi.Event{Type: ...} literals.
func eventTypeArgs(x *ast.CompositeLit, imports map[string]string) []ast.Expr {
	pkg, name := selectorPkg(x.Type, imports)
	if name != "Event" || (pkg != eventsPkg && pkg != pluginapiPkg) {
		return nil
	}
	for _, elt := range x.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Type" {
			return []ast.Expr{kv.Value}
		}
	}
	return nil
}

// TestPublishSitesUseTopicConstants walks every non-test Go file in the
// repo and fails on a publish whose topic is an inline string literal
// instead of a declared topic constant (internal/events, a plugin's
// Topic* constants, or the lateral TopicOf catalog).
func TestPublishSitesUseTopicConstants(t *testing.T) {
	root := repoRoot(t)
	sites := 0
	reported := map[string]bool{} // a bus.Topic(lit) inside bus.NewEvent is one site
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" ||
				name == "testdata" || name == "docs") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		imports := importPaths(f)
		ast.Inspect(f, func(n ast.Node) bool {
			for _, arg := range topicArgs(n, imports) {
				sites++
				lit, ok := containsStringLit(arg)
				if !ok {
					continue
				}
				at := rel + ":" + strconv.Itoa(fset.Position(arg.Pos()).Line)
				if reported[at+lit] {
					continue
				}
				reported[at+lit] = true
				if !literalTopicAllowlist[rel] {
					t.Errorf("%s: topic %s is an inline literal; publish a declared topic constant", at, lit)
					continue
				}
				if val, err := strconv.Unquote(lit); err == nil {
					if err := bus.ValidateTopic(bus.Topic(val)); err != nil {
						t.Errorf("%s: allowlisted literal topic: %v", at, err)
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if sites < 50 {
		t.Fatalf("found %d publish sites; the scan is broken", sites)
	}
}
