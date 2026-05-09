package activecontext

import (
	"bytes"
	"sort"
	"strconv"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
	"hop.top/kit/go/core/util"
)

// Fingerprint hashes the active context's three signal maps into a stable
// 16-char hex string. Deterministic for equal input — used as a cache key
// for triage decisions (Q2.4 from the spec). The digest+truncate step is
// delegated to hop.top/kit/go/core/util.Short so any future change to the
// canonical short-hash format propagates here for free.
func Fingerprint(ac lateral.ActiveContext) string {
	var buf bytes.Buffer
	addMap := func(prefix string, m map[string]float64) {
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
	addMap("s:", ac.SessionTopic)
	addMap("w:", ac.CaptureWindow)
	addMap("i:", ac.InterestRegistry)
	return util.Short(buf.Bytes(), 16)
}
