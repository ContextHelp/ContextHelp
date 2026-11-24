// Package bosobligations implements the peer (receiver) side of
// bos.obligations/v1: synchronous hold, release, and erase execution with
// durable idempotency, SHA-256 subject receipts, and hold-in-force refusals.
package bosobligations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Contract states and error codes from bos.obligations/v1.
const (
	StateAccepted = "accepted"
	StateApplied  = "applied"
	StatePartial  = "partial"
	StateFailed   = "failed"

	KindErase     = "erase"
	KindHold      = "hold"
	KindRelease   = "release"
	KindAnonymize = "anonymize"

	CodeObligationInvalid = "obligation_invalid"
	CodeKindUnsupported   = "kind_unsupported"
	CodeIDReused          = "id_reused"
	CodeHoldInForce       = "hold_in_force"
	CodeObligationUnknown = "obligation_unknown"
	CodeUnavailable       = "unavailable"
)

// Request is an incoming bos.obligations/v1 obligation document.
type Request struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Hold        string   `json:"hold,omitempty"`
	Subjects    []string `json:"subjects"`
	EffectiveAt string   `json:"effective_at"`
	Derived     bool     `json:"derived,omitempty"`
	Tombstone   bool     `json:"tombstone,omitempty"`
}

// ReceiptCounts summarizes acted-on objects and derived data.
type ReceiptCounts struct {
	Objects int64            `json:"objects"`
	Derived map[string]int64 `json:"derived,omitempty"`
}

// Receipt is the cryptographic proof attached to an applied obligation.
type Receipt struct {
	CompletedAt string        `json:"completed_at"`
	Counts      ReceiptCounts `json:"counts"`
	Absent      []string      `json:"absent,omitempty"`
	Digest      string        `json:"digest"`
}

// Status is the response document for POST /bos/v1/obligations and
// GET /bos/v1/obligations/{id}.
type Status struct {
	ID      string   `json:"id"`
	State   string   `json:"state"`
	Receipt *Receipt `json:"receipt,omitempty"`
	Code    string   `json:"code,omitempty"`
	Message string   `json:"message,omitempty"`
}

// Error carries a bos.obligations/v1 wire error code and HTTP status.
type Error struct {
	HTTPStatus int
	Code       string
	Message    string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// SubjectDigest computes the canonical SHA-256 subject digest specified by
// bos.obligations/v1: sort subjects bytewise, join with "\n", hash with SHA-256.
func SubjectDigest(subjects []string) string {
	s := append([]string(nil), subjects...)
	sort.Strings(s)
	sum := sha256.Sum256([]byte(strings.Join(s, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// CanonicalHash returns a deterministic SHA-256 hash of req used to detect
// idempotency key reuse with a different request body (409 id_reused).
func CanonicalHash(req Request) string {
	norm := req
	norm.Subjects = append([]string(nil), req.Subjects...)
	sort.Strings(norm.Subjects)
	b, _ := json.Marshal(norm)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ParseRequest strictly decodes and validates an obligation request JSON body.
func ParseRequest(raw []byte) (Request, error) {
	var req Request
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return Request{}, &Error{HTTPStatus: http.StatusBadRequest, Code: CodeObligationInvalid, Message: "invalid JSON: " + err.Error()}
	}
	if dec.More() {
		return Request{}, &Error{HTTPStatus: http.StatusBadRequest, Code: CodeObligationInvalid, Message: "trailing data in JSON body"}
	}
	if err := validateRequest(req); err != nil {
		return Request{}, err
	}
	return req, nil
}

func validateRequest(req Request) error {
	if strings.TrimSpace(req.ID) == "" {
		return &Error{HTTPStatus: http.StatusBadRequest, Code: CodeObligationInvalid, Message: "id is required"}
	}
	switch req.Kind {
	case KindErase, KindHold, KindRelease, KindAnonymize:
	default:
		return &Error{HTTPStatus: http.StatusBadRequest, Code: CodeObligationInvalid, Message: fmt.Sprintf("unknown kind %q", req.Kind)}
	}
	if len(req.Subjects) == 0 {
		return &Error{HTTPStatus: http.StatusBadRequest, Code: CodeObligationInvalid, Message: "subjects must be non-empty"}
	}
	for _, s := range req.Subjects {
		if strings.TrimSpace(s) == "" {
			return &Error{HTTPStatus: http.StatusBadRequest, Code: CodeObligationInvalid, Message: "subject address must be non-empty"}
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, req.EffectiveAt); err != nil {
		return &Error{HTTPStatus: http.StatusBadRequest, Code: CodeObligationInvalid, Message: "effective_at must be RFC 3339"}
	}
	if (req.Kind == KindHold || req.Kind == KindRelease) && strings.TrimSpace(req.Hold) == "" {
		return &Error{HTTPStatus: http.StatusBadRequest, Code: CodeObligationInvalid, Message: "hold is required for hold and release obligations"}
	}
	return nil
}

// Receiver executes bos.obligations/v1 obligations against a ctxt Service.
type Receiver struct {
	svc *service.Service
}

// NewReceiver creates an obligation receiver backed by svc.
func NewReceiver(svc *service.Service) *Receiver {
	return &Receiver{svc: svc}
}

// Apply executes (or replays) req under an unrestricted owner context.
func (r *Receiver) Apply(ctx context.Context, req Request) (Status, int, error) {
	if err := validateRequest(req); err != nil {
		return Status{}, http.StatusBadRequest, err
	}
	if req.Kind == KindAnonymize {
		return Status{}, http.StatusBadRequest, &Error{
			HTTPStatus: http.StatusBadRequest,
			Code:       CodeKindUnsupported,
			Message:    "ctxt does not support anonymize obligations; use erase",
		}
	}

	ownerCtx := sharing.WithViewer(ctx, sharing.Unrestricted)
	drv := r.svc.Store(ownerCtx)
	reqHash := CanonicalHash(req)

	// Idempotency check.
	if existing, err := drv.Obligations().Get(ownerCtx, req.ID); err == nil && existing != nil {
		if existing.RequestSHA256 != reqHash {
			return Status{}, http.StatusConflict, &Error{
				HTTPStatus: http.StatusConflict,
				Code:       CodeIDReused,
				Message:    "obligation id already used with a different request body",
			}
		}
		var prev Status
		if uerr := json.Unmarshal([]byte(existing.StatusJSON), &prev); uerr != nil {
			return Status{}, http.StatusInternalServerError, &Error{
				HTTPStatus: http.StatusInternalServerError,
				Code:       CodeUnavailable,
				Message:    "stored obligation receipt unreadable",
			}
		}
		return prev, http.StatusOK, nil
	} else if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return Status{}, http.StatusServiceUnavailable, &Error{
			HTTPStatus: http.StatusServiceUnavailable,
			Code:       CodeUnavailable,
			Message:    err.Error(),
		}
	}

	var (
		st  Status
		err error
	)
	switch req.Kind {
	case KindHold:
		st, err = r.applyHold(ownerCtx, drv, req)
	case KindRelease:
		st, err = r.applyRelease(ownerCtx, drv, req)
	case KindErase:
		st, err = r.applyErase(ownerCtx, drv, req)
	}
	if err != nil {
		var oe *Error
		if errors.As(err, &oe) {
			return Status{ID: req.ID, State: StateFailed, Code: oe.Code, Message: oe.Message}, oe.HTTPStatus, oe
		}
		return Status{}, http.StatusServiceUnavailable, &Error{
			HTTPStatus: http.StatusServiceUnavailable,
			Code:       CodeUnavailable,
			Message:    err.Error(),
		}
	}

	rawStatus, _ := json.Marshal(st)
	now := time.Now().UTC()
	if err := drv.Obligations().Save(ownerCtx, &storage.ObligationRecord{
		ID:            req.ID,
		Kind:          req.Kind,
		RequestSHA256: reqHash,
		State:         st.State,
		StatusJSON:    string(rawStatus),
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		if errors.Is(err, storage.ErrObligationIDReused) {
			return Status{}, http.StatusConflict, &Error{
				HTTPStatus: http.StatusConflict,
				Code:       CodeIDReused,
				Message:    "obligation id already used with a different request body",
			}
		}
		return Status{}, http.StatusServiceUnavailable, &Error{
			HTTPStatus: http.StatusServiceUnavailable,
			Code:       CodeUnavailable,
			Message:    err.Error(),
		}
	}
	return st, http.StatusOK, nil
}

// Status returns the persisted Status document for obligation id.
func (r *Receiver) Status(ctx context.Context, id string) (Status, error) {
	ownerCtx := sharing.WithViewer(ctx, sharing.Unrestricted)
	rec, err := r.svc.Store(ownerCtx).Obligations().Get(ownerCtx, id)
	if errors.Is(err, storage.ErrNotFound) {
		return Status{}, &Error{HTTPStatus: http.StatusNotFound, Code: CodeObligationUnknown, Message: "obligation not found"}
	}
	if err != nil {
		return Status{}, &Error{HTTPStatus: http.StatusServiceUnavailable, Code: CodeUnavailable, Message: err.Error()}
	}
	var st Status
	if err := json.Unmarshal([]byte(rec.StatusJSON), &st); err != nil {
		return Status{}, &Error{HTTPStatus: http.StatusInternalServerError, Code: CodeUnavailable, Message: "stored status invalid"}
	}
	return st, nil
}

func (r *Receiver) applyHold(ctx context.Context, drv storage.StorageDriver, req Request) (Status, error) {
	rc := Receipt{
		CompletedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Digest:      SubjectDigest(req.Subjects),
		Counts:      ReceiptCounts{Derived: map[string]int64{}},
	}
	for _, subj := range req.Subjects {
		objs, _, err := drv.Objects().List(ctx, storage.ObjectFilter{SourceKey: subj, Status: storage.ObjectStatusAll})
		if err != nil {
			return Status{}, err
		}
		if _, err := r.svc.PlaceHold(ctx, service.PlaceHoldOptions{
			SourceKey: subj,
			HoldID:    req.Hold,
			Source:    "bos",
			Reason:    "bos obligation " + req.ID,
			PlacedBy:  "bos",
		}); err != nil {
			return Status{}, err
		}
		if len(objs) == 0 {
			rc.Absent = append(rc.Absent, subj)
		} else {
			rc.Counts.Objects += int64(len(objs))
		}
	}
	return Status{ID: req.ID, State: StateApplied, Receipt: &rc}, nil
}

func (r *Receiver) applyRelease(ctx context.Context, drv storage.StorageDriver, req Request) (Status, error) {
	rc := Receipt{
		CompletedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Digest:      SubjectDigest(req.Subjects),
		Counts:      ReceiptCounts{Derived: map[string]int64{}},
	}
	for _, subj := range req.Subjects {
		objs, _, err := drv.Objects().List(ctx, storage.ObjectFilter{SourceKey: subj, Status: storage.ObjectStatusAll})
		if err != nil {
			return Status{}, err
		}
		if _, err := r.svc.ReleaseHold(ctx, service.ReleaseHoldOptions{
			SourceKey:  subj,
			HoldID:     req.Hold,
			Source:     "bos",
			ReleasedBy: "bos",
			Reason:     "bos obligation " + req.ID,
		}); err != nil {
			return Status{}, err
		}
		if len(objs) == 0 {
			rc.Absent = append(rc.Absent, subj)
		} else {
			rc.Counts.Objects += int64(len(objs))
		}
	}
	return Status{ID: req.ID, State: StateApplied, Receipt: &rc}, nil
}

func (r *Receiver) applyErase(ctx context.Context, drv storage.StorageDriver, req Request) (Status, error) {
	// All-or-nothing pre-check: check address-level holds across ALL subjects
	// before deleting any single subject.
	for _, subj := range req.Subjects {
		active, err := drv.Holds().ListActiveBySourceKey(ctx, subj)
		if err != nil {
			return Status{}, err
		}
		if len(active) > 0 {
			return Status{}, &Error{
				HTTPStatus: http.StatusConflict,
				Code:       CodeHoldInForce,
				Message:    fmt.Sprintf("subject %s has %d active hold(s)", subj, len(active)),
			}
		}
	}

	rc := Receipt{
		CompletedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Digest:      SubjectDigest(req.Subjects),
		Counts:      ReceiptCounts{Derived: map[string]int64{}},
	}
	for _, subj := range req.Subjects {
		res, err := r.svc.EraseBySourceKey(ctx, service.EraseBySourceOptions{
			SourceKey:    subj,
			Tombstone:    req.Tombstone,
			Source:       "bos",
			ObligationID: req.ID,
			ErasedBy:     "bos",
		})
		if errors.Is(err, storage.ErrObjectHeld) {
			return Status{}, &Error{
				HTTPStatus: http.StatusConflict,
				Code:       CodeHoldInForce,
				Message:    err.Error(),
			}
		}
		if err != nil {
			return Status{}, err
		}
		if len(res.MatchedIDs) == 0 {
			rc.Absent = append(rc.Absent, subj)
		} else {
			rc.Counts.Objects += int64(len(res.MatchedIDs))
		}
		if len(res.DerivedDeletedIDs) > 0 {
			rc.Counts.Derived["objects"] += int64(len(res.DerivedDeletedIDs))
		}
		if res.ScrubbedPages+res.DeletedPages > 0 {
			rc.Counts.Derived["pages"] += int64(res.ScrubbedPages + res.DeletedPages)
		}
	}
	return Status{ID: req.ID, State: StateApplied, Receipt: &rc}, nil
}
