package meeting

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

// fakeRecorder is a controllable MeetingRecorder.
type fakeRecorder struct {
	mu        sync.Mutex
	startErr  error
	stopErr   error
	stopRes   RecordResult
	startCalls atomic.Int64
}

func (f *fakeRecorder) Start(_ context.Context, opts RecordOptions) (RecordHandle, error) {
	f.mu.Lock()
	err := f.startErr
	stopRes := f.stopRes
	stopErr := f.stopErr
	f.mu.Unlock()
	f.startCalls.Add(1)
	if err != nil {
		return nil, err
	}
	return &fakeHandle{
		startedAt: time.Now(),
		mode:      opts.Mode,
		stopRes:   stopRes,
		stopErr:   stopErr,
	}, nil
}

type fakeHandle struct {
	startedAt time.Time
	mode      Mode

	mu      sync.Mutex
	paused  bool
	stopped bool

	stopRes RecordResult
	stopErr error
}

func (h *fakeHandle) Pause(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.paused = true
	return nil
}

func (h *fakeHandle) Resume(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.paused = false
	return nil
}

func (h *fakeHandle) Stop(_ context.Context) (RecordResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopped = true
	if h.stopErr != nil {
		return RecordResult{}, h.stopErr
	}
	res := h.stopRes
	if res.Duration == 0 {
		res.Duration = time.Since(h.startedAt)
	}
	if res.FilePath == "" {
		res.FilePath = "/tmp/fake-recording.mp4"
	}
	res.Mode = h.mode
	return res, nil
}

func (h *fakeHandle) Status() RecordStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	return RecordStatus{
		Active:     !h.stopped,
		Paused:     h.paused,
		Mode:       h.mode,
		StartedAt:  h.startedAt,
		ElapsedSec: time.Since(h.startedAt).Seconds(),
	}
}

type capturingPub struct {
	mu     sync.Mutex
	topics []string
}

func (p *capturingPub) Publish(_ context.Context, topic, _ string, _ any) error {
	p.mu.Lock()
	p.topics = append(p.topics, topic)
	p.mu.Unlock()
	return nil
}

func (p *capturingPub) HasTopic(t string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.topics {
		if s == t {
			return true
		}
	}
	return false
}

func newSource(t *testing.T, recorder MeetingRecorder, cfg Config) *Source {
	t.Helper()
	if cfg.MediaDir == "" {
		cfg.MediaDir = t.TempDir()
	}
	s, err := New(recorder, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestNew_RequiresRecorderAndMediaDir(t *testing.T) {
	t.Parallel()
	if _, err := New(nil, Config{MediaDir: "/tmp"}); err == nil {
		t.Error("New(nil): expected error")
	}
	if _, err := New(&fakeRecorder{}, Config{}); err == nil {
		t.Error("New(no MediaDir): expected error")
	}
}

func TestNew_AppliesDefaults(t *testing.T) {
	t.Parallel()
	s, _ := New(&fakeRecorder{}, Config{MediaDir: "/tmp"})
	if s.cfg.MediaRetentionHours != 48 {
		t.Errorf("MediaRetentionHours = %d, want 48", s.cfg.MediaRetentionHours)
	}
	if s.cfg.LocalMaxGB != 20 {
		t.Errorf("LocalMaxGB = %d, want 20", s.cfg.LocalMaxGB)
	}
}

func TestSource_NameIsConstant(t *testing.T) {
	t.Parallel()
	s := newSource(t, &fakeRecorder{}, Config{})
	if s.Name() != "meeting" {
		t.Errorf("Name = %q, want meeting", s.Name())
	}
}

func TestSource_TriggerStartsRecording(t *testing.T) {
	t.Parallel()
	rec := &fakeRecorder{}
	pub := &capturingPub{}
	s := newSource(t, rec, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, pub)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	if err := s.Trigger(ctx, RecordOptions{Mode: ModeAudioOnly, Label: "test"}); err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	if rec.startCalls.Load() != 1 {
		t.Errorf("recorder.Start called %d times, want 1", rec.startCalls.Load())
	}
	for _, want := range []string{
		"ctxt.ambient.meeting.requested",
		"ctxt.ambient.meeting.started",
		"ctxt.ambient.meeting.indicator_displayed",
	} {
		if !pub.HasTopic(want) {
			t.Errorf("missing topic %q", want)
		}
	}
}

func TestSource_TriggerWhenAlreadyRecordingReturnsError(t *testing.T) {
	t.Parallel()
	s := newSource(t, &fakeRecorder{}, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	_ = s.Trigger(ctx, RecordOptions{Mode: ModeAudioOnly})
	err := s.Trigger(ctx, RecordOptions{Mode: ModeAudioOnly})
	if !errors.Is(err, ErrAlreadyRecording) {
		t.Errorf("second Trigger: want ErrAlreadyRecording, got %v", err)
	}
}

func TestSource_StopRecordingEmitsRawEvent(t *testing.T) {
	t.Parallel()
	rec := &fakeRecorder{stopRes: RecordResult{
		FilePath: "/tmp/q3-planning.mp4",
		Duration: 30 * time.Minute,
		Bytes:    500 * 1024 * 1024,
	}}
	pub := &capturingPub{}
	s := newSource(t, rec, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, pub)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	_ = s.Trigger(ctx, RecordOptions{Mode: ModeFull, Label: "Q3 planning", SessionID: "sess_xyz"})
	result, err := s.StopRecording(ctx, EndUser)
	if err != nil {
		t.Fatalf("StopRecording: %v", err)
	}
	if result.EndReason != EndUser {
		t.Errorf("EndReason = %q, want %q", result.EndReason, EndUser)
	}

	select {
	case ev := <-s.Events():
		if ev.Kind != ambient.KindMeeting {
			t.Errorf("Kind = %q, want KindMeeting", ev.Kind)
		}
		if string(ev.Payload) != "/tmp/q3-planning.mp4" {
			t.Errorf("Payload = %q", ev.Payload)
		}
		if ev.SessionID != "sess_xyz" {
			t.Errorf("SessionID = %q, want sess_xyz", ev.SessionID)
		}
		if ev.SuggestedPipeline != "video.full" {
			t.Errorf("SuggestedPipeline = %q, want video.full", ev.SuggestedPipeline)
		}
		if ev.Metadata["label"] != "Q3 planning" {
			t.Errorf("Metadata[label] = %v, want Q3 planning", ev.Metadata["label"])
		}
		if ev.Metadata["mode"] != string(ModeFull) {
			t.Errorf("Metadata[mode] = %v, want full", ev.Metadata["mode"])
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive event after StopRecording")
	}

	for _, want := range []string{
		"ctxt.ambient.meeting.stopped",
		"ctxt.ambient.meeting.enqueued",
	} {
		if !pub.HasTopic(want) {
			t.Errorf("missing topic %q", want)
		}
	}
}

func TestSource_AudioOnlyRoutesToAudioTranscribe(t *testing.T) {
	t.Parallel()
	rec := &fakeRecorder{stopRes: RecordResult{FilePath: "/tmp/test.m4a"}}
	s := newSource(t, rec, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	_ = s.Trigger(ctx, RecordOptions{Mode: ModeAudioOnly})
	_, _ = s.StopRecording(ctx, EndUser)

	select {
	case ev := <-s.Events():
		if ev.SuggestedPipeline != "audio.transcribe" {
			t.Errorf("audio-only mode SuggestedPipeline = %q, want audio.transcribe", ev.SuggestedPipeline)
		}
	case <-time.After(time.Second):
		t.Fatal("no event after StopRecording")
	}
}

func TestSource_StopRecordingWithNoActiveReturnsError(t *testing.T) {
	t.Parallel()
	s := newSource(t, &fakeRecorder{}, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	_, err := s.StopRecording(ctx, EndUser)
	if !errors.Is(err, ErrNoActiveRecording) {
		t.Errorf("StopRecording with no active: want ErrNoActiveRecording, got %v", err)
	}
}

func TestSource_PauseAndResume(t *testing.T) {
	t.Parallel()
	pub := &capturingPub{}
	s := newSource(t, &fakeRecorder{}, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, pub)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	_ = s.Trigger(ctx, RecordOptions{Mode: ModeFull})
	if err := s.PauseRecording(ctx); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if !pub.HasTopic("ctxt.ambient.meeting.paused") {
		t.Error("missing meeting.paused topic")
	}
	if err := s.ResumeRecording(ctx); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if !pub.HasTopic("ctxt.ambient.meeting.resumed") {
		t.Error("missing meeting.resumed topic")
	}
}

func TestSource_DrainStopsActiveRecording(t *testing.T) {
	t.Parallel()
	rec := &fakeRecorder{}
	pub := &capturingPub{}
	s := newSource(t, rec, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, pub)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	_ = s.Trigger(ctx, RecordOptions{Mode: ModeFull})
	if err := s.Drain(ctx); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	// Drain should have called StopRecording with EndShutdown.
	deadline := time.After(time.Second)
	for {
		if pub.HasTopic("ctxt.ambient.meeting.stopped") {
			return
		}
		select {
		case <-deadline:
			t.Fatal("Drain did not stop active recording")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestSource_TriggerSurfacesRecorderError(t *testing.T) {
	t.Parallel()
	rec := &fakeRecorder{startErr: errors.New("permission denied")}
	pub := &capturingPub{}
	s := newSource(t, rec, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, pub)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	if err := s.Trigger(ctx, RecordOptions{Mode: ModeFull}); err == nil {
		t.Fatal("expected Trigger error")
	}
	if !pub.HasTopic("ctxt.ambient.meeting.failed") {
		t.Error("missing meeting.failed topic")
	}
}

func TestSource_StatusReturnsRecordingState(t *testing.T) {
	t.Parallel()
	s := newSource(t, &fakeRecorder{}, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	if status := s.Status(); status.Active {
		t.Error("Status before Trigger: should not be Active")
	}

	_ = s.Trigger(ctx, RecordOptions{Mode: ModeFull})
	status := s.Status()
	if !status.Active {
		t.Error("Status during recording: want Active=true")
	}
	if status.Mode != ModeFull {
		t.Errorf("Status.Mode = %q, want full", status.Mode)
	}
}

func TestFingerprintMeeting_Stable(t *testing.T) {
	t.Parallel()
	t1 := time.Date(2026, 5, 5, 14, 0, 0, 0, time.UTC)
	a := fingerprintMeeting("/tmp/a.mp4", "sess_xyz", t1)
	b := fingerprintMeeting("/tmp/a.mp4", "sess_xyz", t1)
	if a != b {
		t.Error("identical inputs must yield identical fingerprints")
	}
	c := fingerprintMeeting("/tmp/a.mp4", "sess_OTHER", t1)
	if a == c {
		t.Error("distinct session_id must yield distinct fingerprints")
	}
}

// Compile-time check.
var _ ambient.Source = (*Source)(nil)
