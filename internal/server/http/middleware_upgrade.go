package http

import (
	"context"
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// UpgradeHeader sets upgrade.HeaderName on the response while probe
// reports an upgrade that is not idle (in progress, failed, awaiting
// consent), so every client renders the ADR-070 banner from the instance
// it talks to. probe is the /healthz upgrade probe; nil, or a nil
// snapshot, sets nothing.
//
// Mounted inside /api/v1 after authentication: like the verbose
// /healthz, the upgrade state goes only to callers the instance admits.
func UpgradeHeader(probe func(context.Context) *UpgradeSnapshot) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if probe == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if snap := probe(r.Context()); snap != nil {
				if v := upgrade.EncodeHeader(snap.status()); v != "" {
					w.Header().Set(upgrade.HeaderName, v)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// status maps the wire snapshot back to the fields the header carries.
func (s *UpgradeSnapshot) status() upgrade.Status {
	return upgrade.Status{
		State:      upgrade.State(s.State),
		Bucket:     upgrade.Bucket(s.Bucket),
		Target:     s.Target,
		Progress:   s.Progress,
		Done:       s.Done,
		Total:      s.Total,
		Failed:     s.Failed,
		EtaSeconds: s.EtaSeconds,
		LastError:  s.LastError,
	}
}
