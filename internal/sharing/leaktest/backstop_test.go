package leaktest

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// Backstop precision (ADR-080 "Testing"): a restricted viewer's
// whoami, its refused audience and policy listings and its error
// bodies come back intact; a projected-out value under the rune floor
// (the client's city, "Lee") blanks nothing; and none of it makes the
// backstop fire.
func TestBackstopPrecision(t *testing.T) {
	f := Start(t, storageutil.NewTestDriver(t))
	before := sharing.BackstopHits()
	colleague, agent := f.Colleague(), f.Principal(NameAgent)

	whoami := fetch(t, f, colleague, "/whoami")
	require.Equal(t, http.StatusOK, whoami.Status, "%s", whoami.Body)
	var me struct {
		Principal string `json:"principal"`
		Shared    struct {
			Owner    string   `json:"owner"`
			Policies []string `json:"policies"`
		} `json:"shared"`
	}
	require.NoError(t, json.Unmarshal(whoami.Body, &me))
	assert.Equal(t, ColleagueID, me.Principal)
	assert.Equal(t, []string{sharing.TemplateColleagues}, me.Shared.Policies)

	for _, path := range []string{"/share/audiences", "/share/policies"} {
		r := fetch(t, f, colleague, path)
		assert.Equal(t, http.StatusForbidden, r.Status, path)
		var env httpserver.ErrorEnvelope
		require.NoError(t, json.Unmarshal(r.Body, &env), "%s: %s", path, r.Body)
		assert.Equal(t, httpserver.CodeInsufficientScope, env.Error.Code, path)
		assert.NotEmpty(t, env.Error.Message)
	}

	notFound := fetch(t, f, colleague, "/objects/no-such-object")
	assert.Equal(t, http.StatusNotFound, notFound.Status)
	assert.JSONEq(t, `{"error":{"code":"NOT_FOUND","message":"object not found","details":{}}}`, string(notFound.Body))
	job := fetch(t, f, colleague, "/jobs/"+f.JobID)
	assert.Equal(t, http.StatusForbidden, job.Status)
	assert.Contains(t, string(job.Body), httpserver.CodeNotShared)

	// The agent projects title and org only; the client's city "Lee"
	// is withheld, under the rune floor, and Sarah Lee keeps her name.
	list := fetch(t, f, agent, "/objects?limit=100")
	require.Equal(t, http.StatusOK, list.Status, "%s", list.Body)
	assert.Contains(t, string(list.Body), "Sarah Lee @ Acme")
	assert.Contains(t, string(list.Body), "Chris Day @ Globex")
	assert.NotContains(t, string(list.Body), SarahEmail)
	assert.NotContains(t, string(list.Body), "Head of Ops")

	assert.Equal(t, before, sharing.BackstopHits(), "the backstop fired on a clean request")
}

// The whole read surface, as every restricted principal, never makes
// the backstop fire: every hit is a leak the scoped reads or the
// render layer let through, and a strict test instance refuses it.
func TestBackstopNeverFiresOnTheSuite(t *testing.T) {
	f := Start(t, storageutil.NewTestDriver(t))
	before := sharing.BackstopHits()
	for _, p := range f.Restricted() {
		for _, c := range Registry() {
			Run(t, f, p, c)
		}
	}
	assert.Equal(t, before, sharing.BackstopHits())
}
