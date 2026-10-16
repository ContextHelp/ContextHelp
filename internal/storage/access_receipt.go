package storage

import (
	"errors"
	"fmt"
	"slices"
)

// AccessDecision is the verdict a read audit entry records (ADR-080 §9): what
// the instance did with a restricted viewer's request.
type AccessDecision string

const (
	// AccessDecisionAllowed: the request was admitted and answered. A
	// response the backstop stripped a hit from is still allowed; the
	// failing backstop check says so.
	AccessDecisionAllowed AccessDecision = "allowed"
	// AccessDecisionDenied: a check refused the request, or the backstop
	// refused the response. At least one check is FAIL.
	AccessDecisionDenied AccessDecision = "denied"
	// AccessDecisionDeferred: no verdict was reached and the request failed
	// closed, for example the backstop could not read the response.
	// At least one check is UNKNOWN.
	AccessDecisionDeferred AccessDecision = "deferred"
)

// Valid reports whether d is one of the three decisions.
func (d AccessDecision) Valid() bool {
	return d == AccessDecisionAllowed || d == AccessDecisionDenied || d == AccessDecisionDeferred
}

// AccessOutcome is the result of one check in an entry's trail.
type AccessOutcome string

const (
	// AccessOutcomePass: the check ran and the request met it.
	AccessOutcomePass AccessOutcome = "PASS"
	// AccessOutcomeFail: the check ran and refused the request.
	AccessOutcomeFail AccessOutcome = "FAIL"
	// AccessOutcomeUnknown: the check ran and could not decide.
	AccessOutcomeUnknown AccessOutcome = "UNKNOWN"
	// AccessOutcomeNotApplicable: the check ran and had nothing to decide
	// for this request (no ceiling to apply, no scoped read made).
	// A check the request never reached is not recorded at all; it is
	// never PASS.
	AccessOutcomeNotApplicable AccessOutcome = "NOT_APPLICABLE"
)

// Valid reports whether o is one of the four outcomes.
func (o AccessOutcome) Valid() bool {
	switch o {
	case AccessOutcomePass, AccessOutcomeFail, AccessOutcomeUnknown, AccessOutcomeNotApplicable:
		return true
	}
	return false
}

// AccessCheck is one step of a request's check trail: which check ran,
// what it found and a stable machine reason code. It carries no value
// from the request or the data.
type AccessCheck struct {
	// Check is the check id, for example "scope" or "authn.api_key".
	Check   string        `json:"check"`
	Outcome AccessOutcome `json:"outcome"`
	// Reason is a stable snake_case code, for example "scope_missing".
	Reason string `json:"reason"`
}

// DigestPrefix starts every operation digest and names its algorithm
// and version, so a digest that also covers a request body can replace
// it without being mistaken for it.
const DigestPrefix = "v1:sha256:"

// ValidateAccessEntry reports whether e may be appended: a principal and
// a route, and the receipt fields validateReceipt checks. The audit
// writer calls it per entry, so one malformed entry does not take its
// batch down with it.
func ValidateAccessEntry(e *AccessEntry) error {
	if e == nil {
		return errors.New("access entry: nil")
	}
	if e.PrincipalID == "" || e.Route == "" {
		return errors.New("access entry: principal id and route are required")
	}
	return validateReceipt(e)
}

// validateReceipt checks the receipt fields of an entry as
// AccessLogStore.Append documents: a known decision, a digest, checks
// with ids, known outcomes and reasons, and a decision the trail
// supports (denied needs a FAIL, deferred an UNKNOWN).
func validateReceipt(e *AccessEntry) error {
	if !e.Decision.Valid() {
		return fmt.Errorf("access entry: decision %q is not allowed, denied or deferred", e.Decision)
	}
	if e.OperationDigest == "" {
		return errors.New("access entry: operation digest is required")
	}
	for i, c := range e.Checks {
		switch {
		case c.Check == "":
			return fmt.Errorf("access entry: check %d has no id", i)
		case !c.Outcome.Valid():
			return fmt.Errorf("access entry: check %q has outcome %q", c.Check, c.Outcome)
		case c.Reason == "":
			return fmt.Errorf("access entry: check %q has no reason", c.Check)
		}
	}
	has := func(o AccessOutcome) bool {
		return slices.ContainsFunc(e.Checks, func(c AccessCheck) bool { return c.Outcome == o })
	}
	switch {
	case e.Decision == AccessDecisionDenied && !has(AccessOutcomeFail):
		return errors.New("access entry: a denied entry needs a FAIL check")
	case e.Decision == AccessDecisionDeferred && !has(AccessOutcomeUnknown):
		return errors.New("access entry: a deferred entry needs an UNKNOWN check")
	}
	return nil
}
