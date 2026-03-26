# US-0005: Video Processing with Scenes

**System Types:** ctxt, dpkms (self-hosted)
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Agents & LLMs](../../personas/agents-llms-tools.md)

---

## User Goal

As a knowledge worker, I want to add video files (lectures, demos, meetings) and have them automatically transcribed, scene-segmented, and indexed so that I can search and retrieve knowledge from video content without watching it again.

---

## Context

Video is one of the richest and most time-consuming knowledge formats. A 60-minute recorded lecture, product demo, or meeting contains far more context than a text summary ever captures -- speaker tone, visual diagrams drawn on whiteboards, slide transitions, code walkthroughs on screen. Yet most knowledge systems treat video as an opaque blob: store it, maybe attach a title, and move on.

The cost of not decomposing video is steep. A developer who attended a 90-minute architecture review cannot quickly find "the part where we discussed the caching layer." A product manager cannot search across 40 recorded user interviews for moments where participants mentioned onboarding friction. The video exists, but the knowledge inside it is locked behind linear playback.

This story unlocks video knowledge through a multi-modal pipeline that combines audio transcription with visual scene detection. The output is a structured KnowledgeObject where each Section is a timestamped segment aligned to scene boundaries -- containing both the transcript of what was said and a description of what was shown. This enables timestamp-precise search: "find the moment in last Tuesday's demo where the error appeared on screen" returns a direct link to 34:17 in the recording.

---

## Acceptance Criteria

- [ ] User can add video files via `ctxt add --file lecture.mp4` with automatic format detection
- [ ] Supported formats: MP4, MOV, WEBM, AVI, MKV
- [ ] System extracts audio track and transcribes it with timestamps
- [ ] System samples keyframes and detects visual scene changes
- [ ] Each scene produces a Section with timestamp, transcript segment, and frame description
- [ ] Video metadata (duration, resolution, fps, codec) stored in Metadata
- [ ] Processing is async -- returns job ID immediately
- [ ] User can choose audio-only processing via `--pipeline video.audio_only`
- [ ] Corrupt, unsupported-codec, or oversized files are rejected with clear error messages
- [ ] Processed video content is searchable within enrichment completion time

---

## Implementation Notes

### CLI Interface

```bash
# Basic video capture -- format auto-detected
ctxt add --file lecture.mp4

# Explicit type hint
ctxt add --file demo.webm --type video

# Audio-only pipeline (skip visual analysis, faster)
ctxt add --file meeting.mov --pipeline video.audio_only

# With profile and project context
ctxt add --file sprint-review.mp4 --profile engineering --project mobile-app

# Returns immediately with JSON
{
  "job_id": "j-vid-8a3f21",
  "object_id": "o-vid-c72b90",
  "status": "pending_enrichment",
  "pipeline": "video.full",
  "source": "/home/user/lecture.mp4",
  "metadata": {
    "duration_seconds": 3847,
    "resolution": "1920x1080",
    "fps": 30,
    "format": "mp4"
  }
}

# Check job progress (video processing can take minutes)
ctxt job get j-vid-8a3f21
{
  "job_id": "j-vid-8a3f21",
  "status": "processing",
  "pipeline": "video.full",
  "progress": {
    "current_step": "AudioTranscriber",
    "steps_completed": 3,
    "steps_total": 11,
    "percent": 27
  }
}
```

### REST API

```
POST /analyze
Content-Type: multipart/form-data

file: <binary video data>
content_type: video/mp4
pipeline: video.full
profile: engineering

-> 202 Accepted
{
  "job_id": "j-vid-8a3f21",
  "object_id": "o-vid-c72b90",
  "status": "pending_enrichment",
  "pipeline": "video.full"
}
```

For large files, chunked upload is supported:

```
POST /upload/init
Content-Type: application/json

{
  "filename": "lecture.mp4",
  "content_type": "video/mp4",
  "total_size": 1073741824,
  "chunk_size": 10485760
}

-> 200 OK
{
  "upload_id": "up-9f2c3d",
  "chunk_count": 103,
  "chunk_size": 10485760
}

PUT /upload/up-9f2c3d/chunks/0
Content-Type: application/octet-stream

<binary chunk data>

-> 200 OK
{"chunk": 0, "received": 10485760}

# After all chunks uploaded:
POST /upload/up-9f2c3d/complete
Content-Type: application/json

{
  "pipeline": "video.full",
  "profile": "engineering"
}

-> 202 Accepted
{
  "job_id": "j-vid-8a3f21",
  "object_id": "o-vid-c72b90",
  "status": "pending_enrichment",
  "pipeline": "video.full"
}
```

### Pipeline Steps

**`video.full`** (audio + visual analysis):

```
FileReader → FormatDetector → AudioExtractor → AudioTranscriber →
FrameSampler → SceneDetector → FrameOCR → TimelineAssembler →
Sectioner → Tagger → EmbeddingGenerator
```

**`video.audio_only`** (transcription only, no visual analysis):

```
FileReader → FormatDetector → AudioExtractor → AudioTranscriber →
Sectioner → Tagger → EmbeddingGenerator
```

Each step implements the `PipelineStep` interface:

```go
type PipelineStep interface {
    Name() string
    Run(ctx context.Context, draft *KnowledgeObject) (*KnowledgeObject, error)
}
```

Step responsibilities:

| Step | Input | Output |
|------|-------|--------|
| FileReader | File path | Raw bytes in draft.RawContent, file metadata |
| FormatDetector | Raw bytes | Detected codec, container format, validates support |
| AudioExtractor | Video bytes | Extracted audio track (WAV/FLAC) in temp storage |
| AudioTranscriber | Audio bytes | Timestamped transcript segments |
| FrameSampler | Video bytes | Keyframes extracted at interval or scene boundaries |
| SceneDetector | Keyframes + video | Scene boundary timestamps (threshold or ML-based) |
| FrameOCR | Keyframes | OCR text per frame (slides, whiteboard, code on screen) |
| TimelineAssembler | Transcript + scenes + OCR | Unified timeline merging audio and visual streams |
| Sectioner | Unified timeline | Scene-aligned Sections with timestamps |
| Tagger | Sections + transcript | Tags from vocabulary |
| EmbeddingGenerator | Sections + transcript | Embeddings per section + full-document embedding |

### Backend Processing

1. `ctxt` receives file via CLI (`--file`) or REST API (multipart/chunked upload)
2. FileReader reads video file from disk, stores raw file reference in `Source`
3. FormatDetector probes the container and codec; rejects unsupported formats with error
4. AudioExtractor demuxes the audio track to a temporary WAV/FLAC file using ffmpeg
5. AudioTranscriber sends audio to the configured speech-to-text provider; receives timestamped transcript segments (e.g., Whisper output with word-level timestamps)
6. FrameSampler extracts keyframes at the configured interval (default: every 10 seconds) or at scene boundaries if SceneDetector runs first (two-pass mode)
7. SceneDetector analyzes frame sequence for visual discontinuities; threshold-based (histogram difference > configurable threshold) or ML-based (CLIP similarity drop)
8. FrameOCR runs OCR on each sampled keyframe to extract text visible on screen (slides, code, whiteboard)
9. TimelineAssembler merges the three streams (transcript segments, scene boundaries, OCR text) into a unified timeline ordered by timestamp
10. Sectioner creates a Section for each scene, containing: start/end timestamps, transcript segment for that time range, frame description and OCR text, and scene transition type
11. Tagger assigns tags from vocabulary based on transcript content and OCR text
12. EmbeddingGenerator creates embeddings for each Section (enabling per-scene search) plus a full-document embedding
13. KnowledgeObject is persisted to `objects` table; graph edges created for mentions/entities
14. Job status updated to `completed`

### KnowledgeObject Structure

```go
// Resulting KnowledgeObject for a processed video
KnowledgeObject{
    ID:          "o-vid-c72b90",
    Type:        "video",
    Subtype:     "full",           // or "audio_only"
    RawContent:  "<full transcript text>",
    ContentType: "video/mp4",
    Source: Source{
        Type: "file",
        Path: "/home/user/lecture.mp4",
        Size: 1073741824,
    },
    Metadata: map[string]any{
        "duration_seconds": 3847,
        "resolution":       "1920x1080",
        "fps":              30,
        "codec_video":      "h264",
        "codec_audio":      "aac",
        "format":           "mp4",
        "scene_count":      24,
        "transcript_words":  28340,
    },
    Sections: []Section{
        {
            ID:        "sec-001",
            Title:     "Introduction",
            Start:     "00:00:00",
            End:       "00:03:42",
            Content:   "Welcome everyone to the architecture review...",
            Metadata: map[string]any{
                "frame_description": "Title slide: Q1 Architecture Review",
                "ocr_text":          "Q1 Architecture Review - Platform Team",
                "scene_type":        "slide",
            },
        },
        {
            ID:        "sec-002",
            Title:     "Caching Layer Discussion",
            Start:     "00:03:42",
            End:       "00:12:15",
            Content:   "So let's talk about the caching layer...",
            Metadata: map[string]any{
                "frame_description": "Whiteboard diagram showing Redis cluster topology",
                "ocr_text":          "Redis Primary -> Redis Replica, TTL: 300s",
                "scene_type":        "whiteboard",
            },
        },
        // ... additional sections
    },
    Tags:      []string{"architecture", "caching", "redis"},
    Mentions:  []Mention{{Entity: "@redis"}, {Entity: "@platform-team"}},
    Pipeline:  "video.full",
}
```

### Configuration

```yaml
# In configuration.yaml
video:
  # Maximum file size (bytes). Default: 2GB
  maxFileSize: 2147483648

  # Maximum video duration (seconds). Default: 7200 (2 hours)
  maxDuration: 7200

  # Processing timeout (seconds). Default: 1800 (30 minutes)
  processingTimeout: 1800

  # Frame sampling interval (seconds). Default: 10
  frameSamplingInterval: 10

  # Scene detection method: "threshold" or "ml"
  sceneDetection:
    method: threshold
    # Histogram difference threshold (0.0-1.0). Default: 0.4
    threshold: 0.4
    # Minimum scene duration (seconds) to avoid over-segmentation
    minSceneDuration: 5

  # Supported formats (validated by FormatDetector)
  supportedFormats:
    - mp4
    - mov
    - webm
    - avi
    - mkv

  # Audio extraction settings
  audio:
    # Output format for extracted audio track
    format: wav
    # Sample rate for transcription
    sampleRate: 16000

  # Transcription provider (reuses audio pipeline config from US-0004)
  transcription:
    provider: ${aiProvider}
    model: whisper-large-v3
    language: auto  # auto-detect or specify ISO code

  # OCR settings for frame analysis
  frameOCR:
    enabled: true
    provider: ${aiProvider}
    # Minimum confidence threshold for OCR results
    confidenceThreshold: 0.7

  # ffmpeg binary path (auto-detected if on PATH)
  ffmpegPath: auto
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt add --file lecture.mp4` returns job ID within 2 seconds; server creates job record with `source_type=video` and `pipeline=video.full`
- [ ] CLI: Job status transitions from `pending` -> `processing` -> `completed`
- [ ] CLI: Completed object contains timestamped Sections aligned to scene boundaries — GET /objects/{object_id} confirms each Section has `start`, `end`, and `metadata.scene_type` fields
- [ ] CLI: Full transcript stored in RawContent, video metadata (`duration_seconds`, `resolution`, `fps`, `format`) stored in Metadata — GET /objects/{object_id} confirms all four keys present
- [ ] CLI: `ctxt add --file meeting.mov --pipeline video.audio_only` sends `"pipeline": "video.audio_only"` in the request payload; stored object's pipeline field equals `"video.audio_only"`
- [ ] CLI: `ctxt add --file sprint-review.mp4 --profile engineering --project mobile-app` sends `"profile": "engineering"` and `"project": "mobile-app"` in the server request payload; stored object Metadata contains both fields
- [ ] Pipeline: `video.full` runs all 11 steps in correct order — stored object pipeline.steps_completed contains all 11 step names
- [ ] Pipeline: `video.audio_only` skips FrameSampler, SceneDetector, FrameOCR, TimelineAssembler — stored object pipeline.steps_completed excludes those four names
- [ ] Format: MP4, MOV, WEBM files accepted and processed correctly
- [ ] Format: Unsupported codec (e.g., VP9 without ffmpeg support) returns clear error
- [ ] Error: Corrupt video file rejected with descriptive error message
- [ ] Error: File exceeding `maxFileSize` (default 2GB) rejected before processing starts
- [ ] Error: Processing timeout (default 30 min) cancels job and reports timeout error
- [ ] REST API: Multipart upload with `pipeline=video.full` and `profile=engineering` returns 202 + job ID; server records both fields; GET /objects/{object_id} confirms them in stored object
- [ ] REST API: Chunked upload flow (init, chunks, complete) works for large files; final complete request receives 202 with job ID
- [ ] Search: Transcript text searchable via `ctxt search "caching layer"`
- [ ] Search: OCR text from frames searchable via `ctxt search "Redis Primary"`
- [ ] Config: Custom `frameSamplingInterval` changes keyframe extraction rate
- [ ] Config: Scene detection threshold adjusts segmentation granularity

---

## Related Stories

- [US-0003](./US-0003-image-ocr-and-analysis.md) -- Frame OCR reuses the image analysis pipeline for per-frame text extraction
- [US-0004](./US-0004-audio-transcription-and-indexing.md) -- Audio track extraction and transcription reuses the audio pipeline
- [US-0061](../search/US-0061-visual-similarity-search.md) -- Multimodal search enables searching video frames by visual similarity
- [US-0001](./US-0001-text-capture-minimal-friction.md) -- Base capture flow and async job pattern
- [US-0012](../enrichment/US-0012-generate-summaries-and-sections.md) -- Sectioner step shared with text summarization enrichment

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Solo Developer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/solo-developer.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)

---

## E2E Tests

[us0005_video_processing_test.go](../../../test/integration/us0005_video_processing_test.go)
