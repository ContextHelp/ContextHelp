package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Layer is one config file's own YAML document, read on its own: no other
// cascade file, `-c` overlay, env value or built-in default is in it. The
// config-changing commands edit a Layer, never the merged [Config], so the
// file they save gains only the keys they meant to change.
//
// Edits work on the yaml.v3 node tree, so comments, key order, quoting
// and flow style survive. Blank lines, which yaml.v3 drops, are put back
// on save, and the file's own indent width is kept.
//
// Key paths are segments rather than a dotted string: profile and
// registry names may themselves contain dots.
type Layer struct {
	path   string
	orig   []byte // nil when the file does not exist yet
	doc    *yaml.Node
	root   *yaml.Node
	indent int
	dirty  bool
}

// EditLayer opens path's own layer, applies edit and saves the result when
// edit returns nil and changed something. A missing file is created
// holding only what edit set. On error nothing is written.
func EditLayer(path string, edit func(*Layer) error) error {
	l, err := OpenLayer(path)
	if err != nil {
		return err
	}
	if err := edit(l); err != nil {
		return err
	}
	return l.Save()
}

// OpenLayer reads path as a single config layer. A missing file yields an
// empty layer that [Layer.Save] creates.
func OpenLayer(path string) (*Layer, error) {
	l := &Layer{path: path, indent: 2}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		l.doc = &yaml.Node{Kind: yaml.DocumentNode}
	case err != nil:
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	default:
		l.orig = data
		l.indent = detectIndent(data)
		var doc yaml.Node
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("config: parse %s: %w", path, err)
		}
		l.doc = &doc
	}

	if l.doc.Kind != yaml.DocumentNode {
		l.doc = &yaml.Node{Kind: yaml.DocumentNode}
	}
	if len(l.doc.Content) == 0 {
		l.doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	root := l.doc.Content[0]
	if isNull(root) {
		*root = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", HeadComment: root.HeadComment}
	}
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config: %s: top level is not a mapping", path)
	}
	l.root = root
	return l, nil
}

// Path is the file this layer reads and writes.
func (l *Layer) Path() string { return l.path }

// Has reports whether keys is present in this file.
func (l *Layer) Has(keys ...string) bool { return l.lookup(keys) != nil }

// Keys lists the mapping keys under keys in file order; nil when keys is
// absent or not a mapping.
func (l *Layer) Keys(keys ...string) []string {
	n := l.lookup(keys)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	out := make([]string, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, n.Content[i].Value)
	}
	return out
}

// Decode decodes the value at keys into out. It reports false, and leaves
// out untouched, when this file does not set keys.
func (l *Layer) Decode(out any, keys ...string) (bool, error) {
	n := l.lookup(keys)
	if n == nil {
		return false, nil
	}
	if err := n.Decode(out); err != nil {
		return true, fmt.Errorf("config: %s: decode %s: %w", l.path, strings.Join(keys, "."), err)
	}
	return true, nil
}

// Set writes value at keys, creating missing parent mappings. An existing
// value is replaced in place: its comments, anchor, quoting and (for a
// non-empty collection) flow style carry over. Setting the value already
// there changes nothing.
func (l *Layer) Set(value any, keys ...string) error {
	if len(keys) == 0 {
		return errors.New("config: Set needs a key")
	}
	node, err := encodeNode(value)
	if err != nil {
		return fmt.Errorf("config: encode %s: %w", strings.Join(keys, "."), err)
	}
	parent, err := l.walkOrCreate(keys[:len(keys)-1])
	if err != nil {
		return err
	}
	leaf := keys[len(keys)-1]
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value != leaf {
			continue
		}
		old := parent.Content[i+1]
		carryPresentation(old, node)
		if sameNode(old, node) {
			return nil
		}
		// Mutate in place so an alias pointing at old stays valid.
		*old = *node
		l.dirty = true
		return nil
	}
	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: leaf}, node)
	l.dirty = true
	return nil
}

// Delete removes keys from this file, reporting whether it was there.
func (l *Layer) Delete(keys ...string) bool {
	if len(keys) == 0 {
		return false
	}
	parent := l.lookup(keys[:len(keys)-1])
	if parent == nil || parent.Kind != yaml.MappingNode {
		return false
	}
	leaf := keys[len(keys)-1]
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == leaf {
			parent.Content = append(parent.Content[:i], parent.Content[i+2:]...)
			l.dirty = true
			return true
		}
	}
	return false
}

// RemoveItems drops every item of the sequence at keys that match
// selects, returning how many went. A missing or non-sequence value
// removes nothing.
func (l *Layer) RemoveItems(match func(item *yaml.Node) (bool, error), keys ...string) (int, error) {
	seq := l.lookup(keys)
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return 0, nil
	}
	kept := seq.Content[:0:0]
	removed := 0
	for _, item := range seq.Content {
		drop, err := match(item)
		if err != nil {
			return 0, fmt.Errorf("config: %s: %s: %w", l.path, strings.Join(keys, "."), err)
		}
		if drop {
			removed++
			continue
		}
		kept = append(kept, item)
	}
	if removed > 0 {
		seq.Content = kept
		l.dirty = true
	}
	return removed, nil
}

// Save writes the layer back when an edit changed it. The write is atomic
// (temp file + rename in the same directory), leaves the file mode 0600,
// and goes through a symlinked config file to its target.
func (l *Layer) Save() error {
	if !l.dirty {
		return nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(l.indent)
	if err := enc.Encode(l.doc); err != nil {
		return fmt.Errorf("config: encode %s: %w", l.path, err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("config: encode %s: %w", l.path, err)
	}
	out := buf.Bytes()
	if l.orig != nil {
		out = restoreBlankLines(l.orig, out)
	}

	path := l.path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("config: create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("config: write %s: %w", l.path, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("config: write %s: %w", l.path, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("config: write %s: %w", l.path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("config: write %s: %w", l.path, err)
	}
	l.orig = out
	l.dirty = false
	return nil
}

// lookup returns the node at keys, or nil. No keys means the root.
func (l *Layer) lookup(keys []string) *yaml.Node {
	cur := l.root
	for _, k := range keys {
		if cur.Kind != yaml.MappingNode {
			return nil
		}
		next := mappingValue(cur, k)
		if next == nil {
			return nil
		}
		cur = next
	}
	return cur
}

// walkOrCreate returns the mapping at keys, creating missing (or null)
// levels. A level holding anything else is an error rather than being
// overwritten: that value is the user's data.
func (l *Layer) walkOrCreate(keys []string) (*yaml.Node, error) {
	cur := l.root
	for i, k := range keys {
		next := mappingValue(cur, k)
		switch {
		case next == nil:
			unflowEmpty(cur)
			next = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			cur.Content = append(cur.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}, next)
		case isNull(next):
			*next = yaml.Node{
				Kind:        yaml.MappingNode,
				Tag:         "!!map",
				HeadComment: next.HeadComment,
				LineComment: next.LineComment,
				FootComment: next.FootComment,
			}
		case next.Kind != yaml.MappingNode:
			return nil, fmt.Errorf("config: %s: %s is not a mapping", l.path, strings.Join(keys[:i+1], "."))
		}
		cur = next
	}
	unflowEmpty(cur)
	return cur, nil
}

// unflowEmpty turns an empty `{}` that is about to gain a key into a block
// mapping; a non-empty flow mapping keeps the style its author chose.
func unflowEmpty(m *yaml.Node) {
	if len(m.Content) == 0 {
		m.Style &^= yaml.FlowStyle
	}
}

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null"
}

// encodeNode encodes value into a node. yaml.Node.Encode panics on values
// yaml cannot represent (funcs, channels); that becomes an error.
func encodeNode(value any) (node *yaml.Node, err error) {
	defer func() {
		if r := recover(); r != nil {
			node, err = nil, fmt.Errorf("unencodable value: %v", r)
		}
	}()
	var n yaml.Node
	if err := n.Encode(value); err != nil {
		return nil, err
	}
	return &n, nil
}

// carryPresentation copies old's comments, anchor and style choices onto
// its replacement so an edit changes the value and nothing around it.
func carryPresentation(old, n *yaml.Node) {
	if n.HeadComment == "" {
		n.HeadComment = old.HeadComment
	}
	if n.LineComment == "" {
		n.LineComment = old.LineComment
	}
	if n.FootComment == "" {
		n.FootComment = old.FootComment
	}
	n.Anchor = old.Anchor
	switch {
	case old.Kind == yaml.ScalarNode && n.Kind == yaml.ScalarNode:
		quoted := old.Style & (yaml.SingleQuotedStyle | yaml.DoubleQuotedStyle)
		if quoted != 0 && n.ShortTag() == "!!str" && n.Value != "" {
			n.Style = quoted
		}
	case old.Kind == n.Kind && len(old.Content) > 0 &&
		(n.Kind == yaml.SequenceNode || n.Kind == yaml.MappingNode):
		n.Style |= old.Style & yaml.FlowStyle
	}
}

// sameNode reports whether replacing old with n would change the file.
func sameNode(old, n *yaml.Node) bool {
	a, errA := yaml.Marshal(old)
	b, errB := yaml.Marshal(n)
	return errA == nil && errB == nil && bytes.Equal(a, b)
}

// detectIndent returns the file's indent step: the first increase in
// leading spaces after a line that opens a block (ends in ':'). Defaults
// to 2, the yaml.v3 house style, when nothing is indented.
func detectIndent(data []byte) int {
	prevIndent, prevOpens := 0, false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(trimmed)
		if prevOpens && indent > prevIndent && !strings.HasPrefix(trimmed, "- ") {
			if step := indent - prevIndent; step >= 2 && step <= 8 {
				return step
			}
			return 2
		}
		code, _, _ := strings.Cut(trimmed, " #")
		prevIndent, prevOpens = indent, strings.HasSuffix(strings.TrimSpace(code), ":")
	}
	return 2
}

// maxBlankRestoreCells bounds the line-diff table; config files are far
// smaller, and a pathological one just loses its blank lines.
const maxBlankRestoreCells = 4 << 20

// restoreBlankLines puts back the blank lines yaml.v3 drops when it
// re-encodes a document. It aligns the original and re-encoded lines by
// longest common subsequence and keeps every blank original line the
// re-encode deleted, at its original position. Changed lines are taken
// from the re-encode, so the edit itself always wins.
func restoreBlankLines(orig, out []byte) []byte {
	a := splitLines(orig)
	b := splitLines(out)
	n, m := len(a), len(b)
	if n == 0 || (n+1)*(m+1) > maxBlankRestoreCells {
		return out
	}
	// lcs[i][j] = LCS length of a[i:] and b[j:].
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	res := make([]string, 0, m+8)
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			res = append(res, b[j])
			i++
			j++
		case j < m && (i == n || lcs[i][j+1] >= lcs[i+1][j]):
			// Prefer the new line first, so a blank line that separated
			// the edited block from the next one stays after the edit.
			res = append(res, b[j])
			j++
		default:
			if strings.TrimSpace(a[i]) == "" {
				res = append(res, a[i])
			}
			i++
		}
	}
	return []byte(strings.Join(res, "\n") + "\n")
}

func splitLines(data []byte) []string {
	s := strings.TrimSuffix(string(data), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
