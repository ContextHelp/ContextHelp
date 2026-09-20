// Package ica — local mirrors of ica domain types for JSON interop.
//
// These structs mirror hop.top/ica/internal/domain types so the ctxt
// plugin can serialize/deserialize NormalizedItem without importing
// ica as a Go dependency.
package ica

import "time"

// MediaType represents the type of content present in an item.
type MediaType string

const (
	MediaText  MediaType = "text"
	MediaImage MediaType = "image"
	MediaAudio MediaType = "audio"
	MediaVideo MediaType = "video"
)

// NormalizedItem mirrors ica's canonical content structure.
type NormalizedItem struct {
	ID                 string              `json:"id"`
	CanonicalURL       string              `json:"canonical_url"`
	PublishedAt        time.Time           `json:"published_at"`
	Language           string              `json:"language"`
	ContentHash        string              `json:"content_hash"`
	Title              string              `json:"title"`
	Description        string              `json:"description"`
	ArticleBody        string              `json:"article_body"`
	Transcript         string              `json:"transcript"`
	PrimarySource      Source              `json:"primary_source"`
	RightsMetadata     Rights              `json:"rights_metadata"`
	MediaTypesPresent  []MediaType         `json:"media_types_present"`
	MediaAssets        []MediaAsset        `json:"media_assets"`
	StructuredAnalysis *StructuredAnalysis `json:"structured_analysis,omitempty"`
	ExtractedEntities  []Entity            `json:"extracted_entities"`
	Embedding          []float32           `json:"embedding"`
	DistChannels       []DistChannel       `json:"distribution_channels"`
	ChannelCount       int                 `json:"channel_count"`
}

// Source identifies the origin of content.
type Source struct {
	Domain          string `json:"domain"`
	Publisher       string `json:"publisher"`
	AuthorOrSpeaker string `json:"author_or_speaker"`
	FeedURL         string `json:"feed_url"`
}

// Rights holds licensing metadata.
type Rights struct {
	License string `json:"license"`
}

// MediaAsset represents an attached media resource.
type MediaAsset struct {
	Type            string `json:"type"`
	URL             string `json:"url"`
	DurationSeconds int    `json:"duration_seconds"`
}

// Entity is a named entity extracted from content.
type Entity struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// DistChannel records where content was distributed.
type DistChannel struct {
	Domain       string    `json:"domain"`
	URL          string    `json:"url"`
	DiscoveredAt time.Time `json:"discovered_at"`
}

// StructuredAnalysis holds chapter/storyboard breakdowns.
type StructuredAnalysis struct {
	Type       string            `json:"type"`
	Summary    AnalysisSummary   `json:"summary"`
	Chapters   []Chapter         `json:"chapters"`
	Storyboard []StoryboardEntry `json:"storyboard"`
}

// AnalysisSummary holds high-level analysis stats.
type AnalysisSummary struct {
	SceneCount    int    `json:"scene_count"`
	NarrationType string `json:"narration_type"`
}

// Chapter represents a structural segment of analysed content.
type Chapter struct {
	ID               string `json:"id"`
	Phase            string `json:"phase"`
	TimestampRange   string `json:"timestamp_range"`
	VisualSummary    string `json:"visual_summary"`
	VoiceoverSummary string `json:"voiceover_summary"`
	Scenes           []int  `json:"scenes"`
}

// StoryboardEntry is a single scene in a storyboard.
type StoryboardEntry struct {
	SceneNumber       int            `json:"scene_number"`
	Timestamp         string         `json:"timestamp"`
	VisualDescription string         `json:"visual_description"`
	Voiceover         string         `json:"voiceover"`
	Cinematography    Cinematography `json:"cinematography"`
}

// Cinematography captures camera/lighting metadata for a scene.
type Cinematography struct {
	ShotSize       string `json:"shot_size"`
	Lighting       string `json:"lighting"`
	CameraMovement string `json:"camera_movement"`
}
