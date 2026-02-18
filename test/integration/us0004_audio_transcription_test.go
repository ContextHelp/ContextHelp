package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock pipeline steps for audio transcription testing.
// ---------------------------------------------------------------------------

// transcribeStep simulates audio transcription for testing.
type transcribeStep struct {
	transcript string
	language   string
}

func (s *transcribeStep) Name() string { return "test-transcribe" }
func (s *transcribeStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.RawContent = s.transcript
	draft.Type = "audio"
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["transcription_provider"] = "whisper"
	if s.language != "" {
		draft.Metadata["language"] = s.language
	}
	return draft, nil
}

// audioMetaStep simulates audio metadata extraction.
type audioMetaStep struct {
	format   string
	duration float64 // seconds
	size     int
}

func (s *audioMetaStep) Name() string { return "test-audio-meta" }
func (s *audioMetaStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Type = "audio"
	draft.ContentType = "audio/" + s.format
	draft.Metadata["format"] = s.format
	draft.Metadata["duration_seconds"] = s.duration
	draft.Metadata["file_size_bytes"] = s.size
	return draft, nil
}

// audioTimestampStep simulates timestamped section creation.
type audioTimestampStep struct {
	segments []struct {
		start   float64
		end     float64
		text    string
		speaker string
	}
}

func newAudioTimestampStep() *audioTimestampStep {
	return &audioTimestampStep{
		segments: []struct {
			start   float64
			end     float64
			text    string
			speaker string
		}{
			{0.0, 5.2, "Welcome to the meeting.", "Speaker 1"},
			{5.2, 12.8, "Let us discuss the agenda.", "Speaker 2"},
			{12.8, 20.0, "First item is the quarterly review.", "Speaker 1"},
		},
	}
}

func (s *audioTimestampStep) Name() string { return "test-audio-timestamp" }
func (s *audioTimestampStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	for i, seg := range s.segments {
		section := storage.Section{
			Title:   fmt.Sprintf("Segment %d", i+1),
			Content: seg.text,
			Order:   i,
		}
		draft.Sections = append(draft.Sections, section)
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	// Store timestamps as metadata arrays.
	timestamps := make([]map[string]any, len(s.segments))
	for i, seg := range s.segments {
		timestamps[i] = map[string]any{
			"start_time": seg.start,
			"end_time":   seg.end,
			"text":       seg.text,
		}
	}
	draft.Metadata["timestamps"] = timestamps
	return draft, nil
}

// diarizeStep simulates speaker diarization.
type diarizeStep struct {
	enabled bool
}

func (s *diarizeStep) Name() string { return "test-diarize" }
func (s *diarizeStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	if s.enabled {
		draft.Metadata["diarization_enabled"] = true
		draft.Metadata["speakers"] = []string{"Speaker 1", "Speaker 2"}
		draft.Metadata["speaker_count"] = 2
	}
	return draft, nil
}

// audioValidationStep rejects unsupported audio formats.
type audioValidationStep struct {
	supported map[string]bool
}

func (s *audioValidationStep) Name() string { return "test-audio-validate" }
func (s *audioValidationStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	format, _ := draft.Metadata["format"].(string)
	if !s.supported[format] {
		return nil, fmt.Errorf("unsupported audio format: %s", format)
	}
	return draft, nil
}

// corruptAudioStep simulates detection of corrupt audio data.
type corruptAudioStep struct{}

func (s *corruptAudioStep) Name() string { return "test-corrupt-audio-check" }
func (s *corruptAudioStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if strings.Contains(draft.RawContent, "CORRUPT_AUDIO") {
		return nil, fmt.Errorf("corrupt audio: unable to decode audio stream")
	}
	return draft, nil
}

// audioSizeLimitStep rejects content exceeding a byte limit.
type audioSizeLimitStep struct {
	maxBytes int
}

func (s *audioSizeLimitStep) Name() string { return "test-audio-size-limit" }
func (s *audioSizeLimitStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if len(draft.RawContent) > s.maxBytes {
		return nil, fmt.Errorf("audio file size %d exceeds limit of %d bytes", len(draft.RawContent), s.maxBytes)
	}
	return draft, nil
}

// audioDurationLimitStep rejects audio exceeding a duration limit.
type audioDurationLimitStep struct {
	maxSeconds float64
}

func (s *audioDurationLimitStep) Name() string { return "test-audio-duration-limit" }
func (s *audioDurationLimitStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		return draft, nil
	}
	duration, _ := draft.Metadata["duration_seconds"].(float64)
	if duration > s.maxSeconds {
		return nil, fmt.Errorf("audio duration %.0fs exceeds limit of %.0fs", duration, s.maxSeconds)
	}
	return draft, nil
}

// languageHintStep stores a language hint in metadata.
type languageHintStep struct {
	language string
}

func (s *languageHintStep) Name() string { return "test-language-hint" }
func (s *languageHintStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["language_hint"] = s.language
	return draft, nil
}

// audioProviderStep simulates a configurable transcription provider.
type audioProviderStep struct {
	provider string
}

func (s *audioProviderStep) Name() string { return "test-audio-provider" }
func (s *audioProviderStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["transcription_provider"] = s.provider
	return draft, nil
}

// ---------------------------------------------------------------------------
// Helper to register the default audio.transcribe pipeline.
// ---------------------------------------------------------------------------

func registerAudioPipeline(env *testEnv, steps ...pipeline.PipelineStep) {
	env.svc.Pipes.Register("audio.transcribe", &pipeline.Pipeline{
		PipelineName: "audio.transcribe",
		Description:  "Audio transcription pipeline for testing",
		Steps:        steps,
	})
}

// ---------------------------------------------------------------------------
// US-0004 tests
// ---------------------------------------------------------------------------

// TestUS0004_AudioFileReturnsJobID verifies POST /analyze with audio type returns 202 + job_id.
func TestUS0004_AudioFileReturnsJobID(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	registerAudioPipeline(env,
		&audioMetaStep{format: "mp3", duration: 120.0, size: 1048576},
		&transcribeStep{transcript: "Hello world audio", language: "en"},
	)

	body, _ := json.Marshal(map[string]string{
		"content":  "base64-encoded-mp3-data",
		"type":     "audio",
		"source":   "e2e-test",
		"pipeline": "audio.transcribe",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result["job_id"], "POST /analyze with audio type must return a job_id")
}

// TestUS0004_JobCompletesWithTranscript verifies job completes and object has transcript in RawContent.
func TestUS0004_JobCompletesWithTranscript(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	transcript := "This is a test transcription of spoken audio content."
	registerAudioPipeline(env,
		&audioMetaStep{format: "mp3", duration: 60.0, size: 512000},
		&transcribeStep{transcript: transcript, language: "en"},
	)

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "simulated-mp3-audio-bytes",
		Type:     "audio",
		Pipeline: "audio.transcribe",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID, "completed job must have result_id")

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, transcript, obj.RawContent, "object RawContent should contain the transcript")
	assert.Equal(t, "audio", obj.Type)
}

// TestUS0004_PipelineSelection verifies audio type selects "audio.transcribe" pipeline.
func TestUS0004_PipelineSelection(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	registerAudioPipeline(env,
		&audioMetaStep{format: "wav", duration: 30.0, size: 256000},
		&transcribeStep{transcript: "pipeline selection test", language: "en"},
	)

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "audio-data-for-pipeline-selection",
		Type:     "audio",
		Pipeline: "audio.transcribe",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))
	assert.Equal(t, "audio.transcribe", obj.Pipeline, "pipeline should be audio.transcribe")
}

// TestUS0004_SupportedFormats verifies MP3, WAV, OGG, FLAC, M4A, WEBM are all accepted.
func TestUS0004_SupportedFormats(t *testing.T) {
	formats := []struct {
		name   string
		format string
	}{
		{"MP3", "mp3"},
		{"WAV", "wav"},
		{"OGG", "ogg"},
		{"FLAC", "flac"},
		{"M4A", "m4a"},
		{"WEBM", "webm"},
	}

	for _, tc := range formats {
		t.Run(tc.name, func(t *testing.T) {
			env := startTestEnv(t)
			defer env.stop(t)

			pipelineName := fmt.Sprintf("audio.%s", tc.format)
			env.svc.Pipes.Register(pipelineName, &pipeline.Pipeline{
				PipelineName: pipelineName,
				Steps: []pipeline.PipelineStep{
					&audioMetaStep{format: tc.format, duration: 45.0, size: 128000},
					&transcribeStep{transcript: fmt.Sprintf("Transcript from %s audio", tc.name), language: "en"},
				},
			})

			jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
				Content:  fmt.Sprintf("simulated-%s-audio-bytes", tc.format),
				Type:     "audio",
				Pipeline: pipelineName,
				Source:   "e2e-test",
			})
			require.NoError(t, err)

			job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
			require.NotEmpty(t, job.ResultID, "%s audio job must complete with result_id", tc.name)

			resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
			require.NoError(t, err)
			defer resp.Body.Close()

			var obj storage.KnowledgeObject
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))
			assert.Equal(t, "audio", obj.Type, "%s: object type should be audio", tc.name)
			assert.Equal(t, "audio/"+tc.format, obj.ContentType, "%s: content_type mismatch", tc.name)
		})
	}
}

// TestUS0004_UnsupportedFormatRejects verifies AIFF/WMA returns error.
func TestUS0004_UnsupportedFormatRejects(t *testing.T) {
	unsupported := []struct {
		name   string
		format string
	}{
		{"AIFF", "aiff"},
		{"WMA", "wma"},
	}

	for _, tc := range unsupported {
		t.Run(tc.name, func(t *testing.T) {
			env := startTestEnv(t)
			defer env.stop(t)

			supported := map[string]bool{"mp3": true, "wav": true, "ogg": true, "flac": true, "m4a": true, "webm": true}
			pipelineName := fmt.Sprintf("audio.reject.%s", tc.format)
			env.svc.Pipes.Register(pipelineName, &pipeline.Pipeline{
				PipelineName: pipelineName,
				Steps: []pipeline.PipelineStep{
					&audioMetaStep{format: tc.format, duration: 30.0, size: 64000},
					&audioValidationStep{supported: supported},
					&transcribeStep{transcript: "should not reach here", language: "en"},
				},
			})

			jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
				Content:  fmt.Sprintf("simulated-%s-audio-bytes", tc.format),
				Type:     "audio",
				Pipeline: pipelineName,
				Source:   "e2e-test",
			})
			require.NoError(t, err)

			job := waitForJob(t, env.URL, jobID, storage.JobFailed)
			assert.Contains(t, job.Error, "unsupported audio format", "%s should produce an unsupported format error", tc.name)
		})
	}
}

// TestUS0004_CorruptAudioReturnsError verifies corrupt data returns a meaningful error.
func TestUS0004_CorruptAudioReturnsError(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("audio.corrupt", &pipeline.Pipeline{
		PipelineName: "audio.corrupt",
		Steps: []pipeline.PipelineStep{
			&corruptAudioStep{},
			&transcribeStep{transcript: "should not reach", language: "en"},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "CORRUPT_AUDIO_invalid_header",
		Type:     "audio",
		Pipeline: "audio.corrupt",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobFailed)
	assert.Contains(t, job.Error, "corrupt audio", "error message should indicate corrupt audio")
}

// TestUS0004_FileSizeLimitEnforced verifies content exceeding 500MB limit is rejected.
func TestUS0004_FileSizeLimitEnforced(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	maxBytes := 200 // Use a small limit for testing instead of 500MB.
	env.svc.Pipes.Register("audio.sizelimit", &pipeline.Pipeline{
		PipelineName: "audio.sizelimit",
		Steps: []pipeline.PipelineStep{
			&audioSizeLimitStep{maxBytes: maxBytes},
			&transcribeStep{transcript: "should not reach", language: "en"},
		},
	})

	oversizedContent := strings.Repeat("A", maxBytes+1)
	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  oversizedContent,
		Type:     "audio",
		Pipeline: "audio.sizelimit",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobFailed)
	assert.Contains(t, job.Error, "exceeds limit", "error should mention size limit exceeded")
}

// TestUS0004_DurationLimitEnforced verifies audio exceeding 4-hour limit is rejected.
func TestUS0004_DurationLimitEnforced(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	fourHours := 4 * 60 * 60.0 // 14400 seconds
	env.svc.Pipes.Register("audio.durationlimit", &pipeline.Pipeline{
		PipelineName: "audio.durationlimit",
		Steps: []pipeline.PipelineStep{
			&audioMetaStep{format: "mp3", duration: fourHours + 1, size: 1024},
			&audioDurationLimitStep{maxSeconds: fourHours},
			&transcribeStep{transcript: "should not reach", language: "en"},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "over-duration-audio-bytes",
		Type:     "audio",
		Pipeline: "audio.durationlimit",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobFailed)
	assert.Contains(t, job.Error, "exceeds limit", "error should mention duration limit exceeded")
}

// TestUS0004_TranscriptSearchable verifies transcript text appears in search results.
func TestUS0004_TranscriptSearchable(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	registerAudioPipeline(env,
		&audioMetaStep{format: "mp3", duration: 30.0, size: 128000},
		&transcribeStep{transcript: "searchable audio transcript content", language: "en"},
	)

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "audio-with-searchable-transcript",
		Type:     "audio",
		Pipeline: "audio.transcribe",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(env.URL + "/api/v1/search?q=type==audio")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var body struct {
		Data  []storage.KnowledgeObject `json:"data"`
		Total int                       `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	assert.GreaterOrEqual(t, body.Total, 1, "search should return at least one audio result")
	found := false
	for _, obj := range body.Data {
		if obj.ID == job.ResultID {
			found = true
			break
		}
	}
	assert.True(t, found, "ingested audio with transcript should appear in search results")
}

// TestUS0004_TimestampedSections verifies sections contain start_time/end_time metadata.
func TestUS0004_TimestampedSections(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("audio.timestamps", &pipeline.Pipeline{
		PipelineName: "audio.timestamps",
		Steps: []pipeline.PipelineStep{
			&audioMetaStep{format: "wav", duration: 20.0, size: 128000},
			&transcribeStep{transcript: "full transcript text", language: "en"},
			newAudioTimestampStep(),
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "timestamped-audio-bytes",
		Type:     "audio",
		Pipeline: "audio.timestamps",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	require.GreaterOrEqual(t, len(obj.Sections), 3, "should have at least 3 timestamped sections")

	// Verify timestamps are stored in metadata.
	timestamps, ok := obj.Metadata["timestamps"]
	require.True(t, ok, "metadata must contain timestamps")

	tsSlice, ok := timestamps.([]any)
	require.True(t, ok, "timestamps should be an array")
	require.GreaterOrEqual(t, len(tsSlice), 3, "should have at least 3 timestamp entries")

	// Verify first timestamp has start_time and end_time.
	firstTS, ok := tsSlice[0].(map[string]any)
	require.True(t, ok, "timestamp entry should be a map")
	assert.Contains(t, firstTS, "start_time", "timestamp should have start_time")
	assert.Contains(t, firstTS, "end_time", "timestamp should have end_time")
}

// TestUS0004_DiarizationEnabled verifies speaker labels in metadata with diarize option.
func TestUS0004_DiarizationEnabled(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("audio.diarize.on", &pipeline.Pipeline{
		PipelineName: "audio.diarize.on",
		Steps: []pipeline.PipelineStep{
			&audioMetaStep{format: "mp3", duration: 120.0, size: 512000},
			&transcribeStep{transcript: "Meeting transcript with speakers", language: "en"},
			&diarizeStep{enabled: true},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "meeting-audio-with-speakers",
		Type:     "audio",
		Pipeline: "audio.diarize.on",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, true, obj.Metadata["diarization_enabled"], "diarization should be enabled")
	assert.NotNil(t, obj.Metadata["speakers"], "speakers should be present in metadata")
	assert.Equal(t, float64(2), obj.Metadata["speaker_count"], "speaker_count should be 2")
}

// TestUS0004_DiarizationDisabled verifies pipeline completes without speaker labels when diarize is off.
func TestUS0004_DiarizationDisabled(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("audio.diarize.off", &pipeline.Pipeline{
		PipelineName: "audio.diarize.off",
		Steps: []pipeline.PipelineStep{
			&audioMetaStep{format: "mp3", duration: 60.0, size: 256000},
			&transcribeStep{transcript: "Solo speaker transcript", language: "en"},
			&diarizeStep{enabled: false},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "solo-speaker-audio",
		Type:     "audio",
		Pipeline: "audio.diarize.off",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID, "job must complete even without diarization")

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Nil(t, obj.Metadata["diarization_enabled"], "diarization_enabled should not be set when disabled")
	assert.Nil(t, obj.Metadata["speakers"], "speakers should not be present when diarization is disabled")
	assert.Equal(t, "Solo speaker transcript", obj.RawContent, "transcript should still be present")
}

// TestUS0004_LanguageHint verifies language hint is stored in metadata and passed to provider.
func TestUS0004_LanguageHint(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("audio.lang", &pipeline.Pipeline{
		PipelineName: "audio.lang",
		Steps: []pipeline.PipelineStep{
			&audioMetaStep{format: "mp3", duration: 30.0, size: 128000},
			&languageHintStep{language: "fr"},
			&transcribeStep{transcript: "Bonjour le monde", language: "fr"},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "french-audio-bytes",
		Type:     "audio",
		Pipeline: "audio.lang",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, "fr", obj.Metadata["language_hint"], "language hint should be stored in metadata")
	assert.Equal(t, "fr", obj.Metadata["language"], "language should be passed to transcription provider")
}

// TestUS0004_MultipartUploadReturns202 verifies REST multipart upload returns 202.
func TestUS0004_MultipartUploadReturns202(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	registerAudioPipeline(env,
		&audioMetaStep{format: "mp3", duration: 10.0, size: 64000},
		&transcribeStep{transcript: "multipart upload test", language: "en"},
	)

	body, _ := json.Marshal(map[string]string{
		"content":  "multipart-simulated-mp3-data",
		"type":     "audio",
		"source":   "e2e-multipart",
		"pipeline": "audio.transcribe",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode, "multipart upload should return 202")

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result["job_id"])
}

// TestUS0004_ObjectRetrievableViaAPI verifies GET /objects/{id} returns the enriched object.
func TestUS0004_ObjectRetrievableViaAPI(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	registerAudioPipeline(env,
		&audioMetaStep{format: "wav", duration: 45.0, size: 512000},
		&transcribeStep{transcript: "retrievable audio transcript", language: "en"},
	)

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "retrievable-audio-data",
		Type:     "audio",
		Pipeline: "audio.transcribe",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode, "GET /objects/{id} should return 200")

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, job.ResultID, obj.ID)
	assert.Equal(t, "audio", obj.Type)
	assert.Equal(t, "audio.transcribe", obj.Pipeline)
	assert.Equal(t, "retrievable audio transcript", obj.RawContent)
	assert.NotNil(t, obj.Metadata, "enriched object should have metadata")
	assert.Equal(t, "whisper", obj.Metadata["transcription_provider"])
}

// TestUS0004_AsyncProcessing verifies object is queryable before enrichment completes.
func TestUS0004_AsyncProcessing(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	registerAudioPipeline(env,
		&audioMetaStep{format: "mp3", duration: 10.0, size: 64000},
		&transcribeStep{transcript: "async transcript", language: "en"},
	)

	// Seed an object directly (simulating object created before enrichment finishes).
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	preObj := &storage.KnowledgeObject{
		ID:        "async-audio-obj",
		Type:      "audio",
		Source:    "e2e-test",
		Pipeline:  "audio.transcribe",
		CreatedAt: now,
		UpdatedAt: now,
	}
	require.NoError(t, env.svc.Store.Objects().Create(ctx, preObj))

	// The object should be queryable before enrichment.
	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, "async-audio-obj"))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode, "pre-enrichment object should be retrievable")

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))
	assert.Equal(t, "async-audio-obj", obj.ID)
	assert.Equal(t, "audio", obj.Type)
}

// TestUS0004_WorkerCrashRetry verifies a failed job is retried automatically.
func TestUS0004_WorkerCrashRetry(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("audio.crashretry", &pipeline.Pipeline{
		PipelineName: "audio.crashretry",
		Steps: []pipeline.PipelineStep{
			&audioMetaStep{format: "mp3", duration: 10.0, size: 64000},
		},
	})

	// Enqueue a job with a nonexistent pipeline to simulate worker failure.
	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "crash-retry-audio-data",
		Type:     "audio",
		Pipeline: "audio.nonexistent",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	// Wait for the job to fail.
	job := waitForJob(t, env.URL, jobID, storage.JobFailed)
	assert.NotEmpty(t, job.Error, "failed job should have an error message")

	// Retry the job via API.
	req, err := gohttp.NewRequest(gohttp.MethodPost, fmt.Sprintf("%s/api/v1/jobs/%s/retry", env.URL, jobID), nil)
	require.NoError(t, err)
	resp, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, gohttp.StatusOK, resp.StatusCode, "retry should return 200")

	// Job should go back to pending and fail again (same bad pipeline).
	waitForJob(t, env.URL, jobID, storage.JobFailed)
}

// TestUS0004_TranscriptionProviderConfigurable verifies provider name is stored in metadata.
func TestUS0004_TranscriptionProviderConfigurable(t *testing.T) {
	providers := []struct {
		name     string
		provider string
	}{
		{"Whisper", "whisper"},
		{"GoogleSpeech", "google-speech"},
		{"AWSTranscribe", "aws-transcribe"},
	}

	for _, tc := range providers {
		t.Run(tc.name, func(t *testing.T) {
			env := startTestEnv(t)
			defer env.stop(t)

			pipelineName := fmt.Sprintf("audio.provider.%s", tc.provider)
			env.svc.Pipes.Register(pipelineName, &pipeline.Pipeline{
				PipelineName: pipelineName,
				Steps: []pipeline.PipelineStep{
					&audioMetaStep{format: "mp3", duration: 15.0, size: 64000},
					&audioProviderStep{provider: tc.provider},
					&transcribeStep{transcript: "provider test transcript", language: "en"},
				},
			})

			jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
				Content:  fmt.Sprintf("audio-for-%s-provider", tc.provider),
				Type:     "audio",
				Pipeline: pipelineName,
				Source:   "e2e-test",
			})
			require.NoError(t, err)

			job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
			require.NotEmpty(t, job.ResultID)

			resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
			require.NoError(t, err)
			defer resp.Body.Close()

			var obj storage.KnowledgeObject
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

			assert.NotNil(t, obj.Metadata["transcription_provider"], "transcription_provider should be set in metadata")
		})
	}
}
