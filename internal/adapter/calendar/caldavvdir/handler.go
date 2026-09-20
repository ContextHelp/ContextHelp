package caldavvdir

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// newCalDAVHandler returns the thin CalDAV HTTP mux. Phase 2 covers
// the bare minimum needed for clients to discover the calendar and
// list events:
//
//	OPTIONS  /          → DAV header + supported method list
//	PROPFIND /          → calendar properties (resourcetype, displayname)
//	REPORT   /          → calendar-multiget-style response
//	GET      /<uid>.ics → individual event body
//	PUT      /<uid>.ics → write a new event (no policy gate; use Submit)
//	DELETE   /<uid>.ics → remove an event
//
// Handlers respond with intentionally minimal XML — clients that
// strictly validate against the full RFC 4791 schema will likely
// reject; Phase 3 swaps the handler for a go-webdav/caldav-backed
// implementation behind the same Adapter contract.
func newCalDAVHandler(vdir, calendarName string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "OPTIONS":
			w.Header().Set("DAV", "1, 2, 3, calendar-access")
			w.Header().Set("Allow", "OPTIONS, PROPFIND, REPORT, GET, PUT, DELETE")
			w.WriteHeader(http.StatusOK)
		case "PROPFIND":
			handlePROPFIND(w, vdir, calendarName)
		case "REPORT":
			handleREPORT(w, vdir)
		case "GET":
			handleGET(w, r, vdir)
		case "PUT":
			handlePUT(w, r, vdir)
		case "DELETE":
			handleDELETE(w, r, vdir)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	return mux
}

// handlePROPFIND emits a thin multistatus response advertising the
// calendar's resourcetype + displayname.
func handlePROPFIND(w http.ResponseWriter, _ /* vdir */, calendarName string) {
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:response>
    <d:href>/</d:href>
    <d:propstat>
      <d:prop>
        <d:resourcetype>
          <d:collection/>
          <c:calendar/>
        </d:resourcetype>
        <d:displayname>%s</d:displayname>
        <c:supported-calendar-component-set>
          <c:comp name="VEVENT"/>
        </c:supported-calendar-component-set>
      </d:prop>
      <d:status>HTTP/1.1 200 OK</d:status>
    </d:propstat>
  </d:response>
</d:multistatus>`, escapeXML(calendarName))
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	_, _ = w.Write([]byte(body))
}

// handleREPORT walks the vdir and emits each event in a multistatus.
func handleREPORT(w http.ResponseWriter, vdir string) {
	objs, _ := walkVDir(vdir)
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	sb.WriteString(`<d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">` + "\n")
	for _, o := range objs {
		fmt.Fprintf(&sb, `  <d:response>
    <d:href>/%s.ics</d:href>
    <d:propstat>
      <d:prop>
        <d:getetag>"%s"</d:getetag>
        <c:calendar-data>%s</c:calendar-data>
      </d:prop>
      <d:status>HTTP/1.1 200 OK</d:status>
    </d:propstat>
  </d:response>
`, escapeXML(o.ID), escapeXML(o.ID), escapeXML(o.Content))
	}
	sb.WriteString("</d:multistatus>")
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	_, _ = w.Write([]byte(sb.String()))
}

// handleGET serves a single .ics file by UID-derived filename.
func handleGET(w http.ResponseWriter, r *http.Request, vdir string) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if !strings.HasSuffix(name, ".ics") || strings.Contains(name, "/") || strings.Contains(name, "..") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	// #nosec G703 -- name is confined to one path segment by the guard
	// above: it must end in .ics and may contain neither "/" nor "..".
	// net/http percent-decodes URL.Path before the check, so encoded
	// and double-encoded traversal is caught too (probed against ../,
	// %2e%2e%2f, %252f, ..;/ and a NUL-prefixed variant). The taint
	// tracker cannot see through the guard.
	body, err := os.ReadFile(filepath.Join(vdir, name))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	// #nosec G705 -- not an XSS sink. The body is an iCalendar file
	// served as text/calendar, never interpreted as HTML by a client.
	_, _ = w.Write(body)
}

// handlePUT writes an .ics body. Note: PUT bypasses the
// domain.Service gate that Submit uses — clients that need policy
// enforcement should write via the adapter's Submit, not directly
// via the served listener. Phase 3 may bridge PUT through Submit
// when the substrate adds an "inbound from served listener" routing
// convention.
func handlePUT(w http.ResponseWriter, r *http.Request, vdir string) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if !strings.HasSuffix(name, ".ics") || strings.Contains(name, "/") || strings.Contains(name, "..") {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	defer func() { _ = r.Body.Close() }()
	buf := make([]byte, 0, 1024)
	tmp := make([]byte, 1024)
	for {
		n, err := r.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
		if len(buf) > 1<<20 { // 1 MiB cap
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
			return
		}
	}
	// #nosec G703 -- same single-segment guard as handleGET.
	if err := os.WriteFile(filepath.Join(vdir, name), buf, 0o600); err != nil {
		http.Error(w, "write failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// handleDELETE removes an .ics file by name.
func handleDELETE(w http.ResponseWriter, r *http.Request, vdir string) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if !strings.HasSuffix(name, ".ics") || strings.Contains(name, "/") || strings.Contains(name, "..") {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// #nosec G703 -- same single-segment guard as handleGET.
	if err := os.Remove(filepath.Join(vdir, name)); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// escapeXML is the minimal escaper for our generated CalDAV bodies.
// We don't pull encoding/xml because we're emitting fixed templates
// with only handful of dynamic fields.
func escapeXML(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	)
	return r.Replace(s)
}
