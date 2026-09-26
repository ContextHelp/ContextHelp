package pipeline

import (
	"path/filepath"
	"regexp"
	"strings"
)

// ExtensionDetector maps file extensions (e.g. ".pdf") to pipeline names.
type ExtensionDetector struct {
	m map[string]string // ".ext" -> "pipeline.name", all keys lowercased
}

// NewExtensionDetector creates an ExtensionDetector from the given map.
// Map keys should include the leading dot (e.g. ".pdf").
func NewExtensionDetector(m map[string]string) *ExtensionDetector {
	lower := make(map[string]string, len(m))
	for k, v := range m {
		lower[strings.ToLower(k)] = v
	}
	return &ExtensionDetector{m: lower}
}

// Detect implements Detector.
func (d *ExtensionDetector) Detect(in DetectInput) (string, error) {
	if in.Source == "" {
		return "", ErrDelegate
	}
	ext := strings.ToLower(filepath.Ext(in.Source))
	if name, ok := d.m[ext]; ok {
		return name, nil
	}
	return "", ErrDelegate
}

// ContentTestDetector applies a predicate to in.Content to select a pipeline.
type ContentTestDetector struct {
	PipelineName string
	Test         func(content string) bool
}

// NewContentTestDetector creates a ContentTestDetector.
func NewContentTestDetector(name string, test func(string) bool) *ContentTestDetector {
	return &ContentTestDetector{PipelineName: name, Test: test}
}

// Detect implements Detector.
func (d *ContentTestDetector) Detect(in DetectInput) (string, error) {
	if d.Test(in.Content) {
		return d.PipelineName, nil
	}
	return "", ErrDelegate
}

// URLPatternDetector matches URLs via regexp on in.Source.
type URLPatternDetector struct {
	PipelineName string
	Pattern      *regexp.Regexp
}

// NewURLPatternDetector creates a URLPatternDetector.
func NewURLPatternDetector(name string, pattern *regexp.Regexp) *URLPatternDetector {
	return &URLPatternDetector{PipelineName: name, Pattern: pattern}
}

// Detect implements Detector.
func (d *URLPatternDetector) Detect(in DetectInput) (string, error) {
	if !strings.HasPrefix(in.Source, "http://") && !strings.HasPrefix(in.Source, "https://") {
		return "", ErrDelegate
	}
	if d.Pattern.MatchString(in.Source) {
		return d.PipelineName, nil
	}
	return "", ErrDelegate
}
