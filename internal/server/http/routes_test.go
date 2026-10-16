package http

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// APIRouteSpecs mirrors the table NewRouterWithConfig mounts: the same
// entries in the same order, handlers aside. It builds the table with
// no service, so a handler constructor that dereferences its service
// at construction time fails here, not as a short table in the leak
// suite.
func TestAPIRouteSpecsMirrorTable(t *testing.T) {
	table := apiRoutes(newScopeTestService(t), RouterConfig{})
	specs := APIRouteSpecs()
	require.Len(t, specs, len(table))
	for i, rt := range table {
		assert.Equal(t, RouteSpec{Method: rt.Method, Pattern: rt.Pattern, Scope: rt.Scope}, specs[i])
	}
}
