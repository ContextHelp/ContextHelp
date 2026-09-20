package himalaya

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Envelope mirrors the JSON output of `himalaya envelope list -o json`.
type Envelope struct {
	ID            string   `json:"id"`
	Flags         []string `json:"flags"`
	Subject       string   `json:"subject"`
	From          Address  `json:"from"`
	To            Address  `json:"to"`
	Date          string   `json:"date"`
	HasAttachment bool     `json:"has_attachment"`
}

// Address represents an email address with optional display name.
type Address struct {
	Name *string `json:"name"`
	Addr string  `json:"addr"`
}

// String returns "Name <addr>" or just addr.
func (a Address) String() string {
	if a.Name != nil && *a.Name != "" {
		return *a.Name + " <" + a.Addr + ">"
	}
	return a.Addr
}

// Message represents a parsed email with envelope + body + headers.
type Message struct {
	EnvelopeID string
	MessageID  string
	InReplyTo  string
	References []string
	From       Address
	To         Address
	Subject    string
	Body       string
	Date       time.Time
	Flags      []string
	ThreadID   string // computed from In-Reply-To chains
}

// Thread groups messages sharing the same root Message-ID.
type Thread struct {
	ID       string
	Subject  string
	Messages []Message
}

// ParseEnvelopes unmarshals himalaya envelope list JSON output.
func ParseEnvelopes(data []byte) ([]Envelope, error) {
	var envs []Envelope
	if err := json.Unmarshal(data, &envs); err != nil {
		return nil, fmt.Errorf("parse envelopes: %w", err)
	}
	return envs, nil
}

// ParseHeaders extracts Message-Id, In-Reply-To, References from
// a raw himalaya message read output (with headers).
func ParseHeaders(raw string) (messageID, inReplyTo string, refs []string) {
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "message-id:") {
			messageID = extractAngleBracket(
				strings.TrimSpace(line[len("Message-Id:"):]),
			)
		} else if strings.HasPrefix(lower, "in-reply-to:") {
			inReplyTo = extractAngleBracket(
				strings.TrimSpace(line[len("In-Reply-To:"):]),
			)
		} else if strings.HasPrefix(lower, "references:") {
			raw := strings.TrimSpace(line[len("References:"):])
			for _, ref := range strings.Fields(raw) {
				refs = append(refs, extractAngleBracket(ref))
			}
		}
	}
	return
}

// extractAngleBracket strips < > wrappers from a message ID.
func extractAngleBracket(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "<")
	s = strings.TrimSuffix(s, ">")
	return s
}

// ParseBody extracts the body portion from himalaya message read output.
// Headers end at the first blank line.
func ParseBody(raw string) string {
	idx := strings.Index(raw, "\n\n")
	if idx < 0 {
		return raw
	}
	return strings.TrimSpace(raw[idx+2:])
}

// BuildMessage combines envelope metadata with raw message text.
func BuildMessage(env Envelope, rawMessage string) Message {
	msgID, inReplyTo, refs := ParseHeaders(rawMessage)
	body := ParseBody(rawMessage)
	date := parseDate(env.Date)

	return Message{
		EnvelopeID: env.ID,
		MessageID:  msgID,
		InReplyTo:  inReplyTo,
		References: refs,
		From:       env.From,
		To:         env.To,
		Subject:    env.Subject,
		Body:       body,
		Date:       date,
		Flags:      env.Flags,
	}
}

// GroupThreads assigns thread IDs and groups messages into threads
// using In-Reply-To / References chains.
func GroupThreads(msgs []Message) []Thread {
	// Union-find: map message-ID -> root message-ID.
	parent := make(map[string]string)

	var find func(string) string
	find = func(id string) string {
		if p, ok := parent[id]; ok && p != id {
			parent[id] = find(p)
			return parent[id]
		}
		return id
	}
	union := func(a, b string) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}

	// Init each message as its own root.
	for i := range msgs {
		m := &msgs[i]
		if m.MessageID == "" {
			// Synthesize ID from envelope for messages without Message-ID.
			m.MessageID = "himalaya:" + m.EnvelopeID
		}
		parent[m.MessageID] = m.MessageID
	}

	// Link via In-Reply-To and References.
	for _, m := range msgs {
		if m.InReplyTo != "" {
			if _, ok := parent[m.InReplyTo]; !ok {
				parent[m.InReplyTo] = m.InReplyTo
			}
			union(m.InReplyTo, m.MessageID)
		}
		for _, ref := range m.References {
			if _, ok := parent[ref]; !ok {
				parent[ref] = ref
			}
			union(ref, m.MessageID)
		}
	}

	// Assign thread IDs.
	threadMap := make(map[string]*Thread)
	for i := range msgs {
		root := find(msgs[i].MessageID)
		tid := threadIDFromRoot(root)
		msgs[i].ThreadID = tid

		th, ok := threadMap[tid]
		if !ok {
			th = &Thread{ID: tid, Subject: msgs[i].Subject}
			threadMap[tid] = th
		}
		th.Messages = append(th.Messages, msgs[i])
	}

	// Sort threads by earliest message date.
	threads := make([]Thread, 0, len(threadMap))
	for _, th := range threadMap {
		sort.Slice(th.Messages, func(i, j int) bool {
			return th.Messages[i].Date.Before(th.Messages[j].Date)
		})
		if len(th.Messages) > 0 {
			th.Subject = th.Messages[0].Subject
		}
		threads = append(threads, *th)
	}
	sort.Slice(threads, func(i, j int) bool {
		if len(threads[i].Messages) == 0 {
			return true
		}
		if len(threads[j].Messages) == 0 {
			return false
		}
		return threads[i].Messages[0].Date.Before(threads[j].Messages[0].Date)
	})

	return threads
}

// threadIDFromRoot produces a stable, short thread ID from a root
// message ID via SHA-256 truncation.
func threadIDFromRoot(root string) string {
	h := sha256.Sum256([]byte(root))
	return fmt.Sprintf("thread_%x", h[:8])
}

// parseDate attempts multiple date formats himalaya may emit.
func parseDate(s string) time.Time {
	formats := []string{
		"2006-01-02 15:04-07:00",
		"2006-01-02 15:04+00:00",
		time.RFC3339,
		time.RFC1123Z,
		time.RFC1123,
		"Mon, 02 Jan 2006 15:04:05 -0700",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
