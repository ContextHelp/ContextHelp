package embeddings

import "github.com/spf13/pflag"

// Per-invocation flag names (the flag layer). They are local flags on each
// embedding-consuming command, never persistent globals.
const (
	FlagProvider = "embedding-provider"
	FlagModel    = "embedding-model"
	FlagEndpoint = "embedding-endpoint"
)

// AddFlags registers the flag layer on fs, bound to o.
func AddFlags(fs *pflag.FlagSet, o *Overrides) {
	fs.StringVar(&o.Backend, FlagProvider, "",
		"embedding backend for this run (ollama, stub); overrides env and config")
	fs.StringVar(&o.Model, FlagModel, "",
		"embedding model for this run; overrides env and config")
	fs.StringVar(&o.Endpoint, FlagEndpoint, "",
		"embedding endpoint URL for this run (e.g. http://127.0.0.1:11555 for a tunnel); overrides env and config")
}
