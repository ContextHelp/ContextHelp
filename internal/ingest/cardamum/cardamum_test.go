package cardamum

import (
	"slices"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/ingest"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// sarahVCard is a full card: identity, details with parameters, a
// structured address with an escaped comma, and a folded note.
const sarahVCard = "BEGIN:VCARD\r\nVERSION:3.0\r\n" +
	"FN:Sarah Lee\r\nORG:Acme\r\nTITLE:Head of Ops\r\n" +
	"EMAIL;TYPE=INTERNET,WORK:sarah@acme.example\r\n" +
	"TEL;TYPE=CELL:+351 555 0100\r\n" +
	"ADR;TYPE=WORK:;;1 Rua Augusta\\, 2nd floor;Lisbon;;1100-048;Portugal\r\n" +
	"NOTE:kids Leo and Mia\\, \r\n moving to Lisbon.\r\n" +
	"UID:uid-sarah\r\nEND:VCARD\r\n"

func TestExtractVCardField(t *testing.T) {
	vcard := "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Jane Doe\r\n" +
		"ORG:Acme Corp\r\nTITLE:Engineer\r\n" +
		"EMAIL:jane@acme.com\r\n" +
		"NOTE:Referred by\\, someone important.\r\n" +
		"UID:abc-123\r\nEND:VCARD\r\n"

	tests := []struct {
		field string
		want  string
	}{
		{"FN", "Jane Doe"},
		{"ORG", "Acme Corp"},
		{"TITLE", "Engineer"},
		{"EMAIL", "jane@acme.com"},
		{"NOTE", "Referred by, someone important."},
		{"UID", "abc-123"},
		{"TEL", ""},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			got := extractVCardField(vcard, tt.field)
			if got != tt.want {
				t.Errorf("extractVCardField(%q) = %q, want %q",
					tt.field, got, tt.want)
			}
		})
	}
}

func TestExtractVCardFieldFolded(t *testing.T) {
	vcard := "BEGIN:VCARD\r\nVERSION:3.0\r\n" +
		"NOTE:This is a long note that \r\n continues on the next line.\r\n" +
		"UID:fold-test\r\nEND:VCARD\r\n"

	got := extractVCardField(vcard, "NOTE")
	want := "This is a long note that continues on the next line."
	if got != want {
		t.Errorf("folded NOTE = %q, want %q", got, want)
	}
}

// Field parameters are skipped, TEL and ADR are read, and the address
// keeps its escaped comma and yields the locality as the city.
func TestExtractVCardFieldParameters(t *testing.T) {
	if got := extractVCardField(sarahVCard, "EMAIL"); got != "sarah@acme.example" {
		t.Errorf("EMAIL = %q", got)
	}
	if got := extractVCardField(sarahVCard, "TEL"); got != "+351 555 0100" {
		t.Errorf("TEL = %q", got)
	}
	if got := extractVCardField(sarahVCard, "NOTE"); got != "kids Leo and Mia, moving to Lisbon." {
		t.Errorf("NOTE = %q", got)
	}
	address, city := extractVCardAddress(sarahVCard)
	if address != "1 Rua Augusta, 2nd floor, Lisbon, 1100-048, Portugal" || city != "Lisbon" {
		t.Errorf("ADR = %q, city %q", address, city)
	}
	if address, city := extractVCardAddress("BEGIN:VCARD\r\nFN:X\r\nEND:VCARD\r\n"); address != "" || city != "" {
		t.Errorf("no ADR = %q, %q", address, city)
	}
}

// transformSarah transforms the full card.
func transformSarah(t *testing.T) ingest.Object {
	t.Helper()
	objects := TransformCards([]card{{ID: "sarah", AddressbookID: "work", VCard: sarahVCard}})
	if len(objects) != 1 {
		t.Fatalf("got %d objects, want 1", len(objects))
	}
	return objects[0]
}

// The transformed contact keeps email, phone and address in the metadata
// only; the text is the card.
func TestTransformCard_DetailsOutOfTheText(t *testing.T) {
	obj := transformSarah(t)
	if obj.ID != "uid-sarah" || obj.Type != "contact" {
		t.Errorf("obj = %s %s", obj.ID, obj.Type)
	}
	if obj.Content != "Sarah Lee | Head of Ops @ Acme\nLisbon" {
		t.Errorf("Content = %q", obj.Content)
	}
	for _, hidden := range []string{"sarah@acme.example", "555 0100", "Rua Augusta", "Leo"} {
		if strings.Contains(obj.Content, hidden) {
			t.Errorf("Content carries %q", hidden)
		}
	}
	for key, want := range map[string]string{
		"addressbook": "work", "name": "Sarah Lee", "org": "Acme", "role": "Head of Ops", "city": "Lisbon",
		"email": "sarah@acme.example", "phone": "+351 555 0100", "address": "1 Rua Augusta, 2nd floor, Lisbon, 1100-048, Portugal",
	} {
		if obj.Metadata[key] != want {
			t.Errorf("Metadata[%s] = %v, want %q", key, obj.Metadata[key], want)
		}
	}
	if _, ok := obj.Metadata["title"]; ok {
		t.Error("the job title is role, not title")
	}
	if !slices.Equal(obj.Tags, []string{"source:cardamum", "org:Acme", "role:Head of Ops"}) {
		t.Errorf("Tags = %v", obj.Tags)
	}
}

// The note is a personal section node beside the card node; node ids
// are left to the runner.
func TestTransformCard_NoteNode(t *testing.T) {
	obj := transformSarah(t)
	if obj.Graph == nil || len(obj.Graph.Nodes) != 2 {
		t.Fatalf("Graph = %+v, want the card and the note", obj.Graph)
	}
	card, note := obj.Graph.Nodes[0], obj.Graph.Nodes[1]
	if card.NodeType != storage.NodeTypeSection || card.Label != projection.ContactCardLabel || card.Content != obj.Content {
		t.Errorf("card node = %+v", card)
	}
	if storage.NodeClasses(card) != nil {
		t.Errorf("card classes = %v, want none", storage.NodeClasses(card))
	}
	if note.NodeType != storage.NodeTypeSection || note.Label != projection.ContactNoteLabel || note.Order != 1 {
		t.Errorf("note node = %+v", note)
	}
	if note.Content != "kids Leo and Mia, moving to Lisbon." {
		t.Errorf("note content = %q", note.Content)
	}
	if !slices.Equal(storage.NodeClasses(note), []string{storage.ClassPersonal}) {
		t.Errorf("note classes = %v, want personal", storage.NodeClasses(note))
	}
	if card.ID != "" || note.ID != "" {
		t.Error("node ids are the runner's to assign")
	}
}

// Stored, the contact masks to the searchable minimum and the owner's
// index keeps the note, never the email.
func TestTransformCard_MaskedProjection(t *testing.T) {
	obj := transformSarah(t)
	ko := &storage.KnowledgeObject{Type: obj.Type, RawContent: obj.Content, TextContent: obj.Content, Metadata: obj.Metadata, Graph: obj.Graph}
	for _, tg := range obj.Tags {
		ko.Tags = append(ko.Tags, storage.Tag{Label: tg})
	}
	idx := projection.ProjectIndex(ko)
	if idx.MaskedFTSBody != "Sarah Lee | Head of Ops @ Acme\nLisbon\nTags: source:cardamum, org:Acme, role:Head of Ops" {
		t.Errorf("masked body = %q", idx.MaskedFTSBody)
	}
	if !strings.Contains(idx.FTSBody, "kids Leo") || strings.Contains(idx.FTSBody, "sarah@acme.example") {
		t.Errorf("raw body = %q", idx.FTSBody)
	}
}

func TestTransformCards_Minimal(t *testing.T) {
	objects := TransformCards([]card{
		{ID: "bob", AddressbookID: "personal", VCard: "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Bob\r\nUID:uid-bob\r\nEND:VCARD\r\n"},
		{ID: "fallback-id", AddressbookID: "test", VCard: "BEGIN:VCARD\r\nVERSION:3.0\r\nEND:VCARD\r\n"},
	})
	bob := objects[0]
	if bob.ID != "uid-bob" || bob.Content != "Bob" || bob.Metadata["name"] != "Bob" {
		t.Errorf("bob = %+v", bob)
	}
	if bob.Graph == nil || len(bob.Graph.Nodes) != 1 || bob.Graph.Nodes[0].Label != projection.ContactCardLabel {
		t.Errorf("bob graph = %+v, want the card only", bob.Graph)
	}
	for _, key := range []string{"email", "phone", "address", "org", "role", "city"} {
		if _, ok := bob.Metadata[key]; ok {
			t.Errorf("bob carries an empty %s", key)
		}
	}
	empty := objects[1]
	if empty.ID != "fallback-id" || empty.Content != "" || empty.Graph != nil {
		t.Errorf("empty card = %+v, want the card id, no text and no graph", empty)
	}
}
