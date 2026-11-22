package steward_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/steward"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// A valid request is filed pending with the steward as its subject, N
// frozen from approvals.steward.required, and the payload frozen: the
// compiled scope with its digest, a window in the stores' resolution.
func TestFilingFreezesTheScopeAndWindow(t *testing.T) {
	f := newFixture(t)
	f.required = 2
	req := f.request()
	req.NotBefore = req.NotBefore.Add(123456789 * time.Nanosecond)
	r, err := f.file(req)
	require.NoError(t, err)

	assert.Equal(t, steward.KindName, r.Kind)
	assert.Equal(t, steward.ActionRequest, r.Action)
	assert.Equal(t, f.requester.ID, r.RequestedBy)
	assert.Equal(t, f.stewardP.ID, r.SubjectID, "the steward is the subject, so it can never approve")
	assert.Equal(t, 2, r.ApprovalsRequired)
	assert.Equal(t, reason, r.Reason)
	assert.Equal(t, storage.ApprovalRequested, r.Status)

	p, err := steward.DecodePayload(r.Payload)
	require.NoError(t, err)
	assert.Equal(t, f.stewardP.ID, p.StewardID)
	assert.Equal(t, "CASE-117", p.MatterRef)
	digest, err := sharing.StewardScopeDigest(p.Scope)
	require.NoError(t, err)
	assert.Equal(t, digest, p.ScopeDigest)
	assert.Equal(t, req.NotBefore.Truncate(time.Millisecond), p.NotBefore, "times are frozen in the stores' resolution")
	assert.Equal(t, time.UTC, p.ExpiresAt.Location())
	_, err = sharing.ParseStewardScope(p.Scope)
	require.NoError(t, err)

	f.required = 1
	later := f.mustFile(f.request())
	assert.Equal(t, 1, later.ApprovalsRequired)
	assert.Equal(t, 2, f.stored(r.ID).ApprovalsRequired, "lowering the setting never weakens a pending request")
}

// A filing without a not_before opens at the moment of filing.
func TestFilingDefaultsNotBeforeToNow(t *testing.T) {
	f := newFixture(t)
	req := f.request()
	req.NotBefore = time.Time{}
	r := f.mustFile(req)
	p, err := steward.DecodePayload(r.Payload)
	require.NoError(t, err)
	assert.Equal(t, f.clock.now(), p.NotBefore)
}

// steward.enabled off: no steward.grant request can be filed.
func TestFilingIsRefusedWhenStewardAccessIsOff(t *testing.T) {
	f := newFixture(t)
	f.settings.Enabled = false
	_, err := f.file(f.request())
	requireCode(t, err, steward.CodeDisabled)
	pending, err := f.drv.Approvals().List(t.Context(), storage.ApprovalFilter{Kind: steward.KindName})
	require.NoError(t, err)
	assert.Empty(t, pending, "nothing is filed")
}

// The requester must hold request:steward; the owner may not request;
// a reason must be as long as steward.min_reason_chars says.
func TestFilingRequesterRules(t *testing.T) {
	f := newFixture(t)

	_, err := f.svc.File(t.Context(), f.filing(f.plain, f.request()))
	requireCode(t, err, steward.CodeRequesterRole)
	_, err = f.svc.File(t.Context(), f.filing(f.alice, f.request()))
	requireCode(t, err, steward.CodeRequesterRole)

	_, err = f.svc.File(t.Context(), f.filing(f.owner, f.request()))
	requireCode(t, err, approval.CodeOwnerCannotRequest)

	// A device holding request:steward is no requester: only a person is.
	device := &auth.Principal{ID: "prn_device", Kind: storage.PrincipalKindDevice, Roles: []string{auth.RoleStewardRequester}, Scopes: []auth.Scope{auth.ScopeRequestSteward}}
	_, err = f.svc.File(t.Context(), f.filing(device, f.request()))
	requireCode(t, err, steward.CodeRequesterKind)

	short := f.filing(f.requester, f.request())
	short.Reason = "legal hold"
	_, err = f.svc.File(t.Context(), short)
	requireCode(t, err, approval.CodeReasonTooShort)

	// A requester may request for itself.
	self := f.request()
	self.StewardID = f.requester.ID
	filing := f.filing(f.requester, self)
	_, err = f.svc.File(t.Context(), filing)
	require.NoError(t, err)
}

// The steward must be an active person who holds request:steward, and
// is the filing's subject.
func TestFilingStewardRules(t *testing.T) {
	f := newFixture(t)
	fileFor := func(id string) error {
		req := f.request()
		req.StewardID = id
		_, err := f.svc.File(t.Context(), f.filing(f.requester, req))
		return err
	}
	requireCode(t, fileFor(f.plain.ID), steward.CodeStewardIneligible)
	requireCode(t, fileFor(f.alice.ID), steward.CodeStewardIneligible)
	requireCode(t, fileFor("prn_nobody"), steward.CodeStewardIneligible)
	requireCode(t, fileFor(storage.OwnerPrincipal), steward.CodeStewardIneligible)

	dev := &storage.Principal{ID: "prn_device", Kind: storage.PrincipalKindDevice, DisplayName: "device", Roles: []string{auth.RoleReader, auth.RoleStewardRequester}}
	require.NoError(t, f.drv.Auth().Principals().Create(t.Context(), dev))
	requireCode(t, fileFor(dev.ID), steward.CodeStewardIneligible)

	require.NoError(t, f.drv.Auth().Principals().Suspend(t.Context(), f.stewardP.ID))
	requireCode(t, fileFor(f.stewardP.ID), steward.CodeStewardIneligible)

	// The subject is the steward, and nobody else.
	other := f.filing(f.requester, f.request())
	other.SubjectID = f.alice.ID
	_, err := f.svc.File(t.Context(), other)
	requireCode(t, err, approval.CodeInvalid)
}

// Window rules: it must end after it starts and after now, and may not
// exceed steward.max_duration, at the filing's boundaries.
func TestFilingWindowRules(t *testing.T) {
	f := newFixture(t)
	now := f.clock.now()
	cap := f.settings.MaxDuration
	for name, c := range map[string]struct {
		nb, exp time.Time
		code    string
	}{
		"exactly the cap":         {now, now.Add(cap), ""},
		"one millisecond over":    {now, now.Add(cap + time.Millisecond), steward.CodeWindowTooLong},
		"far over":                {now, now.Add(100 * cap), steward.CodeWindowTooLong},
		"empty window":            {now, now, steward.CodeWindowInvalid},
		"inverted window":         {now, now.Add(-time.Hour), steward.CodeWindowInvalid},
		"empty, in the future":    {now.Add(24 * time.Hour), now.Add(24 * time.Hour), steward.CodeWindowInvalid},
		"inverted, in the future": {now.Add(48 * time.Hour), now.Add(24 * time.Hour), steward.CodeWindowInvalid},
		"already over":            {now.Add(-2 * time.Hour), now.Add(-time.Hour), steward.CodeWindowInvalid},
		"opens later":             {now.Add(24 * time.Hour), now.Add(48 * time.Hour), ""},
		"the cap counts the span": {now.Add(24 * time.Hour), now.Add(24*time.Hour + cap), ""},
		"opened in the past":      {now.Add(-time.Hour), now.Add(time.Hour), ""},
		"no end":                  {now, time.Time{}, steward.CodeWindowInvalid},
	} {
		req := f.request()
		req.NotBefore, req.ExpiresAt = c.nb, c.exp
		_, err := f.file(req)
		if c.code == "" {
			assert.NoError(t, err, name)
		} else {
			assert.Equal(t, c.code, approval.CodeOf(err), "%s: %v", name, err)
		}
	}
}

// Scope rules: exactly one of scope and as_audience; a valid policy
// spec; no mcp, events, or export or withheld classes the instance
// does not allow.
func TestFilingScopeRules(t *testing.T) {
	f := newFixture(t)
	file := func(mutate func(*steward.Request)) string {
		req := f.request()
		mutate(&req)
		_, err := f.file(req)
		return approval.CodeOf(err)
	}
	scope := func(spec string) func(*steward.Request) {
		return func(r *steward.Request) { r.Scope = json.RawMessage(spec) }
	}
	for name, mutate := range map[string]func(*steward.Request){
		"neither scope nor audience": func(r *steward.Request) { r.Scope = nil },
		"both":                       func(r *steward.Request) { r.AsAudience = "partners" },
		"unparseable":                scope(`{"data":"type=="}`),
		"unknown key":                scope(`{"data":"type==contact","exclude_clases":["x"]}`),
		"no data filter":             scope(`{"fields":{"contact":["title"]}}`),
		"mcp":                        scope(`{"data":"type==contact","capabilities":{"mcp":true}}`),
		"events":                     scope(`{"data":"type==contact","capabilities":{"events":true}}`),
		"export, not allowed":        scope(`{"data":"type==contact","capabilities":{"export":true}}`),
		"include, not allowed":       scope(`{"data":"type==contact","include_classes":["personal"]}`),
		"redact":                     scope(`{"data":"type==contact","redact":{"mask":["email"]}}`),
		"the stored form":            scope(`{"policies":[{"data":"type==contact"}]}`),
	} {
		assert.Equal(t, steward.CodeScopeInvalid, file(mutate), name)
	}

	// With the instance's switches on, the same asks file; an include of
	// a class that is not withheld still does not.
	f.settings.AllowExport, f.settings.AllowWithheld = true, true
	assert.Empty(t, file(scope(`{"data":"type==contact","capabilities":{"export":true}}`)))
	assert.Empty(t, file(scope(`{"data":"type==contact","include_classes":["personal"]}`)))
	assert.Equal(t, steward.CodeScopeInvalid, file(scope(`{"data":"type==contact","include_classes":["financial"]}`)))
	assert.Equal(t, steward.CodeScopeInvalid, file(scope(`{"data":"type==contact","capabilities":{"mcp":true}}`)), "mcp is never allowed")
}

// A renewal names its predecessor, which must be the same steward's.
func TestFilingRenewalRules(t *testing.T) {
	f := newFixture(t)
	first := f.approved(f.request())

	req := f.request()
	req.RenewsID = first.ID
	r, err := f.file(req)
	require.NoError(t, err)
	p, err := steward.DecodePayload(r.Payload)
	require.NoError(t, err)
	assert.Equal(t, first.ID, p.RenewsID)

	for name, id := range map[string]string{"unknown": "stg_nothing"} {
		req := f.request()
		req.RenewsID = id
		_, err := f.file(req)
		assert.Equal(t, steward.CodeRenewalInvalid, approval.CodeOf(err), name)
	}

	// Another steward's grant is not this one's to renew.
	f.setRoles(f.bob.ID, auth.RoleReader, auth.RoleStewardRequester)
	elseReq := f.request()
	elseReq.StewardID = f.bob.ID
	elseReq.RenewsID = first.ID
	_, err = f.file(elseReq)
	assert.Equal(t, steward.CodeRenewalInvalid, approval.CodeOf(err))
}
