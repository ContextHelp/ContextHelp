package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// CodeInvalidParam is the error code of a 400 caused by a query
// parameter: unknown, repeated, empty or malformed. details.param names
// it.
const CodeInvalidParam = "INVALID_PARAM"

// objectFilterParams select objects. GET /objects and GET
// /objects/facets both take them.
var objectFilterParams = []string{
	"type", "subtype", "tag", "mention", "pipeline", "status",
	"before", "after",
	"meta_type", "topic", "person", "source_type", "since", "until",
}

// objectPageParams order and page a list. Only GET /objects takes them.
var objectPageParams = []string{"limit", "offset", "sort", "dir"}

// objectStatuses are the values of the status parameter. "all" lifts
// the status filter; an absent status means "active".
var objectStatuses = []string{"active", "inbox", "discarded", "raw", "all"}

const (
	defaultListLimit    = 20
	defaultRelatedDepth = 1
	maxRelatedDepth     = 3
	defaultRelatedLimit = 10
	maxRelatedLimit     = 100
)

// paramError is a rejected query parameter.
type paramError struct {
	param string
	msg   string
}

func (e *paramError) Error() string { return e.msg }

func badParam(param, format string, args ...any) *paramError {
	return &paramError{param: param, msg: fmt.Sprintf(format, args...)}
}

func writeParamError(w http.ResponseWriter, e *paramError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(ErrorEnvelope{Error: ErrorBody{
		Code:    CodeInvalidParam,
		Message: e.msg,
		Details: map[string]any{"param": e.param},
	}})
}

// checkParams rejects a parameter outside allowed, and one that is
// repeated or empty: every parameter here takes exactly one value.
// Parameters are checked in sorted order so the error is deterministic.
func checkParams(q url.Values, allowed ...[]string) *paramError {
	names := make([]string, 0, len(q))
	for name := range q {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		known := false
		for _, set := range allowed {
			if slices.Contains(set, name) {
				known = true
				break
			}
		}
		if !known {
			return badParam(name, "unknown parameter %s", name)
		}
		switch vals := q[name]; {
		case len(vals) > 1:
			return badParam(name, "parameter %s given %d times; want one value", name, len(vals))
		case vals[0] == "":
			return badParam(name, "parameter %s is empty", name)
		}
	}
	return nil
}

// parseObjectFilter reads objectFilterParams. Call checkParams first.
func parseObjectFilter(q url.Values) (storage.ObjectFilter, *paramError) {
	f := storage.ObjectFilter{
		Type:           q.Get("type"),
		Subtype:        q.Get("subtype"),
		Tag:            q.Get("tag"),
		Mention:        q.Get("mention"),
		Pipeline:       q.Get("pipeline"),
		MetadataType:   q.Get("meta_type"),
		MetadataTopic:  q.Get("topic"),
		MetadataPerson: q.Get("person"),
		SourceType:     q.Get("source_type"),
	}
	if s := q.Get("status"); s != "" {
		if !slices.Contains(objectStatuses, s) {
			return f, badParam("status", "status must be one of %s", strings.Join(objectStatuses, ", "))
		}
		f.Status = s
	}
	var err *paramError
	if f.After, err = instantParam(q, "after"); err != nil {
		return f, err
	}
	if f.Before, err = instantParam(q, "before"); err != nil {
		return f, err
	}
	if f.MetadataSince, err = dayParam(q, "since"); err != nil {
		return f, err
	}
	if f.MetadataUntil, err = dayParam(q, "until"); err != nil {
		return f, err
	}
	return f, nil
}

// parseObjectPage reads objectPageParams into f.
func parseObjectPage(q url.Values, f *storage.ObjectFilter) *paramError {
	var err *paramError
	if f.Limit, err = intParam(q, "limit", defaultListLimit, 0, 0); err != nil {
		return err
	}
	if f.Offset, err = intParam(q, "offset", 0, 0, 0); err != nil {
		return err
	}
	switch s := q.Get("sort"); s {
	case "", "created_at", "updated_at":
		f.Sort = s
	default:
		return badParam("sort", "sort must be created_at or updated_at")
	}
	switch d := q.Get("dir"); d {
	case "", "asc", "desc":
		f.Dir = d
	default:
		return badParam("dir", "dir must be asc or desc")
	}
	return nil
}

// instantParam parses an RFC 3339 timestamp or a YYYY-MM-DD date
// (midnight UTC), normalized to UTC.
func instantParam(q url.Values, name string) (*time.Time, *paramError) {
	v := q.Get(name)
	if v == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, time.DateOnly} {
		if t, err := time.Parse(layout, v); err == nil {
			t = t.UTC()
			return &t, nil
		}
	}
	return nil, badParam(name, "%s must be an RFC 3339 timestamp or YYYY-MM-DD", name)
}

// dayParam parses a YYYY-MM-DD date. since and until compare against
// metadata.dates_mentioned, which holds dates only.
func dayParam(q url.Values, name string) (*time.Time, *paramError) {
	v := q.Get(name)
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.DateOnly, v)
	if err != nil {
		return nil, badParam(name, "%s must be YYYY-MM-DD", name)
	}
	return &t, nil
}

// intParam parses an integer in [lo, hi]; hi 0 means no upper bound.
func intParam(q url.Values, name string, def, lo, hi int) (int, *paramError) {
	v := q.Get(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || (hi > 0 && n > hi) {
		if hi > 0 {
			return 0, badParam(name, "%s must be an integer from %d to %d", name, lo, hi)
		}
		return 0, badParam(name, "%s must be an integer >= %d", name, lo)
	}
	return n, nil
}

// ListObjects handles GET /api/v1/objects: one page of the objects
// matching the object filter, plus the total match count.
func ListObjects(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if err := checkParams(q, objectFilterParams, objectPageParams); err != nil {
			writeParamError(w, err)
			return
		}
		filter, perr := parseObjectFilter(q)
		if perr != nil {
			writeParamError(w, perr)
			return
		}
		if perr := parseObjectPage(q, &filter); perr != nil {
			writeParamError(w, perr)
			return
		}

		objs, total, err := svc.ListObjects(r.Context(), filter)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if objs == nil {
			objs = []*storage.KnowledgeObject{}
		}
		WriteJSON(w, http.StatusOK, map[string]any{
			"data":  objs,
			"total": total,
		})
	}
}

// ObjectFacets handles GET /api/v1/objects/facets: counts of the objects
// matching the object filter by metadata type. Objects without one count
// under "(none)".
func ObjectFacets(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if err := checkParams(q, objectFilterParams); err != nil {
			writeParamError(w, err)
			return
		}
		filter, perr := parseObjectFilter(q)
		if perr != nil {
			writeParamError(w, perr)
			return
		}
		counts, err := svc.FacetCounts(r.Context(), filter)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"data": counts})
	}
}

// RelatedObjects handles GET /api/v1/objects/{id}/related: objects that
// share a mention target with the object, up to depth hops away.
func RelatedObjects(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if err := checkParams(q, []string{"depth", "limit"}); err != nil {
			writeParamError(w, err)
			return
		}
		depth, perr := intParam(q, "depth", defaultRelatedDepth, 1, maxRelatedDepth)
		if perr != nil {
			writeParamError(w, perr)
			return
		}
		limit, perr := intParam(q, "limit", defaultRelatedLimit, 1, maxRelatedLimit)
		if perr != nil {
			writeParamError(w, perr)
			return
		}

		id := chi.URLParam(r, "id")
		if _, err := svc.GetObject(r.Context(), id); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				WriteError(w, http.StatusNotFound, "NOT_FOUND", "object not found")
				return
			}
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		objs, err := svc.RelatedObjects(r.Context(), id, depth, limit)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"data": objs})
	}
}
