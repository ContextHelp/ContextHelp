// Package cardamum ingests contacts from an addressbook through the
// cardamum CLI.
//
// A contact's details stay out of its text (ADR-080 §5.2, decision
// D11): email, phone and address live in the metadata only, the vCard
// NOTE is its own graph node classed personal, and the stored text is
// the card the contact renderer builds from the identity fields.
package cardamum

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/ingest"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// card mirrors the JSON output of `cardamum cards list --json`.
type card struct {
	ID            string `json:"id"`
	AddressbookID string `json:"addressbook_id"`
	VCard         string `json:"vcard"`
}

// adapterName names the adapter after the cardamum CLI it runs, which is
// also the default binary.
const adapterName = "cardamum"

// Adapter fetches contacts from cardamum.
type Adapter struct {
	binary      string
	addressbook string
	account     string
}

// Option configures the cardamum adapter.
type Option func(*Adapter)

// WithBinary overrides the cardamum binary path.
func WithBinary(path string) Option {
	return func(a *Adapter) { a.binary = path }
}

// WithAccount sets the cardamum account name.
func WithAccount(name string) Option {
	return func(a *Adapter) { a.account = name }
}

// New creates a cardamum adapter for the given addressbook.
func New(addressbook string, opts ...Option) *Adapter {
	a := &Adapter{
		binary:      adapterName,
		addressbook: addressbook,
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

func (a *Adapter) Name() string { return adapterName }

func (a *Adapter) Fetch(ctx context.Context) ([]ingest.Object, error) {
	cards, err := a.listCards(ctx)
	if err != nil {
		return nil, err
	}
	return TransformCards(cards), nil
}

func (a *Adapter) listCards(ctx context.Context) ([]card, error) {
	args := []string{"cards", "list", "--json", a.addressbook}
	if a.account != "" {
		args = append([]string{"-a", a.account}, args...)
	}

	// #nosec G204 -- operator-configured binary, no shell; captured content never reaches argv. a.binary defaults to "cardamum".
	cmd := exec.CommandContext(ctx, a.binary, args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("cardamum: %w", err)
	}

	var cards []card
	if err := json.Unmarshal(out, &cards); err != nil {
		return nil, fmt.Errorf("cardamum: parse: %w", err)
	}
	return cards, nil
}

// TransformCards converts raw cardamum cards into ingest.Objects.
// Exported for testing.
func TransformCards(cards []card) []ingest.Object {
	objects := make([]ingest.Object, 0, len(cards))
	for _, c := range cards {
		obj := transformCard(c)
		objects = append(objects, obj)
	}
	return objects
}

// transformCard builds the contact: identity fields and details in the
// metadata, the card text from the identity fields only, and the graph
// with the card section plus the note as a personal section.
func transformCard(c card) ingest.Object {
	uid := extractVCardField(c.VCard, "UID")
	if uid == "" {
		uid = c.ID
	}

	fn := extractVCardField(c.VCard, "FN")
	org := extractVCardField(c.VCard, "ORG")
	role := extractVCardField(c.VCard, "TITLE")
	email := extractVCardField(c.VCard, "EMAIL")
	phone := extractVCardField(c.VCard, "TEL")
	address, city := extractVCardAddress(c.VCard)
	note := extractVCardField(c.VCard, "NOTE")

	tags := []string{"source:cardamum"}
	if org != "" {
		tags = append(tags, "org:"+org)
	}
	if role != "" {
		tags = append(tags, "role:"+role)
	}

	meta := map[string]any{
		"addressbook": c.AddressbookID,
	}
	for key, value := range map[string]string{
		"name": fn, "org": org, "role": role, "city": city,
		"email": email, "phone": phone, "address": address,
	} {
		if value != "" {
			meta[key] = value
		}
	}

	// The renderer reads the same fields the object will carry.
	draft := &storage.KnowledgeObject{Type: projection.TypeContact, Metadata: meta}
	content := projection.RenderContact(draft, projection.ContactCardFields)

	return ingest.Object{
		ID:       uid,
		Type:     projection.TypeContact,
		Content:  content,
		Tags:     tags,
		Metadata: meta,
		Graph:    contactGraph(content, note),
	}
}

// contactGraph holds the card text as a section the owner's index
// carries, and the note as a section classed personal, so it never
// reaches a viewer whose grant excludes that class. Node IDs are
// assigned by the ingest runner once the object ID is minted.
func contactGraph(content, note string) *storage.ObjectGraph {
	g := &storage.ObjectGraph{}
	if content != "" {
		g.Nodes = append(g.Nodes, storage.GraphNode{
			NodeType: storage.NodeTypeSection, Label: projection.ContactCardLabel, Content: content, Order: len(g.Nodes),
		})
	}
	if note != "" {
		n := storage.GraphNode{NodeType: storage.NodeTypeSection, Label: projection.ContactNoteLabel, Content: note, Order: len(g.Nodes)}
		storage.SetNodeClasses(&n, []string{storage.ClassPersonal})
		g.Nodes = append(g.Nodes, n)
	}
	if len(g.Nodes) == 0 {
		return nil
	}
	return g
}

// unfold joins the continuation lines of raw vCard text (a line
// starting with a space or tab continues the previous one).
func unfold(vcard string) string {
	unfolded := strings.ReplaceAll(vcard, "\r\n ", "")
	unfolded = strings.ReplaceAll(unfolded, "\r\n\t", "")
	return unfolded
}

// rawVCardField returns the first value of field in the vCard text,
// before unescaping, with its parameters ("TEL;TYPE=CELL:…") skipped.
func rawVCardField(vcard, field string) (string, bool) {
	for _, line := range strings.Split(unfold(vcard), "\r\n") {
		line = strings.TrimSpace(line)
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name, _, _ = strings.Cut(name, ";")
		if strings.EqualFold(name, field) {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}

// unescape reverses the vCard backslash escapes of one text value.
func unescape(val string) string {
	val = strings.ReplaceAll(val, "\\,", ",")
	val = strings.ReplaceAll(val, "\\n", "\n")
	val = strings.ReplaceAll(val, "\\N", "\n")
	val = strings.ReplaceAll(val, "\\;", ";")
	return strings.TrimSpace(val)
}

// extractVCardField pulls a single-value field from raw vCard text.
// Handles line folding and field parameters.
func extractVCardField(vcard, field string) string {
	raw, ok := rawVCardField(vcard, field)
	if !ok {
		return ""
	}
	return unescape(raw)
}

// extractVCardAddress reads the first ADR: its seven components (post
// office box, extended address, street, locality, region, postal code,
// country) joined with ", " where present, and the locality as the
// city.
func extractVCardAddress(vcard string) (address, city string) {
	raw, ok := rawVCardField(vcard, "ADR")
	if !ok {
		return "", ""
	}
	parts := splitStructured(raw)
	var present []string
	for _, p := range parts {
		if p != "" {
			present = append(present, p)
		}
	}
	if len(parts) > 3 {
		city = parts[3]
	}
	return strings.Join(present, ", "), city
}

// splitStructured splits a structured value on its unescaped
// semicolons and unescapes each component.
func splitStructured(raw string) []string {
	var parts []string
	var cur strings.Builder
	for i := 0; i < len(raw); i++ {
		switch {
		case raw[i] == '\\' && i+1 < len(raw):
			cur.WriteByte(raw[i])
			cur.WriteByte(raw[i+1])
			i++
		case raw[i] == ';':
			parts = append(parts, unescape(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(raw[i])
		}
	}
	return append(parts, unescape(cur.String()))
}
