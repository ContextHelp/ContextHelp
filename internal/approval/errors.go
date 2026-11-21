package approval

import (
	"errors"
	"fmt"
)

// Stable codes of the mechanism's refusals. The HTTP layer answers
// each with its status and this code; a consumer route that files a
// request answers the filing codes the same way.
const (
	// CodeInvalid: the filing is malformed or its kind's validator
	// refused the payload (400).
	CodeInvalid = "INVALID_REQUEST"
	// CodeKindUnknown: no kind of that name is registered (400 when
	// filing; a stored request of an unregistered kind fails closed).
	CodeKindUnknown = "APPROVAL_KIND_UNKNOWN"
	// CodeReasonTooShort: the kind requires a longer reason (400).
	CodeReasonTooShort = "APPROVAL_REASON_TOO_SHORT"
	// CodeOwnerCannotFile: the owner's changes apply at once; it files
	// nothing on the mechanism (403).
	CodeOwnerCannotFile = "OWNER_CANNOT_FILE"
	// CodeClosed: the request is no longer pending: approved, rejected,
	// withdrawn or stale (409).
	CodeClosed = "APPROVAL_CLOSED"
	// CodeExpired: the request's pending TTL has passed (409).
	CodeExpired = "APPROVAL_EXPIRED"
	// CodeStale: the last approval found the state the payload was
	// frozen against changed; the request is now stale and nothing was
	// applied (409).
	CodeStale = "APPROVAL_STALE"
	// CodeRequesterCannotDecide: the requester decides nothing on its
	// own request (403).
	CodeRequesterCannotDecide = "REQUESTER_CANNOT_DECIDE"
	// CodeBeneficiaryCannotDecide: the decider is the request's subject
	// or a principal it would benefit (403).
	CodeBeneficiaryCannotDecide = "BENEFICIARY_CANNOT_DECIDE"
	// CodeNotEligible: the decider is not an eligible approver of the
	// kind (403).
	CodeNotEligible = "APPROVER_NOT_ELIGIBLE"
	// CodeAlreadyDecided: one principal records one decision per
	// request (409).
	CodeAlreadyDecided = "ALREADY_DECIDED"
	// CodeDigestMismatch: the decision names another payload digest than
	// the request's (409).
	CodeDigestMismatch = "DIGEST_MISMATCH"
	// CodeNotRequester: only the requester or the owner withdraws a
	// request (403).
	CodeNotRequester = "NOT_REQUESTER"
)

// Error is a refusal of the mechanism with its stable code.
type Error struct {
	Code    string
	Message string
	// Err is the cause, when one is wrapped (a validator's error).
	Err error
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.Err }

// refuse returns an Error with code and a formatted message.
func refuse(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// CodeOf returns the code of the Error in err's chain, "" when there is
// none.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
