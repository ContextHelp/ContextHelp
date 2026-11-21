package http

import (
	"net/http"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

// CodeCredentialForOtherPrincipal is a route that would hand the
// caller a working credential (a key secret, a sign-in link, a bound
// identity) for a principal other than itself, reached by a caller
// other than the owner.
const CodeCredentialForOtherPrincipal = authn.CodeCredentialForOtherPrincipal

// guardCredentialDelivery is the one decision point for a credential
// asked for another principal: it lets the owner, and any caller for
// itself, through, and answers anyone else, writing the answer.
// Administering an instance is not access to its data (ADR-082 §10,
// ADR-083 §10 A): a key, a sign-in link or an identity on a login the
// caller controls would let it act as that principal and read what
// that principal is shown, so the admin's response never carries one.
//
// The answer today is a refusal, 403 CREDENTIAL_FOR_OTHER_PRINCIPAL,
// recorded on the request's receipt as route_access FAIL with the
// stable reason sharing.ReasonCredentialOwnerOnly.
// ADR-083 §10 A replaces it with an approval request (202) on the
// shared approval mechanism, the credential minted only in the
// approver's response; that change lands here and nowhere else.
//
// The routes it guards are the mint of a key, the mint of a sign-in link and the bind of an identity, and,
// through guardInviteDelivery, the mint of an invite; removal,
// revocation, suspension and the metadata reads are not deliveries.
func guardCredentialDelivery(w http.ResponseWriter, r *http.Request, principalID string) bool {
	if authn.MayIssueCredentialFor(caller(r), principalID) {
		return true
	}
	sharing.ReceiptFrom(r.Context()).Refuse(sharing.CheckRouteAccess, sharing.ReasonCredentialOwnerOnly)
	WriteError(w, http.StatusForbidden, CodeCredentialForOtherPrincipal, authn.CredentialForOtherPrincipalMessage)
	return false
}

// guardInviteDelivery is guardCredentialDelivery for an invite: a
// credential for a principal that does not exist yet, which redeeming
// creates (in the invite's audience when it names one) and signs in
// as. No caller is that principal, so only the owner passes (ADR-083
// §10 A, credential.invite).
func guardInviteDelivery(w http.ResponseWriter, r *http.Request) bool {
	return guardCredentialDelivery(w, r, "")
}
