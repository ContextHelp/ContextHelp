package pluginapi_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func TestNodeID_Roundtrip(t *testing.T) {
	id := pluginapi.NewNodeID("obj-abc", pluginapi.NodeTypeSection, 0)
	parsed, err := pluginapi.ParseNodeID(id)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.ObjectID != "obj-abc" {
		t.Errorf("got %q", parsed.ObjectID)
	}
	if parsed.NodeType != pluginapi.NodeTypeSection {
		t.Errorf("got %q", parsed.NodeType)
	}
	if parsed.Ordinal != 0 {
		t.Errorf("got %d", parsed.Ordinal)
	}
}

func TestNodeURI_Format(t *testing.T) {
	u := pluginapi.NodeURI("obj-abc", pluginapi.NodeTypeSection, 0)
	if u != "ctxt:node/obj-abc/section/0" {
		t.Errorf("got %q", u)
	}
}

func TestObjectNodeURI_ParseRoundtrip(t *testing.T) {
	raw := "ctxt:node/obj-abc/tag/2"
	ref, err := pluginapi.ParseNodeURI(raw)
	if err != nil {
		t.Fatal(err)
	}
	if ref.ObjectID != "obj-abc" || ref.NodeType != pluginapi.NodeTypeTag || ref.Ordinal != 2 {
		t.Errorf("unexpected ref %+v", ref)
	}
}

func TestNewNodeID_PanicsOnSlashInObjectID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for objectID with '/'")
		}
	}()
	pluginapi.NewNodeID("org/repo", pluginapi.NodeTypeSection, 0)
}

func TestNewNodeID_PanicsOnEmptyObjectID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty objectID")
		}
	}()
	pluginapi.NewNodeID("", pluginapi.NodeTypeSection, 0)
}

func TestNewNodeID_PanicsOnNegativeOrdinal(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for negative ordinal")
		}
	}()
	pluginapi.NewNodeID("obj-1", pluginapi.NodeTypeSection, -1)
}

func TestParseNodeID_MalformedInput(t *testing.T) {
	_, err := pluginapi.ParseNodeID("only-two/parts")
	if err == nil {
		t.Fatal("expected error for malformed input")
	}
}

func TestParseNodeURI_MissingPrefix(t *testing.T) {
	_, err := pluginapi.ParseNodeURI("node/obj/section/0")
	if err == nil {
		t.Fatal("expected error for missing ctxt:node/ prefix")
	}
}
