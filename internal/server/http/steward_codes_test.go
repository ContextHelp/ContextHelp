package http

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
)

// Every refused elevation answers the status the gRPC side answers its
// counterpart of (sharingtest.StewardRefusals), with the stable code
// STEWARD_<REASON>, and never shows the cause.
func TestStewardRefusalStatusesAgreeWithGRPC(t *testing.T) {
	for _, c := range sharingtest.StewardRefusals() {
		c.Refusal.Err = errors.New("dial tcp 10.0.0.7:5432: secret cause")
		rec := httptest.NewRecorder()
		writeStewardRefusal(rec, &authn.Principal{ID: "prn_x"}, c.Refusal)
		assert.Equal(t, c.Status, rec.Code, c.Refusal.Reason)
		assert.Equal(t, c.Refusal.Code(), errorCode(t, rec.Body.Bytes()), c.Refusal.Reason)
		assert.NotContains(t, rec.Body.String(), "10.0.0.7", c.Refusal.Reason)
	}
}
