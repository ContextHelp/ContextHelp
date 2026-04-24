package himalaya

import (
	"testing"
	"time"
)

const goldenEnvelopes = `[
  {
    "id": "100",
    "flags": ["Seen"],
    "subject": "Project kickoff",
    "from": {"name": "Alice", "addr": "alice@example.com"},
    "to": {"name": "Team", "addr": "team@example.com"},
    "date": "2024-07-01 10:00-07:00",
    "has_attachment": false
  },
  {
    "id": "101",
    "flags": [],
    "subject": "Re: Project kickoff",
    "from": {"name": "Bob", "addr": "bob@example.com"},
    "to": {"name": "Team", "addr": "team@example.com"},
    "date": "2024-07-01 11:30-07:00",
    "has_attachment": false
  },
  {
    "id": "102",
    "flags": [],
    "subject": "Unrelated topic",
    "from": {"name": null, "addr": "charlie@example.com"},
    "to": {"name": "Alice", "addr": "alice@example.com"},
    "date": "2024-07-02 09:00+00:00",
    "has_attachment": true
  }
]`

func TestParseEnvelopes(t *testing.T) {
	envs, err := ParseEnvelopes([]byte(goldenEnvelopes))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(envs) != 3 {
		t.Fatalf("expected 3 envelopes, got %d", len(envs))
	}

	e0 := envs[0]
	if e0.ID != "100" {
		t.Errorf("ID: got %q", e0.ID)
	}
	if e0.Subject != "Project kickoff" {
		t.Errorf("Subject: got %q", e0.Subject)
	}
	if e0.From.Addr != "alice@example.com" {
		t.Errorf("From.Addr: got %q", e0.From.Addr)
	}
	if len(e0.Flags) != 1 || e0.Flags[0] != "Seen" {
		t.Errorf("Flags: got %v", e0.Flags)
	}

	// Null name field.
	e2 := envs[2]
	if e2.From.Name != nil {
		t.Errorf("expected nil Name, got %q", *e2.From.Name)
	}
	if e2.From.String() != "charlie@example.com" {
		t.Errorf("String(): got %q", e2.From.String())
	}
}

func TestParseEnvelopes_Empty(t *testing.T) {
	envs, err := ParseEnvelopes([]byte(`[]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(envs) != 0 {
		t.Errorf("expected 0, got %d", len(envs))
	}
}

func TestParseEnvelopes_InvalidJSON(t *testing.T) {
	_, err := ParseEnvelopes([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseHeaders(t *testing.T) {
	raw := "Message-Id: <abc123@example.com>\n" +
		"In-Reply-To: <parent@example.com>\n" +
		"References: <root@example.com> <parent@example.com>\n" +
		"From: Alice <alice@example.com>\n" +
		"Subject: Test\n\nBody here"

	msgID, inReplyTo, refs := ParseHeaders(raw)
	if msgID != "abc123@example.com" {
		t.Errorf("MessageID: got %q", msgID)
	}
	if inReplyTo != "parent@example.com" {
		t.Errorf("InReplyTo: got %q", inReplyTo)
	}
	if len(refs) != 2 {
		t.Fatalf("References: expected 2, got %d", len(refs))
	}
	if refs[0] != "root@example.com" {
		t.Errorf("refs[0]: got %q", refs[0])
	}
}

func TestParseHeaders_NoHeaders(t *testing.T) {
	msgID, inReplyTo, refs := ParseHeaders("Just a body\nno headers")
	if msgID != "" || inReplyTo != "" || len(refs) != 0 {
		t.Errorf("expected empty, got %q %q %v", msgID, inReplyTo, refs)
	}
}

func TestParseBody(t *testing.T) {
	raw := "From: alice@example.com\nSubject: Test\n\nHello world\nSecond line"
	body := ParseBody(raw)
	if body != "Hello world\nSecond line" {
		t.Errorf("Body: got %q", body)
	}
}

func TestParseBody_NoHeaders(t *testing.T) {
	raw := "Just body text"
	body := ParseBody(raw)
	if body != "Just body text" {
		t.Errorf("Body: got %q", body)
	}
}

func TestBuildMessage(t *testing.T) {
	env := Envelope{
		ID:      "100",
		Subject: "Test subject",
		From:    Address{Name: strPtr("Alice"), Addr: "alice@example.com"},
		To:      Address{Name: strPtr("Bob"), Addr: "bob@example.com"},
		Date:    "2024-07-01 10:00-07:00",
	}
	raw := "Message-Id: <msg100@example.com>\n" +
		"In-Reply-To: <msg99@example.com>\n" +
		"From: Alice <alice@example.com>\n" +
		"Subject: Test subject\n\nHello Bob"

	m := BuildMessage(env, raw)
	if m.EnvelopeID != "100" {
		t.Errorf("EnvelopeID: got %q", m.EnvelopeID)
	}
	if m.MessageID != "msg100@example.com" {
		t.Errorf("MessageID: got %q", m.MessageID)
	}
	if m.InReplyTo != "msg99@example.com" {
		t.Errorf("InReplyTo: got %q", m.InReplyTo)
	}
	if m.Body != "Hello Bob" {
		t.Errorf("Body: got %q", m.Body)
	}
	if m.Date.IsZero() {
		t.Error("Date should be parsed")
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		input string
		year  int
	}{
		{"2024-07-01 10:00-07:00", 2024},
		{"2024-07-02 09:00+00:00", 2024},
		{"2024-07-01T10:00:00-07:00", 2024},
		{"", 0},
		{"invalid", 0},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := parseDate(tc.input)
			if tc.year == 0 && !got.IsZero() {
				t.Errorf("expected zero time for %q", tc.input)
			}
			if tc.year != 0 && got.Year() != tc.year {
				t.Errorf("year: got %d, want %d", got.Year(), tc.year)
			}
		})
	}
}

func TestAddressString(t *testing.T) {
	a1 := Address{Name: strPtr("Alice"), Addr: "alice@example.com"}
	if a1.String() != "Alice <alice@example.com>" {
		t.Errorf("got %q", a1.String())
	}

	a2 := Address{Addr: "bob@example.com"}
	if a2.String() != "bob@example.com" {
		t.Errorf("got %q", a2.String())
	}

	a3 := Address{Name: strPtr(""), Addr: "c@example.com"}
	if a3.String() != "c@example.com" {
		t.Errorf("got %q", a3.String())
	}
}

func strPtr(s string) *string { return &s }

// --- Thread grouping tests ---

func TestGroupThreads_SingleThread(t *testing.T) {
	msgs := []Message{
		{
			EnvelopeID: "1",
			MessageID:  "root@example.com",
			Subject:    "Hello",
			Date:       time.Unix(1000, 0),
		},
		{
			EnvelopeID: "2",
			MessageID:  "reply1@example.com",
			InReplyTo:  "root@example.com",
			Subject:    "Re: Hello",
			Date:       time.Unix(2000, 0),
		},
		{
			EnvelopeID: "3",
			MessageID:  "reply2@example.com",
			InReplyTo:  "reply1@example.com",
			References: []string{"root@example.com", "reply1@example.com"},
			Subject:    "Re: Hello",
			Date:       time.Unix(3000, 0),
		},
	}

	threads := GroupThreads(msgs)
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(threads))
	}
	if len(threads[0].Messages) != 3 {
		t.Errorf("expected 3 messages in thread, got %d",
			len(threads[0].Messages))
	}

	// All messages share the same thread ID.
	tid := threads[0].Messages[0].ThreadID
	for _, m := range threads[0].Messages {
		if m.ThreadID != tid {
			t.Errorf("inconsistent thread ID: %q vs %q", m.ThreadID, tid)
		}
	}
}

func TestGroupThreads_MultipleThreads(t *testing.T) {
	msgs := []Message{
		{
			EnvelopeID: "1",
			MessageID:  "thread1@example.com",
			Subject:    "Topic A",
			Date:       time.Unix(1000, 0),
		},
		{
			EnvelopeID: "2",
			MessageID:  "thread2@example.com",
			Subject:    "Topic B",
			Date:       time.Unix(2000, 0),
		},
		{
			EnvelopeID: "3",
			MessageID:  "reply-a@example.com",
			InReplyTo:  "thread1@example.com",
			Subject:    "Re: Topic A",
			Date:       time.Unix(3000, 0),
		},
	}

	threads := GroupThreads(msgs)
	if len(threads) != 2 {
		t.Fatalf("expected 2 threads, got %d", len(threads))
	}

	// Find the thread with 2 messages.
	var multi *Thread
	for i := range threads {
		if len(threads[i].Messages) == 2 {
			multi = &threads[i]
		}
	}
	if multi == nil {
		t.Fatal("expected one thread with 2 messages")
	}
}

func TestGroupThreads_NoMessageID(t *testing.T) {
	msgs := []Message{
		{EnvelopeID: "1", Subject: "No ID", Date: time.Unix(1000, 0)},
		{EnvelopeID: "2", Subject: "Also no ID", Date: time.Unix(2000, 0)},
	}

	threads := GroupThreads(msgs)
	if len(threads) != 2 {
		t.Fatalf("expected 2 threads (no linking), got %d", len(threads))
	}
}

func TestGroupThreads_ReferencesOnly(t *testing.T) {
	msgs := []Message{
		{
			EnvelopeID: "1",
			MessageID:  "a@example.com",
			Subject:    "Start",
			Date:       time.Unix(1000, 0),
		},
		{
			EnvelopeID: "2",
			MessageID:  "b@example.com",
			References: []string{"a@example.com"},
			Subject:    "Re: Start",
			Date:       time.Unix(2000, 0),
		},
	}

	threads := GroupThreads(msgs)
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread via References, got %d", len(threads))
	}
}

func TestGroupThreads_Ordering(t *testing.T) {
	msgs := []Message{
		{
			EnvelopeID: "2",
			MessageID:  "late@example.com",
			InReplyTo:  "early@example.com",
			Date:       time.Unix(5000, 0),
		},
		{
			EnvelopeID: "1",
			MessageID:  "early@example.com",
			Date:       time.Unix(1000, 0),
		},
	}

	threads := GroupThreads(msgs)
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(threads))
	}
	// Messages within thread sorted by date.
	if threads[0].Messages[0].EnvelopeID != "1" {
		t.Errorf("expected earliest message first, got %q",
			threads[0].Messages[0].EnvelopeID)
	}
}
