package discord

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// Message represents a single Discord message from an export.
type Message struct {
	// ExternalID is the stable dedup key (message snowflake ID).
	ExternalID string
	// ChannelID is the Discord channel ID.
	ChannelID string
	// ChannelName is the human-readable channel name.
	ChannelName string
	// GuildID is the Discord guild/server ID.
	GuildID string
	// GuildName is the guild/server name.
	GuildName string
	// AuthorID is the Discord user ID.
	AuthorID string
	// AuthorName is the display name of the author.
	AuthorName string
	// AuthorIsBot is true when the author is a bot.
	AuthorIsBot bool
	// Content is the message body text.
	Content string
	// Timestamp is the message creation time.
	Timestamp time.Time
	// EditedTimestamp is the last-edited time (zero if not edited).
	EditedTimestamp time.Time
	// ReferencedMessageID is set for replies to another message.
	ReferencedMessageID string
	// Attachments contains file attachment names.
	Attachments []string
	// Embeds contains embed titles or descriptions.
	Embeds []string
	// Reactions contains reaction emoji names with their counts.
	Reactions []ReactionCount
	// Source is the canonical source reference (e.g. "discord:<guild>:<channel>").
	Source string
}

// ReactionCount pairs a reaction emoji with how many times it was used.
type ReactionCount struct {
	Emoji string
	Count int
}

// discordExport is the top-level structure of a DiscordChatExporter JSON export.
type discordExport struct {
	Guild struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"guild"`
	Channel struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Category string `json:"category"`
		Type     string `json:"type"`
	} `json:"channel"`
	Messages []discordExportMessage `json:"messages"`
}

// discordExportMessage is a single message entry from a DiscordChatExporter JSON export.
type discordExportMessage struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Timestamp2 string `json:"timestampEdited"`
	Content   string `json:"content"`
	Author    struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Discriminator string `json:"discriminator"`
		IsBot   bool   `json:"isBot"`
	} `json:"author"`
	Attachments []struct {
		ID          string `json:"id"`
		FileName    string `json:"fileName"`
		URL         string `json:"url"`
	} `json:"attachments"`
	Embeds []struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	} `json:"embeds"`
	Reactions []struct {
		Emoji struct {
			Name string `json:"name"`
		} `json:"emoji"`
		Count int `json:"count"`
	} `json:"reactions"`
	Reference struct {
		MessageID string `json:"messageId"`
	} `json:"reference"`
}

// ParseExportFile parses a DiscordChatExporter JSON export file.
func ParseExportFile(path string) ([]Message, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("discord export: read %q: %w", path, err)
	}
	return ParseExport(data)
}

// ParseExport parses DiscordChatExporter JSON export bytes.
func ParseExport(data []byte) ([]Message, error) {
	input := strings.TrimSpace(string(data))
	if input == "" {
		return nil, fmt.Errorf("discord export: input is empty")
	}

	var export discordExport
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, fmt.Errorf("discord export: unmarshal: %w", err)
	}

	guildID := export.Guild.ID
	guildName := export.Guild.Name
	channelID := export.Channel.ID
	channelName := export.Channel.Name
	if channelName == "" {
		channelName = channelID
	}

	source := "discord:" + guildName + ":" + channelName
	if guildName == "" {
		source = "discord:" + channelName
	}

	msgs := make([]Message, 0, len(export.Messages))
	for _, r := range export.Messages {
		// Skip system message types that don't carry user content.
		switch r.Type {
		case "GuildMemberJoin", "ChannelPinnedMessage", "RecipientAdd", "RecipientRemove":
			continue
		}

		if r.ID == "" {
			continue
		}

		ts, err := parseDiscordTimestamp(r.Timestamp)
		if err != nil {
			// Skip messages with unparseable timestamps.
			continue
		}

		var editedTS time.Time
		if r.Timestamp2 != "" {
			if t, err := parseDiscordTimestamp(r.Timestamp2); err == nil {
				editedTS = t
			}
		}

		attachments := make([]string, 0, len(r.Attachments))
		for _, a := range r.Attachments {
			name := a.FileName
			if name == "" {
				name = a.ID
			}
			attachments = append(attachments, name)
		}

		embeds := make([]string, 0, len(r.Embeds))
		for _, e := range r.Embeds {
			label := e.Title
			if label == "" {
				label = e.Description
			}
			if label != "" {
				embeds = append(embeds, label)
			}
		}

		reactions := make([]ReactionCount, 0, len(r.Reactions))
		for _, rx := range r.Reactions {
			reactions = append(reactions, ReactionCount{
				Emoji: rx.Emoji.Name,
				Count: rx.Count,
			})
		}

		msgs = append(msgs, Message{
			ExternalID:          r.ID,
			ChannelID:           channelID,
			ChannelName:         channelName,
			GuildID:             guildID,
			GuildName:           guildName,
			AuthorID:            r.Author.ID,
			AuthorName:          r.Author.Name,
			AuthorIsBot:         r.Author.IsBot,
			Content:             strings.TrimSpace(r.Content),
			Timestamp:           ts,
			EditedTimestamp:     editedTS,
			ReferencedMessageID: r.Reference.MessageID,
			Attachments:         attachments,
			Embeds:              embeds,
			Reactions:           reactions,
			Source:              source,
		})
	}

	// Sort by timestamp ascending for deterministic ordering.
	sort.Slice(msgs, func(i, j int) bool {
		return msgs[i].Timestamp.Before(msgs[j].Timestamp)
	})

	return msgs, nil
}

// parseDiscordTimestamp parses a Discord timestamp string in RFC3339 or ISO 8601 format.
func parseDiscordTimestamp(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	// DiscordChatExporter uses ISO 8601 with offset, e.g. "2024-07-01T08:00:00.000+00:00"
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05+00:00",
		"2006-01-02T15:04:05.000+00:00",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse discord timestamp %q", s)
}

// RenderContent converts a Discord Message into a plain-text representation
// suitable for ingestion.
func RenderContent(m Message) string {
	var sb strings.Builder

	channelLabel := "#" + m.ChannelName
	if m.GuildName != "" {
		channelLabel = m.GuildName + " / " + channelLabel
	}

	sb.WriteString("# Discord message — ")
	sb.WriteString(channelLabel)
	if m.ReferencedMessageID != "" {
		sb.WriteString(" (reply)")
	}
	sb.WriteString("\n\n")

	if m.AuthorName != "" {
		sb.WriteString("**From:** ")
		sb.WriteString(m.AuthorName)
		if m.AuthorIsBot {
			sb.WriteString(" (bot)")
		}
		sb.WriteString("\n")
	}

	if !m.Timestamp.IsZero() {
		sb.WriteString("**Time:** ")
		sb.WriteString(m.Timestamp.UTC().Format(time.RFC3339))
		sb.WriteString("\n")
	}

	if m.ReferencedMessageID != "" {
		sb.WriteString("**Reply to:** ")
		sb.WriteString(m.ReferencedMessageID)
		sb.WriteString("\n")
	}

	if len(m.Attachments) > 0 {
		sb.WriteString("**Attachments:** ")
		sb.WriteString(strings.Join(m.Attachments, ", "))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(m.Content)

	if len(m.Embeds) > 0 {
		sb.WriteString("\n\n**Embeds:**\n")
		for _, e := range m.Embeds {
			sb.WriteString("- ")
			sb.WriteString(e)
			sb.WriteString("\n")
		}
	}

	if len(m.Reactions) > 0 {
		sb.WriteString("\n**Reactions:** ")
		parts := make([]string, 0, len(m.Reactions))
		for _, rx := range m.Reactions {
			parts = append(parts, fmt.Sprintf(":%s: ×%d", rx.Emoji, rx.Count))
		}
		sb.WriteString(strings.Join(parts, " "))
	}

	return sb.String()
}
