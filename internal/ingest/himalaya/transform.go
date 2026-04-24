package himalaya

import (
	"fmt"
	"strings"
	"time"
)

// CtxtObject is the adapter output format per the T-0381 ingestion
// contract. Adapters emit a JSON array of these to stdout; ctxt
// reads, deduplicates by id+source, and indexes.
type CtxtObject struct {
	ID       string            `json:"id"`
	Type     string            `json:"type"`
	Content  string            `json:"content"`
	Tags     []string          `json:"tags"`
	Metadata map[string]string `json:"metadata"`
}

// ToCtxtObject converts a parsed Message into a CtxtObject.
func ToCtxtObject(m Message, source string) CtxtObject {
	tags := buildTags(m)
	meta := buildMetadata(m, source)

	return CtxtObject{
		ID:       stableID(m, source),
		Type:     "email",
		Content:  RenderContent(m),
		Tags:     tags,
		Metadata: meta,
	}
}

// ToCtxtObjects converts a slice of messages.
func ToCtxtObjects(msgs []Message, source string) []CtxtObject {
	objs := make([]CtxtObject, 0, len(msgs))
	for _, m := range msgs {
		objs = append(objs, ToCtxtObject(m, source))
	}
	return objs
}

// stableID produces a deterministic dedup key: source + envelope ID.
func stableID(m Message, source string) string {
	return fmt.Sprintf("himalaya:%s:%s", source, m.EnvelopeID)
}

// buildTags extracts searchable tags from the message.
func buildTags(m Message) []string {
	tags := []string{"source:himalaya", "type:email"}

	if m.From.Addr != "" {
		tags = append(tags, "from:"+m.From.Addr)
	}
	if m.To.Addr != "" {
		tags = append(tags, "to:"+m.To.Addr)
	}
	if m.ThreadID != "" {
		tags = append(tags, "thread:"+m.ThreadID)
	}

	// Extract subject keywords (skip short/stop words).
	for _, kw := range subjectKeywords(m.Subject) {
		tags = append(tags, "subject:"+kw)
	}

	// Contact scope tag for enforcing reply boundaries.
	if m.From.Addr != "" {
		tags = append(tags, "contact:"+normalizeEmail(m.From.Addr))
	}

	return tags
}

// buildMetadata populates the metadata map for ctxt indexing.
func buildMetadata(m Message, source string) map[string]string {
	meta := map[string]string{
		"source":      "himalaya",
		"account":     source,
		"envelope_id": m.EnvelopeID,
	}
	if m.MessageID != "" {
		meta["message_id"] = m.MessageID
	}
	if m.InReplyTo != "" {
		meta["in_reply_to"] = m.InReplyTo
	}
	if m.ThreadID != "" {
		meta["thread_id"] = m.ThreadID
	}
	if !m.Date.IsZero() {
		meta["date"] = m.Date.Format(time.RFC3339)
	}
	if m.From.Addr != "" {
		meta["from"] = m.From.Addr
	}
	if m.To.Addr != "" {
		meta["to"] = m.To.Addr
	}
	meta["subject"] = m.Subject

	return meta
}

// RenderContent formats a message as plain text for indexing.
func RenderContent(m Message) string {
	var sb strings.Builder

	sb.WriteString("# Email")
	if m.InReplyTo != "" {
		sb.WriteString(" (reply)")
	}
	sb.WriteString("\n\n")

	sb.WriteString("**Subject:** ")
	sb.WriteString(m.Subject)
	sb.WriteString("\n")

	sb.WriteString("**From:** ")
	sb.WriteString(m.From.String())
	sb.WriteString("\n")

	sb.WriteString("**To:** ")
	sb.WriteString(m.To.String())
	sb.WriteString("\n")

	if !m.Date.IsZero() {
		sb.WriteString("**Date:** ")
		sb.WriteString(m.Date.Format(time.RFC3339))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(m.Body)

	return sb.String()
}

// subjectKeywords splits subject into lowercase tokens, filtering
// noise like Re:, Fwd:, brackets, and short words.
func subjectKeywords(subject string) []string {
	// Remove Re:/Fwd: prefixes and brackets.
	s := strings.ToLower(subject)
	for _, prefix := range []string{"re:", "fwd:", "fw:"} {
		s = strings.TrimPrefix(s, prefix)
		s = strings.TrimSpace(s)
	}
	// Remove bracketed prefixes like [repo-name].
	if idx := strings.Index(s, "]"); idx >= 0 && strings.HasPrefix(s, "[") {
		s = strings.TrimSpace(s[idx+1:])
	}

	words := strings.FieldsFunc(s, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_')
	})

	var kws []string
	seen := make(map[string]bool)
	for _, w := range words {
		if len(w) < 3 || seen[w] {
			continue
		}
		seen[w] = true
		kws = append(kws, w)
	}
	return kws
}

// normalizeEmail lowercases and trims an email address.
func normalizeEmail(addr string) string {
	return strings.TrimSpace(strings.ToLower(addr))
}
