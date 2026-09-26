package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/importer/discord"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// discordImportConfig is the import.discord job payload the Discord
// importer endpoint enqueues.
type discordImportConfig struct {
	ExportFile string `json:"discord_export_file"`
	Since      string `json:"discord_since"`
	MaxItems   int    `json:"discord_max_items"`
}

// DiscordParser parses the DiscordChatExporter JSON export its job payload
// names and emits one item per selected message into
// draft.Metadata["feed_items"].
//
// Input contract:
//   - draft.RawContent is the JSON job payload: discord_export_file
//     (required), discord_since (RFC3339) and discord_max_items.
//
// Output contract:
//   - draft.Metadata["feed_items"] is set to []map[string]any, one entry per
//     message: its rendered text as content, the channel source as source.
//   - draft.Metadata["discord_channel"] is set to the parsed channel name.
type DiscordParser struct {
	pipeline.BaseContract
}

// NewDiscordParser creates a DiscordParser.
func NewDiscordParser() *DiscordParser {
	return &DiscordParser{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Metadata"},
		}),
	}
}

func (s *DiscordParser) Name() string { return "discord_parser" }

func (s *DiscordParser) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	var cfg discordImportConfig
	if err := json.Unmarshal([]byte(draft.RawContent), &cfg); err != nil {
		return nil, pipeline.Permanent(fmt.Errorf("discord_parser: job payload: %w", err))
	}
	if cfg.ExportFile == "" {
		return nil, pipeline.Permanent(fmt.Errorf("discord_parser: job payload has no discord_export_file"))
	}

	msgs, err := discord.ParseExportFile(cfg.ExportFile)
	if err != nil {
		return nil, fmt.Errorf("discord_parser: %w", err)
	}

	if cfg.Since != "" {
		since, err := time.Parse(time.RFC3339, cfg.Since)
		if err != nil {
			return nil, pipeline.Permanent(fmt.Errorf("discord_parser: discord_since: %w", err))
		}
		filtered := msgs[:0]
		for _, m := range msgs {
			if !m.Timestamp.Before(since) {
				filtered = append(filtered, m)
			}
		}
		msgs = filtered
	}
	if cfg.MaxItems > 0 && len(msgs) > cfg.MaxItems {
		msgs = msgs[:cfg.MaxItems]
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
			"title":                 "#" + m.ChannelName,
			"guid":                  m.ExternalID,
			"external_id":           m.ExternalID,
			"channel_id":            m.ChannelID,
			"channel_name":          m.ChannelName,
			"guild_id":              m.GuildID,
			"guild_name":            m.GuildName,
			"author_id":             m.AuthorID,
			"author_name":           m.AuthorName,
			"author_is_bot":         m.AuthorIsBot,
			"text":                  m.Content,
			"content":               discord.RenderContent(m),
			"timestamp":             m.Timestamp.UTC().Format(time.RFC3339),
			"referenced_message_id": m.ReferencedMessageID,
			"attachments":           m.Attachments,
			"embeds":                m.Embeds,
			"reactions":             reactions,
			"source":                m.Source,
		}
		items = append(items, item)
	}

	draft.Metadata["feed_items"] = items
	draft.Metadata["discord_import_count"] = len(items)

	return draft, nil
}
