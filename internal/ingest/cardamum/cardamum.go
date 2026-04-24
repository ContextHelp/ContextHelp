package cardamum

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// card mirrors the JSON output of `cardamum cards list --json`.
type card struct {
	ID            string `json:"id"`
	AddressbookID string `json:"addressbook_id"`
	VCard         string `json:"vcard"`
}

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
		binary:      "cardamum",
		addressbook: addressbook,
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

func (a *Adapter) Name() string { return "cardamum" }

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

func transformCard(c card) ingest.Object {
	uid := extractVCardField(c.VCard, "UID")
	if uid == "" {
		uid = c.ID
	}

	fn := extractVCardField(c.VCard, "FN")
	org := extractVCardField(c.VCard, "ORG")
	title := extractVCardField(c.VCard, "TITLE")
	email := extractVCardField(c.VCard, "EMAIL")
	note := extractVCardField(c.VCard, "NOTE")

	tags := []string{"source:cardamum"}
	if org != "" {
		tags = append(tags, "org:"+org)
	}
	if title != "" {
		tags = append(tags, "role:"+title)
	}

	meta := map[string]any{
		"addressbook": c.AddressbookID,
	}
	if fn != "" {
		meta["name"] = fn
	}
	if email != "" {
		meta["email"] = email
	}
	if org != "" {
		meta["org"] = org
	}
	if title != "" {
		meta["title"] = title
	}

	content := buildContent(fn, org, title, email, note)

	return ingest.Object{
		ID:       uid,
		Type:     "contact",
		Content:  content,
		Tags:     tags,
		Metadata: meta,
	}
}

func buildContent(fn, org, title, email, note string) string {
	var b strings.Builder
	if fn != "" {
		b.WriteString(fn)
	}
	if title != "" {
		b.WriteString(" | " + title)
	}
	if org != "" {
		b.WriteString(" @ " + org)
	}
	if email != "" {
		b.WriteString("\nEmail: " + email)
	}
	if note != "" {
		b.WriteString("\n" + note)
	}
	return b.String()
}

// extractVCardField pulls a single-value field from raw vCard text.
// Handles line folding (continuation lines starting with space).
func extractVCardField(vcard, field string) string {
	// Unfold: continuation lines start with a space or tab
	unfolded := strings.ReplaceAll(vcard, "\r\n ", "")
	unfolded = strings.ReplaceAll(unfolded, "\r\n\t", "")

	prefix := field + ":"
	for _, line := range strings.Split(unfolded, "\r\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			val := strings.TrimPrefix(line, prefix)
			// unescape vCard backslash escapes
			val = strings.ReplaceAll(val, "\\,", ",")
			val = strings.ReplaceAll(val, "\\n", "\n")
			val = strings.ReplaceAll(val, "\\;", ";")
			return strings.TrimSpace(val)
		}
	}
	return ""
}
