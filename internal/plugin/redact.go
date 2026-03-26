package plugin

import "regexp"

// redactPatterns match common secret formats in free-form plugin output text.
var redactPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{10,}`),
	regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`eyJ[A-Za-z0-9_\-]{20,}\.[A-Za-z0-9_\-]{20,}`), // JWT
	regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`),
	regexp.MustCompile(`ghs_[A-Za-z0-9]{36}`),
	regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key)\s*[:=]\s*\S{8,}`),
}

const redactedPlaceholder = "[REDACTED]"

// RedactSecrets scans s for patterns that match known secret formats and
// replaces any matches with [REDACTED]. Safe to call on arbitrary plugin output
// before persisting to storage.
func RedactSecrets(s string) string {
	for _, re := range redactPatterns {
		s = re.ReplaceAllString(s, redactedPlaceholder)
	}
	return s
}

// RedactMap applies RedactSecrets to every string value in a nested map,
// returning a new map with secrets replaced. The original map is not mutated.
func RedactMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = redactValue(v)
	}
	return out
}

func redactValue(v any) any {
	switch t := v.(type) {
	case string:
		return RedactSecrets(t)
	case map[string]any:
		return RedactMap(t)
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = redactValue(item)
		}
		return out
	default:
		return v
	}
}
