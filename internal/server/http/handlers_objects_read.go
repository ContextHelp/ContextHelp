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
	"github.com/ideacrafterslabs/ctxt/internal/tagfilter"
)

// CodeInvalidParam is the error code of a 400 caused by a query
// parameter: unknown, repeated, empty or malformed. details.param names
// it.
const CodeInvalidParam = "INVALID_PARAM"

// objectFilterParams select objects. GET /objects and GET
// /objects/facets both take them.
var objectFilterParams = []string{
	"type", "subtype", "tagged", "mention", "pipeline", "status",
	"since", "until",
	"meta_type", "topic", "person", "source_type", "mentioned_since", "mentioned_until",
}

// objectPageParams order and page a list. Only GET /objects takes them.
var objectPageParams = []string{"limit", "offset", "sort", "dir", "changed_after"}

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

// repeatableParams may be given more than once; their values read as one
// comma list.
var repeatableParams = []string{"tagged"}

// checkParams rejects a parameter outside allowed, and one that is
// repeated (outside repeatableParams) or empty.
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
		case len(vals) > 1 && !slices.Contains(repeatableParams, name):
			return badParam(name, "parameter %s given %d times; want one value", name, len(vals))
		case slices.Contains(vals, ""):
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
	if f.Tags, err = tagsParam(q, "tagged"); err != nil {
		return f, err
	}
	if f.After, err = instantParam(q, "since", false); err != nil {
		return f, err
	}
	if f.Before, err = instantParam(q, "until", true); err != nil {
		return f, err
	}
	if f.MetadataSince, err = dayParam(q, "mentioned_since"); err != nil {
		return f, err
	}
	if f.MetadataUntil, err = dayParam(q, "mentioned_until"); err != nil {
		return f, err
	}
	return f, nil
}

// parseObjectPage reads objectPageParams into f. changed_after pages by
// change order, so it excludes the parameters that choose another order
// or position: sort, dir and offset.
func parseObjectPage(q url.Values, f *storage.ObjectFilter) *paramError {
	var err *paramError
	if f.ChangedAfter, err = changeSeqParam(q, "changed_after"); err != nil {
		return err
	}
	if f.ChangedAfter != nil {
		for _, name := range []string{"sort", "dir", "offset"} {
			if q.Has(name) {
				return badParam(name, "%s cannot be combined with changed_after: changes list in change order, paged by the cursor", name)
			}
		}
	}
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

// changeSeqParam parses a change sequence: a non-negative integer.
func changeSeqParam(q url.Values, name string) (*int64, *paramError) {
	v := q.Get(name)
	if v == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return nil, badParam(name, "%s must be a non-negative integer", name)
	}
	return &n, nil
}

// rawTaggedQuery is the request's query with `tagged` read from the raw query
// string. Form decoding turns "+" into a space, but "+" is the required
// marker of the tag grammar, so each `tagged` value is decoded with
// url.PathUnescape (a "+" stays; %2B and %20 decode as usual), the values
// keep their order, and every other parameter keeps the standard decoding.
func rawTaggedQuery(r *http.Request) (url.Values, *paramError) {
	q := r.URL.Query()
	q.Del("tagged")
	for _, pair := range strings.Split(r.URL.RawQuery, "&") {
		key, val, _ := strings.Cut(pair, "=")
		if k, err := url.QueryUnescape(key); err != nil || k != "tagged" {
			continue
		}
		v, err := url.PathUnescape(val)
		if err != nil {
			return nil, badParam("tagged", "tagged is not valid percent-encoding")
		}
		q.Add("tagged", v)
	}
	return q, nil
}

// tagsParam parses the tag selection of parameter name (internal/tagfilter
// grammar: a,b any; +a,+b all; +a,b a required, b optional). The parameter
// may repeat as well as hold a comma list. Absent selects all.
func tagsParam(q url.Values, name string) (tagfilter.Filter, *paramError) {
	vals := q[name]
	if len(vals) == 0 || len(vals) == 1 && vals[0] == "" {
		return tagfilter.Filter{}, nil
	}
	f, err := tagfilter.Parse(vals)
	if err != nil {
		return f, badParam(name, "%s: %v", name, err)
	}
	return f, nil
}

// instantParam parses an RFC 3339 timestamp or a YYYY-MM-DD date,
// normalized to UTC. An upper bound that is a date is the start of the
// next day (service.ParseInstant): bounds are half-open.
func instantParam(q url.Values, name string, upper bool) (*time.Time, *paramError) {
	v := q.Get(name)
	if v == "" {
		return nil, nil
	}
	t, err := service.ParseInstant(v, upper)
	if err != nil {
		return nil, badParam(name, "%s must be an RFC 3339 timestamp or YYYY-MM-DD", name)
	}
	return &t, nil
}

// dayParam parses a YYYY-MM-DD date. mentioned_since and mentioned_until
// compare against metadata.dates_mentioned, which holds dates only.
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

// flagParam reports whether boolean query parameter name is exactly
// "true"; anything else, absence included, is false.
func flagParam(q url.Values, name string) bool {
	return q.Get(name) == "true"
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
		q, perr := rawTaggedQuery(r)
		if perr != nil {
			writeParamError(w, perr)
			return
		}
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
		// A change-ordered page cannot also rank by tags matched.
		if filter.ChangedAfter != nil && filter.Tags.Ranks() {
			writeParamError(w, badParam("tagged", "tagged cannot rank results with changed_after (two or more tags with none required, or a plain tag beside a required one): changes list in change order"))
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
		resp := map[string]any{"data": objs, "total": total}
		if filter.ChangedAfter != nil {
			resp["next_changed_after"] = nextChangedAfter(objs, *filter.ChangedAfter)
		}
		WriteJSON(w, http.StatusOK, resp)
	}
}

// nextChangedAfter is the cursor to pass as changed_after for the page
// after objs: the last object's change sequence, or after itself when
// the page is empty (nothing visible changed past it). It is the only
// change sequence a response carries; objects do not expose theirs.
func nextChangedAfter(objs []*storage.KnowledgeObject, after int64) int64 {
	if n := len(objs); n > 0 && objs[n-1].ChangeSeq > after {
		return objs[n-1].ChangeSeq
	}
	return after
}

// ObjectFacets handles GET /api/v1/objects/facets: counts of the objects
// matching the object filter by metadata type. Objects without one count
// under "(none)".
func ObjectFacets(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q, perr := rawTaggedQuery(r)
		if perr != nil {
			writeParamError(w, perr)
			return
		}
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
				WriteError(w, http.StatusNotFound, CodeNotFound, "object not found")
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
