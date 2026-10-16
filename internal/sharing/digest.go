package sharing

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// OperationDigest identifies the operation a request asked for, for the
// read audit (ADR-080 §9): a SHA-256 over the method, the route
// template and the params, as "v1:sha256:<hex>" (storage.DigestPrefix).
//
// params maps a namespaced name ("query.q", "path.id", "req.limit") to
// its values. Names are sorted, and so are the values of each name, so
// the digest does not depend on the order params arrive in; every part
// is quoted, so no two operations share an encoding. The values are
// hashed, never stored. The hash carries no secret: a value with few
// possibilities (a status, a small id) can be confirmed by hashing the
// guesses, so the digest says "this is the operation" to someone who
// already has it, and hides nothing from someone who can guess it.
//
// v1 covers no request body. A digest that also hashes a body takes a
// new version prefix and needs RFC 8785 canonical JSON, which no
// dependency of this module provides; until one does, two requests that
// differ only in their body share a digest.
func OperationDigest(method, route string, params map[string][]string) string {
	var b strings.Builder
	b.WriteString(strconv.Quote(method))
	b.WriteByte('\n')
	b.WriteString(strconv.Quote(route))
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		vals := slices.Clone(params[name])
		sort.Strings(vals)
		b.WriteByte('\n')
		b.WriteString(strconv.Quote(name))
		for _, v := range vals {
			b.WriteByte(' ')
			b.WriteString(strconv.Quote(v))
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	return storage.DigestPrefix + hex.EncodeToString(sum[:])
}
