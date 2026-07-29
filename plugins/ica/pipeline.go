package ica

// PipelineDef describes a plugin-contributed pipeline definition.
type PipelineDef struct {
	Name        string
	Description string
	Steps       []string
	Providers   []string
}

// PipelineDefs returns the pipeline definitions contributed by
// this plugin.
func (p *Plugin) PipelineDefs() []PipelineDef {
	return []PipelineDef{
		{
			Name:        "ica.feed_sync",
			Description: "ICA-backed feed sync pipeline",
			Steps: []string{
				"ica_feed_manager",
				"ica_fetcher",
				"ica_normalizer",
				"item_deduplicator",
				"ica_processor",
				"alternative_detector",
				"item_enqueuer",
			},
			Providers: []string{
				"ica_api",
				"ica_processor",
			},
		},
	}
}
