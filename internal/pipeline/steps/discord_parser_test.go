package steps

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

const discordTestExport = `{
  "guild": {"id": "111111111111111111", "name": "Test Server"},
  "channel": {"id": "222222222222222222", "name": "general", "type": "GuildTextChat"},
  "messages": [
    {
      "id": "333333333333333333",
      "type": "Default",
      "timestamp": "2024-07-01T08:00:00.000+00:00",
      "content": "Hello from pipeline step test!",
      "author": {"id": "444444444444444444", "name": "alice", "isBot": false},
      "attachments": [],
      "embeds": [],
      "reactions": [{"emoji": {"name": "thumbsup"}, "count": 2}],
      "reference": {}
    },
    {
      "id": "333333333333333334",
      "type": "Default",
      "timestamp": "2024-07-01T08:01:00.000+00:00",
      "content": "Reply here",
      "author": {"id": "555555555555555555", "name": "bob", "isBot": false},
      "attachments": [{"id": "F01", "fileName": "file.txt", "url": "https://cdn.discord.com/file.txt"}],
      "embeds": [],
      "reactions": [],
      "reference": {"messageId": "333333333333333333"}
    }
  ]
}`

func writeDiscordExportFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "export.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write export file: %v", err)
	}
	return path
}

func TestDiscordParserStep_Basic(t *testing.T) {
	path := writeDiscordExportFile(t, discordTestExport)

	step := NewDiscordParser()
	draft := discordJob(t, map[string]any{"discord_export_file": path})
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	msgs, ok := got.Metadata["feed_items"].([]map[string]any)
	if !ok {
		t.Fatalf("feed_items missing or wrong type: %T", got.Metadata["feed_items"])
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0]["author_name"] != "alice" {
		t.Errorf("msgs[0].author_name = %v", msgs[0]["author_name"])
	}
	if msgs[0]["channel_name"] != "general" {
		t.Errorf("msgs[0].channel_name = %v", msgs[0]["channel_name"])
	}
	if msgs[0]["external_id"] != "333333333333333333" {
		t.Errorf("msgs[0].external_id = %v", msgs[0]["external_id"])
	}

	channelName, _ := got.Metadata["discord_channel"].(string)
	if channelName != "general" {
		t.Errorf("discord_channel = %q", channelName)
	}

	count, ok := got.Metadata["discord_import_count"].(int)
	if !ok || count != 2 {
		t.Errorf("discord_import_count = %v", got.Metadata["discord_import_count"])
	}
}

// discordJob is an import.discord container draft carrying payload as its
// job payload, as the Discord importer endpoint enqueues it.
func discordJob(t *testing.T, payload map[string]any) *storage.KnowledgeObject {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return &storage.KnowledgeObject{RawContent: string(b), Source: "import:discord"}
}

func TestDiscordParserStep_PayloadWithoutExportFile(t *testing.T) {
	_, err := NewDiscordParser().Run(context.Background(), discordJob(t, map[string]any{}))
	var perm *pipeline.PermanentError
	if !errors.As(err, &perm) {
		t.Fatalf("err = %v, want a permanent error", err)
	}
}

func TestDiscordParserStep_NoSource(t *testing.T) {
	step := NewDiscordParser()
	draft := &storage.KnowledgeObject{}
	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for empty source")
	}
}

func TestDiscordParserStep_SinceFilter(t *testing.T) {
	path := writeDiscordExportFile(t, discordTestExport)

	step := NewDiscordParser()
	// Set since to after both messages.
	draft := discordJob(t, map[string]any{
		"discord_export_file": path,
		"discord_since":       time.Date(2024, 7, 1, 9, 0, 0, 0, time.UTC).Format(time.RFC3339),
	})
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	msgs, ok := got.Metadata["feed_items"].([]map[string]any)
	if !ok {
		t.Fatalf("feed_items missing")
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages after since filter, got %d", len(msgs))
	}
}

func TestDiscordParserStep_MaxItems(t *testing.T) {
	path := writeDiscordExportFile(t, discordTestExport)

	step := NewDiscordParser()
	draft := discordJob(t, map[string]any{"discord_export_file": path, "discord_max_items": 1})
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	msgs, ok := got.Metadata["feed_items"].([]map[string]any)
	if !ok {
		t.Fatalf("feed_items missing")
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 message after max-items cap, got %d", len(msgs))
	}
}

func TestDiscordParserStep_RenderedContent(t *testing.T) {
	path := writeDiscordExportFile(t, discordTestExport)

	step := NewDiscordParser()
	draft := discordJob(t, map[string]any{"discord_export_file": path})
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	msgs := got.Metadata["feed_items"].([]map[string]any)
	rendered, _ := msgs[0]["content"].(string)
	if rendered == "" {
		t.Error("expected non-empty rendered content")
	}
	if !strings.Contains(rendered, "Alice") && !strings.Contains(rendered, "alice") {
		t.Errorf("expected author in rendered content, got: %q", rendered)
	}
}

func TestDiscordParserStep_Name(t *testing.T) {
	step := NewDiscordParser()
	if step.Name() != "discord_parser" {
		t.Errorf("Name() = %q", step.Name())
	}
}

func TestDiscordParserStep_Contract(t *testing.T) {
	step := NewDiscordParser()
	contract := step.Contract()
	if len(contract.Requires) == 0 {
		t.Error("expected non-empty Requires")
	}
	if len(contract.Produces) == 0 {
		t.Error("expected non-empty Produces")
	}
}

func TestDiscordParserStep_MetadataKeys(t *testing.T) {
	path := writeDiscordExportFile(t, discordTestExport)

	step := NewDiscordParser()
	draft := discordJob(t, map[string]any{"discord_export_file": path})
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	msgs := got.Metadata["feed_items"].([]map[string]any)
	m := msgs[0]

	requiredKeys := []string{
		"external_id", "channel_id", "channel_name", "guild_id", "guild_name",
		"author_id", "author_name", "author_is_bot", "content", "timestamp",
		"referenced_message_id", "attachments", "embeds", "reactions", "source", "text",
	}
	for _, key := range requiredKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in message item", key)
		}
	}
}

func TestDiscordParserStep_Idempotency(t *testing.T) {
	path := writeDiscordExportFile(t, discordTestExport)

	step := NewDiscordParser()

	run := func() []string {
		draft := discordJob(t, map[string]any{"discord_export_file": path})
		got, err := step.Run(context.Background(), draft)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		msgs := got.Metadata["feed_items"].([]map[string]any)
		ids := make([]string, len(msgs))
		for i, m := range msgs {
			ids[i], _ = m["external_id"].(string)
		}
		return ids
	}

	ids1 := run()
	ids2 := run()

	b1, _ := json.Marshal(ids1)
	b2, _ := json.Marshal(ids2)
	if string(b1) != string(b2) {
		t.Errorf("idempotency: first=%v second=%v", ids1, ids2)
	}
}
