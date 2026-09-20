// Package meeting is the ambient meeting-capture Source
// (per ADR-066 + ADR-067 + ADR-069, US-0217).
//
// Records video calls (Zoom, Meet, Teams, FaceTime, Discord, Slack
// Huddles) on the user's local machine. Captures system audio + window
// framebuffer in one stream via OS-blessed public APIs:
//
//	macOS 13+:    ScreenCaptureKit (audio + video together)
//	Windows 10+:  WASAPI loopback + Graphics Capture API + Media Foundation
//	Linux:        xdg-desktop-portal ScreenCast + PipeWire monitor
//
// Routes the resulting media file to existing pipelines:
//
//	audio-only mode → audio.transcribe (diarization, alignment, sectioning)
//	full mode       → video.full (audio_extractor → audio_transcriber →
//	                  speaker_diarizer → frame_sampler → scene_detector →
//	                  frame_ocr → timeline_assembler)
//
// MeetingRecorder interface abstracts per-OS capture so tests can drive
// the state machine without touching real audio/video APIs. Per-OS
// implementations (recorder_darwin.go via Swift bridge, recorder_windows.go
// via C++ bridge, recorder_linux.go via gstreamer) land in follow-up
// commits behind GOOS build tags.
//
// Per ADR-069, recording is **explicit-trigger only in v1**. There is no
// always-on / auto-record mode. Auto-detect prompt (T-0517 / Phase 5) is
// opt-in and never starts recording without user confirmation.
package meeting

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

const (
	SourceName = "meeting"
)

// ErrNotSupported is returned by MeetingRecorder implementations on
// platforms where meeting capture isn't yet implemented.
var ErrNotSupported = errors.New("meeting: not supported on this platform")

// ErrAlreadyRecording is returned by Trigger when a recording is already
// active. Only one recording at a time is supported in v1.
var ErrAlreadyRecording = errors.New("meeting: a recording is already active")

// ErrNoActiveRecording is returned by Stop when no recording is currently
// active.
var ErrNoActiveRecording = errors.New("meeting: no active recording")

// Mode selects audio-only vs full audio+video recording.
type Mode string

const (
	ModeAudioOnly Mode = "audio_only"
	ModeFull      Mode = "full"
)

// EndReason tags how a recording ended.
type EndReason string

const (
	EndUser              EndReason = "user"
	EndAutoDetect        EndReason = "auto_detect"
	EndMaxDuration       EndReason = "max_duration"
	EndError             EndReason = "error"
	EndShutdown          EndReason = "shutdown"
	EndPermissionRevoked EndReason = "permission_revoked"
)

// RecordOptions describe one recording. The user's CLI / hotkey / opt-in
// auto-detect supplies these.
type RecordOptions struct {
	Mode        Mode
	Label       string
	SessionID   string // pre-resolved by ADR-067 cutter
	OutputDir   string // <MediaDir>/<SessionID>/
	MaxDuration time.Duration
	// SourceWindow optionally targets a specific window instead of full screen.
	SourceWindow *WindowTarget
}

// WindowTarget identifies a specific window to capture (vs. full screen).
type WindowTarget struct {
	BundleID    string // macOS
	PID         int    // any
	WindowTitle string // substring match across OSes
}

// RecordResult describes a finished recording.
type RecordResult struct {
	FilePath  string
	Duration  time.Duration
	Bytes     int64
	Mode      Mode
	EndReason EndReason
}

// MeetingRecorder is the OS-abstracted recording engine.
//
// Start begins capture; the returned RecordHandle exposes Pause, Resume,
// Stop, and Status. Implementations are expected to fire OS permission
// prompts on first use; subsequent invocations should not re-prompt.
//
// Returning ErrNotSupported signals an unsupported OS / version combination
// (e.g. macOS 12, X11-only Linux); the Source surfaces it as a failed bus
// event.
type MeetingRecorder interface {
	Start(ctx context.Context, opts RecordOptions) (RecordHandle, error)
}

// RecordHandle is the live recording's control surface.
type RecordHandle interface {
	Pause(ctx context.Context) error
	Resume(ctx context.Context) error
	Stop(ctx context.Context) (RecordResult, error)
	Status() RecordStatus
}

// RecordStatus is a snapshot of the in-flight recording.
type RecordStatus struct {
	Active      bool
	Paused      bool
	Mode        Mode
	StartedAt   time.Time
	ElapsedSec  float64
	BytesOnDisk int64
}

// Config configures a Source.
type Config struct {
	// MediaDir is the absolute path under which recordings are written.
	// Per-recording files land in <MediaDir>/<session_id>/<uuid>.{mov,mp4,webm,m4a}.
	MediaDir string
	// MediaRetentionHours controls how long raw media files are kept after
	// a successful pipeline run. Default: 48. Compaction is run by T-0506.
	MediaRetentionHours int
	// LocalMaxGB caps total media-file disk usage. LRU eviction within cap.
	// Default: 20.
	LocalMaxGB int
	// S3Archive enables cold-storage retention beyond the local TTL.
	// Default: false. When true, media files are uploaded to the S3 buffer
	// backend (T-0510) before local eviction.
	S3Archive bool
}

// Source is the meeting-capture ambient source.
type Source struct {
	recorder  MeetingRecorder
	cfg       Config
	events    chan ambient.RawEvent
	publisher ambient.Publisher

	mu      sync.Mutex
	started bool
	stopped bool
	active  *activeRecording
}

type activeRecording struct {
	handle    RecordHandle
	startedAt time.Time
	opts      RecordOptions
}

// New constructs a Source.
func New(recorder MeetingRecorder, cfg Config) (*Source, error) {
	if recorder == nil {
		return nil, fmt.Errorf("meeting: recorder is required")
	}
	if cfg.MediaDir == "" {
		return nil, fmt.Errorf("meeting: MediaDir is required")
	}
	if cfg.MediaRetentionHours <= 0 {
		cfg.MediaRetentionHours = 48
	}
	if cfg.LocalMaxGB <= 0 {
		cfg.LocalMaxGB = 20
	}
	return &Source{
		recorder: recorder,
		cfg:      cfg,
		events:   make(chan ambient.RawEvent, 4),
	}, nil
}

// Name implements ambient.Source.
func (s *Source) Name() string { return SourceName }

// Start implements ambient.Source. Meeting capture is event-driven (Trigger
// calls), not poll-driven, so Start records readiness without doing real
// work.
func (s *Source) Start(_ context.Context, b ambient.Publisher) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("meeting: already started")
	}
	s.started = true
	s.publisher = b
	s.mu.Unlock()
	if b != nil {
		_ = b.Publish(context.Background(), ambient.SourceLifecycleTopic("ready"), SourceName, nil)
	}
	return nil
}

// Events implements ambient.Source.
func (s *Source) Events() <-chan ambient.RawEvent { return s.events }

// Drain implements ambient.Source. Force-stops any active recording with
// EndReason=shutdown so the partial file is enqueued.
func (s *Source) Drain(ctx context.Context) error {
	s.mu.Lock()
	active := s.active
	s.mu.Unlock()
	if active != nil {
		_, _ = s.StopRecording(ctx, EndShutdown)
	}
	return nil
}

// Stop implements ambient.Source.
func (s *Source) Stop(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil
	}
	s.stopped = true
	close(s.events)
	return nil
}

// Trigger starts a recording. Returns ErrAlreadyRecording if one is in
// flight. The CLI / hotkey listener calls Trigger; auto-detect prompt
// (T-0517) does the same after user confirmation.
//
// Bus events emitted (per ADR-069 §5):
//
//	ctxt.ambient.meeting.requested
//	ctxt.ambient.meeting.started      (after recorder.Start succeeds)
//	ctxt.ambient.meeting.indicator_displayed (after .started; UI must show
//	                                          the recording indicator)
//
// kit/policy CEL guards subscribe to .requested and may veto by returning
// an error from their bus handler; the Runner surfaces this as a failure
// and fires .policy_vetoed.
func (s *Source) Trigger(ctx context.Context, opts RecordOptions) error {
	s.mu.Lock()
	if s.active != nil {
		s.mu.Unlock()
		return ErrAlreadyRecording
	}
	s.mu.Unlock()

	if s.publisher != nil {
		_ = s.publisher.Publish(ctx, "ctxt.ambient.meeting.requested", SourceName,
			map[string]any{"mode": string(opts.Mode), "label": opts.Label, "session_id": opts.SessionID})
	}

	handle, err := s.recorder.Start(ctx, opts)
	if err != nil {
		if s.publisher != nil {
			_ = s.publisher.Publish(ctx, "ctxt.ambient.meeting.failed", SourceName,
				map[string]any{"error": err.Error()})
		}
		return err
	}

	s.mu.Lock()
	s.active = &activeRecording{handle: handle, startedAt: time.Now(), opts: opts}
	s.mu.Unlock()

	if s.publisher != nil {
		_ = s.publisher.Publish(ctx, "ctxt.ambient.meeting.started", SourceName,
			map[string]any{"mode": string(opts.Mode), "session_id": opts.SessionID, "started_at": time.Now().Unix()})
		_ = s.publisher.Publish(ctx, "ctxt.ambient.meeting.indicator_displayed", SourceName, nil)
	}
	return nil
}

// Status returns the active recording's status, or zero-value when none.
func (s *Source) Status() RecordStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		return RecordStatus{}
	}
	return s.active.handle.Status()
}

// PauseRecording pauses the active recording.
func (s *Source) PauseRecording(ctx context.Context) error {
	s.mu.Lock()
	active := s.active
	s.mu.Unlock()
	if active == nil {
		return ErrNoActiveRecording
	}
	if err := active.handle.Pause(ctx); err != nil {
		return err
	}
	if s.publisher != nil {
		_ = s.publisher.Publish(ctx, "ctxt.ambient.meeting.paused", SourceName, nil)
	}
	return nil
}

// ResumeRecording resumes a paused recording.
func (s *Source) ResumeRecording(ctx context.Context) error {
	s.mu.Lock()
	active := s.active
	s.mu.Unlock()
	if active == nil {
		return ErrNoActiveRecording
	}
	if err := active.handle.Resume(ctx); err != nil {
		return err
	}
	if s.publisher != nil {
		_ = s.publisher.Publish(ctx, "ctxt.ambient.meeting.resumed", SourceName, nil)
	}
	return nil
}

// StopRecording finalises the active recording and emits a RawEvent for
// substrate ingest. Returns the RecordResult so the daemon can move/copy
// the file as needed.
func (s *Source) StopRecording(ctx context.Context, reason EndReason) (RecordResult, error) {
	s.mu.Lock()
	active := s.active
	if active == nil {
		s.mu.Unlock()
		return RecordResult{}, ErrNoActiveRecording
	}
	s.active = nil
	s.mu.Unlock()

	result, err := active.handle.Stop(ctx)
	if err != nil {
		if s.publisher != nil {
			_ = s.publisher.Publish(ctx, "ctxt.ambient.meeting.failed", SourceName,
				map[string]any{"error": err.Error()})
		}
		return result, err
	}
	result.EndReason = reason
	result.Mode = active.opts.Mode

	if s.publisher != nil {
		_ = s.publisher.Publish(ctx, "ctxt.ambient.meeting.stopped", SourceName, map[string]any{
			"duration_sec": result.Duration.Seconds(),
			"bytes":        result.Bytes,
			"end_reason":   string(reason),
		})
	}

	pipeline := "video.full"
	kind := ambient.KindMeeting
	if active.opts.Mode == ModeAudioOnly {
		pipeline = "audio.transcribe"
	}

	ev := ambient.RawEvent{
		Source:            SourceName,
		OccurredAt:        active.startedAt,
		Kind:              kind,
		Payload:           []byte(result.FilePath),
		Fingerprint:       fingerprintMeeting(result.FilePath, active.opts.SessionID, active.startedAt),
		SuggestedPipeline: pipeline,
		SessionID:         active.opts.SessionID,
		Metadata: map[string]any{
			"file_path":    result.FilePath,
			"label":        active.opts.Label,
			"mode":         string(active.opts.Mode),
			"duration_sec": result.Duration.Seconds(),
			"bytes":        result.Bytes,
			"end_reason":   string(reason),
		},
	}
	select {
	case s.events <- ev:
		if s.publisher != nil {
			_ = s.publisher.Publish(ctx, "ctxt.ambient.meeting.enqueued", SourceName,
				map[string]any{"pipeline": pipeline, "file_path": result.FilePath})
		}
	case <-ctx.Done():
		return result, ctx.Err()
	}

	return result, nil
}

// fingerprintMeeting returns a stable identifier for a meeting recording.
// Distinct recordings (different session_id or start time) produce
// distinct fingerprints; replays of the same recording produce identical
// fingerprints so the substrate dedups.
func fingerprintMeeting(filePath, sessionID string, startedAt time.Time) string {
	return fmt.Sprintf("meeting|%s|%s|%d", sessionID, filePath, startedAt.UnixNano())
}

// Compile-time check.
var _ ambient.Source = (*Source)(nil)
