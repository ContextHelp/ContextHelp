package auth

// CodeCredentialForOtherPrincipal is the stable error code of a refused
// credential delivery: a caller other than the owner asking for a
// working credential (an API key secret, a sign-in link, a bound
// identity) for a principal other than itself. It is the same on every
// surface that reports it (HTTP error envelope, CLI output).
const CodeCredentialForOtherPrincipal = "CREDENTIAL_FOR_OTHER_PRINCIPAL"

// CredentialForOtherPrincipalMessage names the reason for the refusal.
const CredentialForOtherPrincipalMessage = "only the owner can issue a credential for another principal: " +
	"administering an instance is not access to its data, and a credential for a principal reaches that principal's data"

// MayIssueCredentialFor reports whether by may receive a working
// credential for principalID: the owner for any principal, any caller
// for itself. Administering an instance is not access to its data
// (ADR-082 §10, ADR-083 §10), and a credential for a principal is that principal's
// view of the data, so an administrator never receives one for another
// principal. Removing, revoking and suspending are not deliveries and
// stay with the scopes.
func MayIssueCredentialFor(by *Principal, principalID string) bool {
	return by.IsOwner() || by.IsSelf(principalID)
}
