package contracttest

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

// FramesFixture describes the video a frames backend reads in a test.
type FramesFixture struct {
	Source providers.MediaSource
	// DurationMS is the video's length.
	DurationMS int64
	// SceneCutsMS are the scene changes the backend will find in it, at
	// least a second apart; empty when the backend finds none. A fixture
	// should also hold cuts closer than 1/MaxFPS to one of these, so the
	// max-rate bound has something to drop.
	SceneCutsMS []int64
}

// FramesCase is one frames backend with the video it reads.
type FramesCase struct {
	Name string
	// Stub marks the placeholder backend (see Case.Stub).
	Stub bool
	New  func(t testing.TB) (providers.FrameProvider, FramesFixture)
}

// Frames is the frames role's suite. Backends must: reject unusable
// options, honour the rate bounds, the frame cap and scene detection,
// yield frames from index 0 in time order with image bytes, stop when
// the context is canceled or yield fails, and keep nothing after yielding.
func Frames(t *testing.T, cases []FramesCase) {
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			p, f := c.New(t)
			framesSuite(t, c, p, f)
		})
	}
}

// collect gathers what p yields, raw (no provider-side checks), so the
// suite's own assertions judge the backend.
func collect(ctx context.Context, p providers.FrameProvider, src providers.MediaSource, o providers.FrameOptions) ([]providers.Frame, error) {
	var out []providers.Frame
	err := p.ExtractFrames(ctx, src, o, func(f providers.Frame) error {
		out = append(out, f)
		return nil
	})
	return out, err
}

// framesRun is one frames backend and its fixture under test.
type framesRun struct {
	ctx context.Context
	p   providers.FrameProvider
	f   FramesFixture
}

func framesSuite(t *testing.T, _ FramesCase, p providers.FrameProvider, f FramesFixture) {
	requireName(t, p.Name())
	r := framesRun{ctx: context.Background(), p: p, f: f}
	r.invalidInput(t)
	r.stopping(t)
	r.rateBounds(t)
	r.sceneCuts(t)
}

// invalidInput: unusable options and a nil source are refused.
func (r framesRun) invalidInput(t *testing.T) {
	t.Run("rejects invalid options", func(t *testing.T) {
		for name, o := range map[string]providers.FrameOptions{
			"none":       {},
			"negative":   {MinFPS: -1, SceneDetection: true},
			"min>max":    {MinFPS: 2, MaxFPS: 1},
			"equal":      {MinFPS: 0.3, MaxFPS: 0.3},
			"threshold":  {SceneDetection: true, SceneThreshold: 2},
			"nan":        {MaxFPS: math.NaN()},
			"inf":        {MaxFPS: math.Inf(1)},
			"too fast":   {MaxFPS: 5000},
			"max frames": {MaxFPS: 1, MaxFrames: -1},
		} {
			if _, err := collect(r.ctx, r.p, r.f.Source, o); !errors.Is(err, providers.ErrInvalidFrameOptions) {
				t.Errorf("%s: err = %v, want ErrInvalidFrameOptions", name, err)
			}
		}
	})

	t.Run("rejects nil source", func(t *testing.T) {
		if _, err := collect(r.ctx, r.p, nil, providers.FrameOptions{MaxFPS: 1}); err == nil {
			t.Error("want error")
		}
	})
}

// stopping: a canceled context, and a failing yield, end the run.
func (r framesRun) stopping(t *testing.T) {
	t.Run("honours canceled context", func(t *testing.T) {
		if _, err := collect(canceled(), r.p, r.f.Source, providers.FrameOptions{MaxFPS: 1}); err == nil {
			t.Error("want error")
		}
	})

	t.Run("stops when the context is canceled mid-run", func(t *testing.T) {
		cctx, cancel := context.WithCancel(r.ctx)
		defer cancel()
		n := 0
		err := r.p.ExtractFrames(cctx, r.f.Source, providers.FrameOptions{MaxFPS: 0.05}, func(providers.Frame) error {
			n++
			cancel()
			return nil
		})
		if err == nil || n != 1 {
			t.Errorf("err = %v after %d frames, want an error after exactly 1", err, n)
		}
	})

	t.Run("stops when yield fails", func(t *testing.T) {
		boom := errors.New("boom")
		n := 0
		err := r.p.ExtractFrames(r.ctx, r.f.Source, providers.FrameOptions{MaxFPS: 0.05}, func(providers.Frame) error {
			n++
			return boom
		})
		if !errors.Is(err, boom) || n != 1 {
			t.Errorf("err = %v after %d frames, want boom after exactly 1", err, n)
		}
	})
}

func (r framesRun) check(t *testing.T, o providers.FrameOptions) []providers.Frame {
	t.Helper()
	frames, err := collect(r.ctx, r.p, r.f.Source, o)
	if err != nil {
		t.Fatalf("ExtractFrames(%+v): %v", o, err)
	}
	if len(frames) == 0 {
		t.Fatalf("no frames for %+v", o)
	}
	if len(frames) > o.EffectiveMaxFrames() {
		t.Errorf("%d frames exceed the cap %d", len(frames), o.EffectiveMaxFrames())
	}
	for i, fr := range frames {
		if fr.Index != i {
			t.Errorf("frame %d: Index = %d", i, fr.Index)
		}
		if fr.TimestampMS < 0 || fr.TimestampMS >= r.f.DurationMS {
			t.Errorf("frame %d: TimestampMS %d outside [0,%d)", i, fr.TimestampMS, r.f.DurationMS)
		}
		if i > 0 && fr.TimestampMS <= frames[i-1].TimestampMS {
			t.Errorf("frame %d: TimestampMS %d not after %d", i, fr.TimestampMS, frames[i-1].TimestampMS)
		}
		if len(fr.Data) == 0 {
			t.Errorf("frame %d: no image bytes", i)
		}
		if fr.ContentType == "" {
			t.Errorf("frame %d: empty ContentType", i)
		}
	}
	if frames[0].TimestampMS != 0 {
		t.Errorf("first frame at %dms, want 0", frames[0].TimestampMS)
	}
	return frames
}

// rateBounds: the rate bounds, the frame cap and rate-only spacing.
func (r framesRun) rateBounds(t *testing.T) {
	t.Run("honours max frame rate", func(t *testing.T) {
		o := providers.FrameOptions{MaxFPS: 1, SceneDetection: true}
		frames := r.check(t, o)
		for i := 1; i < len(frames); i++ {
			if gap := frames[i].TimestampMS - frames[i-1].TimestampMS; gap < o.MinGapMS() {
				t.Errorf("frames %d-%d are %dms apart, below %dms", i-1, i, gap, o.MinGapMS())
			}
		}
	})

	t.Run("honours min frame rate", func(t *testing.T) {
		o := providers.FrameOptions{MinFPS: 0.1, MaxFPS: 1, SceneDetection: true}
		frames := r.check(t, o)
		for i := 1; i < len(frames); i++ {
			if gap := frames[i].TimestampMS - frames[i-1].TimestampMS; gap > o.MaxGapMS() {
				t.Errorf("frames %d-%d are %dms apart, above %dms", i-1, i, gap, o.MaxGapMS())
			}
		}
	})

	t.Run("honours both bounds in a narrow window", func(t *testing.T) {
		// The window between the bounds (1000ms to 1666ms) is narrower than
		// twice the minimum gap, so a scene cut cannot always be kept: the
		// bounds win.
		o := providers.FrameOptions{MinFPS: 0.6, MaxFPS: 1, SceneDetection: true}
		frames := r.check(t, o)
		for i := 1; i < len(frames); i++ {
			gap := frames[i].TimestampMS - frames[i-1].TimestampMS
			if gap < o.MinGapMS() || gap > o.MaxGapMS() {
				t.Errorf("frames %d-%d are %dms apart, outside [%d,%d]", i-1, i, gap, o.MinGapMS(), o.MaxGapMS())
			}
		}
	})

	t.Run("honours the frame cap", func(t *testing.T) {
		frames := r.check(t, providers.FrameOptions{MaxFPS: 0.5, MaxFrames: 2})
		if len(frames) != 2 {
			t.Errorf("got %d frames, want 2", len(frames))
		}
	})

	t.Run("rate only, without scene detection", func(t *testing.T) {
		o := providers.FrameOptions{MaxFPS: 0.05}
		frames := r.check(t, o)
		for i := 1; i < len(frames); i++ {
			if gap := frames[i].TimestampMS - frames[i-1].TimestampMS; gap != o.MinGapMS() {
				t.Errorf("frames %d-%d are %dms apart, want even %dms", i-1, i, gap, o.MinGapMS())
			}
		}
	})
}

// sceneCuts: scene detection selects the fixture's cuts.
func (r framesRun) sceneCuts(t *testing.T) {
	t.Run("scene detection selects cuts", func(t *testing.T) {
		if len(r.f.SceneCutsMS) == 0 {
			t.Skip("backend fixture has no scene cuts")
		}
		frames := r.check(t, providers.FrameOptions{MaxFPS: 1, SceneDetection: true})
		at := map[int64]bool{}
		for _, fr := range frames {
			at[fr.TimestampMS] = true
		}
		for _, cut := range r.f.SceneCutsMS {
			if !at[cut] {
				t.Errorf("no frame at scene cut %dms", cut)
			}
		}
	})
}
