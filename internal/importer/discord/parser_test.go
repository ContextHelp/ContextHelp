package discord

import (
	"strings"
	"testing"
	"time"
)

const goldenDiscordExport = `{
  "guild": {
    "id": "111111111111111111",
    "name": "My Server"
  },
  "channel": {
    "id": "222222222222222222",
    "name": "general",
    "type": "GuildTextChat"
  },
  "messages": [
    {
      "id": "333333333333333333",
      "type": "Default",
      "timestamp": "2024-07-01T08:00:00.000+00:00",
      "content": "Hello everyone!",
      "author": {
        "id": "444444444444444444",
        "name": "alice",
        "discriminator": "0001",
        "isBot": false
      },
      "attachments": [],
      "embeds": [],
      "reactions": [
        {"emoji": {"name": "thumbsup"}, "count": 5}
      ],
      "reference": {}
    },
    {
      "id": "333333333333333334",
      "type": "Default",
      "timestamp": "2024-07-01T08:01:00.000+00:00",
      "content": "Hey Alice!",
      "author": {
        "id": "555555555555555555",
        "name": "bob",
        "discriminator": "0002",
        "isBot": false
      },
      "attachments": [
        {"id": "F01", "fileName": "screenshot.png", "url": "https://cdn.discord.com/screenshot.png"}
      ],
      "embeds": [],
      "reactions": [],
      "reference": {"messageId": "333333333333333333"}
    },
    {
      "id": "333333333333333335",
      "type": "GuildMemberJoin",
      "timestamp": "2024-07-01T08:02:00.000+00:00",
      "content": "Welcome to the server!",
      "author": {
        "id": "666666666666666666",
        "name": "system",
        "isBot": true
      },
      "attachments": [],
      "embeds": [],
      "reactions": [],
      "reference": {}
    },
    {
      "id": "333333333333333336",
      "type": "Default",
      "timestamp": "2024-07-01T08:03:00.000+00:00",
      "content": "Bot announcement",
      "author": {
        "id": "777777777777777777",
        "name": "NewsBot",
        "isBot": true
      },
      "attachments": [],
      "embeds": [
        {"title": "Breaking News", "description": "Something happened"}
      ],
      "reactions": [],
      "reference": {}
    }
  ]
}`

func TestParseExport_Basic(t *testing.T) {
	msgs, err := ParseExport([]byte(goldenDiscordExport))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// GuildMemberJoin should be filtered out, so 3 messages expected.
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
}

func TestParseExport_FirstMessage(t *testing.T) {
	msgs, err := ParseExport([]byte(goldenDiscordExport))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	m := msgs[0]
	if m.ExternalID != "333333333333333333" {
		t.Errorf("ExternalID: got %q", m.ExternalID)
	}
	if m.ChannelName != "general" {
		t.Errorf("ChannelName: got %q", m.ChannelName)
	}
	if m.GuildName != "My Server" {
		t.Errorf("GuildName: got %q", m.GuildName)
	}
	if m.AuthorName != "alice" {
		t.Errorf("AuthorName: got %q", m.AuthorName)
	}
	if m.AuthorIsBot {
		t.Error("alice should not be a bot")
	}
	if m.Content != "Hello everyone!" {
		t.Errorf("Content: got %q", m.Content)
	}
	if len(m.Reactions) != 1 {
		t.Fatalf("Reactions: got %v", m.Reactions)
	}
	if m.Reactions[0].Emoji != "thumbsup" || m.Reactions[0].Count != 5 {
		t.Errorf("Reaction[0]: got %+v", m.Reactions[0])
	}
	if m.Source != "discord:My Server:general" {
		t.Errorf("Source: got %q", m.Source)
	}
}

func TestParseExport_Reply(t *testing.T) {
	msgs, err := ParseExport([]byte(goldenDiscordExport))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Second message (bob) references alice's message.
	m := msgs[1]
	if m.ReferencedMessageID != "333333333333333333" {
		t.Errorf("ReferencedMessageID: got %q", m.ReferencedMessageID)
	}
	if len(m.Attachments) != 1 || m.Attachments[0] != "screenshot.png" {
		t.Errorf("Attachments: got %v", m.Attachments)
	}
}

func TestParseExport_BotWithEmbed(t *testing.T) {
	msgs, err := ParseExport([]byte(goldenDiscordExport))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	m := msgs[2] // NewsBot message
	if !m.AuthorIsBot {
		t.Error("NewsBot should be flagged as bot")
	}
	if len(m.Embeds) != 1 || m.Embeds[0] != "Breaking News" {
		t.Errorf("Embeds: got %v", m.Embeds)
	}
}

func TestParseExport_Empty(t *testing.T) {
	export := `{"guild":{"id":"1","name":"G"},"channel":{"id":"2","name":"ch"},"messages":[]}`
	msgs, err := ParseExport([]byte(export))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestParseExport_EmptyInput(t *testing.T) {
	_, err := ParseExport([]byte(""))
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestParseExport_InvalidJSON(t *testing.T) {
	_, err := ParseExport([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseExport_Timestamps(t *testing.T) {
	msgs, err := ParseExport([]byte(goldenDiscordExport))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	expected := time.Date(2024, 7, 1, 8, 0, 0, 0, time.UTC)
	if !msgs[0].Timestamp.Equal(expected) {
		t.Errorf("Timestamp: got %v, want %v", msgs[0].Timestamp, expected)
	}
}

func TestParseExport_SortedByTimestamp(t *testing.T) {
	msgs, err := ParseExport([]byte(goldenDiscordExport))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for i := 1; i < len(msgs); i++ {
		if msgs[i].Timestamp.Before(msgs[i-1].Timestamp) {
			t.Errorf("messages not sorted: msgs[%d].Timestamp=%v < msgs[%d].Timestamp=%v",
				i, msgs[i].Timestamp, i-1, msgs[i-1].Timestamp)
		}
	}
}

func TestParseExport_UniqueExternalIDs(t *testing.T) {
	msgs, err := ParseExport([]byte(goldenDiscordExport))
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

func TestRenderContent_Basic(t *testing.T) {
	m := Message{
		ExternalID:  "333333333333333333",
		ChannelName: "general",
		GuildName:   "My Server",
		AuthorName:  "alice",
		Content:     "Hello everyone!",
		Timestamp:   time.Date(2024, 7, 1, 8, 0, 0, 0, time.UTC),
		Reactions: []ReactionCount{
			{Emoji: "thumbsup", Count: 5},
		},
	}

	rendered := RenderContent(m)
	if !strings.Contains(rendered, "# Discord message") {
		t.Error("expected header in rendered output")
	}
	if !strings.Contains(rendered, "My Server") {
		t.Error("expected guild name in rendered output")
	}
	if !strings.Contains(rendered, "#general") {
		t.Error("expected channel name in rendered output")
	}
	if !strings.Contains(rendered, "alice") {
		t.Error("expected author in rendered output")
	}
	if !strings.Contains(rendered, "Hello everyone!") {
		t.Error("expected content in rendered output")
	}
	if !strings.Contains(rendered, "thumbsup") {
		t.Error("expected reaction in rendered output")
	}
}

func TestRenderContent_Reply(t *testing.T) {
	m := Message{
		ChannelName:         "dev",
		GuildName:           "My Server",
		AuthorName:          "bob",
		Content:             "See my reply",
		Timestamp:           time.Date(2024, 7, 1, 8, 1, 0, 0, time.UTC),
		ReferencedMessageID: "333333333333333333",
	}
	rendered := RenderContent(m)
	if !strings.Contains(rendered, "reply") {
		t.Error("expected reply label in rendered output")
	}
	if !strings.Contains(rendered, "333333333333333333") {
		t.Error("expected referenced message ID in rendered output")
	}
}

func TestRenderContent_Bot(t *testing.T) {
	m := Message{
		ChannelName: "announcements",
		GuildName:   "My Server",
		AuthorName:  "NewsBot",
		AuthorIsBot: true,
		Content:     "News update",
		Timestamp:   time.Date(2024, 7, 1, 9, 0, 0, 0, time.UTC),
		Embeds:      []string{"Breaking News"},
	}
	rendered := RenderContent(m)
	if !strings.Contains(rendered, "bot") {
		t.Error("expected bot label in rendered output")
	}
	if !strings.Contains(rendered, "Breaking News") {
		t.Error("expected embed in rendered output")
	}
}

func TestParseDiscordTimestamp(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"2024-07-01T08:00:00.000+00:00", false},
		{"2024-07-01T08:00:00Z", false},
		{"2024-07-01T08:00:00+00:00", false},
		{"", true},
		{"not-a-date", true},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			_, err := parseDiscordTimestamp(tc.input)
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
