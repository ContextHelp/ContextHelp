package ica

import (
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	uri "hop.top/cite/scheme"
)

// NormalizedItemToDraft converts an ica NormalizedItem into a ctxt
// KnowledgeObject draft suitable for pipeline ingestion.
func NormalizedItemToDraft(
	item *NormalizedItem,
) *pluginapi.KnowledgeObject {
	if item == nil {
		return nil
	}

	ko := &pluginapi.KnowledgeObject{
		ID:          item.ID,
		RawContent:  item.ArticleBody,
		TextContent: item.ArticleBody,
		ContentHash: item.ContentHash,
		Source:      item.CanonicalURL,
		Metadata:    make(map[string]any),
	}

	// type/subtype inference
	ko.Type, ko.Subtype = inferType(item.MediaTypesPresent)

	// scalar metadata
	setIfNonEmpty(ko.Metadata, "title", item.Title)
	setIfNonEmpty(ko.Metadata, "description", item.Description)
	setIfNonEmpty(ko.Metadata, "language", item.Language)
	setIfNonEmpty(ko.Metadata, "domain", item.PrimarySource.Domain)
	setIfNonEmpty(ko.Metadata, "publisher", item.PrimarySource.Publisher)
	setIfNonEmpty(ko.Metadata, "author", item.PrimarySource.AuthorOrSpeaker)
	setIfNonEmpty(ko.Metadata, "feed_url", item.PrimarySource.FeedURL)

	if !item.PublishedAt.IsZero() {
		ko.Metadata["published_at"] = item.PublishedAt.Format(
			time.RFC3339,
		)
	}
	if item.ChannelCount > 0 {
		ko.Metadata["channel_count"] = item.ChannelCount
	}

	// media assets
	if len(item.MediaAssets) > 0 {
		assets := make([]map[string]any, len(item.MediaAssets))
		for i, a := range item.MediaAssets {
			m := map[string]any{
				"type": a.Type,
				"url":  a.URL,
			}
			if a.DurationSeconds > 0 {
				m["duration_seconds"] = a.DurationSeconds
			}
			assets[i] = m
		}
		ko.Metadata["media_assets"] = assets
	}

	// media type tags
	for _, mt := range item.MediaTypesPresent {
		ko.Tags = append(ko.Tags, pluginapi.Tag{
			Label:  "media:" + string(mt),
			Source: "media",
		})
	}

	// dist channels → mentions
	for _, ch := range item.DistChannels {
		if ch.URL == "" {
			continue
		}
		ko.Mentions = append(ko.Mentions, uri.URI{
			Scheme:    "https",
			Namespace: ch.Domain,
			ID:        ch.URL,
		})
	}

	// extracted entities → mentions
	for _, e := range item.ExtractedEntities {
		ko.Mentions = append(ko.Mentions, uri.URI{
			Scheme:    "ctxt",
			Namespace: "entity",
			ID: fmt.Sprintf(
				"%s/%s", e.Type, e.Value,
			),
		})
	}

	// chapters → sections
	if item.StructuredAnalysis != nil {
		for i, ch := range item.StructuredAnalysis.Chapters {
			ko.Sections = append(ko.Sections, pluginapi.Section{
				Title:   ch.Phase,
				Content: ch.VoiceoverSummary,
				Order:   i,
				Metadata: map[string]any{
					"chapter_id":      ch.ID,
					"timestamp_range": ch.TimestampRange,
				},
			})
		}
	}

	return ko
}

// DraftToNormalizedItem converts a ctxt KnowledgeObject back into an
// ica NormalizedItem. Used by the ica_processor step.
func DraftToNormalizedItem(
	ko *pluginapi.KnowledgeObject,
) *NormalizedItem {
	if ko == nil {
		return nil
	}

	item := &NormalizedItem{
		ID:           ko.ID,
		CanonicalURL: ko.Source,
		ArticleBody:  ko.RawContent,
		ContentHash:  ko.ContentHash,
	}

	if ko.Metadata != nil {
		item.Title = metaStr(ko.Metadata, "title")
		item.Description = metaStr(ko.Metadata, "description")
		item.Language = metaStr(ko.Metadata, "language")
		item.PrimarySource.Domain = metaStr(ko.Metadata, "domain")
		item.PrimarySource.Publisher = metaStr(
			ko.Metadata, "publisher",
		)
		item.PrimarySource.AuthorOrSpeaker = metaStr(
			ko.Metadata, "author",
		)
		item.PrimarySource.FeedURL = metaStr(ko.Metadata, "feed_url")

		if v := metaStr(ko.Metadata, "published_at"); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				item.PublishedAt = t
			}
		}

		if v, ok := ko.Metadata["channel_count"].(int); ok {
			item.ChannelCount = v
		} else if v, ok := ko.Metadata["channel_count"].(float64); ok {
			item.ChannelCount = int(v)
		}

		item.MediaAssets = decodeMediaAssets(ko.Metadata)
	}

	// tags → media types
	for _, t := range ko.Tags {
		if t.Source == "media" && len(t.Label) > 6 &&
			t.Label[:6] == "media:" {
			item.MediaTypesPresent = append(
				item.MediaTypesPresent,
				MediaType(t.Label[6:]),
			)
		}
	}

	// mentions → dist channels + entities
	for _, m := range ko.Mentions {
		if m.Scheme == "ctxt" && m.Namespace == "entity" {
			e := parseEntityID(m.ID)
			if e.Type != "" {
				item.ExtractedEntities = append(
					item.ExtractedEntities, e,
				)
			}
			continue
		}
		item.DistChannels = append(item.DistChannels, DistChannel{
			Domain: m.Namespace,
			URL:    m.ID,
		})
	}

	// sections → chapters
	if len(ko.Sections) > 0 {
		sa := &StructuredAnalysis{}
		for _, s := range ko.Sections {
			ch := Chapter{
				Phase:            s.Title,
				VoiceoverSummary: s.Content,
			}
			if s.Metadata != nil {
				if v, ok := s.Metadata["chapter_id"].(string); ok {
					ch.ID = v
				}
				if v, ok := s.Metadata["timestamp_range"].(string); ok {
					ch.TimestampRange = v
				}
			}
			sa.Chapters = append(sa.Chapters, ch)
		}
		item.StructuredAnalysis = sa
	}

	return item
}

// inferType determines KO type/subtype from media types present.
func inferType(mts []MediaType) (string, string) {
	has := make(map[MediaType]bool, len(mts))
	for _, mt := range mts {
		has[mt] = true
	}
	switch {
	case has[MediaVideo]:
		return "media", "video"
	case has[MediaAudio]:
		return "media", "audio"
	case has[MediaImage]:
		return "media", "image"
	default:
		return "article", ""
	}
}

func setIfNonEmpty(m map[string]any, key, val string) {
	if val != "" {
		m[key] = val
	}
}

func metaStr(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// parseEntityID splits "type/value" into Entity fields.
func parseEntityID(id string) Entity {
	for i := 0; i < len(id); i++ {
		if id[i] == '/' {
			return Entity{Type: id[:i], Value: id[i+1:]}
		}
	}
	return Entity{}
}

// decodeMediaAssets extracts media_assets from metadata.
func decodeMediaAssets(m map[string]any) []MediaAsset {
	raw, ok := m["media_assets"]
	if !ok {
		return nil
	}
	slice, ok := raw.([]map[string]any)
	if !ok {
		// handle []any (e.g. from JSON round-trip)
		iSlice, ok := raw.([]any)
		if !ok {
			return nil
		}
		slice = make([]map[string]any, 0, len(iSlice))
		for _, v := range iSlice {
			if mm, ok := v.(map[string]any); ok {
				slice = append(slice, mm)
			}
		}
	}

	out := make([]MediaAsset, 0, len(slice))
	for _, mm := range slice {
		a := MediaAsset{}
		if v, ok := mm["type"].(string); ok {
			a.Type = v
		}
		if v, ok := mm["url"].(string); ok {
			a.URL = v
		}
		if v, ok := mm["duration_seconds"].(int); ok {
			a.DurationSeconds = v
		} else if v, ok := mm["duration_seconds"].(float64); ok {
			a.DurationSeconds = int(v)
		}
		out = append(out, a)
	}
	return out
}
