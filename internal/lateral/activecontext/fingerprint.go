package activecontext

import (
	"bytes"
	"sort"
	"strconv"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
	"hop.top/kit/go/core/util"
)

// Fingerprint hashes the active context's signal maps and author hints
// into a stable 16-char hex string. Deterministic for equal input — used
// as a cache key for triage decisions (Q2.4 from the spec). The
// digest+truncate step is delegated to hop.top/kit/go/core/util.Short so
// any future change to the canonical short-hash format propagates here
// for free.
//
// AuthorHints participate in the fingerprint because they influence
// strategy probe behaviour (e.g. github author_other_pr). Two ACs with
// identical signal maps but different AuthorHints would otherwise share
// a cache key despite producing different probe outputs.
//
// Note: the cached `ac.Fingerprint` field reflects the AC at the moment
// activecontext.Resolve constructed it — typically before the daemon
// session middleware sets AuthorHints. Callers using Fingerprint as a
// cache key over an AC that may have AuthorHints set later MUST call
// Fingerprint(ac) again to obtain the current hash; the cached field
// captures the pre-AuthorHints state.
func Fingerprint(ac lateral.ActiveContext) string {
	var buf bytes.Buffer
	addFloatMap := func(prefix string, m map[string]float64) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			buf.WriteString(prefix)
			buf.WriteString(k)
			buf.WriteByte('=')
			buf.WriteString(strconv.FormatFloat(m[k], 'f', 4, 64))
			buf.WriteByte('\n')
		}
	}
	addStringMap := func(prefix string, m map[string]string) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			buf.WriteString(prefix)
			buf.WriteString(k)
			buf.WriteByte('=')
			buf.WriteString(m[k])
			buf.WriteByte('\n')
		}
	}
	addFloatMap("s:", ac.SessionTopic)
	addFloatMap("w:", ac.CaptureWindow)
	addFloatMap("i:", ac.InterestRegistry)
	addStringMap("a:", ac.AuthorHints)
	return util.Short(buf.Bytes(), 16)
}
