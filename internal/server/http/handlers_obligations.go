package http

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/bosobligations"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

const obligationsBodyLimit = 4 << 20

type obligationRoutes struct {
	rcv *bosobligations.Receiver
}

func newObligationRoutes(svc *service.Service) obligationRoutes {
	return obligationRoutes{rcv: bosobligations.NewReceiver(svc)}
}

func writeObligationError(w http.ResponseWriter, id string, err error) {
	var oe *bosobligations.Error
	if !errors.As(err, &oe) {
		oe = &bosobligations.Error{
			HTTPStatus: http.StatusServiceUnavailable,
			Code:       bosobligations.CodeUnavailable,
			Message:    err.Error(),
		}
	}
	st := bosobligations.Status{
		ID:      id,
		State:   bosobligations.StateFailed,
		Code:    oe.Code,
		Message: oe.Message,
	}
	WriteJSON(w, oe.HTTPStatus, st)
}

func (o obligationRoutes) submit(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, obligationsBodyLimit))
	if err != nil {
		writeObligationError(w, "", &bosobligations.Error{
			HTTPStatus: http.StatusBadRequest,
			Code:       bosobligations.CodeObligationInvalid,
			Message:    "body unreadable or too large",
		})
		return
	}
	req, err := bosobligations.ParseRequest(raw)
	if err != nil {
		writeObligationError(w, "", err)
		return
	}
	if idem := strings.TrimSpace(r.Header.Get("Idempotency-Key")); idem != "" && idem != req.ID {
		writeObligationError(w, req.ID, &bosobligations.Error{
			HTTPStatus: http.StatusBadRequest,
			Code:       bosobligations.CodeObligationInvalid,
			Message:    "Idempotency-Key header must match body.id",
		})
		return
	}
	st, code, err := o.rcv.Apply(r.Context(), req)
	if err != nil {
		writeObligationError(w, req.ID, err)
		return
	}
	WriteJSON(w, code, st)
}

func (o obligationRoutes) status(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	st, err := o.rcv.Status(r.Context(), id)
	if err != nil {
		writeObligationError(w, id, err)
		return
	}
	WriteJSON(w, http.StatusOK, st)
}
