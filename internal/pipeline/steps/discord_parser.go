package steps

import (
	"context"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/importer/discord"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// DiscordParser parses a DiscordChatExporter JSON export file and emits normalized
// message items into draft.Metadata["discord_messages"].
//
// Input contract:
//   - draft.Source must contain the path to the Discord export JSON file.
//
// Output contract:
//   - draft.Metadata["discord_messages"] is set to []map[string]any, one entry per message.
//   - draft.Metadata["discord_channel"] is set to the parsed channel name.
type DiscordParser struct {
	pipeline.BaseContract
	// Since filters out messages older than this time. Zero means no filter.
	Since time.Time
	// MaxItems caps the number of messages returned. 0 means no cap.
	MaxItems int
}

// NewDiscordParser creates a DiscordParser.
func NewDiscordParser() *DiscordParser {
	return &DiscordParser{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"Source"},
			Produces: []string{"Metadata"},
		}),
	}
}

func (s *DiscordParser) Name() string { return "discord_parser" }

func (s *DiscordParser) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	exportFile := draft.Source
	if exportFile == "" {
		return nil, fmt.Errorf("discord_parser: no source path in draft.Source")
	}

	msgs, err := discord.ParseExportFile(exportFile)
	if err != nil {
		return nil, fmt.Errorf("discord_parser: %w", err)
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

	// Extract channel name from first message (they all share one).
	channelName := ""
	if len(msgs) > 0 {
		channelName = msgs[0].ChannelName
	}
	draft.Metadata["discord_channel"] = channelName

	items := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		reactions := make([]map[string]any, 0, len(m.Reactions))
		for _, rx := range m.Reactions {
			reactions = append(reactions, map[string]any{
				"emoji": rx.Emoji,
				"count": rx.Count,
			})
		}
		item := map[string]any{
			"external_id":           m.ExternalID,
			"channel_id":            m.ChannelID,
			"channel_name":          m.ChannelName,
			"guild_id":              m.GuildID,
			"guild_name":            m.GuildName,
			"author_id":             m.AuthorID,
			"author_name":           m.AuthorName,
			"author_is_bot":         m.AuthorIsBot,
			"content":               m.Content,
			"timestamp":             m.Timestamp.UTC().Format(time.RFC3339),
			"referenced_message_id": m.ReferencedMessageID,
			"attachments":           m.Attachments,
			"embeds":                m.Embeds,
			"reactions":             reactions,
			"source":                m.Source,
			// pre-rendered content for enqueue
			"rendered": discord.RenderContent(m),
		}
		items = append(items, item)
	}

	draft.Metadata["discord_messages"] = items
	draft.Metadata["discord_import_count"] = len(items)

	return draft, nil
}
