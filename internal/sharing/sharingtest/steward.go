package sharingtest

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// StewardOptions wires a Resolver for steward grants over settings,
// expanding roles into scopes the way the authentication chain does.
func StewardOptions(settings func() sharing.StewardSettings) sharing.StewardOptions {
	return sharing.StewardOptions{
		Settings: settings,
		HoldsRequestSteward: func(roles []string) bool {
			return slices.Contains(authn.ScopesForRoles(roles), authn.ScopeRequestSteward)
		},
	}
}

// StewardRefusalCase is one refusal of an elevation and the HTTP status
// it is answered with; the gRPC answer is its counterpart
// (PermissionDenied for 403, Unavailable for 503).
type StewardRefusalCase struct {
	Refusal *sharing.StewardRefusal
	Status  int
}

// StewardRefusals lists every reason an elevation is refused for, each
// with the status both transports answer it with: denied is 403,
// unavailability is 503.
func StewardRefusals() []StewardRefusalCase {
	denied := []string{
		sharing.StewardReasonGrantUnknown, sharing.StewardReasonNotSteward, sharing.StewardReasonGrantNotActive,
		sharing.StewardReasonGrantExpired, sharing.StewardReasonGrantRevoked, sharing.StewardReasonRoleMissing,
		sharing.StewardReasonScopeChanged, sharing.StewardReasonScopeInvalid, sharing.StewardReasonStewardDisabled,
		sharing.StewardReasonIdentityUnknown, sharing.StewardReasonKeyPolicyInvalid,
	}
	out := make([]StewardRefusalCase, 0, len(denied)+1)
	for _, r := range denied {
		out = append(out, StewardRefusalCase{Refusal: &sharing.StewardRefusal{Reason: r}, Status: 403})
	}
	return append(out, StewardRefusalCase{
		Refusal: &sharing.StewardRefusal{Reason: sharing.StewardReasonGrantUnavailable, Deferred: true}, Status: 503,
	})
}

// GrantSteward writes an approved steward grant for stewardID, which
// must exist in the registry, with scope (its stored form,
// {"policies":[...]}, as sharing.ParseStewardScope reads it) over
// [notBefore, expiresAt), on a fresh approval request, and returns it.
// The grant carries scope's true digest, as an approval writes it.
func GrantSteward(t testing.TB, driver storage.StorageDriver, stewardID, scope string, notBefore, expiresAt time.Time) *storage.StewardGrant {
	t.Helper()
	digest, err := sharing.StewardScopeDigest(json.RawMessage(scope))
	if err != nil {
		t.Fatalf("sharingtest: scope digest: %v", err)
	}
	return GrantStewardWithDigest(t, driver, stewardID, scope, digest, notBefore, expiresAt)
}

// GrantStewardWithDigest is GrantSteward with the stored digest given:
// a digest that is not scope's is a grant whose scope was tampered
// with.
func GrantStewardWithDigest(t testing.TB, driver storage.StorageDriver, stewardID, scope, digest string, notBefore, expiresAt time.Time) *storage.StewardGrant {
	t.Helper()
	ctx := context.Background()
	req := &storage.ApprovalRequest{
		Kind: "steward.grant", Action: "steward.grant.request", RequestedBy: storage.OwnerPrincipal, SubjectID: stewardID,
		Reason: "a long enough reason for a fixture", Payload: json.RawMessage(`{}`), PayloadDigest: "v1:sha256:aa",
		ApprovalsRequired: 1, RequestedAt: notBefore.Add(-time.Hour), ExpiresAt: notBefore.Add(72 * time.Hour),
	}
	if err := driver.Approvals().Create(ctx, req); err != nil {
		t.Fatalf("sharingtest: steward request: %v", err)
	}
	g := &storage.StewardGrant{
		RequestID: req.ID, StewardID: stewardID, Scope: json.RawMessage(scope), ScopeDigest: digest,
		NotBefore: notBefore, ExpiresAt: expiresAt, ApprovedAt: notBefore.Add(-time.Hour),
	}
	if err := driver.Sharing().StewardGrants().Create(ctx, g); err != nil {
		t.Fatalf("sharingtest: steward grant: %v", err)
	}
	return g
}
