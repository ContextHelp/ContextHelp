package leaktest

import (
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// shareCases lists the HTTP read routes of the sharing registry
// (ADR-080 §11): audiences, policies, templates, grants and the
// ceiling, under admin:shares. The fixture holds no audience, policy
// or grant, so the keyed routes answer their not-found path and the
// listings are empty; the ceiling names the owner, who has none.
func shareCases() []Case {
	return []Case{
		get("/share/audiences"),
		getAt("/share/audiences/{audience}", "/share/audiences/"+none),
		get("/share/policies"),
		getAt("/share/policies/{policy}", "/share/policies/"+none),
		get("/share/templates"),
		get("/share/grants"),
		getAt("/principals/{id}/ceiling", "/principals/"+storage.OwnerPrincipal+"/ceiling"),
		// The read audit, under admin:audit: the owner's listing of what
		// the colleague read.
		getAt("/share/audit", "/share/audit?principal="+ColleagueID),
	}
}
