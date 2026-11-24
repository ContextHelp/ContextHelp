package config

import "math"

// Limits and defaults of the frames role. providers.frames is validated
// here at load time and again by the providers package at run time; both
// read these values.
const (
	// DefaultFrameMaxFrames caps a frame result when max_frames is unset.
	DefaultFrameMaxFrames = 200
	// DefaultFrameMaxDimension scales frames down to this many pixels on
	// the longest side when max_dimension is unset.
	DefaultFrameMaxDimension = 1280
	// FrameMaxFPS is the highest frame rate min_fps and max_fps accept.
	FrameMaxFPS = 1000
	// FrameMaxFramesLimit is the highest max_frames.
	FrameMaxFramesLimit = 10_000
	// FrameMaxDimensionLimit is the highest max_dimension.
	FrameMaxDimensionLimit = 16_384
	// FrameNoScaling, as max_dimension, turns scaling off.
	FrameNoScaling = -1
)

// FrameGapsMS converts frame-rate bounds to spacing, in milliseconds: the
// smallest gap allowed between frames (from maxFPS, rounded up) and the
// largest (from minFPS, rounded down). 0 means the bound is unset.
func FrameGapsMS(minFPS, maxFPS float64) (minGap, maxGap int64) {
	if maxFPS > 0 {
		minGap = int64(math.Ceil(1000 / maxFPS))
	}
	if minFPS > 0 {
		maxGap = int64(math.Floor(1000 / minFPS))
	}
	return minGap, maxGap
}
