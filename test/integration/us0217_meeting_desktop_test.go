package integration

// US-0217: Meeting Capture (Desktop, Audio + Video) — per ADR-069 + T-0513..T-0516, T-0519.
//
// E2E acceptance criteria from docs/stories/capture/US-0217-meeting-capture-desktop.md.
// Verifies: per-OS MeetingRecorder implementations (ScreenCaptureKit on
// macOS 13+, WASAPI loopback + Graphics Capture API on Windows 10+,
// xdg-desktop-portal + PipeWire on Linux) produce playable media files,
// route to audio.transcribe / video.full pipelines, recording indicator
// is mandatory, CEL veto fires before OS permission prompt, retention
// tier evicts media files at TTL.
//
// **Status: skeleton.** Substrate (state machine + retention) is
// implemented + unit-tested (T-0513..T-0519). Per-OS Recorder
// implementations are deferred follow-ups behind GOOS build tags
// (Swift bridge for ScreenCaptureKit / C++ for Media Foundation /
// gstreamer for Linux).

import (
	"testing"
)

func TestMeeting_AudioOnly_macOS(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: macOS recorder (ScreenCaptureKit Swift bridge — T-0513 " +
		"follow-up). Substrate state machine is unit-tested with fakeRecorder")
}

func TestMeeting_Full_macOS(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: macOS recorder full-mode (ScreenCaptureKit audio + window — " +
		"T-0514 follow-up)")
}

func TestMeeting_Full_Windows(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: Windows recorder (WASAPI loopback + Graphics Capture + " +
		"Media Foundation C++ bridge — T-0515 follow-up)")
}

func TestMeeting_Full_LinuxWayland(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: Linux recorder (xdg-desktop-portal + PipeWire + gstreamer — " +
		"T-0516 follow-up)")
}

func TestMeeting_Full_LinuxX11Fallback(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: Linux X11 recorder (XComposite + XDamage second-class " +
		"fallback — part of T-0516)")
}

func TestMeeting_HotkeyTrigger(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: global hotkey listener registration (per OS); CLI Trigger " +
		"path is unit-tested via TestSource_TriggerStartsRecording")
}

func TestMeeting_RecordingIndicatorMandatory(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: indicator UI implementation per OS (menubar pulse on " +
		"macOS / tray on Windows / portal-mediated on Linux); substrate emits " +
		"ctxt.ambient.meeting.indicator_displayed which is unit-tested")
}

func TestMeeting_CELVetoBeforePermission(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: kit/policy CEL rule wiring at ctxt.ambient.meeting.requested " +
		"(must fire before recorder.Start which would surface OS permission prompt)")
}

func TestMeeting_DeviceChangeMidRecording(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: per-OS recorder must hook AVCaptureSession (macOS) / " +
		"AudioDeviceChange (Windows) / PipeWire device-change events to fire " +
		"ctxt.ambient.meeting.device_changed (substrate emits the topic)")
}

func TestMeeting_RetentionEviction(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: E2E harness — retention is unit-tested in " +
		"TestRetention_TTLEvictsOldFiles; E2E verifies a real meeting recording " +
		"is evicted after media_retention_hours")
}

func TestMeeting_S3Archive(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: meeting/retention.go S3 archive hook is stubbed; needs " +
		"wiring to internal/ambient/buffer/s3 (currently s3 buffer is for events, " +
		"not media files — needs a separate kit/blob.Store for media)")
}
