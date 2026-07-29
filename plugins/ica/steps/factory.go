package steps

import (
	"net/http"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	ica "github.com/ideacrafterslabs/ctxt/plugins/ica"
)

// Factory returns a StepFactory wired to the concrete step
// constructors in this package. Call ica.NewWithSteps(Factory())
// to get a fully-wired plugin.
func Factory() ica.StepFactory {
	return ica.StepFactory{
		NewFeedManager: func(
			apiURL string, client *http.Client,
		) pluginapi.PipelineStep {
			return NewICAFeedManager(apiURL, client)
		},
		NewFetcher: func(
			apiURL string, client *http.Client,
		) pluginapi.PipelineStep {
			return NewICAFetcher(apiURL, client)
		},
		NewNormalizer: func(
			distChannelsAsMentions bool,
		) pluginapi.PipelineStep {
			return NewICANormalizer(distChannelsAsMentions)
		},
		NewProcessor: func(
			processorURL string, client *http.Client,
		) pluginapi.PipelineStep {
			return NewICAProcessor(processorURL, client)
		},
	}
}
