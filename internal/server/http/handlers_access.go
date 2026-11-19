package http

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/bosaccess"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// accessBodyLimit bounds an access set body.
const accessBodyLimit = 64 << 20

// AccessReceiver is the access state an external authority provisions: the
// peer side of bos.access/v1 (internal/bosaccess.Peer).
type AccessReceiver interface {
	Apply(bosaccess.Set) (bosaccess.Ack, error)
	State() bosaccess.Report
	Profile() bosaccess.Profile
	Scope() string
}

// accessStatus is the HTTP status of each error code of the contract.
var accessStatus = map[string]int{
	bosaccess.CodeEpochStale:     http.StatusConflict,
	bosaccess.CodeBaseEpoch:      http.StatusConflict,
	bosaccess.CodeDigestMismatch: http.StatusUnprocessableEntity,
	bosaccess.CodeSetInvalid:     http.StatusBadRequest,
	bosaccess.CodeUnavailable:    http.StatusServiceUnavailable,
	"scope_unknown":              http.StatusNotFound,
	"forbidden":                  http.StatusForbidden,
}

type accessError struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	AppliedEpoch *int64 `json:"applied_epoch,omitempty"`
}

func writeAccessError(w http.ResponseWriter, err error) {
	var ae *bosaccess.Error
	if !errors.As(err, &ae) {
		ae = &bosaccess.Error{Code: bosaccess.CodeUnavailable, Message: err.Error()}
	}
	status, ok := accessStatus[ae.Code]
	if !ok {
		status = http.StatusBadRequest
	}
	body := accessError{Code: ae.Code, Message: ae.Message}
	if ae.AppliedEpoch > 0 {
		body.AppliedEpoch = &ae.AppliedEpoch
	}
	WriteJSON(w, status, body)
}

// accessRoutes serves the routes bos calls to provision access
// (bos.access/v1): PUT replaces the set of the scope, PATCH applies a delta,
// GET .../state says what is held, and the profile is served for the
// handshake. On a non-private instance the caller is a principal of kind
// peer, as for the federation push; a private instance accepts its owner.
type accessRoutes struct {
	rc  RouterConfig
	rcv AccessReceiver
}

// guard admits only the one authority configured: provisioning is bound to
// the principal named by sharing.authority.principal (a peer principal) and
// to the scope named by sharing.authority.scope. Unconfigured, it refuses
// everything: no caller claims a scope by being first.
func (a accessRoutes) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.rc.AccessPrincipal == "" || a.rc.AccessScope == "" {
			writeAccessError(w, &bosaccess.Error{Code: "forbidden", Message: "access provisioning is not configured"})
			return
		}
		if a.rc.Auth != nil {
			p, ok := authn.FromContext(r.Context())
			if !ok || p.Kind != storage.PrincipalKindPeer || p.ID != a.rc.AccessPrincipal {
				writeAccessError(w, &bosaccess.Error{Code: "forbidden", Message: "access provisioning is for the configured authority only"})
				return
			}
		}
		next(w, r)
	}
}

// scopeOK reports whether the business of the path is the configured scope:
// the part of its address after the last slash.
func (a accessRoutes) scopeOK(business string) bool {
	s := a.rc.AccessScope
	return business != "" && s[strings.LastIndex(s, "/")+1:] == business
}

func (a accessRoutes) apply(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, accessBodyLimit))
	if err != nil {
		writeAccessError(w, &bosaccess.Error{Code: bosaccess.CodeSetInvalid, Message: "body unreadable or too large"})
		return
	}
	set, err := bosaccess.ParseSet(raw)
	if err != nil {
		a.audit(r, bosaccess.Set{}, bosaccess.Ack{}, err)
		writeAccessError(w, err)
		return
	}
	business := chi.URLParam(r, "business")
	if !a.scopeOK(business) || set.Scope != a.rc.AccessScope {
		err := &bosaccess.Error{Code: "scope_unknown", Message: "the peer is not bound to that scope"}
		a.audit(r, set, bosaccess.Ack{}, err)
		writeAccessError(w, err)
		return
	}
	if (r.Method == http.MethodPut) != (set.Mode == bosaccess.ModeFull) {
		err := &bosaccess.Error{Code: bosaccess.CodeSetInvalid, Message: "PUT carries a full set, PATCH a delta"}
		a.audit(r, set, bosaccess.Ack{}, err)
		writeAccessError(w, err)
		return
	}
	ack, err := a.rcv.Apply(set)
	a.audit(r, set, ack, err)
	if err != nil {
		writeAccessError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, ack)
}

func (a accessRoutes) state(w http.ResponseWriter, r *http.Request) {
	if !a.scopeOK(chi.URLParam(r, "business")) {
		writeAccessError(w, &bosaccess.Error{Code: "scope_unknown", Message: "the peer holds nothing for that scope"})
		return
	}
	WriteJSON(w, http.StatusOK, a.rcv.State())
}

// audit records what bos sent and what ctxt answered; never the content of a
// set, only counts and ids of the refusal.
func (a accessRoutes) audit(r *http.Request, set bosaccess.Set, ack bosaccess.Ack, err error) {
	if a.rc.AuditLog == nil {
		return
	}
	actor := storage.OwnerPrincipal
	if p, ok := authn.FromContext(r.Context()); ok {
		actor = p.ID
	}
	payload := map[string]any{"epoch": set.Epoch, "mode": set.Mode, "digest": set.Digest,
		"grants": len(set.Grants), "record_access": len(set.RecordAccess)}
	event := "access.applied"
	if err != nil {
		event = "access.refused"
		var ae *bosaccess.Error
		if errors.As(err, &ae) {
			payload["code"] = ae.Code
		}
	} else {
		payload["state"] = ack.State
		payload["unrepresentable"] = len(ack.Unrepresentable)
		if ack.RecordAccess != nil {
			payload["unrepresentable_entries"] = len(ack.RecordAccess.Unrepresentable)
		}
	}
	_ = a.rc.AuditLog.Append(r.Context(), &storage.AuditEntry{
		ID: storage.NewULID(), EventType: event, Actor: actor, Payload: payload, CreatedAt: time.Now().UTC(),
	})
}

func (a accessRoutes) profile(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, a.rcv.Profile())
}
