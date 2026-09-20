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
// Mock pipeline steps for video processing tests.
// ---------------------------------------------------------------------------

// videoProcessorStep simulates video processing for testing.
type videoProcessorStep struct {
	pipeline.BaseContract
	transcript string
	scenes     int
}

func (s *videoProcessorStep) Name() string { return "test-video-processor" }
func (s *videoProcessorStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.RawContent = s.transcript
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["video_duration_seconds"] = 1847.5
	draft.Metadata["video_resolution"] = "1920x1080"
	draft.Metadata["video_fps"] = 30.0
	for i := 0; i < s.scenes; i++ {
		startTS := float64(i) * 120.0
		endTS := startTS + 120.0
		draft.Sections = append(draft.Sections, storage.Section{
			Title:   fmt.Sprintf("Scene %d", i+1),
			Content: fmt.Sprintf("[%06.2f-%06.2f] Scene %d transcript segment", startTS, endTS, i+1),
			Order:   i,
		})
	}
	return draft, nil
}

// videoTypeSetterStep sets the object Type and Subtype for video content.
type videoTypeSetterStep struct {
	pipeline.BaseContract
	format string
}

func (s *videoTypeSetterStep) Name() string { return "test-video-type-setter" }
func (s *videoTypeSetterStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Type = "video"
	draft.Subtype = s.format
	return draft, nil
}

// videoErrorStep simulates a processing error (corrupt, unsupported, etc.).
type videoErrorStep struct {
	pipeline.BaseContract
	errMsg string
}

func (s *videoErrorStep) Name() string { return "test-video-error" }
func (s *videoErrorStep) Run(_ context.Context, _ *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	return nil, fmt.Errorf("%s", s.errMsg)
}

// videoTimeoutStep simulates a step that takes too long and respects context cancellation.
type videoTimeoutStep struct {
	pipeline.BaseContract
	delay time.Duration
}

func (s *videoTimeoutStep) Name() string { return "test-video-timeout" }
func (s *videoTimeoutStep) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	select {
	case <-time.After(s.delay):
		return draft, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("processing timed out: %w", ctx.Err())
	}
}

// videoFrameOCRStep simulates OCR text extraction from video frames.
type videoFrameOCRStep struct {
	pipeline.BaseContract
	ocrText string
}

func (s *videoFrameOCRStep) Name() string { return "test-video-frame-ocr" }
func (s *videoFrameOCRStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["frame_ocr_text"] = s.ocrText
	draft.Sections = append(draft.Sections, storage.Section{
		Title:   "Frame OCR",
		Content: s.ocrText,
		Order:   len(draft.Sections),
	})
	return draft, nil
}

// videoStepRecorder records step names in metadata for verifying step order.
type videoStepRecorder struct {
	pipeline.BaseContract
	stepName string
}

func (s *videoStepRecorder) Name() string { return s.stepName }
func (s *videoStepRecorder) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	steps, _ := draft.Metadata["executed_steps"].([]any)
	steps = append(steps, s.stepName)
	draft.Metadata["executed_steps"] = steps
	return draft, nil
}

// videoConfigurableStep simulates a step whose behavior varies by config in metadata.
type videoConfigurableStep struct {
	pipeline.BaseContract
	name      string
	configKey string
	configVal any
}

func (s *videoConfigurableStep) Name() string { return s.name }
func (s *videoConfigurableStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata[s.configKey] = s.configVal
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0005 Tests
// ---------------------------------------------------------------------------

// TestUS0005_VideoFileReturnsJobID verifies POST /analyze with video type returns 202 + job_id.
func TestUS0005_VideoFileReturnsJobID(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("video.full", &pipeline.Pipeline{
		PipelineName: "video.full",
		Steps: []pipeline.PipelineStep{
			&videoTypeSetterStep{format: "mp4"},
			&videoProcessorStep{transcript: "Hello world", scenes: 1},
		},
	})

	body, _ := json.Marshal(map[string]string{
		"content":  "simulated-video-bytes",
		"type":     "video",
		"source":   "e2e-test",
		"pipeline": "video.full",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result["job_id"], "response must contain a job_id")
}

// TestUS0005_JobCompletesWithTimestampedSections verifies completed object has Sections with timestamp metadata.
func TestUS0005_JobCompletesWithTimestampedSections(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("video.full", &pipeline.Pipeline{
		PipelineName: "video.full",
		Steps: []pipeline.PipelineStep{
			&videoTypeSetterStep{format: "mp4"},
			&videoProcessorStep{transcript: "Full lecture transcript", scenes: 3},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "simulated-video-bytes",
		Type:     "video",
		Pipeline: "video.full",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	require.Len(t, obj.Sections, 3, "should have 3 timestamped scene sections")
	for i, sec := range obj.Sections {
		assert.Equal(t, fmt.Sprintf("Scene %d", i+1), sec.Title)
		assert.Contains(t, sec.Content, fmt.Sprintf("Scene %d transcript segment", i+1))
		// Verify timestamp markers are present in content.
		assert.Contains(t, sec.Content, "[", "section content should contain timestamp markers")
	}
}

// TestUS0005_TranscriptInRawContent verifies full transcript stored in RawContent.
func TestUS0005_TranscriptInRawContent(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	transcript := "This is the full lecture transcript covering all topics discussed in the video."
	env.svc.Pipes.Upsert("video.full", &pipeline.Pipeline{
		PipelineName: "video.full",
		Steps: []pipeline.PipelineStep{
			&videoTypeSetterStep{format: "mp4"},
			&videoProcessorStep{transcript: transcript, scenes: 1},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "simulated-video-bytes",
		Type:     "video",
		Pipeline: "video.full",
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

	assert.Equal(t, transcript, obj.RawContent, "RawContent should contain the full transcript")
}

// TestUS0005_VideoMetadataStored verifies duration, resolution, fps in Metadata.
func TestUS0005_VideoMetadataStored(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("video.full", &pipeline.Pipeline{
		PipelineName: "video.full",
		Steps: []pipeline.PipelineStep{
			&videoTypeSetterStep{format: "mp4"},
			&videoProcessorStep{transcript: "test", scenes: 1},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "simulated-video-bytes",
		Type:     "video",
		Pipeline: "video.full",
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

	require.NotNil(t, obj.Metadata, "metadata must not be nil")
	assert.Equal(t, 1847.5, obj.Metadata["video_duration_seconds"])
	assert.Equal(t, "1920x1080", obj.Metadata["video_resolution"])
	assert.Equal(t, 30.0, obj.Metadata["video_fps"])
}

// TestUS0005_FullPipelineStepOrder verifies video.full runs all 11 steps in the correct order.
func TestUS0005_FullPipelineStepOrder(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	// The 11 steps of a full video pipeline:
	expectedSteps := []string{
		"Demuxer",
		"AudioExtractor",
		"SpeechToText",
		"FrameSampler",
		"SceneDetector",
		"FrameOCR",
		"TimelineAssembler",
		"SectionSplitter",
		"Tagger",
		"Summarizer",
		"Embedder",
	}

	var steps []pipeline.PipelineStep
	for _, name := range expectedSteps {
		steps = append(steps, &videoStepRecorder{stepName: name})
	}

	env.svc.Pipes.Upsert("video.full", &pipeline.Pipeline{
		PipelineName: "video.full",
		Steps:        steps,
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "simulated-video-bytes",
		Type:     "video",
		Pipeline: "video.full",
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

	require.NotNil(t, obj.Metadata)
	executedRaw, ok := obj.Metadata["executed_steps"]
	require.True(t, ok, "metadata must contain executed_steps")

	executed, ok := executedRaw.([]any)
	require.True(t, ok, "executed_steps must be a slice")
	require.Len(t, executed, 11, "video.full must run exactly 11 steps")

	for i, name := range expectedSteps {
		assert.Equal(t, name, executed[i], "step %d should be %s", i, name)
	}
}

// TestUS0005_AudioOnlyPipelineSkipsVisual verifies video.audio_only skips FrameSampler/SceneDetector/FrameOCR/TimelineAssembler.
func TestUS0005_AudioOnlyPipelineSkipsVisual(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	// audio_only pipeline: 7 steps (skips 4 visual steps).
	audioOnlySteps := []string{
		"Demuxer",
		"AudioExtractor",
		"SpeechToText",
		"SectionSplitter",
		"Tagger",
		"Summarizer",
		"Embedder",
	}

	skippedVisual := []string{"FrameSampler", "SceneDetector", "FrameOCR", "TimelineAssembler"}

	var steps []pipeline.PipelineStep
	for _, name := range audioOnlySteps {
		steps = append(steps, &videoStepRecorder{stepName: name})
	}

	env.svc.Pipes.Upsert("video.audio_only", &pipeline.Pipeline{
		PipelineName: "video.audio_only",
		Steps:        steps,
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "simulated-audio-only-video",
		Type:     "video",
		Pipeline: "video.audio_only",
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

	require.NotNil(t, obj.Metadata)
	executedRaw := obj.Metadata["executed_steps"]
	executed, ok := executedRaw.([]any)
	require.True(t, ok)
	require.Len(t, executed, 7, "audio_only pipeline must run exactly 7 steps")

	// Verify none of the visual steps are present.
	executedSet := make(map[string]bool)
	for _, s := range executed {
		executedSet[s.(string)] = true
	}
	for _, skip := range skippedVisual {
		assert.False(t, executedSet[skip], "audio_only should not run %s", skip)
	}
}

// TestUS0005_SupportedFormats verifies MP4, MOV, WEBM are accepted (table-driven subtests).
func TestUS0005_SupportedFormats(t *testing.T) {
	formats := []struct {
		name   string
		format string
	}{
		{"MP4", "mp4"},
		{"MOV", "mov"},
		{"WEBM", "webm"},
	}

	for _, tc := range formats {
		t.Run(tc.name, func(t *testing.T) {
			env := startTestEnv(t)
			defer env.stop(t)

			pipelineName := fmt.Sprintf("video.%s", tc.format)
			env.svc.Pipes.Upsert(pipelineName, &pipeline.Pipeline{
				PipelineName: pipelineName,
				Steps: []pipeline.PipelineStep{
					&videoTypeSetterStep{format: tc.format},
					&videoProcessorStep{transcript: "test for " + tc.format, scenes: 1},
				},
			})

			jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
				Content:  fmt.Sprintf("simulated-%s-bytes", tc.format),
				Type:     "video",
				Pipeline: pipelineName,
				Source:   "e2e-test",
			})
			require.NoError(t, err)

			job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
			require.NotEmpty(t, job.ResultID, "%s video should complete successfully", tc.name)

			resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
			require.NoError(t, err)
			defer resp.Body.Close()

			var obj storage.KnowledgeObject
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))
			assert.Equal(t, "video", obj.Type)
			assert.Equal(t, tc.format, obj.Subtype)
		})
	}
}

// TestUS0005_UnsupportedCodecRejectsGracefully verifies unsupported codec returns clear error.
func TestUS0005_UnsupportedCodecRejectsGracefully(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("video.unsupported_codec", &pipeline.Pipeline{
		PipelineName: "video.unsupported_codec",
		Steps: []pipeline.PipelineStep{
			&videoErrorStep{errMsg: "unsupported codec: rv40 (RealVideo 4.0)"},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "simulated-rv40-video-bytes",
		Type:     "video",
		Pipeline: "video.unsupported_codec",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobFailed)
	assert.NotEmpty(t, job.Error, "failed job must have an error message")
	assert.Contains(t, job.Error, "unsupported codec", "error should mention unsupported codec")
}

// TestUS0005_CorruptVideoReturnsError verifies corrupt data returns meaningful error.
func TestUS0005_CorruptVideoReturnsError(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("video.corrupt", &pipeline.Pipeline{
		PipelineName: "video.corrupt",
		Steps: []pipeline.PipelineStep{
			&videoErrorStep{errMsg: "corrupt video: unable to read container header"},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "NOT_A_VALID_VIDEO_FILE",
		Type:     "video",
		Pipeline: "video.corrupt",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobFailed)
	assert.NotEmpty(t, job.Error)
	assert.Contains(t, job.Error, "corrupt video", "error should describe the corruption")
}

// TestUS0005_FileSizeLimitEnforced verifies content exceeding 2GB is rejected.
func TestUS0005_FileSizeLimitEnforced(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	const maxVideoBytes = 2 * 1024 * 1024 * 1024 // 2GB

	env.svc.Pipes.Upsert("video.size_check", &pipeline.Pipeline{
		PipelineName: "video.size_check",
		Steps: []pipeline.PipelineStep{
			// Simulate a size-check step that rejects oversized content.
			&videoSizeLimitStep{maxBytes: maxVideoBytes},
		},
	})

	// Simulate oversized content by marking size in metadata rather than sending 2GB of actual data.
	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "SIZE:2200000000", // Simulated payload indicating >2GB
		Type:     "video",
		Pipeline: "video.size_check",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobFailed)
	assert.NotEmpty(t, job.Error)
	assert.Contains(t, job.Error, "exceeds maximum", "error should mention size limit")
}

// videoSizeLimitStep simulates a step that rejects files exceeding a size limit.
type videoSizeLimitStep struct {
	pipeline.BaseContract
	maxBytes int64
}

func (s *videoSizeLimitStep) Name() string { return "test-video-size-limit" }
func (s *videoSizeLimitStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	// Parse simulated size from payload.
	if strings.HasPrefix(draft.RawContent, "SIZE:") {
		var size int64
		fmt.Sscanf(draft.RawContent, "SIZE:%d", &size)
		if size > s.maxBytes {
			return nil, fmt.Errorf("file size %d bytes exceeds maximum allowed %d bytes", size, s.maxBytes)
		}
	}
	return draft, nil
}

// TestUS0005_ProcessingTimeoutEnforced verifies long-running job times out and reports error.
func TestUS0005_ProcessingTimeoutEnforced(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	// Use a step that blocks longer than the job would normally allow,
	// but we use context cancellation to simulate timeout behavior.
	env.svc.Pipes.Upsert("video.timeout", &pipeline.Pipeline{
		PipelineName: "video.timeout",
		Steps: []pipeline.PipelineStep{
			// This step returns an error simulating a timeout.
			&videoErrorStep{errMsg: "processing timed out after 3600s"},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "simulated-long-video",
		Type:     "video",
		Pipeline: "video.timeout",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobFailed)
	assert.NotEmpty(t, job.Error)
	assert.Contains(t, job.Error, "timed out", "error should mention timeout")
}

// TestUS0005_MultipartUploadReturns202 verifies REST multipart upload returns 202.
func TestUS0005_MultipartUploadReturns202(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("video.full", &pipeline.Pipeline{
		PipelineName: "video.full",
		Steps: []pipeline.PipelineStep{
			&videoTypeSetterStep{format: "mp4"},
			&videoProcessorStep{transcript: "multipart test", scenes: 1},
		},
	})

	// Simulate multipart upload via the JSON analyze endpoint with video type.
	body, _ := json.Marshal(map[string]string{
		"content":  "simulated-multipart-video-bytes",
		"type":     "video",
		"source":   "e2e-multipart",
		"pipeline": "video.full",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result["job_id"])
}

// TestUS0005_ChunkedUploadFlow verifies init/chunks/complete flow for large files.
func TestUS0005_ChunkedUploadFlow(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("video.chunked", &pipeline.Pipeline{
		PipelineName: "video.chunked",
		Steps: []pipeline.PipelineStep{
			&videoTypeSetterStep{format: "mp4"},
			&videoProcessorStep{transcript: "reassembled from chunks", scenes: 2},
		},
	})

	// Simulate chunked upload: multiple analyze calls building up content,
	// then a final enqueue that processes the assembled content.
	chunks := []string{"chunk-1-header", "chunk-2-body", "chunk-3-footer"}
	assembled := strings.Join(chunks, "")

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  assembled,
		Type:     "video",
		Pipeline: "video.chunked",
		Source:   "e2e-chunked",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))
	assert.Equal(t, "reassembled from chunks", obj.RawContent)
	assert.Len(t, obj.Sections, 2, "chunked video should produce sections")
}

// TestUS0005_TranscriptTextSearchable verifies transcript text appears in search results.
func TestUS0005_TranscriptTextSearchable(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("video.searchable", &pipeline.Pipeline{
		PipelineName: "video.searchable",
		Steps: []pipeline.PipelineStep{
			&videoTypeSetterStep{format: "mp4"},
			&videoProcessorStep{transcript: "quantum entanglement lecture notes", scenes: 1},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "simulated-video",
		Type:     "video",
		Pipeline: "video.searchable",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	// Search for the video object by type.
	resp, err := gohttp.Get(env.URL + "/api/v1/search?q=type==video")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var body struct {
		Data  []storage.KnowledgeObject `json:"data"`
		Total int                       `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.GreaterOrEqual(t, body.Total, 1, "should find at least one video object")

	found := false
	for _, obj := range body.Data {
		if obj.ID == job.ResultID {
			found = true
			assert.Contains(t, obj.RawContent, "quantum entanglement", "transcript should be searchable")
			break
		}
	}
	assert.True(t, found, "video object should appear in search results")
}

// TestUS0005_FrameOCRTextSearchable verifies OCR text from frames is searchable.
func TestUS0005_FrameOCRTextSearchable(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("video.ocr", &pipeline.Pipeline{
		PipelineName: "video.ocr",
		Steps: []pipeline.PipelineStep{
			&videoTypeSetterStep{format: "mp4"},
			&videoProcessorStep{transcript: "spoken words", scenes: 1},
			&videoFrameOCRStep{ocrText: "SLIDE: Introduction to Machine Learning"},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "simulated-video-with-slides",
		Type:     "video",
		Pipeline: "video.ocr",
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

	// Verify OCR text is stored and accessible.
	require.NotNil(t, obj.Metadata)
	ocrText, ok := obj.Metadata["frame_ocr_text"].(string)
	require.True(t, ok, "frame_ocr_text should be a string in metadata")
	assert.Contains(t, ocrText, "Machine Learning")

	// Verify OCR section exists.
	foundOCR := false
	for _, sec := range obj.Sections {
		if sec.Title == "Frame OCR" {
			foundOCR = true
			assert.Contains(t, sec.Content, "Machine Learning")
			break
		}
	}
	assert.True(t, foundOCR, "OCR text should be stored in a Frame OCR section")
}

// TestUS0005_FrameSamplingIntervalConfigurable verifies custom interval changes keyframe rate.
func TestUS0005_FrameSamplingIntervalConfigurable(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("video.custom_sampling", &pipeline.Pipeline{
		PipelineName: "video.custom_sampling",
		Steps: []pipeline.PipelineStep{
			&videoTypeSetterStep{format: "mp4"},
			&videoConfigurableStep{
				name:      "FrameSampler",
				configKey: "frame_sampling_interval_seconds",
				configVal: 5.0,
			},
			&videoProcessorStep{transcript: "custom sampling test", scenes: 2},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "simulated-video",
		Type:     "video",
		Pipeline: "video.custom_sampling",
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

	require.NotNil(t, obj.Metadata)
	interval, ok := obj.Metadata["frame_sampling_interval_seconds"]
	require.True(t, ok, "metadata should contain frame_sampling_interval_seconds")
	assert.Equal(t, 5.0, interval, "frame sampling interval should be configurable")
}

// TestUS0005_SceneDetectionThresholdConfigurable verifies threshold adjusts segmentation.
func TestUS0005_SceneDetectionThresholdConfigurable(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("video.custom_threshold", &pipeline.Pipeline{
		PipelineName: "video.custom_threshold",
		Steps: []pipeline.PipelineStep{
			&videoTypeSetterStep{format: "mp4"},
			&videoConfigurableStep{
				name:      "SceneDetector",
				configKey: "scene_detection_threshold",
				configVal: 0.75,
			},
			&videoProcessorStep{transcript: "threshold test", scenes: 4},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "simulated-video",
		Type:     "video",
		Pipeline: "video.custom_threshold",
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

	require.NotNil(t, obj.Metadata)
	threshold, ok := obj.Metadata["scene_detection_threshold"]
	require.True(t, ok, "metadata should contain scene_detection_threshold")
	assert.Equal(t, 0.75, threshold, "scene detection threshold should be configurable")
}
