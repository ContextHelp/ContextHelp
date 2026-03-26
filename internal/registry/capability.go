package registry

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// CapabilityWarning describes a mismatch between what the client needs and what
// the registry supports.
type CapabilityWarning struct {
	Feature string
	Message string
}

// CheckCapabilities compares the registry manifest against the required feature set
// and returns a (possibly empty) list of warnings.
//
// requiredFeatures is a set of keys from: "entity_sync", "taxonomy", "translations".
// clientVersion is the running ctxt version string (semver or "dev").
func CheckCapabilities(
	manifest *storage.RegistryManifest,
	clientVersion string,
	requiredFeatures ...string,
) []CapabilityWarning {
	if manifest == nil {
		return nil
	}

	var warnings []CapabilityWarning

	// Version handshake: warn if client is older than registry's min requirement.
	if manifest.MinClientVersion != "" && clientVersion != "" && clientVersion != "dev" {
		if cmp := compareSemver(clientVersion, manifest.MinClientVersion); cmp < 0 {
			warnings = append(warnings, CapabilityWarning{
				Feature: "version",
				Message: fmt.Sprintf(
					"registry requires ctxt >= %s (running %s); some features may be unavailable",
					manifest.MinClientVersion, clientVersion,
				),
			})
		}
	}

	caps := manifest.Capabilities
	for _, f := range requiredFeatures {
		switch f {
		case "entity_sync":
			if !caps.EntitySync {
				warnings = append(warnings, CapabilityWarning{
					Feature: "entity_sync",
					Message: "registry does not support entity sync; mention resolution may be incomplete",
				})
			}
		case "taxonomy":
			if !caps.Taxonomy {
				warnings = append(warnings, CapabilityWarning{
					Feature: "taxonomy",
					Message: "registry does not expose a taxonomy; namespace suggestions unavailable",
				})
			}
		case "translations":
			if !caps.Translations {
				warnings = append(warnings, CapabilityWarning{
					Feature: "translations",
					Message: "registry does not provide translations; display will use slugs only",
				})
			}
		}
	}

	return warnings
}

// compareSemver returns -1, 0, or 1 (a < b, a == b, a > b).
// Non-numeric pre-release segments are treated as lower than numeric ones.
func compareSemver(a, b string) int {
	aParts := parseSemver(a)
	bParts := parseSemver(b)
	for i := 0; i < 3; i++ {
		if aParts[i] < bParts[i] {
			return -1
		}
		if aParts[i] > bParts[i] {
			return 1
		}
	}
	return 0
}

func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	// Strip pre-release suffix (e.g. "-alpha", "+build").
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.SplitN(v, ".", 3)
	var out [3]int
	for i, p := range parts {
		if i >= 3 {
			break
		}
		n, err := strconv.Atoi(p)
		if err == nil {
			out[i] = n
		}
	}
	return out
}
