package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func TestPrintTable(t *testing.T) {
	var buf bytes.Buffer
	printTable(&buf, []string{"ID", "Title"}, [][]string{
		{"obj-1", "First object"},
		{"obj-2", "Second object"},
	})
	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("ID")) {
		t.Fatalf("expected header 'ID' in output: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("obj-1")) {
		t.Fatalf("expected 'obj-1' in output: %s", out)
	}
}

func TestOutputJSON(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]string{"key": "value"}
	if err := outputJSON(&buf, data); err != nil {
		t.Fatalf("outputJSON: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"key"`)) {
		t.Fatalf("expected JSON key in output: %s", buf.String())
	}
}

func TestIsJSONOutput(t *testing.T) {
	// isJSONOutput reads viper "output.format"
	// Default should be false (text)
	if isJSONOutput() {
		t.Fatal("expected text output by default")
	}
}

// TestKoLabel_FallbackToSummary ensures koLabel uses the first summary when
// DocumentProjection.Title is empty (no graph nodes).
func TestKoLabel_FallbackToSummary(t *testing.T) {
	ko := &storage.KnowledgeObject{
		ID:        "obj_abc",
		Summaries: []string{"Best UX practices for signup flows"},
	}
	label := koLabel(ko)
	if !strings.Contains(label, "obj_abc") {
		t.Errorf("label should contain ID; got %q", label)
	}
	if !strings.Contains(label, "Best UX practices") {
		t.Errorf("label should contain summary; got %q", label)
	}
}

// TestKoLabel_FallbackToID returns object ID when no title or summary exists.
func TestKoLabel_FallbackToID(t *testing.T) {
	ko := &storage.KnowledgeObject{ID: "obj_xyz"}
	label := koLabel(ko)
	if label != "obj_xyz" {
		t.Errorf("expected %q; got %q", "obj_xyz", label)
	}
}

// TestKoLabel_GraphTitle uses DocumentProjection.Title from a graph summary node.
// The projection derives Title via graph nodes when Graph is populated.
// Currently ProjectDocument.Title is empty (no title node type yet), so this
// exercises the fallback path via graph with no title node.
func TestKoLabel_GraphNoTitle(t *testing.T) {
	ko := &storage.KnowledgeObject{
		ID:        "obj_g1",
		Summaries: []string{"Graph-canonical object summary"},
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{
					ID:       pluginapi.NewNodeID("obj_g1", pluginapi.NodeTypeSection, 0),
					NodeType: pluginapi.NodeTypeSection,
					Label:    "Introduction",
					Content:  "First section content",
				},
			},
		},
	}
	label := koLabel(ko)
	// ProjectDocument.Title is empty (no title node); should fall back to summary.
	if !strings.Contains(label, "Graph-canonical object summary") {
		t.Errorf("expected summary fallback in label; got %q", label)
	}
}
