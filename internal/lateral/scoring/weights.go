package scoring

// Weights are the blended-scoring weights for the three active-context signals.
// Defaults are the v1 stake-in-the-ground from the spec; usage data drives
// per-source-class overrides via ctxt lateral suggest-weights.
type Weights struct {
	SessionTopic     float64
	CaptureWindow    float64
	InterestRegistry float64
}

// DefaultWeights returns the v1 default weight set: session 0.5, window 0.3,
// interest 0.2.
func DefaultWeights() Weights {
	return Weights{
		SessionTopic:     0.5,
		CaptureWindow:    0.3,
		InterestRegistry: 0.2,
	}
}
