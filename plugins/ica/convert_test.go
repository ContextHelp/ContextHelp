package ica

import (
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	uri "hop.top/cite/scheme"
)

func TestNormalizedItemToDraft(t *testing.T) {
	pub := time.Date(2025, 3, 15, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		item   *NormalizedItem
		assert func(t *testing.T, ko *pluginapi.KnowledgeObject)
	}{
		{
			name: "nil returns nil",
			item: nil,
			assert: func(t *testing.T, ko *pluginapi.KnowledgeObject) {
				assert.Nil(t, ko)
			},
		},
		{
			name: "article with text only",
			item: &NormalizedItem{
				ID:           "item-1",
				CanonicalURL: "https://example.com/post",
				Title:        "Test Post",
				Description:  "A test",
				ArticleBody:  "Body text",
				ContentHash:  "abc123",
				Language:     "en",
				PublishedAt:  pub,
				PrimarySource: Source{
					Domain:          "example.com",
					Publisher:       "Example",
					AuthorOrSpeaker: "Alice",
					FeedURL:         "https://example.com/feed",
				},
				MediaTypesPresent: []MediaType{MediaText},
				ChannelCount:      2,
			},
			assert: func(t *testing.T, ko *pluginapi.KnowledgeObject) {
				assert.Equal(t, "item-1", ko.ID)
				assert.Equal(t, "article", ko.Type)
				assert.Equal(t, "", ko.Subtype)
				assert.Equal(t, "Body text", ko.RawContent)
				assert.Equal(t, "Body text", ko.TextContent)
				assert.Equal(t, "abc123", ko.ContentHash)
				assert.Equal(t,
					"https://example.com/post", ko.Source,
				)
				assert.Equal(t,
					"Test Post", ko.Metadata["title"],
				)
				assert.Equal(t,
					"A test", ko.Metadata["description"],
				)
				assert.Equal(t, "en", ko.Metadata["language"])
				assert.Equal(t,
					"example.com", ko.Metadata["domain"],
				)
				assert.Equal(t,
					"Example", ko.Metadata["publisher"],
				)
				assert.Equal(t,
					"Alice", ko.Metadata["author"],
				)
				assert.Equal(t,
					"https://example.com/feed",
					ko.Metadata["feed_url"],
				)
				assert.Equal(t,
					pub.Format(time.RFC3339),
					ko.Metadata["published_at"],
				)
				assert.Equal(t, 2, ko.Metadata["channel_count"])

				require.Len(t, ko.Tags, 1)
				assert.Equal(t, "media:text", ko.Tags[0].Label)
				assert.Equal(t, "media", ko.Tags[0].Source)
			},
		},
		{
			name: "video type inference",
			item: &NormalizedItem{
				MediaTypesPresent: []MediaType{
					MediaText, MediaVideo,
				},
			},
			assert: func(t *testing.T, ko *pluginapi.KnowledgeObject) {
				assert.Equal(t, "media", ko.Type)
				assert.Equal(t, "video", ko.Subtype)
			},
		},
		{
			name: "audio type inference",
			item: &NormalizedItem{
				MediaTypesPresent: []MediaType{MediaAudio},
			},
			assert: func(t *testing.T, ko *pluginapi.KnowledgeObject) {
				assert.Equal(t, "media", ko.Type)
				assert.Equal(t, "audio", ko.Subtype)
			},
		},
		{
			name: "image type inference",
			item: &NormalizedItem{
				MediaTypesPresent: []MediaType{
					MediaText, MediaImage,
				},
			},
			assert: func(t *testing.T, ko *pluginapi.KnowledgeObject) {
				assert.Equal(t, "media", ko.Type)
				assert.Equal(t, "image", ko.Subtype)
			},
		},
		{
			name: "media assets",
			item: &NormalizedItem{
				MediaAssets: []MediaAsset{
					{
						Type:            "video",
						URL:             "https://cdn.example.com/v.mp4",
						DurationSeconds: 120,
					},
				},
			},
			assert: func(t *testing.T, ko *pluginapi.KnowledgeObject) {
				raw, ok := ko.Metadata["media_assets"]
				require.True(t, ok)
				assets, ok := raw.([]map[string]any)
				require.True(t, ok)
				require.Len(t, assets, 1)
				assert.Equal(t, "video", assets[0]["type"])
				assert.Equal(t,
					"https://cdn.example.com/v.mp4",
					assets[0]["url"],
				)
				assert.Equal(t, 120, assets[0]["duration_seconds"])
			},
		},
		{
			name: "dist channels as mentions",
			item: &NormalizedItem{
				DistChannels: []DistChannel{
					{
						Domain: "youtube.com",
						URL:    "https://youtube.com/watch?v=abc",
					},
					{Domain: "x.com", URL: ""},
				},
			},
			assert: func(t *testing.T, ko *pluginapi.KnowledgeObject) {
				require.Len(t, ko.Mentions, 1)
				assert.Equal(t, "https", ko.Mentions[0].Scheme)
				assert.Equal(t,
					"youtube.com", ko.Mentions[0].Namespace,
				)
			},
		},
		{
			name: "entities as mentions",
			item: &NormalizedItem{
				ExtractedEntities: []Entity{
					{Type: "person", Value: "Alice"},
					{Type: "org", Value: "Acme"},
				},
			},
			assert: func(t *testing.T, ko *pluginapi.KnowledgeObject) {
				require.Len(t, ko.Mentions, 2)
				assert.Equal(t, uri.URI{
					Scheme:    "ctxt",
					Namespace: "entity",
					ID:        "person/Alice",
				}, ko.Mentions[0])
				assert.Equal(t, uri.URI{
					Scheme:    "ctxt",
					Namespace: "entity",
					ID:        "org/Acme",
				}, ko.Mentions[1])
			},
		},
		{
			name: "chapters as sections",
			item: &NormalizedItem{
				StructuredAnalysis: &StructuredAnalysis{
					Chapters: []Chapter{
						{
							ID:               "ch-1",
							Phase:            "Introduction",
							TimestampRange:   "00:00-01:30",
							VoiceoverSummary: "Opening remarks",
						},
						{
							ID:               "ch-2",
							Phase:            "Main",
							TimestampRange:   "01:30-10:00",
							VoiceoverSummary: "Core content",
						},
					},
				},
			},
			assert: func(t *testing.T, ko *pluginapi.KnowledgeObject) {
				require.Len(t, ko.Sections, 2)
				assert.Equal(t, "Introduction", ko.Sections[0].Title)
				assert.Equal(t,
					"Opening remarks", ko.Sections[0].Content,
				)
				assert.Equal(t, 0, ko.Sections[0].Order)
				assert.Equal(t,
					"ch-1", ko.Sections[0].Metadata["chapter_id"],
				)
				assert.Equal(t, 1, ko.Sections[1].Order)
			},
		},
		{
			// ICA's vector comes from a model outside the embedding
			// registry, so it never lands on the object.
			name: "embedding dropped",
			item: &NormalizedItem{
				Embedding: []float32{0.1, 0.2, 0.3},
			},
			assert: func(t *testing.T, ko *pluginapi.KnowledgeObject) {
				assert.Empty(t, ko.Vectors)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ko := NormalizedItemToDraft(tt.item)
			tt.assert(t, ko)
		})
	}
}

func TestDraftToNormalizedItem(t *testing.T) {
	tests := []struct {
		name   string
		ko     *pluginapi.KnowledgeObject
		assert func(t *testing.T, item *NormalizedItem)
	}{
		{
			name: "nil returns nil",
			ko:   nil,
			assert: func(t *testing.T, item *NormalizedItem) {
				assert.Nil(t, item)
			},
		},
		{
			name: "basic fields",
			ko: &pluginapi.KnowledgeObject{
				ID:          "ko-1",
				RawContent:  "Body text",
				ContentHash: "abc123",
				Source:      "https://example.com/post",
				Metadata: map[string]any{
					"title":         "Test Post",
					"description":   "A test",
					"language":      "en",
					"domain":        "example.com",
					"publisher":     "Example",
					"author":        "Alice",
					"feed_url":      "https://example.com/feed",
					"published_at":  "2025-03-15T10:00:00Z",
					"channel_count": 3,
				},
			},
			assert: func(t *testing.T, item *NormalizedItem) {
				assert.Equal(t, "ko-1", item.ID)
				assert.Equal(t,
					"https://example.com/post", item.CanonicalURL,
				)
				assert.Equal(t, "Body text", item.ArticleBody)
				assert.Equal(t, "abc123", item.ContentHash)
				assert.Equal(t, "Test Post", item.Title)
				assert.Equal(t, "A test", item.Description)
				assert.Equal(t, "en", item.Language)
				assert.Equal(t,
					"example.com", item.PrimarySource.Domain,
				)
				assert.Equal(t,
					"Example", item.PrimarySource.Publisher,
				)
				assert.Equal(t,
					"Alice", item.PrimarySource.AuthorOrSpeaker,
				)
				assert.Equal(t,
					"https://example.com/feed",
					item.PrimarySource.FeedURL,
				)
				assert.Equal(t,
					time.Date(2025, 3, 15, 10, 0, 0, 0, time.UTC),
					item.PublishedAt,
				)
				assert.Equal(t, 3, item.ChannelCount)
				assert.Nil(t, item.Embedding)
			},
		},
		{
			name: "tags to media types",
			ko: &pluginapi.KnowledgeObject{
				Tags: []pluginapi.Tag{
					{Label: "media:video", Source: "media"},
					{Label: "media:text", Source: "media"},
					{Label: "topic:go", Source: "auto"},
				},
			},
			assert: func(t *testing.T, item *NormalizedItem) {
				require.Len(t, item.MediaTypesPresent, 2)
				assert.Equal(t,
					MediaVideo, item.MediaTypesPresent[0],
				)
				assert.Equal(t,
					MediaText, item.MediaTypesPresent[1],
				)
			},
		},
		{
			name: "mentions to channels and entities",
			ko: &pluginapi.KnowledgeObject{
				Mentions: []uri.URI{
					{
						Scheme:    "ctxt",
						Namespace: "entity",
						ID:        "person/Alice",
					},
					{
						Scheme:    "https",
						Namespace: "youtube.com",
						ID:        "https://youtube.com/watch?v=x",
					},
				},
			},
			assert: func(t *testing.T, item *NormalizedItem) {
				require.Len(t, item.ExtractedEntities, 1)
				assert.Equal(t, "person", item.ExtractedEntities[0].Type)
				assert.Equal(t, "Alice", item.ExtractedEntities[0].Value)

				require.Len(t, item.DistChannels, 1)
				assert.Equal(t,
					"youtube.com", item.DistChannels[0].Domain,
				)
			},
		},
		{
			name: "sections to chapters",
			ko: &pluginapi.KnowledgeObject{
				Sections: []pluginapi.Section{
					{
						Title:   "Intro",
						Content: "Opening",
						Order:   0,
						Metadata: map[string]any{
							"chapter_id":      "ch-1",
							"timestamp_range": "00:00-01:00",
						},
					},
				},
			},
			assert: func(t *testing.T, item *NormalizedItem) {
				require.NotNil(t, item.StructuredAnalysis)
				chs := item.StructuredAnalysis.Chapters
				require.Len(t, chs, 1)
				assert.Equal(t, "ch-1", chs[0].ID)
				assert.Equal(t, "Intro", chs[0].Phase)
				assert.Equal(t, "Opening", chs[0].VoiceoverSummary)
				assert.Equal(t,
					"00:00-01:00", chs[0].TimestampRange,
				)
			},
		},
		{
			name: "media assets from metadata",
			ko: &pluginapi.KnowledgeObject{
				Metadata: map[string]any{
					"media_assets": []map[string]any{
						{
							"type":             "audio",
							"url":              "https://cdn/a.mp3",
							"duration_seconds": 60,
						},
					},
				},
			},
			assert: func(t *testing.T, item *NormalizedItem) {
				require.Len(t, item.MediaAssets, 1)
				assert.Equal(t, "audio", item.MediaAssets[0].Type)
				assert.Equal(t,
					"https://cdn/a.mp3", item.MediaAssets[0].URL,
				)
				assert.Equal(t, 60, item.MediaAssets[0].DurationSeconds)
			},
		},
		{
			name: "channel_count as float64",
			ko: &pluginapi.KnowledgeObject{
				Metadata: map[string]any{
					"channel_count": float64(5),
				},
			},
			assert: func(t *testing.T, item *NormalizedItem) {
				assert.Equal(t, 5, item.ChannelCount)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := DraftToNormalizedItem(tt.ko)
			tt.assert(t, item)
		})
	}
}

func TestRoundTrip(t *testing.T) {
	pub := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

	original := &NormalizedItem{
		ID:           "rt-1",
		CanonicalURL: "https://example.com/video",
		PublishedAt:  pub,
		Language:     "en",
		ContentHash:  "hash123",
		Title:        "Round Trip",
		Description:  "Testing round trip",
		ArticleBody:  "Full body content",
		PrimarySource: Source{
			Domain:          "example.com",
			Publisher:       "Pub",
			AuthorOrSpeaker: "Bob",
			FeedURL:         "https://example.com/rss",
		},
		MediaTypesPresent: []MediaType{MediaText, MediaVideo},
		MediaAssets: []MediaAsset{
			{Type: "video", URL: "https://cdn/v.mp4", DurationSeconds: 300},
		},
		StructuredAnalysis: &StructuredAnalysis{
			Chapters: []Chapter{
				{
					ID:               "ch-1",
					Phase:            "Intro",
					TimestampRange:   "00:00-02:00",
					VoiceoverSummary: "Welcome",
				},
			},
		},
		ExtractedEntities: []Entity{
			{Type: "person", Value: "Bob"},
		},
		Embedding:    []float32{0.1, 0.2},
		ChannelCount: 4,
	}

	ko := NormalizedItemToDraft(original)
	require.NotNil(t, ko)

	rt := DraftToNormalizedItem(ko)
	require.NotNil(t, rt)

	// core fields
	assert.Equal(t, original.ID, rt.ID)
	assert.Equal(t, original.CanonicalURL, rt.CanonicalURL)
	assert.Equal(t, original.PublishedAt, rt.PublishedAt)
	assert.Equal(t, original.Language, rt.Language)
	assert.Equal(t, original.ContentHash, rt.ContentHash)
	assert.Equal(t, original.Title, rt.Title)
	assert.Equal(t, original.Description, rt.Description)
	assert.Equal(t, original.ArticleBody, rt.ArticleBody)
	assert.Equal(t, original.PrimarySource, rt.PrimarySource)
	assert.Nil(t, rt.Embedding, "ICA's vector is not carried on the object")
	assert.Equal(t, original.ChannelCount, rt.ChannelCount)

	// media types preserved
	assert.Equal(t,
		original.MediaTypesPresent, rt.MediaTypesPresent,
	)

	// entities preserved
	assert.Equal(t,
		original.ExtractedEntities, rt.ExtractedEntities,
	)

	// chapters preserved
	require.NotNil(t, rt.StructuredAnalysis)
	require.Len(t, rt.StructuredAnalysis.Chapters, 1)
	assert.Equal(t,
		original.StructuredAnalysis.Chapters[0].ID,
		rt.StructuredAnalysis.Chapters[0].ID,
	)
	assert.Equal(t,
		original.StructuredAnalysis.Chapters[0].Phase,
		rt.StructuredAnalysis.Chapters[0].Phase,
	)
	assert.Equal(t,
		original.StructuredAnalysis.Chapters[0].VoiceoverSummary,
		rt.StructuredAnalysis.Chapters[0].VoiceoverSummary,
	)

	// media assets survive round-trip
	require.Len(t, rt.MediaAssets, 1)
	assert.Equal(t, original.MediaAssets[0].Type, rt.MediaAssets[0].Type)
	assert.Equal(t, original.MediaAssets[0].URL, rt.MediaAssets[0].URL)
	assert.Equal(t,
		original.MediaAssets[0].DurationSeconds,
		rt.MediaAssets[0].DurationSeconds,
	)
}
