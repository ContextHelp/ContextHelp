package pipeline

import "errors"

// DetectInput carries the information available when selecting a pipeline.
// Source is where the content came from: a URL, a file path, or a capture
// label ("argument", "stdin", ...) that names no location. Content is the
// captured text itself. Extension and URL rules read Source only; length and
// structure rules read Content only.
type DetectInput struct {
	Source      string // URL, file path, capture label, or empty
	ContentType string // MIME type if already known, or empty
	Content     string // raw content
}

// ErrDelegate is a sentinel error that a Detector returns when it cannot
// make a determination and wants the registry to try the next detector.
var ErrDelegate = errors.New("detector: delegate to next")

// Detector selects a pipeline name for the given input.
// Returning ("", ErrDelegate) passes control to the next registered detector.
// Returning ("", someOtherError) is a hard failure.
// Returning (name, nil) selects that pipeline.
type Detector interface {
	Detect(in DetectInput) (pipelineName string, err error)
}

// DetectorFunc is a function adapter that implements Detector.
type DetectorFunc func(DetectInput) (string, error)

// Detect implements Detector.
func (f DetectorFunc) Detect(in DetectInput) (string, error) { return f(in) }
