package config

import (
	"fmt"
	"math"
)

// validateFramesRole checks a providers.frames block (or a per-pipeline
// override of it) at field prefix base.
func validateFramesRole(c ProviderBackendConfig, base string) []ValidationError {
	var errs []ValidationError
	add := func(field, msg string) {
		errs = append(errs, ValidationError{Field: base + "." + field, Message: msg})
	}
	errs = append(errs, validateProviderBackend(base+".backend", "frames", c.Backend)...)
	fps := func(field string, v float64) bool {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > FrameMaxFPS {
			add(field, fmt.Sprintf("must be 0 (unset) or in (0, %d], got %v", FrameMaxFPS, v))
			return false
		}
		return true
	}
	okMin, okMax := fps("min_fps", c.MinFPS), fps("max_fps", c.MaxFPS)
	if okMin && okMax && c.MinFPS > 0 && c.MaxFPS > 0 {
		minGap, maxGap := FrameGapsMS(c.MinFPS, c.MaxFPS)
		if minGap > maxGap {
			add("min_fps", fmt.Sprintf("min_fps %v and max_fps %v leave no valid spacing: the minimum gap (%dms) exceeds the maximum gap (%dms)", c.MinFPS, c.MaxFPS, minGap, maxGap))
		}
	}
	if math.IsNaN(c.SceneThreshold) || c.SceneThreshold < 0 || c.SceneThreshold > 1 {
		add("scene_threshold", fmt.Sprintf("must be between 0 and 1, got %v", c.SceneThreshold))
	}
	if c.MaxFrames < 0 || c.MaxFrames > FrameMaxFramesLimit {
		add("max_frames", fmt.Sprintf("must be 0 (default %d) to %d, got %d", DefaultFrameMaxFrames, FrameMaxFramesLimit, c.MaxFrames))
	}
	if c.MaxDimension < FrameNoScaling || c.MaxDimension > FrameMaxDimensionLimit {
		add("max_dimension", fmt.Sprintf("must be %d (no scaling), 0 (default %d) or up to %d, got %d", FrameNoScaling, DefaultFrameMaxDimension, FrameMaxDimensionLimit, c.MaxDimension))
	}
	return errs
}

// validateSummarizationRole checks a providers.summarization block (or a
// per-pipeline override of it) at field prefix base.
func validateSummarizationRole(c ProviderBackendConfig, base string) []ValidationError {
	var errs []ValidationError
	errs = append(errs, validateProviderBackend(base+".backend", "summarization", c.Backend)...)
	if c.PromptTemplate != "" {
		if _, err := ParseSummaryTemplate(c.PromptTemplate); err != nil {
			errs = append(errs, ValidationError{Field: base + ".prompt_template", Message: err.Error()})
		}
	}
	if c.NumCtx < 0 {
		errs = append(errs, ValidationError{Field: base + ".num_ctx", Message: fmt.Sprintf("must not be negative, got %d", c.NumCtx)})
	}
	return errs
}
