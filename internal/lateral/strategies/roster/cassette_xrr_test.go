package roster

// Migration target for T-0334. When xrr lands, each platform package
// referenced by roster (google, arxiv, wikipedia, medium, substack,
// beehiiv, youtube) gets its own loadCassette helper that swaps the
// stub *Client with an xrr.NewHTTPClient-backed real client.
//
// See docs/lateral/xrr-cassette-migration.md for the migration plan.

// loadCassette is the per-platform migration seam. Returns nil today
// — the platform-specific helper will assemble cassette path +
// xrr client + adapter wrapper.
func loadCassette(_ string) any {
	return nil
}

// Reference call: keeps the migration seam in the build graph until
// the xrr-enabled PR replaces it.
var _ = loadCassette
