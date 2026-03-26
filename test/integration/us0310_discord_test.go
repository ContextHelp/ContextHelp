package integration

// US-0310: Discord export import (DiscordChatExporter JSON format).

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/importer/discord"
)

// TestUS0310_DiscordParseExportFile verifies fixture parsing end-to-end.
func TestUS0310_DiscordParseExportFile(t *testing.T) {
	t.Parallel()

	msgs, err := discord.ParseExportFile(testdataPath("discord", "export.json"))
	require.NoError(t, err)
	require.NotEmpty(t, msgs, "must parse messages from discord fixture")
}

// TestUS0310_DiscordMessageFields verifies all expected fields are populated.
func TestUS0310_DiscordMessageFields(t *testing.T) {
	t.Parallel()

	msgs, err := discord.ParseExportFile(testdataPath("discord", "export.json"))
	require.NoError(t, err)
	require.NotEmpty(t, msgs)

	first := msgs[0]
	assert.NotEmpty(t, first.ExternalID, "ExternalID must be set")
	assert.NotEmpty(t, first.ChannelID, "ChannelID must be set")
	assert.NotEmpty(t, first.ChannelName, "ChannelName must be set")
	assert.NotEmpty(t, first.GuildID, "GuildID must be set")
	assert.NotEmpty(t, first.GuildName, "GuildName must be set")
	assert.NotEmpty(t, first.AuthorID, "AuthorID must be set")
	assert.NotEmpty(t, first.AuthorName, "AuthorName must be set")
	assert.NotEmpty(t, first.Content, "Content must be set")
	assert.False(t, first.Timestamp.IsZero(), "Timestamp must be parsed")
	assert.NotEmpty(t, first.Source, "Source must be set")
	assert.Contains(t, first.Source, "discord:", "Source must be prefixed with 'discord:'")
}

// TestUS0310_DiscordSourceAttribution verifies source format.
func TestUS0310_DiscordSourceAttribution(t *testing.T) {
	t.Parallel()

	msgs, err := discord.ParseExportFile(testdataPath("discord", "export.json"))
	require.NoError(t, err)
	require.NotEmpty(t, msgs)

	// All messages from the same export share the same source.
	source := msgs[0].Source
	for _, m := range msgs {
		assert.Equal(t, source, m.Source, "all messages must share same source")
	}
	assert.Contains(t, source, "Test Guild")
	assert.Contains(t, source, "general")
}

// TestUS0310_DiscordReactionsExtracted verifies reactions are parsed.
func TestUS0310_DiscordReactionsExtracted(t *testing.T) {
	t.Parallel()

	msgs, err := discord.ParseExportFile(testdataPath("discord", "export.json"))
	require.NoError(t, err)
	require.NotEmpty(t, msgs)

	// First message has a thumbsup reaction in fixture.
	assert.NotEmpty(t, msgs[0].Reactions, "reactions must be parsed")
}

// TestUS0310_DiscordAttachmentsExtracted verifies file attachments are parsed.
func TestUS0310_DiscordAttachmentsExtracted(t *testing.T) {
	t.Parallel()

	msgs, err := discord.ParseExportFile(testdataPath("discord", "export.json"))
	require.NoError(t, err)
	require.Len(t, msgs, 2)

	// Second message has an attachment in the fixture.
	assert.NotEmpty(t, msgs[1].Attachments, "attachments must be extracted")
	assert.Contains(t, msgs[1].Attachments[0], "img.png")
}

// TestUS0310_DiscordReplyReferenceExtracted verifies reply reference.
func TestUS0310_DiscordReplyReferenceExtracted(t *testing.T) {
	t.Parallel()

	msgs, err := discord.ParseExportFile(testdataPath("discord", "export.json"))
	require.NoError(t, err)
	require.Len(t, msgs, 2)

	// Second message replies to the first.
	assert.NotEmpty(t, msgs[1].ReferencedMessageID, "reply reference must be set")
}

// TestUS0310_DiscordRenderContent verifies RenderContent output.
func TestUS0310_DiscordRenderContent(t *testing.T) {
	t.Parallel()

	msgs, err := discord.ParseExportFile(testdataPath("discord", "export.json"))
	require.NoError(t, err)
	require.NotEmpty(t, msgs)

	for _, m := range msgs {
		rendered := discord.RenderContent(m)
		assert.NotEmpty(t, rendered)
		assert.Contains(t, rendered, m.Content)
	}
}

// TestUS0310_DiscordParseExportBytes verifies in-memory parsing.
func TestUS0310_DiscordParseExportBytes(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
	  "guild": {"id": "G1", "name": "My Server"},
	  "channel": {"id": "C1", "name": "test-channel", "type": "GuildText"},
	  "messages": [
	    {
	      "id": "M001",
	      "type": "Default",
	      "timestamp": "2026-01-15T10:00:00+00:00",
	      "content": "Test message",
	      "author": {"id": "U1", "name": "testuser", "isBot": false},
	      "attachments": [],
	      "embeds": [],
	      "reactions": [],
	      "reference": null
	    }
	  ]
	}`)

	msgs, err := discord.ParseExport(raw)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	m := msgs[0]
	assert.Equal(t, "M001", m.ExternalID)
	assert.Equal(t, "test-channel", m.ChannelName)
	assert.Equal(t, "testuser", m.AuthorName)
	assert.Equal(t, "Test message", m.Content)
}
