package slack

import (
	"strings"
	"testing"
	"time"
)

const goldenSlackDay = `[
  {
    "type": "message",
    "user": "U01ABC123",
    "username": "alice",
    "text": "Hello team!",
    "ts": "1719830400.000100",
    "reactions": [
      {"name": "thumbsup", "count": 3},
      {"name": "rocket", "count": 1}
    ]
  },
  {
    "type": "message",
    "user": "U01XYZ789",
    "username": "bob",
    "text": "Hi Alice!",
    "ts": "1719830460.000200",
    "thread_ts": "1719830400.000100"
  },
  {
    "type": "message",
    "subtype": "channel_join",
    "user": "U09NEW",
    "text": "<@U09NEW> joined the channel",
    "ts": "1719830500.000300"
  },
  {
    "type": "message",
    "user": "U01ABC123",
    "username": "alice",
    "text": "Message with file",
    "ts": "1719830600.000400",
    "files": [{"name": "report.pdf", "id": "F01"}]
  }
]`

func TestParseMessagesJSON_Basic(t *testing.T) {
	msgs, err := ParseMessagesJSON([]byte(goldenSlackDay), "general")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// channel_join should be skipped, so 3 messages expected.
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	m0 := msgs[0]
	if m0.ExternalID != "general:1719830400.000100" {
		t.Errorf("ExternalID: got %q", m0.ExternalID)
	}
	if m0.ChannelName != "general" {
		t.Errorf("ChannelName: got %q", m0.ChannelName)
	}
	if m0.Username != "alice" {
		t.Errorf("Username: got %q", m0.Username)
	}
	if m0.Text != "Hello team!" {
		t.Errorf("Text: got %q", m0.Text)
	}
	if m0.IsReply {
		t.Error("first message should not be a reply")
	}
	if len(m0.Reactions) != 2 {
		t.Errorf("Reactions: got %v", m0.Reactions)
	}
	if m0.Source != "slack:general" {
		t.Errorf("Source: got %q", m0.Source)
	}

	// Second message is a thread reply.
	m1 := msgs[1]
	if !m1.IsReply {
		t.Error("second message should be a reply")
	}
	if m1.ThreadTS != "1719830400.000100" {
		t.Errorf("ThreadTS: got %q", m1.ThreadTS)
	}

	// Third message has a file attachment.
	m2 := msgs[2]
	if len(m2.Files) != 1 || m2.Files[0] != "report.pdf" {
		t.Errorf("Files: got %v", m2.Files)
	}
}

func TestParseMessagesJSON_Timestamps(t *testing.T) {
	msgs, err := ParseMessagesJSON([]byte(goldenSlackDay), "general")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// 1719830400 seconds = 2024-07-01T08:00:00Z
	expected := time.Unix(1719830400, 0).UTC()
	if !msgs[0].Timestamp.Equal(expected) {
		t.Errorf("Timestamp: got %v, want %v", msgs[0].Timestamp, expected)
	}
}

func TestParseMessagesJSON_Empty(t *testing.T) {
	msgs, err := ParseMessagesJSON([]byte(`[]`), "general")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestParseMessagesJSON_InvalidJSON(t *testing.T) {
	_, err := ParseMessagesJSON([]byte(`not json`), "general")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseMessagesJSON_ChannelJoinFiltered(t *testing.T) {
	input := `[
    {"type":"message","subtype":"channel_join","user":"U01","text":"joined","ts":"1719830400.001"},
    {"type":"message","subtype":"channel_leave","user":"U02","text":"left","ts":"1719830401.001"},
    {"type":"message","user":"U03","text":"real message","ts":"1719830402.001"}
  ]`
	msgs, err := ParseMessagesJSON([]byte(input), "random")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message after filtering, got %d", len(msgs))
	}
	if msgs[0].Text != "real message" {
		t.Errorf("Text: got %q", msgs[0].Text)
	}
}

func TestParseMessagesJSON_AttachmentText(t *testing.T) {
	input := `[{
    "type": "message",
    "user": "U01",
    "text": "main text",
    "ts": "1719830400.001",
    "attachments": [{"text": "attachment body"}]
  }]`
	msgs, err := ParseMessagesJSON([]byte(input), "ch")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message")
	}
	if !strings.Contains(msgs[0].Text, "attachment body") {
		t.Errorf("expected attachment text merged, got %q", msgs[0].Text)
	}
}

func TestRenderContent(t *testing.T) {
	m := Message{
		ExternalID:  "general:1719830400.000100",
		ChannelName: "general",
		UserID:      "U01",
		Username:    "alice",
		Text:        "Hello team!",
		Timestamp:   time.Unix(1719830400, 0).UTC(),
		Reactions:   []string{"thumbsup", "rocket"},
	}

	rendered := RenderContent(m)
	if !strings.Contains(rendered, "# Slack message") {
		t.Error("expected header in rendered output")
	}
	if !strings.Contains(rendered, "#general") {
		t.Error("expected channel name in rendered output")
	}
	if !strings.Contains(rendered, "alice") {
		t.Error("expected username in rendered output")
	}
	if !strings.Contains(rendered, "Hello team!") {
		t.Error("expected message text in rendered output")
	}
	if !strings.Contains(rendered, "thumbsup") {
		t.Error("expected reactions in rendered output")
	}
}

func TestRenderContent_Reply(t *testing.T) {
	m := Message{
		ChannelName: "dev",
		Username:    "bob",
		Text:        "See my reply",
		Timestamp:   time.Unix(1719830460, 0).UTC(),
		IsReply:     true,
	}
	rendered := RenderContent(m)
	if !strings.Contains(rendered, "thread reply") {
		t.Error("expected thread reply label in rendered output")
	}
}

func TestParseSlackTimestamp(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
		wantSec int64
	}{
		{"1719830400.000100", false, 1719830400},
		{"1719830460.000200", false, 1719830460},
		{"", true, 0},
		{"invalid", true, 0},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseSlackTimestamp(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Unix() != tc.wantSec {
				t.Errorf("got unix %d, want %d", got.Unix(), tc.wantSec)
			}
		})
	}
}

func TestParseMessagesJSON_Dedup_ExternalIDs(t *testing.T) {
	msgs, err := ParseMessagesJSON([]byte(goldenSlackDay), "general")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	seen := make(map[string]bool)
	for _, m := range msgs {
		if seen[m.ExternalID] {
			t.Errorf("duplicate ExternalID: %q", m.ExternalID)
		}
		seen[m.ExternalID] = true
	}
}
