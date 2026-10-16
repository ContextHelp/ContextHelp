package projection

import (
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Contacts are the first structured type (ADR-080 §5.2): their text is
// rebuilt from fields rather than masked, so a field that ingest keeps
// out of the text (email, phone, address, the vCard note) can never
// reach a viewer whose policy does not project it.

// TypeContact is the object type of a contact.
const TypeContact = "contact"

// Contact field names: the vocabulary of a share policy's
// fields.contact list and of RenderContact.
const (
	// ContactTitle is the person's name (metadata.name, else the first
	// summary).
	ContactTitle   = "title"
	ContactOrg     = "org"
	ContactRole    = "role"
	ContactCity    = "city"
	ContactEmail   = "email"
	ContactPhone   = "phone"
	ContactAddress = "address"
	// ContactTags renders the object's tag labels.
	ContactTags = "tags"
	// ContactNote is the vCard NOTE, stored as a section node labelled
	// note and classed personal.
	ContactNote = "note"
)

// ContactNoteLabel is the label of the graph node that holds a contact's
// note.
const ContactNoteLabel = "note"

// ContactCardLabel is the label of the graph node that holds the card
// text ingest indexes for the owner.
const ContactCardLabel = "card"

// ContactFields lists every contact field in card order.
var ContactFields = []string{ContactTitle, ContactRole, ContactOrg, ContactCity, ContactEmail, ContactPhone, ContactAddress, ContactTags, ContactNote}

// ContactCardFields are the fields ingest puts in the stored card text:
// the identity of the person without any detail a policy may withhold.
var ContactCardFields = []string{ContactTitle, ContactRole, ContactOrg, ContactCity}

// ContactSearchableMinimum is the field set that enters the masked
// projection of a contact (decision D21): what any viewer may match on.
// Fields outside it (email, phone, address, note) never reach the
// masked index.
var ContactSearchableMinimum = []string{ContactTitle, ContactOrg, ContactRole, ContactTags, ContactCity}

// ContactFieldClasses are the classes the contact type declares per
// field (ADR-080 §3): contact details and the note.
var ContactFieldClasses = map[string]string{
	ContactEmail:   storage.ClassContactDetails,
	ContactPhone:   storage.ClassContactDetails,
	ContactAddress: storage.ClassContactDetails,
	ContactNote:    storage.ClassPersonal,
}

// ContactField returns the value of field for ko: a string of its
// metadata for the scalar fields, the tag labels joined for tags, the
// note node's content for note, "" when absent.
func ContactField(ko *storage.KnowledgeObject, field string) string {
	if ko == nil {
		return ""
	}
	switch field {
	case ContactTitle:
		if s := metaString(ko, "name"); s != "" {
			return s
		}
		if len(ko.Summaries) > 0 {
			return strings.TrimSpace(ko.Summaries[0])
		}
		return ""
	case ContactOrg, ContactRole, ContactCity, ContactEmail, ContactPhone, ContactAddress:
		return metaString(ko, field)
	case ContactTags:
		labels := make([]string, 0, len(ko.Tags))
		for _, t := range ko.Tags {
			if t.Label != "" {
				labels = append(labels, t.Label)
			}
		}
		return strings.Join(labels, ", ")
	case ContactNote:
		return contactNote(ko)
	}
	return ""
}

func metaString(ko *storage.KnowledgeObject, key string) string {
	s, _ := ko.Metadata[key].(string)
	return strings.TrimSpace(s)
}

// contactNote joins the contents of the section nodes labelled note.
func contactNote(ko *storage.KnowledgeObject) string {
	if ko.Graph == nil {
		return ""
	}
	var parts []string
	for _, n := range ko.Graph.Nodes {
		if n.NodeType == storage.NodeTypeSection && n.Label == ContactNoteLabel && strings.TrimSpace(n.Content) != "" {
			parts = append(parts, strings.TrimSpace(n.Content))
		}
	}
	return strings.Join(parts, "\n")
}

// RenderContact builds a contact's text from the fields in fields that
// have a value: the identity line "Name | Role @ Org", then one line per
// detail ("City", "Email: …", "Phone: …", "Address: …", "Tags: …") and
// the note last. Fields not listed are absent from the text, whatever
// the object holds. A contact with no listed field renders "".
func RenderContact(ko *storage.KnowledgeObject, fields []string) string {
	has := func(f string) bool {
		for _, x := range fields {
			if x == f {
				return true
			}
		}
		return false
	}
	value := func(f string) string {
		if !has(f) {
			return ""
		}
		return ContactField(ko, f)
	}
	var lines []string
	var head strings.Builder
	head.WriteString(value(ContactTitle))
	if role := value(ContactRole); role != "" {
		if head.Len() > 0 {
			head.WriteString(" | ")
		}
		head.WriteString(role)
	}
	if org := value(ContactOrg); org != "" {
		if head.Len() > 0 {
			head.WriteString(" @ ")
		}
		head.WriteString(org)
	}
	if head.Len() > 0 {
		lines = append(lines, head.String())
	}
	if city := value(ContactCity); city != "" {
		lines = append(lines, city)
	}
	for _, f := range []struct{ field, label string }{
		{ContactEmail, "Email"}, {ContactPhone, "Phone"}, {ContactAddress, "Address"}, {ContactTags, "Tags"},
	} {
		if v := value(f.field); v != "" {
			lines = append(lines, f.label+": "+v)
		}
	}
	if note := value(ContactNote); note != "" {
		lines = append(lines, note)
	}
	return strings.Join(lines, "\n")
}

func init() {
	RegisterSearchableText(TypeContact, func(ko *storage.KnowledgeObject) (string, bool) {
		return RenderContact(ko, ContactSearchableMinimum), true
	})
}
