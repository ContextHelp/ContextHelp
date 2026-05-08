package activecontext

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// Fingerprint hashes the active context's three signal maps into a stable
// 16-char hex string. Deterministic for equal input — used as a cache key
// for triage decisions (Q2.4 from the spec).
func Fingerprint(ac lateral.ActiveContext) string {
	h := sha256.New()
	addMap := func(prefix string, m map[string]float64) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			h.Write([]byte(prefix))
			h.Write([]byte(k))
			h.Write([]byte("="))
			h.Write([]byte(strconv.FormatFloat(m[k], 'f', 4, 64)))
			h.Write([]byte("\n"))
		}
	}
	addMap("s:", ac.SessionTopic)
	addMap("w:", ac.CaptureWindow)
	addMap("i:", ac.InterestRegistry)
	return hex.EncodeToString(h.Sum(nil))[:16]
}
