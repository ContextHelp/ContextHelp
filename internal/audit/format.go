package audit

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Format is the output format for audit log export.
type Format int

const (
	FormatJSON   Format = iota // NDJSON — one JSON object per line
	FormatCEF                  // ArcSight Common Event Format v0
	FormatSyslog               // RFC 5424 structured-data text
)

// ParseFormat converts a format name string to Format.
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(s) {
	case "json":
		return FormatJSON, nil
	case "cef":
		return FormatCEF, nil
	case "syslog":
		return FormatSyslog, nil
	default:
		return 0, fmt.Errorf("unknown audit export format %q: must be json, cef, or syslog", s)
	}
}

// Marshal serialises a single audit entry in the requested format.
func Marshal(e *storage.AuditEntry, f Format) (string, error) {
	switch f {
	case FormatJSON:
		b, err := json.Marshal(e)
		if err != nil {
			return "", err
		}
		return string(b), nil
	case FormatCEF:
		return marshalCEF(e), nil
	case FormatSyslog:
		return marshalSyslog(e), nil
	default:
		return "", fmt.Errorf("unsupported format %d", f)
	}
}

// marshalCEF produces an ArcSight CEF v0 record.
// Format: CEF:Version|Device Vendor|Device Product|Device Version|Signature ID|Name|Severity|Extension
func marshalCEF(e *storage.AuditEntry) string {
	ext := fmt.Sprintf("rt=%s act=%s suid=%s target=%s",
		e.CreatedAt.UTC().Format(time.RFC3339),
		escCEF(e.EventType),
		escCEF(e.Actor),
		escCEF(e.ObjectID),
	)
	return fmt.Sprintf("CEF:0|IdeacrafterLabs|ctxt|1.0|%s|%s|5|%s",
		escCEF(e.EventType),
		escCEF(e.EventType),
		ext,
	)
}

// marshalSyslog produces an RFC 5424 structured-data line.
func marshalSyslog(e *storage.AuditEntry) string {
	// PRI = facility 16 (local0), severity 6 (informational) → 16*8+6 = 134
	const pri = 134
	ts := e.CreatedAt.UTC().Format(time.RFC3339)
	sd := fmt.Sprintf(`[ctxt@32473 event_type="%s" object_id="%s" actor="%s" id="%s"]`,
		escSD(e.EventType), escSD(e.ObjectID), escSD(e.Actor), escSD(e.ID))
	return fmt.Sprintf("<%d>1 %s - ctxt - %s %s -", pri, ts, e.ID, sd)
}

// escCEF escapes CEF extension field values (| \ =).
func escCEF(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `|`, `\|`)
	s = strings.ReplaceAll(s, `=`, `\=`)
	return s
}

// escSD escapes RFC 5424 SD-PARAM values (" \ ]).
func escSD(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, `]`, `\]`)
	return s
}
