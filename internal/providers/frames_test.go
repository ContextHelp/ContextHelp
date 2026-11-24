package providers

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestPlanFrameTimes(t *testing.T) {
	tests := []struct {
		name string
		dur  int64
		cuts []int64
		opts FrameOptions
		want []int64
	}{
		{"even at max rate", 10_000, nil, FrameOptions{MaxFPS: 0.2}, []int64{0, 5000}},
		{"even at min rate when set", 10_000, nil, FrameOptions{MinFPS: 0.5, MaxFPS: 1}, []int64{0, 2000, 4000, 6000, 8000}},
		{"cuts only", 20_000, []int64{7000, 3000}, FrameOptions{SceneDetection: true}, []int64{0, 3000, 7000}},
		{"max rate drops close cuts", 20_000, []int64{3000, 3400, 9000}, FrameOptions{MaxFPS: 1, SceneDetection: true}, []int64{0, 3000, 9000}},
		{"min rate fills gaps", 20_000, []int64{9000}, FrameOptions{MinFPS: 0.2, SceneDetection: true}, []int64{0, 5000, 9000, 14000, 19000}},
		{"cut at start or end ignored", 10_000, []int64{0, 10_000, 12_000}, FrameOptions{SceneDetection: true}, []int64{0}},
		{"cuts ignored without scene detection", 10_000, []int64{3000}, FrameOptions{MaxFPS: 0.2}, []int64{0, 5000}},
		{"no duration", 0, nil, FrameOptions{MaxFPS: 1}, nil},
		// A cut right after a filler keeps its place: the filler moves back.
		{"cut just after a filler is kept", 30_000, []int64{10_500}, FrameOptions{MinFPS: 0.1, MaxFPS: 1, SceneDetection: true}, []int64{0, 9500, 10_500, 20_500}},
		{"frame cap thins by time bucket", 100_000, nil, FrameOptions{MaxFPS: 1, MaxFrames: 4}, []int64{0, 25_000, 50_000, 75_000}},
		// A cut wins its bucket over the filler before it; bucket 0 keeps frame 0.
		{"frame cap prefers a cut in the bucket", 100_000, []int64{30_000, 60_000, 90_000}, FrameOptions{MinFPS: 0.1, MaxFPS: 1, SceneDetection: true, MaxFrames: 4}, []int64{0, 30_000, 60_000, 90_000}},
		// The window between the bounds is narrower than twice the minimum gap:
		// the cut gives way, so no gap exceeds the maximum.
		{"narrow window drops the cut", 5000, []int64{1800}, FrameOptions{MinFPS: 0.6, MaxFPS: 1, SceneDetection: true}, []int64{0, 1666, 3332, 4998}},
		{"narrow window keeps a cut that fits", 10_000, []int64{3000}, FrameOptions{MinFPS: 0.6, MaxFPS: 1, SceneDetection: true}, []int64{0, 1666, 3000, 4666, 6332, 7998, 9664}},
		{"frame cap of one", 100_000, nil, FrameOptions{MaxFPS: 1, MaxFrames: 1}, []int64{0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := planFrameTimes(tt.dur, tt.cuts, tt.opts); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlanFrameTimesIsBounded(t *testing.T) {
	// A day at 1000 fps would be 86 million frames; the plan stays within
	// the candidate cap and then the frame cap.
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	got := planFrameTimes(maxVideoDurationMS, nil, FrameOptions{MinFPS: 1000, MaxFPS: 1000})
	runtime.ReadMemStats(&after)
	if len(got) != DefaultMaxFrames {
		t.Fatalf("got %d frames, want the default cap %d", len(got), DefaultMaxFrames)
	}
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 64<<20 {
		t.Errorf("planning allocated %d MiB; the candidate list must stay bounded", alloc>>20)
	}
	// A flood of scene cuts is capped too.
	cuts := make([]int64, 500_000)
	for i := range cuts {
		cuts[i] = int64(i+1) * 10
	}
	got = planFrameTimes(5_000_010, cuts, FrameOptions{SceneDetection: true})
	if len(got) != DefaultMaxFrames {
		t.Fatalf("got %d frames for a cut flood, want %d", len(got), DefaultMaxFrames)
	}
	// The cuts are decimated across the whole video, not cut off at a prefix.
	if last := got[len(got)-1]; last < 4_500_000 {
		t.Errorf("the last frame is at %dms: a cut flood must be sampled across the full length", last)
	}
	// A video of any real length is sampled across all of it.
	long := int64(40 * 3600 * 1000) // beyond the old 24h cap
	got = planFrameTimes(long, nil, FrameOptions{MinFPS: 0.001})
	if len(got) == 0 || got[len(got)-1] < long*9/10 {
		t.Errorf("a 40h video is not covered: %d frames, last at %v", len(got), got[len(got)-1])
	}
}

func TestFrameOptionsWithDefaults(t *testing.T) {
	d := DefaultFrameOptions()
	tests := []struct {
		name     string
		in, want FrameOptions
	}{
		{"nothing set", FrameOptions{}, FrameOptions{MinFPS: d.MinFPS, MaxFPS: d.MaxFPS, MaxFrames: d.MaxFrames, MaxDimension: d.MaxDimension}},
		{"only max set keeps default min", FrameOptions{MaxFPS: 2}, FrameOptions{MinFPS: d.MinFPS, MaxFPS: 2, MaxFrames: d.MaxFrames, MaxDimension: d.MaxDimension}},
		{"only min set keeps default max", FrameOptions{MinFPS: 0.5}, FrameOptions{MinFPS: 0.5, MaxFPS: d.MaxFPS, MaxFrames: d.MaxFrames, MaxDimension: d.MaxDimension}},
		{"max below the default min leaves min unset", FrameOptions{MaxFPS: 0.05}, FrameOptions{MaxFPS: 0.05, MaxFrames: d.MaxFrames, MaxDimension: d.MaxDimension}},
		{"min at the default max leaves max unset", FrameOptions{MinFPS: 1}, FrameOptions{MinFPS: 1, MaxFrames: d.MaxFrames, MaxDimension: d.MaxDimension}},
	}
	for _, tt := range tests {
		got := tt.in.WithDefaults()
		if got != tt.want {
			t.Errorf("%s: got %+v, want %+v", tt.name, got, tt.want)
		}
		if err := got.Validate(); err != nil {
			t.Errorf("%s: defaults do not validate: %v", tt.name, err)
		}
	}
}

func TestFrameOptionsValidate(t *testing.T) {
	bad := []FrameOptions{
		{},
		{MinFPS: -1, SceneDetection: true},
		{MinFPS: 2, MaxFPS: 1},
		{SceneDetection: true, SceneThreshold: 1.5},
		{MaxFPS: math.NaN()},
		{MaxFPS: math.Inf(1)},
		{MinFPS: math.Inf(1), SceneDetection: true},
		{SceneDetection: true, SceneThreshold: math.NaN()},
		{MaxFPS: 1001},
		{MinFPS: 0.3, MaxFPS: 0.3}, // ceil(3333.3) > floor(3333.3)
		{MaxFPS: 1, MaxFrames: -1},
		{MaxFPS: 1, MaxFrames: 10_001},
		{MaxFPS: 1, MaxDimension: -2},
		{MaxFPS: 1, MaxDimension: 99_999},
	}
	for _, o := range bad {
		if err := o.Validate(); !errors.Is(err, ErrInvalidFrameOptions) {
			t.Errorf("%+v: err = %v", o, err)
		}
	}
	for _, o := range []FrameOptions{{MaxFPS: 1}, {MinFPS: 1}, {SceneDetection: true}, {MinFPS: 0.1, MaxFPS: 1, SceneDetection: true}, {MinFPS: 0.5, MaxFPS: 0.5}, {MaxFPS: 1000}, {MaxFPS: 1, MaxDimension: -1}} {
		if err := o.Validate(); err != nil {
			t.Errorf("%+v: %v", o, err)
		}
	}
}

// bytesSource is a MediaSource that is not a local file.
type bytesSource string

func (s bytesSource) Open(context.Context) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(string(s))), nil
}

func TestSpoolToFile(t *testing.T) {
	path, cleanup, err := spoolToFile(context.Background(), bytesSource("media"))
	if err != nil {
		t.Fatal(err)
	}
	if got := readFileString(t, path); got != "media" {
		t.Errorf("spooled %q", got)
	}
	cleanup()
	if fileExists(path) {
		t.Error("cleanup left the spool file")
	}

	local, cleanupLocal, err := spoolToFile(context.Background(), FileSource("/some/where.mp4"))
	if err != nil || local != "/some/where.mp4" {
		t.Fatalf("local source: %q, %v", local, err)
	}
	cleanupLocal()
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// brokenFrames yields whatever its frames say.
type brokenFrames []Frame

func (b brokenFrames) Name() string { return "broken" }
func (b brokenFrames) ExtractFrames(_ context.Context, _ MediaSource, _ FrameOptions, yield FrameYield) error {
	for _, f := range b {
		if err := yield(f); err != nil {
			return err
		}
	}
	return nil
}

func TestStreamFramesChecksTheContract(t *testing.T) {
	png := []byte{1}
	ok := func(i int, ms int64) Frame {
		return Frame{Index: i, TimestampMS: ms, ContentType: "image/png", Data: png}
	}
	tests := []struct {
		name   string
		frames brokenFrames
		bad    bool
	}{
		{"valid", brokenFrames{ok(0, 0), ok(1, 5)}, false},
		{"index does not start at 0", brokenFrames{ok(1, 0)}, true},
		{"index skips", brokenFrames{ok(0, 0), ok(2, 5)}, true},
		{"timestamps repeat", brokenFrames{ok(0, 5), ok(1, 5)}, true},
		{"timestamps go back", brokenFrames{ok(0, 5), ok(1, 2)}, true},
		{"negative timestamp", brokenFrames{ok(0, -1)}, true},
		{"no bytes", brokenFrames{{Index: 0, ContentType: "image/png"}}, true},
		{"no content type", brokenFrames{{Index: 0, Data: png}}, true},
	}
	for _, tt := range tests {
		err := StreamFrames(context.Background(), tt.frames, FileSource("/x.mp4"), FrameOptions{MaxFPS: 1}, func(Frame) error { return nil })
		if tt.bad != errors.Is(err, ErrInvalidFrame) {
			t.Errorf("%s: err = %v, want invalid=%v", tt.name, err, tt.bad)
		}
	}
	// A backend that ignores the cap is caught.
	var many brokenFrames
	for i := 0; i < 3; i++ {
		many = append(many, Frame{Index: i, TimestampMS: int64(i), ContentType: "image/png", Data: png})
	}
	err := StreamFrames(context.Background(), many, FileSource("/x.mp4"), FrameOptions{MaxFPS: 1, MaxFrames: 2}, func(Frame) error { return nil })
	if !errors.Is(err, ErrInvalidFrame) {
		t.Errorf("over the cap: err = %v", err)
	}
}

func TestSpoolToFileRules(t *testing.T) {
	if _, _, err := spoolToFile(context.Background(), FileSource("relative.mp4")); !errors.Is(err, errRelativeMediaPath) || !errors.Is(err, ErrUnusableMedia) {
		t.Errorf("relative path: err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := spoolToFile(ctx, bytesSource("media")); err == nil {
		t.Error("canceled context: want error")
	}
}
