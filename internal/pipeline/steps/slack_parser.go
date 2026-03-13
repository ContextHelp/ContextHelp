package steps

import (
	"context"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/importer/slack"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// SlackParser parses a Slack workspace export directory and emits normalized
// message items into draft.Metadata["slack_messages"].
//
// Input contract:
//   - draft.Source must contain the path to the Slack export directory.
//
// Output contract:
//   - draft.Metadata["slack_messages"] is set to []map[string]any, one entry per message.
//   - draft.Metadata["slack_channel"] is set to the channel filter (or "all").
type SlackParser struct {
	pipeline.BaseContract
	// Since filters out messages older than this time. Zero means no filter.
	Since time.Time
	// MaxItems caps the number of messages returned. 0 means no cap.
	MaxItems int
}

// NewSlackParser creates a SlackParser.
func NewSlackParser() *SlackParser {
	return &SlackParser{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"Source"},
			Produces: []string{"Metadata"},
		}),
	}
}

func (s *SlackParser) Name() string { return "slack_parser" }

func (s *SlackParser) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	exportDir := draft.Source
	if exportDir == "" {
		return nil, fmt.Errorf("slack_parser: no source path in draft.Source")
	}

	msgs, err := slack.ParseExportDir(exportDir)
	if err != nil {
		return nil, fmt.Errorf("slack_parser: %w", err)
	}

	// Apply since filter.
	if !s.Since.IsZero() {
		filtered := msgs[:0]
		for _, m := range msgs {
			if !m.Timestamp.Before(s.Since) {
				filtered = append(filtered, m)
			}
		}
		msgs = filtered
	}

	// Apply max-items cap.
	if s.MaxItems > 0 && len(msgs) > s.MaxItems {
		msgs = msgs[:s.MaxItems]
	}

	items := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		item := map[string]any{
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
			"source":       m.Source,
			// pre-rendered content for enqueue
			"content": slack.RenderContent(m),
		}
		items = append(items, item)
	}

	draft.Metadata["slack_messages"] = items
	draft.Metadata["slack_import_count"] = len(items)

	return draft, nil
}
