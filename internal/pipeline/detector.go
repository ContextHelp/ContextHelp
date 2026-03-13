package pipeline

import "errors"

// DetectInput carries the information available when selecting a pipeline.
// Detectors may inspect Source (file path or URL), ContentType (MIME), and
// a short content sniff (first ~512 bytes of RawContent).
type DetectInput struct {
	Source      string // file path, URL, or empty
	ContentType string // MIME type if already known, or empty
	Sniff       string // first ~512 bytes of raw content
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
