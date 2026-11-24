package representation_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/ideacrafterslabs/ctxt/internal/representation"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The published schema and the Go closed set must not drift.
func TestSchemaEnumMatchesKinds(t *testing.T) {
	raw, err := os.ReadFile("../../schemas/representation_node.json")
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Properties struct {
			Representation struct {
				Enum []string `json:"enum"`
			} `json:"representation"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	got := s.Properties.Representation.Enum
	want := representation.Kinds()
	if len(got) != len(want) {
		t.Fatalf("schema %v, code %v", got, want)
	}
	for i := range want {
		if got[i] != string(want[i]) {
			t.Errorf("kind %d: schema %q, code %q", i, got[i], want[i])
		}
	}
}

// Emit output validates against the published schema, for every kind and
// anchor, and a frame kind without frame_index does not.
func TestEmitOutputValidatesAgainstSchema(t *testing.T) {
	raw, err := os.ReadFile("../../schemas/representation_node.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("mem:///representation_node.json", doc); err != nil {
		t.Fatal(err)
	}
	sch, err := c.Compile("mem:///representation_node.json")
	if err != nil {
		t.Fatal(err)
	}
	conf := 0.4
	d := &storage.KnowledgeObject{ID: "o"}
	for _, k := range representation.Kinds() {
		a := representation.Anchor{}.WithSpan(0, 10).WithSpeaker("S1").WithPage(1).WithImage(0)
		if k == representation.FrameCaption || k == representation.FrameOCR {
			a = a.WithFrameIndex(2)
		}
		n, err := representation.Emit(d, representation.Spec{Kind: k, Content: "x " + string(k), Anchor: a,
			Provider: "p", Model: "m", Confidence: &conf, Origin: representation.OriginSidecar})
		if err != nil {
			t.Fatal(err)
		}
		var v any
		b, _ := json.Marshal(n.Metadata)
		if err := json.Unmarshal(b, &v); err != nil {
			t.Fatal(err)
		}
		if err := sch.Validate(v); err != nil {
			t.Errorf("%s: %v", k, err)
		}
	}
	bad := map[string]any{"representation": "frame_ocr"}
	if err := sch.Validate(bad); err == nil {
		t.Error("frame_ocr without frame_index must fail the schema")
	}
}
