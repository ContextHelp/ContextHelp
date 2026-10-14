package http

import (
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// Stats handles GET /api/v1/stats: counts of objects by type, jobs by
// status, entities, feeds, reminders and resurfacing candidates, for an
// optional ?profile=. Under an inbound gate a principal counts only the
// entities it may list.
func Stats(svc *service.Service, gate *registry.InboundGate) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if err := checkParams(q, []string{"profile"}); err != nil {
			writeParamError(w, err)
			return
		}
		req := service.StatsRequest{Profile: q.Get("profile")}
		if g := entityGate(r, gate); g != nil {
			principal := gatePrincipal(r)
			req.EntityVisible = func(namespace string) bool {
				return g.Check(r.Context(), principal, namespace) == nil
			}
		}
		st, err := svc.Stats(r.Context(), req)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, st)
	}
}
