package http

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// The stats entity count never discloses more than GET /entities lists:
// a gated principal counts the entities of its granted namespaces only;
// the admin owner counts every entity.
func TestStatsEntityCountFollowsGate(t *testing.T) {
	ts, _, _, _ := newGatedServerWithDriver(t, storageutil.NewTestDriver(t))

	for token, want := range map[string]int{"tok-valid": 1, "tok-admin": 2} {
		resp, body := gatedDoAs(t, token, http.MethodGet, ts.URL+"/api/v1/stats")
		require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
		var got struct {
			Entities int `json:"entities"`
		}
		require.NoError(t, json.Unmarshal(body, &got))
		assert.Equal(t, want, got.Entities, token)
	}
}
