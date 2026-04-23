//go:build c12n

package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"hop.top/c12n"
)

func init() {
	classifyFunc = classifyWithC12n
}

func classifyWithC12n(text string) (*classifyResult, error) {
	pipe, err := c12n.NewPipeline(c12n.PipelineConfig{
		MaxConcurrency: 4,
		Timeout:        30 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("init pipeline: %w", err)
	}
	defer pipe.Close()

	ctx := c12n.ClassificationContext{Text: text}
	raw, err := pipe.Evaluate(ctx)
	if err != nil {
		return nil, fmt.Errorf("evaluate: %w", err)
	}

	pr, err := c12n.ParseResult(raw)
	if err != nil {
		return nil, fmt.Errorf("parse result: %w", err)
	}

	result := &classifyResult{}
	for _, s := range pr.Results {
		result.Results = append(result.Results, classifySignal{
			Name:       s.Name,
			Type:       string(s.Type),
			Confidence: s.Confidence,
			Labels:     s.Labels,
		})
	}
	for _, e := range pr.Errors {
		b, _ := json.Marshal(e)
		result.Errors = append(result.Errors, b)
	}
	return result, nil
}
