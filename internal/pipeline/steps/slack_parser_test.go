package steps

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// writeSlackExportDir creates a temporary Slack export directory for testing.
func writeSlackExportDir(t *testing.T, channelName string, dayJSON string) string {
	t.Helper()
	dir := t.TempDir()
	channelDir := filepath.Join(dir, channelName)
	if err := os.MkdirAll(channelDir, 0o755); err != nil {
		t.Fatalf("mkdir channel: %v", err)
	}
	if err := os.WriteFile(filepath.Join(channelDir, "2024-07-01.json"), []byte(dayJSON), 0o644); err != nil {
		t.Fatalf("write day file: %v", err)
	}
	return dir
}

const slackTestDayJSON = `[
  {
    "type": "message",
    "user": "U01ABC",
    "username": "alice",
    "text": "Hello from pipeline test!",
    "ts": "1719830400.000100"
  },
  {
    "type": "message",
    "user": "U02XYZ",
    "username": "bob",
    "text": "Hi back!",
    "ts": "1719830460.000200",
    "thread_ts": "1719830400.000100"
  }
]`

func TestSlackParserStep_Basic(t *testing.T) {
	dir := writeSlackExportDir(t, "general", slackTestDayJSON)

	step := NewSlackParser()
	draft := slackJob(t, map[string]any{"slack_export_dir": dir})
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
	if msgs[0]["username"] != "alice" {
		t.Errorf("msgs[0].username = %v", msgs[0]["username"])
	}
	if msgs[0]["channel_name"] != "general" {
		t.Errorf("msgs[0].channel_name = %v", msgs[0]["channel_name"])
	}
	if msgs[0]["external_id"] != "general:1719830400.000100" {
		t.Errorf("msgs[0].external_id = %v", msgs[0]["external_id"])
	}

	count, ok := got.Metadata["slack_import_count"].(int)
	if !ok || count != 2 {
		t.Errorf("slack_import_count = %v", got.Metadata["slack_import_count"])
	}
}

// slackJob is an import.slack container draft carrying payload as its job
// payload, as the Slack importer endpoint enqueues it.
func slackJob(t *testing.T, payload map[string]any) *storage.KnowledgeObject {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return &storage.KnowledgeObject{RawContent: string(b), Source: "import:slack"}
}

func TestSlackParserStep_ChannelFilter(t *testing.T) {
	dir := writeSlackExportDir(t, "general", slackTestDayJSON)
	if err := os.MkdirAll(filepath.Join(dir, "random"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "random", "2024-07-01.json"), []byte(slackTestDayJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := NewSlackParser().Run(context.Background(), slackJob(t, map[string]any{
		"slack_export_dir":     dir,
		"slack_channel_filter": []string{"Random"},
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	msgs := got.Metadata["feed_items"].([]map[string]any)
	if len(msgs) != 2 {
		t.Fatalf("expected the 2 #random messages, got %d", len(msgs))
	}
	for _, m := range msgs {
		if m["channel_name"] != "random" {
			t.Errorf("channel_name = %v, want random", m["channel_name"])
		}
	}
}

func TestSlackParserStep_PayloadWithoutExportDir(t *testing.T) {
	_, err := NewSlackParser().Run(context.Background(), slackJob(t, map[string]any{}))
	var perm *pipeline.PermanentError
	if !errors.As(err, &perm) {
		t.Fatalf("err = %v, want a permanent error", err)
	}
}

func TestSlackParserStep_NoSource(t *testing.T) {
	step := NewSlackParser()
	draft := &storage.KnowledgeObject{}
	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for empty source")
	}
}

func TestSlackParserStep_SinceFilter(t *testing.T) {
	dir := writeSlackExportDir(t, "general", slackTestDayJSON)

	step := NewSlackParser()
	// Set since to after both messages (1719830460 is the second message).
	draft := slackJob(t, map[string]any{
		"slack_export_dir": dir,
		"slack_since":      time.Unix(1719830500, 0).UTC().Format(time.RFC3339),
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

func TestSlackParserStep_MaxItems(t *testing.T) {
	dir := writeSlackExportDir(t, "general", slackTestDayJSON)

	step := NewSlackParser()
	draft := slackJob(t, map[string]any{"slack_export_dir": dir, "slack_max_items": 1})
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

func TestSlackParserStep_RenderedContent(t *testing.T) {
	dir := writeSlackExportDir(t, "general", slackTestDayJSON)

	step := NewSlackParser()
	draft := slackJob(t, map[string]any{"slack_export_dir": dir})
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	msgs := got.Metadata["feed_items"].([]map[string]any)
	rendered, _ := msgs[0]["content"].(string)
	if rendered == "" {
		t.Error("expected non-empty rendered content")
	}
	if len(rendered) < 10 {
		t.Errorf("rendered content too short: %q", rendered)
	}
}

func TestSlackParserStep_Name(t *testing.T) {
	step := NewSlackParser()
	if step.Name() != "slack_parser" {
		t.Errorf("Name() = %q", step.Name())
	}
}

func TestSlackParserStep_Contract(t *testing.T) {
	step := NewSlackParser()
	contract := step.Contract()
	if len(contract.Requires) == 0 {
		t.Error("expected non-empty Requires")
	}
	if len(contract.Produces) == 0 {
		t.Error("expected non-empty Produces")
	}
}

func TestSlackParserStep_MetadataKeys(t *testing.T) {
	dir := writeSlackExportDir(t, "dev", slackTestDayJSON)

	step := NewSlackParser()
	draft := slackJob(t, map[string]any{"slack_export_dir": dir})
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	msgs := got.Metadata["feed_items"].([]map[string]any)
	m := msgs[0]

	requiredKeys := []string{
		"external_id", "channel_name", "user_id", "username", "text",
		"timestamp", "thread_ts", "is_reply", "reactions", "files", "source", "content",
	}
	for _, key := range requiredKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in message item", key)
		}
	}
}

func TestSlackParserStep_Idempotency(t *testing.T) {
	dir := writeSlackExportDir(t, "general", slackTestDayJSON)

	step := NewSlackParser()

	// Run twice on the same directory and compare external IDs.
	run := func() []string {
		draft := slackJob(t, map[string]any{"slack_export_dir": dir})
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
