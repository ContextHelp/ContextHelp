package cardamum

import (
	"testing"
)

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

func TestTransformCards(t *testing.T) {
	cards := []card{
		{
			ID:            "jane",
			AddressbookID: "work",
			VCard: "BEGIN:VCARD\r\nVERSION:3.0\r\n" +
				"FN:Jane Doe\r\nORG:Acme\r\nTITLE:CTO\r\n" +
				"EMAIL:jane@acme.com\r\n" +
				"UID:uid-jane\r\nEND:VCARD\r\n",
		},
		{
			ID:            "bob",
			AddressbookID: "personal",
			VCard: "BEGIN:VCARD\r\nVERSION:3.0\r\n" +
				"FN:Bob\r\nUID:uid-bob\r\nEND:VCARD\r\n",
		},
	}

	objects := TransformCards(cards)

	if len(objects) != 2 {
		t.Fatalf("got %d objects, want 2", len(objects))
	}

	// First card: full contact
	obj := objects[0]
	if obj.ID != "uid-jane" {
		t.Errorf("obj[0].ID = %q, want uid-jane", obj.ID)
	}
	if obj.Type != "contact" {
		t.Errorf("obj[0].Type = %q, want contact", obj.Type)
	}
	if obj.Metadata["name"] != "Jane Doe" {
		t.Errorf("obj[0].Metadata[name] = %v, want Jane Doe",
			obj.Metadata["name"])
	}
	if obj.Metadata["email"] != "jane@acme.com" {
		t.Errorf("obj[0].Metadata[email] = %v, want jane@acme.com",
			obj.Metadata["email"])
	}
	if obj.Metadata["org"] != "Acme" {
		t.Errorf("obj[0].Metadata[org] = %v, want Acme",
			obj.Metadata["org"])
	}

	// Check tags include org and role
	wantTags := map[string]bool{
		"source:cardamum": true,
		"org:Acme":        true,
		"role:CTO":        true,
	}
	for _, tag := range obj.Tags {
		delete(wantTags, tag)
	}
	if len(wantTags) > 0 {
		t.Errorf("missing tags: %v", wantTags)
	}

	// Second card: minimal contact (uses UID for ID)
	obj2 := objects[1]
	if obj2.ID != "uid-bob" {
		t.Errorf("obj[1].ID = %q, want uid-bob", obj2.ID)
	}
	if obj2.Metadata["name"] != "Bob" {
		t.Errorf("obj[1].Metadata[name] = %v, want Bob",
			obj2.Metadata["name"])
	}
}

func TestTransformCardFallbackID(t *testing.T) {
	c := card{
		ID:            "fallback-id",
		AddressbookID: "test",
		VCard:         "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:No UID\r\nEND:VCARD\r\n",
	}
	objects := TransformCards([]card{c})
	if objects[0].ID != "fallback-id" {
		t.Errorf("expected fallback to card.ID, got %q", objects[0].ID)
	}
}

func TestBuildContent(t *testing.T) {
	got := buildContent("Jane", "Acme", "CTO", "j@a.com", "Note text")
	if got == "" {
		t.Fatal("expected non-empty content")
	}
	for _, want := range []string{"Jane", "CTO", "Acme", "j@a.com", "Note text"} {
		if !contains(got, want) {
			t.Errorf("content missing %q: %s", want, got)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
