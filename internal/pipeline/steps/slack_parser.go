package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/importer/slack"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// slackImportConfig is the import.slack job payload the Slack importer
// endpoint enqueues.
type slackImportConfig struct {
	ExportDir     string   `json:"slack_export_dir"`
	ChannelFilter []string `json:"slack_channel_filter"`
	Since         string   `json:"slack_since"`
	MaxItems      int      `json:"slack_max_items"`
}

// SlackParser parses the Slack workspace export its job payload names and
// emits one item per selected message into draft.Metadata["feed_items"].
//
// Input contract:
//   - draft.RawContent is the JSON job payload: slack_export_dir (required),
//     slack_channel_filter, slack_since (RFC3339) and slack_max_items.
//
// Output contract:
//   - draft.Metadata["feed_items"] is set to []map[string]any, one entry per
//     message: its rendered text as content, the channel source as source.
//   - draft.Metadata["slack_import_count"] is the number of items.
type SlackParser struct {
	pipeline.BaseContract
}

// NewSlackParser creates a SlackParser.
func NewSlackParser() *SlackParser {
	return &SlackParser{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Metadata"},
		}),
	}
}

func (s *SlackParser) Name() string { return "slack_parser" }

func (s *SlackParser) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	var cfg slackImportConfig
	if err := json.Unmarshal([]byte(draft.RawContent), &cfg); err != nil {
		return nil, pipeline.Permanent(fmt.Errorf("slack_parser: job payload: %w", err))
	}
	if cfg.ExportDir == "" {
		return nil, pipeline.Permanent(fmt.Errorf("slack_parser: job payload has no slack_export_dir"))
	}
	var since time.Time
	if cfg.Since != "" {
		t, err := time.Parse(time.RFC3339, cfg.Since)
		if err != nil {
			return nil, pipeline.Permanent(fmt.Errorf("slack_parser: slack_since: %w", err))
		}
		since = t
	}
	channels := make(map[string]bool, len(cfg.ChannelFilter))
	for _, c := range cfg.ChannelFilter {
		channels[strings.ToLower(strings.TrimSpace(c))] = true
	}

	msgs, err := slack.ParseExportDir(cfg.ExportDir)
	if err != nil {
		return nil, fmt.Errorf("slack_parser: %w", err)
	}

	items := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		if !since.IsZero() && m.Timestamp.Before(since) {
			continue
		}
		if len(channels) > 0 && !channels[strings.ToLower(m.ChannelName)] {
			continue
		}
		if cfg.MaxItems > 0 && len(items) >= cfg.MaxItems {
			break
		}
		items = append(items, map[string]any{
			"title":        "#" + m.ChannelName,
			"content":      slack.RenderContent(m),
			"source":       m.Source,
			"guid":         m.ExternalID,
			"external_id":  m.ExternalID,
			"channel_name": m.ChannelName,
			"user_id":      m.UserID,
			"username":     m.Username,
			"text":         m.Text,
			"timestamp":    m.Timestamp.UTC().Format(time.RFC3339),
			"thread_ts":    m.ThreadTS,
			"is_reply":     m.IsReply,
			"reactions":    m.Reactions,
			"files":        m.Files,
		})
	}

	draft.Metadata["feed_items"] = items
	draft.Metadata["slack_import_count"] = len(items)

	return draft, nil
}
