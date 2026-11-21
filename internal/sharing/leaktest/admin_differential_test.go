package leaktest

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// Differential: an administrator's answer to every case is the same
// byte for byte whether or not the corpus exists. The second instance
// seeds nothing the administrator could see (nothing at all), so any
// difference is a leak through a count, an order, a score or a word.
func TestAdminDifferentialHiddenRowsChangeNothing(t *testing.T) {
	adminDifferentialHiddenRowsChangeNothing(t, storageutil.NewTestDriver)
}

func adminDifferentialHiddenRowsChangeNothing(t *testing.T, newDriver driverFunc) {
	with := Start(t, newDriver(t))
	for _, p := range with.Admins() {
		without := Start(t, newDriver(t), WithoutHiddenFor(p.Name))
		q := without.Admin(p.Name)
		require.Empty(t, without.VisibleObjects(q))
		for _, c := range Registry() {
			if !p.Reaches(c) {
				continue
			}
			t.Run(p.Name+"/"+c.Route, func(t *testing.T) {
				a := Run(t, with, p, c)
				b := Run(t, without, q, c)
				assert.Equal(t, a.Status, b.Status, "status")
				assert.Equal(t, a.Code, b.Code, "code")
				assert.Equal(t, normalizeAdmin(c.Route, b.Body), normalizeAdmin(c.Route, a.Body), "body with the corpus differs from without it")
			})
		}
	}
}

// registryRoutes are the routes that list the registry (principals,
// audiences, policies, grants, the caller's own credentials): rows
// created in the same millisecond list in no fixed order, and the
// order of a configuration list says nothing about rows. Knowledge
// listings keep their order: it is a ranking, and a ranking can leak.
func registryRoute(route string) bool {
	_, pattern, _ := strings.Cut(route, " ")
	return strings.HasPrefix(pattern, "/self/") || strings.HasPrefix(pattern, "/share/") || strings.HasPrefix(pattern, "/principals")
}

// sessionID is the id of a web UI session, minted per instance.
var sessionID = regexp.MustCompile(`uis_[0-9a-f]{24}`)

// normalizeAdmin is normalizeFor with the registry listings put in a
// canonical order, every array sorted by its rows' text, and the
// instance-minted session ids blanked.
func normalizeAdmin(route string, body []byte) string {
	s := sessionID.ReplaceAllString(string(body), "uis_<volatile>")
	if !registryRoute(route) {
		return normalizeFor(route, []byte(s))
	}
	var v any
	if json.Unmarshal([]byte(normalize([]byte(s))), &v) != nil {
		return normalize([]byte(s))
	}
	out, err := json.Marshal(sortArrays(v))
	if err != nil {
		return normalize([]byte(s))
	}
	return string(out)
}

// sortArrays sorts every array in v by the JSON text of its elements.
func sortArrays(v any) any {
	switch x := v.(type) {
	case []any:
		for i := range x {
			x[i] = sortArrays(x[i])
		}
		sort.Slice(x, func(i, j int) bool {
			a, _ := json.Marshal(x[i])
			b, _ := json.Marshal(x[j])
			return string(a) < string(b)
		})
		return x
	case map[string]any:
		for k := range x {
			x[k] = sortArrays(x[k])
		}
	}
	return v
}
