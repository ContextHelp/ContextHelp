# US-0004: Audio Transcription and Indexing

**System Types:** ctxt, dpkms (self-hosted)
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker, I want to capture audio recordings (meetings, voice notes, interviews) and have them automatically transcribed and indexed so that spoken information becomes searchable in my knowledge base.

---

## Context

A significant portion of organizational knowledge is generated in spoken form: meetings, standups, one-on-ones, conference talks, voice memos, and interviews. This information is typically lost the moment a meeting ends unless someone takes manual notes. Even when recordings are saved, they remain opaque to search -- a 60-minute meeting recording cannot be keyword-searched, linked to projects, or composed into briefs without a transcript.

Automatic transcription with speaker diarization solves this by converting audio into timestamped, speaker-attributed text segments. A knowledge worker can capture a voice memo on a commute, drop the file into `ctxt`, and find it later by searching for what was said rather than trying to remember when or where it was recorded. For teams, meeting recordings become first-class knowledge objects with every decision, action item, and mention automatically extracted from the transcript.

The transcription provider is pluggable to support different deployment models. A self-hosted installation behind a firewall can run Whisper locally with no external calls, while cloud deployments can leverage higher-accuracy services like OpenAI or Deepgram for real-time or near-real-time transcription. Speaker diarization is optional and can be enabled when multi-speaker attribution matters (meetings, interviews) or disabled for single-speaker content (voice memos, dictation).

---

## Acceptance Criteria

- [ ] User can ingest an audio file via `ctxt add --file meeting.mp3` and receive a job ID within 1 second
- [ ] System auto-detects audio format and selects the `audio.transcribe` pipeline
- [ ] Supported formats: MP3, WAV, OGG, FLAC, M4A, WEBM (audio-only)
- [ ] Unsupported or corrupt audio files return a clear error message without crashing the pipeline
- [ ] Transcript text is stored in the knowledge object and is searchable
- [ ] Timestamped segments are stored in Sections with start/end times
- [ ] Speaker labels are stored in Metadata when diarization is enabled
- [ ] Files exceeding the configurable size limit (default 500MB) are rejected with a descriptive error
- [ ] Audio exceeding the configurable duration limit (default 4 hours) is rejected with a descriptive error
- [ ] User can provide a language hint to improve transcription accuracy
- [ ] Processing is fully async: the user is never blocked waiting for transcription to complete

---

## Implementation Notes

### CLI Interface
```bash
# Basic audio capture (auto-detects format, selects audio.transcribe pipeline)
ctxt add --file meeting.mp3

# Explicit type hint
ctxt add --file voice-note.wav --type audio

# With language hint for non-English content
ctxt add --file interview.ogg --lang fr

# With speaker diarization enabled and focus profile
ctxt add --file standup.m4a --diarize --profile engineering --project mobile-app

# Returns immediately with JSON
{
  "job_id": "j-aud-9e4f1a",
  "object_id": "o-aud-b72d3c",
  "status": "pending_enrichment",
  "pipeline": "audio.transcribe",
  "will_enrich_by": "2025-01-18T10:35:00Z"
}
```

### REST API
```
POST /analyze
Content-Type: multipart/form-data

------boundary
Content-Disposition: form-data; name="file"; filename="meeting.mp3"
Content-Type: audio/mpeg

<binary audio data>
------boundary
Content-Disposition: form-data; name="source_type"

audio
------boundary
Content-Disposition: form-data; name="type_hint"

audio.transcribe
------boundary
Content-Disposition: form-data; name="options"
Content-Type: application/json

{
  "language": "en",
  "diarize": true
}
------boundary--

-> 202 Accepted
{
  "job_id": "j-aud-9e4f1a",
  "object_id": "o-aud-b72d3c",
  "status": "pending_enrichment",
  "pipeline": "audio.transcribe"
}
```

### Pipeline Steps

**audio.transcribe** (with optional diarization):
```
FileReader -> FormatDetector -> AudioTranscriber -> SpeakerDiarizer (optional) -> TimestampAligner -> Sectioner -> Tagger -> EmbeddingGenerator
```

Each step implements the `PipelineStep` interface:

```go
type FormatDetectorStep struct{}

func (s *FormatDetectorStep) Name() string { return "format_detector" }

func (s *FormatDetectorStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    mime := detectAudioMIME(draft.RawContent)

    supported := map[string]bool{
        "audio/mpeg": true,  // MP3
        "audio/wav":  true,  // WAV
        "audio/ogg":  true,  // OGG
        "audio/flac": true,  // FLAC
        "audio/mp4":  true,  // M4A
        "audio/webm": true,  // WEBM
    }

    if !supported[mime] {
        return nil, fmt.Errorf("format_detector: unsupported audio format %s", mime)
    }

    // Probe audio metadata
    probe, err := probeAudio(draft.Source.Path)
    if err != nil {
        return nil, fmt.Errorf("format_detector: corrupt audio: %w", err)
    }

    if probe.Duration > s.maxDuration {
        return nil, fmt.Errorf("format_detector: duration %v exceeds limit %v",
            probe.Duration, s.maxDuration)
    }

    draft.ContentType = mime
    draft.Metadata["audio_format"] = probe.Format
    draft.Metadata["audio_duration_seconds"] = probe.Duration.Seconds()
    draft.Metadata["audio_sample_rate"] = probe.SampleRate
    draft.Metadata["audio_channels"] = probe.Channels
    draft.Metadata["audio_bitrate"] = probe.Bitrate
    draft.Metadata["file_size_bytes"] = len(draft.RawContent)

    return draft, nil
}
```

```go
type AudioTranscriberStep struct {
    provider TranscriptionProvider  // Whisper local, OpenAI, Deepgram
}

func (s *AudioTranscriberStep) Name() string { return "audio_transcriber" }

func (s *AudioTranscriberStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    lang := ""
    if v, ok := draft.Metadata["language_hint"]; ok {
        lang = v.(string)
    }

    result, err := s.provider.Transcribe(ctx, draft.Source.Path, TranscribeOptions{
        Language: lang,
        Format:   draft.ContentType,
    })
    if err != nil {
        return nil, fmt.Errorf("audio_transcriber: %w", err)
    }

    // Store full transcript as RawContent (post-transcription)
    draft.RawContent = []byte(result.FullText)
    draft.Metadata["transcription_provider"] = s.provider.Name()
    draft.Metadata["transcription_language"] = result.DetectedLanguage
    draft.Metadata["transcription_confidence"] = result.Confidence

    // Store timestamped segments for later alignment
    draft.Metadata["_raw_segments"] = result.Segments

    return draft, nil
}
```

```go
type SpeakerDiarizerStep struct {
    provider DiarizationProvider
    enabled  bool
}

func (s *SpeakerDiarizerStep) Name() string { return "speaker_diarizer" }

func (s *SpeakerDiarizerStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    if !s.enabled {
        return draft, nil  // Skip if diarization disabled
    }

    segments := draft.Metadata["_raw_segments"].([]TranscriptSegment)

    diarized, err := s.provider.Diarize(ctx, draft.Source.Path, segments)
    if err != nil {
        // Diarization failure is non-fatal; continue without speaker labels
        draft.Metadata["diarization_error"] = err.Error()
        return draft, nil
    }

    draft.Metadata["speaker_count"] = diarized.SpeakerCount
    draft.Metadata["speakers"] = diarized.SpeakerLabels
    draft.Metadata["_raw_segments"] = diarized.Segments

    return draft, nil
}
```

### Transcription Provider Interface

```go
type TranscriptionProvider interface {
    Name() string
    Transcribe(ctx context.Context, audioPath string, opts TranscribeOptions) (*TranscriptResult, error)
}

type TranscribeOptions struct {
    Language string  // ISO 639-1 language hint (e.g., "en", "fr", "ja")
    Format   string  // MIME type of the audio
}

type TranscriptResult struct {
    FullText         string              // Complete transcript text
    Segments         []TranscriptSegment // Timestamped segments
    DetectedLanguage string              // Auto-detected language
    Confidence       float64             // 0.0 to 1.0
}

type TranscriptSegment struct {
    StartTime time.Duration
    EndTime   time.Duration
    Text      string
    Speaker   string  // Populated after diarization
}

// Whisper (local, no network calls)
type WhisperProvider struct {
    modelSize string  // tiny, base, small, medium, large
    binPath   string
}

// Cloud API (e.g., OpenAI Whisper API, Deepgram)
type CloudTranscriptionProvider struct {
    endpoint string
    apiKey   string
    model    string
}
```

### Backend Processing

1. `ctxt` receives the audio file via CLI (`--file`) or REST API (multipart upload)
2. `FileReader` loads the audio bytes and validates file size against the limit (default 500MB)
3. `FormatDetector` validates the MIME type against the supported set and rejects unsupported formats
4. `FormatDetector` probes audio metadata (duration, sample rate, channels, bitrate) and rejects files exceeding the duration limit (default 4 hours)
5. System selects the `audio.transcribe` pipeline
6. Creates Job in `jobs` table with status `pending` and returns job ID immediately
7. Worker picks up job and runs the pipeline:
   - `FileReader` loads audio bytes from disk or upload buffer
   - `FormatDetector` validates format and extracts audio metadata
   - `AudioTranscriber` sends audio to the configured provider (Whisper, OpenAI, Deepgram) and receives timestamped transcript segments
   - `SpeakerDiarizer` (if enabled) attributes segments to speakers; failure is non-fatal
   - `TimestampAligner` reconciles transcript segments with diarization output, producing clean time-aligned sections
   - `Sectioner` groups segments into logical sections (by topic shift, speaker change, or silence gaps)
   - `Tagger` assigns tags from vocabulary based on transcript content
   - `EmbeddingGenerator` generates embeddings for the full transcript and per-section
8. Stores the enriched KnowledgeObject with transcript in RawContent, timestamped segments in Sections, speaker labels and audio metadata in Metadata
9. Creates graph edges for any extracted mentions from the transcript
10. Marks job as `completed` (or `failed` with error detail)

### Knowledge Object Structure

```json
{
  "id": "o-aud-b72d3c",
  "type": "audio",
  "subtype": "transcribe",
  "raw_content": "Alice: Good morning everyone. Let's start with the sprint review. Bob: Sure. The backend API migration is at 80 percent...",
  "content_type": "audio/mpeg",
  "metadata": {
    "audio_format": "mp3",
    "audio_duration_seconds": 1847.5,
    "audio_sample_rate": 44100,
    "audio_channels": 2,
    "audio_bitrate": 192000,
    "file_size_bytes": 44340000,
    "transcription_provider": "whisper",
    "transcription_language": "en",
    "transcription_confidence": 0.94,
    "speaker_count": 3,
    "speakers": ["Speaker_1", "Speaker_2", "Speaker_3"]
  },
  "sections": [
    {
      "title": "Sprint Review Opening",
      "content": "Good morning everyone. Let's start with the sprint review.",
      "metadata": {
        "start_time": "00:00:00",
        "end_time": "00:00:12",
        "speaker": "Speaker_1"
      }
    },
    {
      "title": "Backend API Migration Update",
      "content": "Sure. The backend API migration is at 80 percent. We hit a blocker with the authentication middleware refactor but resolved it yesterday.",
      "metadata": {
        "start_time": "00:00:12",
        "end_time": "00:00:34",
        "speaker": "Speaker_2"
      }
    }
  ],
  "tags": ["meeting", "sprint-review", "backend", "api-migration"],
  "mentions": [
    "@project.backend-api-migration",
    "@system.authentication-middleware"
  ],
  "source": {
    "path": "/data/objects/o-aud-b72d3c/original.mp3",
    "original_filename": "meeting.mp3",
    "ingested_at": "2025-01-18T10:30:45Z"
  },
  "pipeline": {
    "name": "audio.transcribe",
    "steps_completed": [
      "file_reader",
      "format_detector",
      "audio_transcriber",
      "speaker_diarizer",
      "timestamp_aligner",
      "sectioner",
      "tagger",
      "embedding_generator"
    ],
    "completed_at": "2025-01-18T10:34:52Z"
  }
}
```

### Configuration

```yaml
# In configuration.yaml
pipelines:
  audio.transcribe:
    steps:
      - file_reader
      - format_detector
      - audio_transcriber
      - speaker_diarizer
      - timestamp_aligner
      - sectioner
      - tagger
      - embedding_generator

transcription:
  provider: whisper             # whisper | openai | deepgram
  languageHint: ""              # ISO 639-1 code; empty for auto-detect
  whisper:
    modelSize: base             # tiny, base, small, medium, large
    binPath: /usr/local/bin/whisper
    device: cpu                 # cpu | cuda
  openai:
    model: whisper-1
    apiKey: ${OPENAI_API_KEY}
  deepgram:
    model: nova-2
    apiKey: ${DEEPGRAM_API_KEY}

diarization:
  enabled: false                # Enable speaker attribution
  provider: pyannote            # pyannote | cloud
  pyannote:
    modelPath: /models/pyannote/speaker-diarization
  minSpeakers: 1                # Minimum expected speakers
  maxSpeakers: 10               # Maximum expected speakers

audio:
  maxFileSize: 524288000        # 500MB in bytes
  maxDuration: 14400            # 4 hours in seconds
  supportedFormats:
    - audio/mpeg
    - audio/wav
    - audio/ogg
    - audio/flac
    - audio/mp4
    - audio/webm

sectioner:
  strategy: silence_and_speaker # silence_and_speaker | fixed_interval | topic_shift
  silenceGapSeconds: 5.0        # Gap threshold for section breaks
  maxSectionDuration: 300       # Max 5 minutes per section
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt add --file meeting.mp3` returns job ID within 1 second; server creates job record with `source_type=audio`
- [ ] CLI: Job status transitions from `pending` to `completed` within 120 seconds for a short recording
- [ ] CLI: Retrieved object contains full transcript text in RawContent
- [ ] CLI: `ctxt add --file voice-note.wav --type audio` sends `"type_hint": "audio"` in the server request payload; stored object's pipeline equals `"audio.transcribe"`
- [ ] CLI: `ctxt add --file interview.ogg --lang fr` sends `"language": "fr"` in the server request payload (within the options field); stored object Metadata contains `"transcription_language": "fr"` (or the detected language)
- [ ] CLI: `ctxt add --file standup.m4a --diarize` sends `"diarize": true` in the server request payload; stored object Metadata contains `"speaker_count"` and `"speakers"` keys
- [ ] CLI: `ctxt add --file standup.m4a --profile engineering --project mobile-app` sends `"profile": "engineering"` and `"project": "mobile-app"` in the server request payload; stored object Metadata contains both fields
- [ ] Format: MP3, WAV, OGG, FLAC, M4A, WEBM files all processed successfully
- [ ] Format: Unsupported format (e.g., AIFF, WMA) returns descriptive error without crash
- [ ] Format: Corrupt/truncated audio file returns error with clear message
- [ ] Size: File exceeding 500MB limit is rejected with size limit error
- [ ] Duration: Audio exceeding 4-hour limit is rejected with duration limit error
- [ ] Transcript: Extracted text is searchable via `ctxt search "words from meeting"`
- [ ] Timestamps: Sections contain start/end time metadata — GET /objects/{object_id} confirms Section Metadata has `start_time` and `end_time` keys
- [ ] Diarization: With `--diarize`, speaker labels appear in Sections and Metadata; stored object Metadata `speaker_count` > 0
- [ ] Diarization: Without `--diarize`, pipeline completes without speaker attribution; no `speaker_count` in Metadata
- [ ] Language: `--lang fr` hint is received by server and stored; transcription provider uses the supplied language code
- [ ] REST API: Multipart upload via `POST /analyze` with `options={"language":"en","diarize":true}` returns 202 + job ID; server persists both options
- [ ] REST API: `GET /objects/{object_id}` returns enriched object with transcript, timestamped Sections, and audio metadata (`audio_format`, `audio_duration_seconds`) all populated
- [ ] Async: User can immediately query for the object before transcription completes
- [ ] Resilience: Worker crash during transcription step causes automatic job retry

---

## Related Stories

- [US-0001](./US-0001-text-capture-minimal-friction.md) -- Base text capture functionality
- [US-0003](./US-0003-image-ocr-and-analysis.md) -- Image OCR (sibling multimodal ingestion)
- [US-0005](./US-0005-video-processing-with-scenes.md) -- Video processing extracts audio track for transcription
- [US-0009](../enrichment/US-0009-extract-entities-and-mentions.md) -- Entity extraction from transcript text
- [US-0010](../enrichment/US-0010-extract-decisions-and-tasks.md) -- Decision/task extraction from meeting transcripts
- [US-0011](../enrichment/US-0011-assign-tags-from-vocabulary.md) -- Tag assignment from transcript content

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Solo Developer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/solo-developer.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)

---

## E2E Tests

[us0004_audio_transcription_test.go](../../../test/integration/us0004_audio_transcription_test.go)
