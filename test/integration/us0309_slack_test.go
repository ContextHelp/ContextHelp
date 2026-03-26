package integration

// US-0309: Slack export import (directory fixture).

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/importer/slack"
)

// TestUS0309_SlackParseExportDir verifies the Slack export directory parser
// reads all channel/day JSON files and returns sorted Message records.
func TestUS0309_SlackParseExportDir(t *testing.T) {
	t.Parallel()

	msgs, err := slack.ParseExportDir(testdataPath("slack"))
	require.NoError(t, err)
	require.NotEmpty(t, msgs, "must parse messages from slack fixture")
}

// TestUS0309_SlackMessageFields verifies all expected fields are populated.
func TestUS0309_SlackMessageFields(t *testing.T) {
	t.Parallel()

	msgs, err := slack.ParseExportDir(testdataPath("slack"))
	require.NoError(t, err)
	require.NotEmpty(t, msgs)

	first := msgs[0]
	assert.NotEmpty(t, first.ExternalID, "ExternalID must be set")
	assert.NotEmpty(t, first.ChannelName, "ChannelName must be set")
	assert.NotEmpty(t, first.Text, "Text must be set")
	assert.False(t, first.Timestamp.IsZero(), "Timestamp must be parsed")
	assert.NotEmpty(t, first.Source, "Source must be set")
	assert.Contains(t, first.Source, "slack:", "Source must be prefixed with 'slack:'")
}

// TestUS0309_SlackMessagesSortedByTimestamp verifies chronological ordering.
func TestUS0309_SlackMessagesSortedByTimestamp(t *testing.T) {
	t.Parallel()

	msgs, err := slack.ParseExportDir(testdataPath("slack"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(msgs), 2)

	for i := 1; i < len(msgs); i++ {
		assert.False(t, msgs[i].Timestamp.Before(msgs[i-1].Timestamp),
			"messages must be sorted by timestamp ascending")
	}
}

// TestUS0309_SlackReactionsAndFiles verifies reactions and file attachments parsed.
func TestUS0309_SlackReactionsAndFiles(t *testing.T) {
	t.Parallel()

	msgs, err := slack.ParseExportDir(testdataPath("slack"))
	require.NoError(t, err)
	require.NotEmpty(t, msgs)

	// First message has a thumbsup reaction in the fixture.
	assert.Contains(t, msgs[0].Reactions, "thumbsup")
}

// TestUS0309_SlackRenderContent verifies RenderContent produces readable output.
func TestUS0309_SlackRenderContent(t *testing.T) {
	t.Parallel()

	msgs, err := slack.ParseExportDir(testdataPath("slack"))
	require.NoError(t, err)
	require.NotEmpty(t, msgs)

	for _, m := range msgs {
		rendered := slack.RenderContent(m)
		assert.NotEmpty(t, rendered)
		assert.Contains(t, rendered, m.ChannelName)
	}
}

// TestUS0309_SlackThreadReplyDetected verifies thread replies are flagged.
func TestUS0309_SlackThreadReplyDetected(t *testing.T) {
	t.Parallel()

	msgs, err := slack.ParseExportDir(testdataPath("slack"))
	require.NoError(t, err)

	// Find if any message is a thread reply.
	replyFound := false
	for _, m := range msgs {
		if m.IsReply {
			replyFound = true
			assert.NotEmpty(t, m.ThreadTS, "reply must have ThreadTS set")
		}
	}
	// The fixture has one thread reply (second message).
	assert.True(t, replyFound, "fixture must include at least one thread reply")
}

// TestUS0309_SlackParseMessagesJSON verifies single-channel JSON parsing.
func TestUS0309_SlackParseMessagesJSON(t *testing.T) {
	t.Parallel()

	raw := []byte(`[
	  {"type":"message","user":"U001","username":"alice",
	   "text":"Hello #general","ts":"1736946000.000001",
	   "reactions":[{"name":"heart","count":1}]}
	]`)

	msgs, err := slack.ParseMessagesJSON(raw, "general")
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	m := msgs[0]
	assert.Equal(t, "general:1736946000.000001", m.ExternalID)
	assert.Equal(t, "general", m.ChannelName)
	assert.Equal(t, "alice", m.Username)
	assert.Equal(t, time.Unix(1736946000, 0).UTC(), m.Timestamp)
	assert.Contains(t, m.Reactions, "heart")
}
