package providers

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

type notRecordedErr struct{}

func (notRecordedErr) Error() string            { return "no recording" }
func (notRecordedErr) CommandNotRecorded() bool { return true }

// The seam maps a runner's (stdout, stderr, code, err) onto RunCommand's
// result and error the way a real process failure would be reported.
func TestRunCommandSeamErrorMapping(t *testing.T) {
	cases := []struct {
		name       string
		code       int
		err        error
		wantErr    string
		wantIs     error
		wantResult bool
	}{
		{name: "success", code: 0},
		{name: "exit 2 with stderr", code: 2, err: errors.New("exit status 2"), wantErr: "tool exited with code 2: boom"},
		{name: "start failure wraps the cause", code: -1, err: exec.ErrNotFound, wantErr: "tool: executable file not found", wantIs: exec.ErrNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			run := func(context.Context, string, ...string) (string, string, int, error) {
				return "out", "boom", c.code, c.err
			}
			ctx := WithCommandRunner(context.Background(), run)
			res, err := RunCommand(ctx, "tool")
			if res == nil || res.Stdout != "out" || res.ExitCode != c.code {
				t.Errorf("result = %+v, want stdout out and exit %d", res, c.code)
			}
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, c.wantErr)
			}
			if c.wantIs != nil && !errors.Is(err, c.wantIs) {
				t.Errorf("errors.Is(%v, %v) = false", err, c.wantIs)
			}
		})
	}
}

func TestIsCommandNotRecorded(t *testing.T) {
	wrapped := errors.Join(errors.New("x"), notRecordedErr{})
	if !IsCommandNotRecorded(wrapped) || IsCommandNotRecorded(errors.New("plain")) {
		t.Error("IsCommandNotRecorded must match only errors carrying the marker")
	}
}

func TestWhisperFallbackSkippedOnReplayMiss(t *testing.T) {
	for _, c := range []struct {
		name  string
		err   error
		calls int
	}{
		{"flag mismatch retries with the second flag set", errors.New("exit status 1"), 2},
		{"replay miss does not retry", notRecordedErr{}, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			calls := 0
			run := func(context.Context, string, ...string) (string, string, int, error) {
				calls++
				return "", "", -1, c.err
			}
			ctx := WithCommandRunner(context.Background(), run)
			_, err := NewWhisperTranscriptionProvider("whisper").Transcribe(ctx, "a.wav", TranscribeOptions{})
			if err == nil {
				t.Fatal("want an error")
			}
			if calls != c.calls {
				t.Errorf("calls = %d, want %d", calls, c.calls)
			}
		})
	}
}

// Structural tsv rows report conf -1 and a numeric height just before it;
// only word rows carry confidence.
func TestMeanWordConfidence(t *testing.T) {
	tsv := "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n" +
		"1\t1\t0\t0\t0\t0\t0\t0\t560\t140\t-1\t\n" +
		"4\t1\t1\t1\t1\t0\t26\t31\t272\t31\t-1\t\n" +
		"5\t1\t1\t1\t1\t1\t26\t32\t151\t30\t96.5\tCassette\n" +
		"5\t1\t1\t1\t1\t2\t189\t31\t109\t31\t94.5\tfixture\n"
	if got := meanWordConfidence(tsv); got < 0.9549 || got > 0.9551 {
		t.Errorf("meanWordConfidence = %v, want 0.955", got)
	}
	if got := meanWordConfidence("level\tx\n"); got != 0 {
		t.Errorf("no words = %v, want 0", got)
	}
}
