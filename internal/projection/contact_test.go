package projection_test

import (
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// sarah is a contact as cardamum ingests it: details in the metadata,
// the card and the note as section nodes, the note classed personal.
func sarah() *storage.KnowledgeObject {
	ko := &storage.KnowledgeObject{
		ID: "c-sarah", Type: projection.TypeContact,
		RawContent: "Sarah Lee | Head of Ops @ Acme\nLisbon", TextContent: "Sarah Lee | Head of Ops @ Acme\nLisbon",
		Metadata: map[string]any{
			"name": "Sarah Lee", "org": "Acme", "role": "Head of Ops", "city": "Lisbon",
			"email": "sarah@acme.example", "phone": "+351 555 0100", "address": "1 Rua Augusta, Lisbon",
		},
		Tags: []storage.Tag{{Label: "work"}, {Label: "client"}},
		Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{
			{ID: "c-sarah/section/0", NodeType: storage.NodeTypeSection, Label: projection.ContactCardLabel, Content: "Sarah Lee | Head of Ops @ Acme\nLisbon"},
			{ID: "c-sarah/section/1", NodeType: storage.NodeTypeSection, Label: projection.ContactNoteLabel, Content: "kids Leo and Mia, moving to Lisbon"},
		}},
	}
	storage.SetNodeClasses(&ko.Graph.Nodes[1], []string{storage.ClassPersonal})
	return ko
}

func TestContactField(t *testing.T) {
	ko := sarah()
	for field, want := range map[string]string{
		projection.ContactTitle: "Sarah Lee", projection.ContactOrg: "Acme", projection.ContactRole: "Head of Ops",
		projection.ContactCity: "Lisbon", projection.ContactEmail: "sarah@acme.example", projection.ContactPhone: "+351 555 0100",
		projection.ContactAddress: "1 Rua Augusta, Lisbon", projection.ContactTags: "work, client",
		projection.ContactNote: "kids Leo and Mia, moving to Lisbon", "bogus": "",
	} {
		if got := projection.ContactField(ko, field); got != want {
			t.Errorf("ContactField(%s) = %q, want %q", field, got, want)
		}
	}
	// The title falls back to the first summary; nothing panics on nil.
	if got := projection.ContactField(&storage.KnowledgeObject{Summaries: []string{" Bob "}}, projection.ContactTitle); got != "Bob" {
		t.Errorf("title from summary = %q", got)
	}
	if projection.ContactField(nil, projection.ContactTitle) != "" || projection.ContactField(&storage.KnowledgeObject{}, projection.ContactNote) != "" {
		t.Error("nil and empty contacts must render nothing")
	}
}

// RenderContact shows only the listed fields: the colleague card has no
// email, phone, address or note; the partner card has the details but
// still no note; the minimum is what the masked index holds.
func TestRenderContact(t *testing.T) {
	ko := sarah()
	for name, tc := range map[string]struct {
		fields []string
		want   string
	}{
		"colleagues": {[]string{"title", "org", "role", "tags", "city"}, "Sarah Lee | Head of Ops @ Acme\nLisbon\nTags: work, client"},
		"partners":   {[]string{"title", "org", "role", "email", "phone", "tags", "city"}, "Sarah Lee | Head of Ops @ Acme\nLisbon\nEmail: sarah@acme.example\nPhone: +351 555 0100\nTags: work, client"},
		"owner":      {projection.ContactFields, "Sarah Lee | Head of Ops @ Acme\nLisbon\nEmail: sarah@acme.example\nPhone: +351 555 0100\nAddress: 1 Rua Augusta, Lisbon\nTags: work, client\nkids Leo and Mia, moving to Lisbon"},
		"card":       {projection.ContactCardFields, "Sarah Lee | Head of Ops @ Acme\nLisbon"},
		"org only":   {[]string{"org"}, "Acme"},
		"nothing":    {nil, ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := projection.RenderContact(ko, tc.fields); got != tc.want {
				t.Fatalf("RenderContact = %q, want %q", got, tc.want)
			}
		})
	}
	for _, hidden := range []string{"sarah@acme.example", "555", "Rua Augusta", "Leo"} {
		if strings.Contains(projection.RenderContact(ko, projection.ContactSearchableMinimum), hidden) {
			t.Errorf("searchable minimum carries %q", hidden)
		}
	}
}

// A contact's masked projection is its searchable minimum, never the
// stored text or the note; the raw projection keeps card and note for
// the owner.
func TestProjectIndex_ContactMaskedIsTheSearchableMinimum(t *testing.T) {
	ko := sarah()
	idx := projection.ProjectIndex(ko)
	if idx.MaskedFTSBody != "Sarah Lee | Head of Ops @ Acme\nLisbon\nTags: work, client" || idx.MaskedEmbeddingText != idx.MaskedFTSBody {
		t.Fatalf("masked = %q / %q", idx.MaskedFTSBody, idx.MaskedEmbeddingText)
	}
	if !strings.Contains(idx.FTSBody, "kids Leo and Mia") || !strings.Contains(idx.FTSBody, "Sarah Lee") {
		t.Errorf("raw FTS body = %q, want the card and the note", idx.FTSBody)
	}
	if strings.Contains(idx.FTSBody, "sarah@acme.example") {
		t.Errorf("raw FTS body carries the email: %q", idx.FTSBody)
	}
	if got := projection.MaskedEmbeddingText(ko); got != idx.MaskedFTSBody {
		t.Errorf("MaskedEmbeddingText = %q", got)
	}
	// A contact with nothing in the minimum masks to "", never to its
	// stored text.
	bare := &storage.KnowledgeObject{Type: projection.TypeContact, RawContent: "secret@x.example", Metadata: map[string]any{"email": "secret@x.example"}}
	if idx := projection.ProjectIndex(bare); idx.MaskedFTSBody != "" {
		t.Errorf("bare contact masked = %q", idx.MaskedFTSBody)
	}
}

func TestContactFieldClasses(t *testing.T) {
	for _, f := range []string{projection.ContactEmail, projection.ContactPhone, projection.ContactAddress} {
		if projection.ContactFieldClasses[f] != storage.ClassContactDetails {
			t.Errorf("%s class = %q", f, projection.ContactFieldClasses[f])
		}
	}
	if projection.ContactFieldClasses[projection.ContactNote] != storage.ClassPersonal {
		t.Error("note class")
	}
	for _, f := range projection.ContactSearchableMinimum {
		if _, classed := projection.ContactFieldClasses[f]; classed {
			t.Errorf("searchable minimum field %s carries a class", f)
		}
	}
}
