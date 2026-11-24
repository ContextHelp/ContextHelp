package providers

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// sceneFixture puts a fixture ffmpeg first on PATH for the test.
func sceneFixture(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture tools are sh scripts")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil { // #nosec G306 -- fixture must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// The scene pass stops ffmpeg once the cut cap is in: this ffmpeg reports
// five cuts and then would run for 30s.
func TestSceneCutsStopAtTheCap(t *testing.T) {
	sceneFixture(t, `printf 'pts_time:1\npts_time:2\npts_time:3\npts_time:4\npts_time:5\n' >&2
exec sleep 30`)
	old := sceneCutCap
	sceneCutCap = 3
	defer func() { sceneCutCap = old }()

	start := time.Now()
	cuts, err := sceneCutsMS(context.Background(), "/x/v.mp4", 0, 60_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(cuts) != 3 || cuts[2] != 3000 {
		t.Errorf("cuts = %v, want the first three", cuts)
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("took %v: ffmpeg was not stopped at the cap", time.Since(start))
	}
}

// The scene pass runs without progress stats and only over the probed
// duration, and does not buffer output it does not need.
func TestSceneCutsArgs(t *testing.T) {
	log := filepath.Join(t.TempDir(), "args")
	sceneFixture(t, `echo "$*" > `+log)
	if _, err := sceneCutsMS(context.Background(), "/x/v.mp4", 0.4, 90_500); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(log)
	for _, want := range []string{"-nostats", "-t 90.500", "-i file:/x/v.mp4", "gt(scene,0.4)"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("args lack %q: %s", want, raw)
		}
	}
}

func TestSceneCutsReportsFailure(t *testing.T) {
	sceneFixture(t, `echo 'Invalid data found' >&2; exit 1`)
	_, err := sceneCutsMS(context.Background(), "/x/v.mp4", 0, 1000)
	if err == nil || !strings.Contains(err.Error(), "Invalid data found") {
		t.Fatalf("err = %v, want ffmpeg's last output", err)
	}
}

// A line longer than the reader's buffer is skipped, not fatal: the cuts
// after it are still read.
func TestSceneCutsSurviveAnOverlongLine(t *testing.T) {
	sceneFixture(t, `head -c 2000000 /dev/zero | tr '\0' a >&2
echo >&2
printf 'pts_time:1\npts_time:2\n' >&2`)
	cuts, err := sceneCutsMS(context.Background(), "/x/v.mp4", 0, 60_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(cuts) != 2 || cuts[1] != 2000 {
		t.Errorf("cuts = %v, want the two after the long line", cuts)
	}
}
