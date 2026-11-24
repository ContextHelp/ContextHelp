package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// MediaSource is where a provider reads media from. It is the seam to the
// media accessor: a source opens the original's bytes and knows nothing
// about where they live (local file, blob store, remote). Providers that
// need random access spool the stream themselves.
type MediaSource interface {
	// Open returns a reader over the media bytes. The caller closes it.
	Open(ctx context.Context) (io.ReadCloser, error)
}

// LocalFileSource is implemented by sources that already sit on the local
// filesystem, so a backend can read them in place instead of copying.
type LocalFileSource interface {
	MediaSource
	// LocalPath returns the file's path.
	LocalPath() string
}

// FileSource is a MediaSource over a local file.
type FileSource string

// Open opens the file.
func (s FileSource) Open(ctx context.Context) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return os.Open(string(s))
}

// LocalPath returns the file's path.
func (s FileSource) LocalPath() string { return string(s) }

// Frame is one selected video frame.
type Frame struct {
	// Index is the frame's position in the result, from 0.
	Index int
	// TimestampMS is the frame's time in the video, in milliseconds.
	TimestampMS int64
	// ContentType is the image's media type (e.g. image/png).
	ContentType string
	// Data holds the image bytes.
	Data []byte
}

// Limits on frame selection. They bound memory and work whatever the
// video claims about itself. The configurable ones are shared with config
// validation.
const (
	// DefaultMaxFrames caps a result when FrameOptions.MaxFrames is unset.
	DefaultMaxFrames = config.DefaultFrameMaxFrames
	// DefaultMaxDimension is the longest side frames are scaled down to
	// when FrameOptions.MaxDimension is unset in DefaultFrameOptions.
	DefaultMaxDimension = config.DefaultFrameMaxDimension

	maxFramesLimit    = config.FrameMaxFramesLimit
	maxDimensionLimit = config.FrameMaxDimensionLimit
	maxFPSLimit       = config.FrameMaxFPS
	// maxVideoDurationMS only guards against overflow and non-finite
	// headers (about 31 years); real recordings of any length are
	// sampled across their full length, bounded by MaxFrames.
	maxVideoDurationMS = 1_000_000_000_000
	// planCandidateCap bounds the timestamps (and scene cuts) considered
	// before MaxFrames thins them.
	planCandidateCap = 100_000
	// maxSpoolBytes bounds a spooled non-local media source.
	maxSpoolBytes = 8 << 30
)

// FrameOptions says which frames a FrameProvider selects.
//
// Scene detection picks frames at visual cuts. The rate bounds then shape
// the selection: no two frames are closer than 1/MaxFPS seconds, and no
// two consecutive frames are further apart than 1/MinFPS seconds (gaps
// are filled with evenly spaced frames). Without scene detection, frames
// are evenly spaced at MinFPS, or at MaxFPS when MinFPS is unset.
//
// MaxFrames wins over MinFPS: a result that would exceed it is thinned to
// at most one frame per 1/MaxFrames of the video's length (a scene cut
// wins its slot over a filler frame), so very long videos stay bounded.
type FrameOptions struct {
	// MinFPS is the minimum frame rate; 0 means no minimum.
	MinFPS float64
	// MaxFPS is the maximum frame rate; 0 means no maximum.
	MaxFPS float64
	// SceneDetection selects frames at scene changes.
	SceneDetection bool
	// SceneThreshold is how large a change, from 0 to 1, counts as a scene
	// change; 0 means the backend's default. The scale is relative to the
	// backend's own change score: the same value can select different
	// frames on different backends.
	SceneThreshold float64
	// MaxFrames caps the number of frames; 0 means DefaultMaxFrames.
	MaxFrames int
	// MaxDimension scales frames down so the longest side is at most this
	// many pixels. A backend does not scale for 0 or less; WithDefaults
	// turns 0 into DefaultMaxDimension, and -1 (config.FrameNoScaling)
	// is the explicit "no scaling".
	MaxDimension int
}

// DefaultFrameOptions are the options used when nothing is configured.
func DefaultFrameOptions() FrameOptions {
	return FrameOptions{MinFPS: 0.1, MaxFPS: 1, SceneDetection: true, MaxFrames: DefaultMaxFrames, MaxDimension: DefaultMaxDimension}
}

// WithDefaults fills each unset bound from DefaultFrameOptions, unless
// the default would contradict a bound that is set (a default minimum
// at or above the set maximum, or a default maximum at or below the set
// minimum, stays unset). Scene detection is left as given.
func (o FrameOptions) WithDefaults() FrameOptions {
	d := DefaultFrameOptions()
	if o.MinFPS == 0 && (o.MaxFPS == 0 || o.MaxFPS > d.MinFPS) {
		o.MinFPS = d.MinFPS
	}
	if o.MaxFPS == 0 && (o.MinFPS == 0 || o.MinFPS < d.MaxFPS) {
		o.MaxFPS = d.MaxFPS
	}
	if o.MaxFrames == 0 {
		o.MaxFrames = d.MaxFrames
	}
	if o.MaxDimension == 0 {
		o.MaxDimension = d.MaxDimension
	}
	return o
}

// EffectiveMaxFrames is MaxFrames, or DefaultMaxFrames when unset.
func (o FrameOptions) EffectiveMaxFrames() int {
	if o.MaxFrames == 0 {
		return DefaultMaxFrames
	}
	return o.MaxFrames
}

// ErrUnusableMedia marks media no retry can fix: a path that is not
// absolute, or a header with an unusable duration. Callers treat it as
// permanent.
var ErrUnusableMedia = errors.New("unusable media")

// ErrInvalidFrameOptions marks FrameOptions no backend can honour.
var ErrInvalidFrameOptions = errors.New("invalid frame options")

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// Validate reports why o cannot select frames.
func (o FrameOptions) Validate() error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidFrameOptions, fmt.Sprintf(format, a...))
	}
	for name, v := range map[string]float64{"min_fps": o.MinFPS, "max_fps": o.MaxFPS} {
		if !finite(v) || v < 0 || v > maxFPSLimit {
			return bad("%s %g must be 0 (unset) or in (0, %d]", name, v, maxFPSLimit)
		}
	}
	switch {
	case o.MinFPS > 0 && o.MaxFPS > 0 && o.MinGapMS() > o.MaxGapMS():
		return bad("min_fps %g and max_fps %g leave no valid spacing (%dms minimum gap exceeds %dms maximum gap)", o.MinFPS, o.MaxFPS, o.MinGapMS(), o.MaxGapMS())
	case !finite(o.SceneThreshold) || o.SceneThreshold < 0 || o.SceneThreshold > 1:
		return bad("scene threshold %g outside 0..1", o.SceneThreshold)
	case o.MaxFrames < 0 || o.MaxFrames > maxFramesLimit:
		return bad("max_frames %d outside 0..%d", o.MaxFrames, maxFramesLimit)
	case o.MaxDimension < config.FrameNoScaling || o.MaxDimension > maxDimensionLimit:
		return bad("max_dimension %d outside %d..%d", o.MaxDimension, config.FrameNoScaling, maxDimensionLimit)
	case o.MinFPS == 0 && o.MaxFPS == 0 && !o.SceneDetection:
		return bad("nothing selects frames: set a frame rate or enable scene detection")
	}
	return nil
}

// MinGapMS is the smallest spacing between frames the options allow, in
// milliseconds; 0 when MaxFPS is unset.
func (o FrameOptions) MinGapMS() int64 {
	minGap, _ := config.FrameGapsMS(o.MinFPS, o.MaxFPS)
	return minGap
}

// MaxGapMS is the largest spacing between consecutive frames the options
// allow, in milliseconds; 0 when MinFPS is unset.
func (o FrameOptions) MaxGapMS() int64 {
	_, maxGap := config.FrameGapsMS(o.MinFPS, o.MaxFPS)
	return maxGap
}

// FrameYield receives each frame as it is produced. Returning an error
// stops extraction and becomes ExtractFrames' error. A backend does not
// keep a frame after yielding it.
type FrameYield func(Frame) error

// FrameProvider selects and extracts keyframes from a video (the frames
// role). Backends are interchangeable: nothing in the interface depends on
// how a backend finds or decodes frames.
type FrameProvider interface {
	Name() string
	// ExtractFrames yields the selected frames in time order, with Index
	// running from 0 and TimestampMS strictly increasing, at most
	// opts.EffectiveMaxFrames of them. A video without a video stream
	// yields none. It fails with ErrInvalidFrameOptions when opts fails
	// Validate, and stops at the first error yield returns.
	ExtractFrames(ctx context.Context, src MediaSource, opts FrameOptions, yield FrameYield) error
}

// ErrInvalidFrame marks a frame a backend yielded that breaks the
// FrameProvider contract.
var ErrInvalidFrame = errors.New("invalid frame from provider")

// StreamFrames runs p and checks every frame it yields against the
// contract (Index from 0, strictly increasing timestamps, image bytes and
// content type present, at most MaxFrames) before passing it to yield, so
// any backend, however it is built, is checked in production.
func StreamFrames(ctx context.Context, p FrameProvider, src MediaSource, opts FrameOptions, yield FrameYield) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	var n int
	last := int64(-1)
	maxFrames := opts.EffectiveMaxFrames()
	return p.ExtractFrames(ctx, src, opts, func(f Frame) error {
		switch {
		case f.Index != n:
			return fmt.Errorf("%w: %s frame %d has Index %d", ErrInvalidFrame, p.Name(), n, f.Index)
		case f.TimestampMS < 0 || f.TimestampMS <= last:
			return fmt.Errorf("%w: %s frame %d at %dms is not after %dms", ErrInvalidFrame, p.Name(), n, f.TimestampMS, last)
		case len(f.Data) == 0:
			return fmt.Errorf("%w: %s frame %d has no image bytes", ErrInvalidFrame, p.Name(), n)
		case f.ContentType == "":
			return fmt.Errorf("%w: %s frame %d has no content type", ErrInvalidFrame, p.Name(), n)
		case n >= maxFrames:
			return fmt.Errorf("%w: %s yielded more than %d frames", ErrInvalidFrame, p.Name(), maxFrames)
		}
		n++
		last = f.TimestampMS
		return yield(f)
	})
}

// CollectFrames returns every frame StreamFrames yields. It holds them
// all in memory: for tests and short results, not long videos.
func CollectFrames(ctx context.Context, p FrameProvider, src MediaSource, opts FrameOptions) ([]Frame, error) {
	var frames []Frame
	err := StreamFrames(ctx, p, src, opts, func(f Frame) error {
		frames = append(frames, f)
		return nil
	})
	return frames, err
}

// planFrameTimes returns the timestamps, in milliseconds, of the frames to
// extract from a video of durationMS, given the scene cuts found in it
// (sorted or not). It is the one place the rate bounds, scene cuts and
// MaxFrames are combined, so every backend that can list cuts selects the
// same frames.
func planFrameTimes(durationMS int64, cutsMS []int64, o FrameOptions) []int64 {
	if durationMS <= 0 {
		return nil
	}
	minGap, maxGap := o.MinGapMS(), o.MaxGapMS()
	if !o.SceneDetection {
		cutsMS = nil
		if maxGap == 0 {
			maxGap = minGap // evenly spaced at MaxFPS
		}
	}
	// Coarsen fillers so a long video cannot make an unbounded plan.
	if floor := (durationMS + planCandidateCap - 1) / planCandidateCap; maxGap > 0 && maxGap < floor {
		maxGap = floor
	}
	cuts := append([]int64(nil), cutsMS...)
	sort.Slice(cuts, func(i, j int) bool { return cuts[i] < cuts[j] })
	cuts = decimate(cuts, planCandidateCap)

	times := []int64{0}
	isCut := []bool{false}
	add := func(t int64, cut bool) {
		times = append(times, t)
		isCut = append(isCut, cut)
	}
	last := int64(0)
	// fill adds evenly spaced frames until the next frame, at until, is
	// within maxGap. When a cut follows (cutFollows), the last filler is
	// pulled back so the cut stays at least minGap after it. When that is
	// not possible (the window between the bounds is too narrow), the
	// filler stays where maxGap puts it and the cut is the one dropped,
	// so no gap ever exceeds maxGap.
	fill := func(until int64, cutFollows bool) {
		for maxGap > 0 && until-last > maxGap {
			next := last + maxGap
			if cutFollows && until-next < minGap {
				if pulled := until - minGap; pulled-last >= minGap {
					next = pulled
				}
			}
			last = next
			add(last, false)
		}
	}
	for _, c := range cuts {
		if c <= last || c >= durationMS || c-last < minGap {
			continue
		}
		fill(c, true)
		if c-last < minGap {
			continue // a filler landed too close: the cut gives way
		}
		add(c, true)
		last = c
	}
	fill(durationMS-1, false)
	return thin(times, isCut, durationMS, o.EffectiveMaxFrames())
}

// decimate returns n evenly spaced entries of v, or v when it has no more
// than n.
func decimate(v []int64, n int) []int64 {
	if len(v) <= n {
		return v
	}
	out := make([]int64, n)
	for i := range out {
		out[i] = v[i*(len(v)-1)/(n-1)]
	}
	return out
}

// thin reduces times to at most n by time buckets: one frame per
// durationMS/n of the video, preferring a scene cut over a filler within a
// bucket. The first bucket keeps the frame at 0. times itself is returned
// when it has no more than n entries.
func thin(times []int64, isCut []bool, durationMS int64, n int) []int64 {
	if len(times) <= n {
		return times
	}
	if n <= 1 {
		return times[:1]
	}
	width := (durationMS + int64(n) - 1) / int64(n)
	out := make([]int64, 0, n)
	picked, pickedCut := int64(-1), false
	bucket := int64(-1)
	flush := func() {
		if bucket >= 0 {
			out = append(out, picked)
		}
	}
	for i, t := range times {
		b := t / width
		switch {
		case b != bucket:
			flush()
			bucket, picked, pickedCut = b, t, isCut[i]
		case isCut[i] && !pickedCut && bucket != 0:
			picked, pickedCut = t, true
		}
	}
	flush()
	return out
}

// errRelativeMediaPath marks a local media path ffmpeg could read as an
// option, a protocol or a URL.
var errRelativeMediaPath = errors.New("media path must be absolute")

// requireAbsMediaPath rejects a local media path that is not absolute: a
// relative one could be read by ffmpeg as an option or a protocol.
func requireAbsMediaPath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: %w: %q", ErrUnusableMedia, errRelativeMediaPath, path)
	}
	return nil
}

// spoolToFile copies src into a temp file and returns its path and a
// cleanup func, for tools that need a seekable file. A LocalFileSource is
// used in place and must be an absolute path.
func spoolToFile(ctx context.Context, src MediaSource) (string, func(), error) {
	if lf, ok := src.(LocalFileSource); ok {
		if err := requireAbsMediaPath(lf.LocalPath()); err != nil {
			return "", nil, err
		}
		return lf.LocalPath(), func() {}, nil
	}
	r, err := src.Open(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("open media: %w", err)
	}
	defer r.Close()
	dir, err := os.MkdirTemp("", "ctxt-media-*")
	if err != nil {
		return "", nil, fmt.Errorf("spool media: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	path := filepath.Join(dir, "media")
	f, err := os.Create(path) // #nosec G304 -- path is inside a fresh temp dir
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("spool media: %w", err)
	}
	n, err := io.Copy(f, io.LimitReader(&ctxReader{ctx: ctx, r: r}, maxSpoolBytes+1))
	if err == nil && n > maxSpoolBytes {
		err = fmt.Errorf("media larger than %d bytes", int64(maxSpoolBytes))
	}
	if err != nil {
		_ = f.Close()
		cleanup()
		return "", nil, fmt.Errorf("spool media: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("spool media: %w", err)
	}
	return path, cleanup, nil
}

// ctxReader stops reading once ctx is done.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// StubFrameProvider returns placeholder frames for testing: a 2m30s video
// without scene changes, so frames follow the rate bounds alone.
type StubFrameProvider struct{}

func NewStubFrameProvider() *StubFrameProvider { return &StubFrameProvider{} }

func (p *StubFrameProvider) Name() string { return backendStub }

// stubFrameDurationMS is the video length the stub pretends to have.
const stubFrameDurationMS = 150_000

// stubFramePNG is a 1x1 PNG.
var stubFramePNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0xf8, 0xff, 0xff, 0x3f,
	0x00, 0x05, 0xfe, 0x02, 0xfe, 0xa7, 0x35, 0x81, 0x84, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
	0x44, 0xae, 0x42, 0x60, 0x82,
}

func (p *StubFrameProvider) ExtractFrames(ctx context.Context, src MediaSource, opts FrameOptions, yield FrameYield) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	if src == nil {
		return errNilMediaSource
	}
	return emitFrames(ctx, planFrameTimes(stubFrameDurationMS, nil, opts), func(int64) ([]byte, error) {
		return stubFramePNG, nil
	}, "image/png", yield)
}

var errNilMediaSource = errors.New("frames: nil media source")

// emitFrames yields a frame for each planned timestamp, fetching each
// image with get. An empty image (a timestamp past the last decodable
// frame) is dropped, so a plan that overshoots ends in fewer frames, not
// an error. Index counts the frames yielded.
func emitFrames(ctx context.Context, times []int64, get func(ms int64) ([]byte, error), contentType string, yield FrameYield) error {
	n := 0
	for _, ms := range times {
		if err := ctx.Err(); err != nil {
			return err
		}
		data, err := get(ms)
		if err != nil {
			return fmt.Errorf("frame at %dms: %w", ms, err)
		}
		if len(data) == 0 {
			continue
		}
		if err := yield(Frame{Index: n, TimestampMS: ms, ContentType: contentType, Data: data}); err != nil {
			return err
		}
		n++
	}
	return nil
}
