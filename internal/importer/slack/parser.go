package slack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Message represents a single Slack message from a workspace export.
type Message struct {
	// ExternalID is the stable dedup key (channel + timestamp).
	ExternalID string
	// ChannelName is the name of the channel this message belongs to.
	ChannelName string
	// UserID is the Slack user ID of the sender.
	UserID string
	// Username is the display name of the sender (if available).
	Username string
	// Text is the message body.
	Text string
	// Timestamp is the message creation time.
	Timestamp time.Time
	// ThreadTS is the parent thread timestamp, empty for top-level messages.
	ThreadTS string
	// IsReply is true when this message is a thread reply.
	IsReply bool
	// Reactions is the list of reaction emoji names on the message.
	Reactions []string
	// Files contains metadata about attached files (names only, not content).
	Files []string
	// Source is the canonical source reference (e.g. "slack:<workspace>").
	Source string
}

// slackExportMessage is the raw JSON structure from a Slack export file.
type slackExportMessage struct {
	Type     string  `json:"type"`
	Subtype  string  `json:"subtype"`
	Text     string  `json:"text"`
	User     string  `json:"user"`
	Username string  `json:"username"`
	Ts       string  `json:"ts"`
	ThreadTs string  `json:"thread_ts"`
	ReplyTo  *string `json:"reply_to"`
	ParentTs string  `json:"parent_user_ts"`

	Reactions []struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	} `json:"reactions"`

	Files []struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	} `json:"files"`

	Attachments []struct {
		Text string `json:"text"`
	} `json:"attachments"`
}

// ParseExportDir parses a Slack workspace export directory.
//
// The standard Slack export layout is:
//
//	<export>/
//	  channels.json
//	  <channel-name>/
//	    YYYY-MM-DD.json
//	    ...
//
// Each per-day JSON file contains an array of message objects.
func ParseExportDir(dir string) ([]Message, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("slack export: stat %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("slack export: %q is not a directory", dir)
	}

	// Enumerate channel sub-directories.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("slack export: read dir %q: %w", dir, err)
	}

	var msgs []Message
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		channelName := entry.Name()
		channelDir := filepath.Join(dir, channelName)

		dayFiles, err := os.ReadDir(channelDir)
		if err != nil {
			return nil, fmt.Errorf("slack export: read channel dir %q: %w", channelDir, err)
		}

		for _, df := range dayFiles {
			if df.IsDir() {
				continue
			}
			if strings.ToLower(filepath.Ext(df.Name())) != ".json" {
				continue
			}

			data, err := os.ReadFile(filepath.Join(channelDir, df.Name()))
			if err != nil {
				return nil, fmt.Errorf("slack export: read %q: %w", df.Name(), err)
			}

			parsed, err := parseMessagesJSON(data, channelName)
			if err != nil {
				return nil, fmt.Errorf("slack export: parse %s/%s: %w", channelName, df.Name(), err)
			}
			msgs = append(msgs, parsed...)
		}
	}

	// Sort by timestamp ascending for deterministic ordering.
	sort.Slice(msgs, func(i, j int) bool {
		return msgs[i].Timestamp.Before(msgs[j].Timestamp)
	})

	return msgs, nil
}

// ParseMessagesJSON parses raw Slack export JSON bytes for a single channel/day file.
func ParseMessagesJSON(data []byte, channelName string) ([]Message, error) {
	return parseMessagesJSON(data, channelName)
}

func parseMessagesJSON(data []byte, channelName string) ([]Message, error) {
	var raw []slackExportMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	msgs := make([]Message, 0, len(raw))
	for _, r := range raw {
		// Skip non-message subtypes that don't carry user content.
		switch r.Subtype {
		case "channel_join", "channel_leave", "channel_purpose", "channel_topic",
			"channel_name", "bot_add", "bot_remove":
			continue
		}
		if r.Type != "message" && r.Type != "" {
			continue
		}

		ts, err := parseSlackTimestamp(r.Ts)
		if err != nil {
			// Skip unparseable timestamps rather than aborting.
			continue
		}

		externalID := channelName + ":" + r.Ts

		reactions := make([]string, 0, len(r.Reactions))
		for _, rx := range r.Reactions {
			reactions = append(reactions, rx.Name)
		}

		files := make([]string, 0, len(r.Files))
		for _, f := range r.Files {
			name := f.Name
			if name == "" {
				name = f.ID
			}
			files = append(files, name)
		}

		// Include attachment text in message body if present.
		text := r.Text
		for _, att := range r.Attachments {
			if att.Text != "" {
				text += "\n" + att.Text
			}
		}
		text = strings.TrimSpace(text)

		isReply := r.ThreadTs != "" && r.ThreadTs != r.Ts

		msgs = append(msgs, Message{
			ExternalID:  externalID,
			ChannelName: channelName,
			UserID:      r.User,
			Username:    r.Username,
			Text:        text,
			Timestamp:   ts,
			ThreadTS:    r.ThreadTs,
			IsReply:     isReply,
			Reactions:   reactions,
			Files:       files,
			Source:      "slack:" + channelName,
		})
	}

	return msgs, nil
}

// parseSlackTimestamp converts a Slack timestamp string (e.g. "1719830400.123456")
// to a time.Time.
func parseSlackTimestamp(ts string) (time.Time, error) {
	if ts == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	// Slack timestamps are Unix seconds with fractional component separated by '.'.
	parts := strings.SplitN(ts, ".", 2)
	sec, err := parseIntegral(parts[0])
	if err != nil {
		return time.Time{}, fmt.Errorf("parse unix seconds from %q: %w", ts, err)
	}
	return time.Unix(sec, 0).UTC(), nil
}

func parseIntegral(s string) (int64, error) {
	var n int64
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("non-digit character in %q", s)
		}
		n = n*10 + int64(ch-'0')
	}
	return n, nil
}

// RenderContent converts a Slack Message into a plain-text representation
// suitable for ingestion.
func RenderContent(m Message) string {
	var sb strings.Builder

	sb.WriteString("# Slack message — #")
	sb.WriteString(m.ChannelName)
	if m.IsReply {
		sb.WriteString(" (thread reply)")
	}
	sb.WriteString("\n\n")

	if m.Username != "" {
		sb.WriteString("**From:** ")
		sb.WriteString(m.Username)
		sb.WriteString("\n")
	} else if m.UserID != "" {
		sb.WriteString("**From:** ")
		sb.WriteString(m.UserID)
		sb.WriteString("\n")
	}

	if !m.Timestamp.IsZero() {
		sb.WriteString("**Time:** ")
		sb.WriteString(m.Timestamp.UTC().Format(time.RFC3339))
		sb.WriteString("\n")
	}

	if len(m.Files) > 0 {
		sb.WriteString("**Attachments:** ")
		sb.WriteString(strings.Join(m.Files, ", "))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(m.Text)

	if len(m.Reactions) > 0 {
		sb.WriteString("\n\n**Reactions:** :")
		sb.WriteString(strings.Join(m.Reactions, ": :"))
		sb.WriteString(":")
	}

	return sb.String()
}
