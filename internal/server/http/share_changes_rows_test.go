package http

import (
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// httpRow is one row of ADR-083 §10 B's route table, driven over HTTP:
// the state it is reached from, the call, what a non-owner
// administrator is answered, and what the owner is.
type httpRow struct {
	name string
	// setup puts the registry in the state the call is made against.
	setup func(f *shareFx)
	// call is the request: method, path under /api/v1 and body.
	call func(f *shareFx) (method, path string, body any)
	// adminStatus is the answer to a non-owner administrator;
	// http.StatusAccepted is a pending request of action. ownerStatus
	// is the owner's: the change applied at once, never pending.
	adminStatus, ownerStatus int
	action                   string
	// benefits is true when dana, who holds approve:shares, is a
	// beneficiary of the change (a pending row only).
	benefits bool
	// unchanged is true when the change leaves every row as it was.
	unchanged bool
}

func (r httpRow) pending() bool { return r.adminStatus == http.StatusAccepted }

func httpRows() []httpRow {
	later := map[string]any{"expires_in": 7200}
	return []httpRow{
		{
			name: "POST /share/audiences",
			call: func(*shareFx) (string, string, any) {
				return http.MethodPost, "/share/audiences", map[string]any{"name": "partners"}
			},
			adminStatus: http.StatusCreated, ownerStatus: http.StatusCreated,
		},
		{
			name:  "PUT member, new membership",
			setup: func(f *shareFx) { f.seedTeam() },
			call: func(f *shareFx) (string, string, any) {
				return http.MethodPut, "/share/audiences/team/members/" + f.dana.id, map[string]any{}
			},
			adminStatus: http.StatusAccepted, ownerStatus: http.StatusOK, action: sharing.ActionMemberPut, benefits: true,
		},
		{
			name: "PUT member, expiry later",
			setup: func(f *shareFx) {
				f.seedTeam()
				f.mustOwner(http.StatusOK, http.MethodPut, "/share/audiences/team/members/"+f.dana.id, map[string]any{"expires_in": 3600})
			},
			call: func(f *shareFx) (string, string, any) {
				return http.MethodPut, "/share/audiences/team/members/" + f.dana.id, later
			},
			adminStatus: http.StatusAccepted, ownerStatus: http.StatusOK, action: sharing.ActionMemberPut, benefits: true,
		},
		{
			name: "PUT member, expiry earlier",
			setup: func(f *shareFx) {
				f.seedTeam()
				f.mustOwner(http.StatusOK, http.MethodPut, "/share/audiences/team/members/"+f.dana.id, map[string]any{"expires_in": 7200})
			},
			call: func(f *shareFx) (string, string, any) {
				return http.MethodPut, "/share/audiences/team/members/" + f.dana.id, map[string]any{"expires_in": 3600}
			},
			adminStatus: http.StatusOK, ownerStatus: http.StatusOK,
		},
		{
			name: "PUT member, same expiry",
			setup: func(f *shareFx) {
				f.seedTeam()
				f.mustOwner(http.StatusOK, http.MethodPut, "/share/audiences/team/members/"+f.dana.id, map[string]any{"expires_at": "2031-01-01T00:00:00Z"})
			},
			call: func(f *shareFx) (string, string, any) {
				return http.MethodPut, "/share/audiences/team/members/" + f.dana.id, map[string]any{"expires_at": "2031-01-01T00:00:00Z"}
			},
			adminStatus: http.StatusOK, ownerStatus: http.StatusOK, unchanged: true,
		},
		{
			name: "DELETE member",
			setup: func(f *shareFx) {
				f.seedTeam()
				f.mustOwner(http.StatusOK, http.MethodPut, "/share/audiences/team/members/"+f.dana.id, map[string]any{})
			},
			call: func(f *shareFx) (string, string, any) {
				return http.MethodDelete, "/share/audiences/team/members/" + f.dana.id, nil
			},
			adminStatus: http.StatusNoContent, ownerStatus: http.StatusNoContent,
		},
		{
			name: "POST /share/policies",
			call: func(*shareFx) (string, string, any) {
				return http.MethodPost, "/share/policies", map[string]any{"document": partnerDoc}
			},
			adminStatus: http.StatusCreated, ownerStatus: http.StatusCreated,
		},
		{
			name: "POST /share/templates/{name}",
			call: func(*shareFx) (string, string, any) {
				return http.MethodPost, "/share/templates/" + sharing.TemplatePartners, nil
			},
			adminStatus: http.StatusCreated, ownerStatus: http.StatusCreated,
		},
		{
			name:  "PUT policy, no grant in force",
			setup: func(f *shareFx) { f.seedTeam() },
			call: func(*shareFx) (string, string, any) {
				return http.MethodPut, "/share/policies/base", map[string]any{"document": widerDoc}
			},
			adminStatus: http.StatusOK, ownerStatus: http.StatusOK,
		},
		{
			name:  "PUT policy, widened with a grant in force",
			setup: func(f *shareFx) { f.seedGranted() },
			call: func(*shareFx) (string, string, any) {
				return http.MethodPut, "/share/policies/base", map[string]any{"document": widerDoc}
			},
			adminStatus: http.StatusAccepted, ownerStatus: http.StatusOK, action: sharing.ActionPolicyUpdate, benefits: true,
		},
		{
			name:  "PUT policy, narrowed with a grant in force",
			setup: func(f *shareFx) { f.seedGranted() },
			call: func(*shareFx) (string, string, any) {
				return http.MethodPut, "/share/policies/base", map[string]any{"document": narrowDoc}
			},
			adminStatus: http.StatusOK, ownerStatus: http.StatusOK,
		},
		{
			name: "POST /share/grants",
			setup: func(f *shareFx) {
				f.seedTeam()
				f.mustOwner(http.StatusOK, http.MethodPut, "/share/audiences/team/members/"+f.dana.id, map[string]any{})
			},
			call: func(*shareFx) (string, string, any) {
				return http.MethodPost, "/share/grants", map[string]any{"audience": "team", "policy": "base"}
			},
			adminStatus: http.StatusAccepted, ownerStatus: http.StatusCreated, action: sharing.ActionGrantCreate, benefits: true,
		},
		{
			name:  "DELETE /share/grants/{grant}",
			setup: func(f *shareFx) { f.seedGranted() },
			call: func(f *shareFx) (string, string, any) {
				grants := f.bundle.svc.Store(ownerCtx()).Sharing().Grants()
				list, err := grants.List(ownerCtx(), storage.GrantFilter{})
				if err != nil || len(list) != 1 {
					f.t.Fatalf("seeded grants: %v, %v", list, err)
				}
				return http.MethodDelete, "/share/grants/" + list[0].ID, nil
			},
			adminStatus: http.StatusNoContent, ownerStatus: http.StatusNoContent,
		},
		{
			name: "PUT ceiling, lowered",
			setup: func(f *shareFx) {
				f.mustOwner(http.StatusOK, http.MethodPut, "/principals/"+f.dana.id+"/ceiling", map[string]any{"deny_classes": []string{"personal"}})
			},
			call: func(f *shareFx) (string, string, any) {
				return http.MethodPut, "/principals/" + f.dana.id + "/ceiling", map[string]any{"deny_classes": []string{}}
			},
			adminStatus: http.StatusAccepted, ownerStatus: http.StatusOK, action: sharing.ActionCeilingSet, benefits: true,
		},
		{
			name: "PUT ceiling, raised",
			call: func(f *shareFx) (string, string, any) {
				return http.MethodPut, "/principals/" + f.dana.id + "/ceiling", map[string]any{"deny_classes": []string{"personal"}}
			},
			adminStatus: http.StatusOK, ownerStatus: http.StatusOK,
		},
		{
			name: "PUT ceiling, unchanged",
			setup: func(f *shareFx) {
				f.mustOwner(http.StatusOK, http.MethodPut, "/principals/"+f.dana.id+"/ceiling", map[string]any{"deny_classes": []string{"personal"}})
			},
			call: func(f *shareFx) (string, string, any) {
				return http.MethodPut, "/principals/" + f.dana.id + "/ceiling", map[string]any{"deny_classes": []string{"personal"}}
			},
			adminStatus: http.StatusOK, ownerStatus: http.StatusOK, unchanged: true,
		},
		{
			name:  "PUT key policy, unbound to bound",
			setup: func(f *shareFx) { f.seedTeam() },
			call: func(f *shareFx) (string, string, any) {
				return http.MethodPut, "/share/keys/" + f.danaKeyID() + "/policy", map[string]any{"policy": "base"}
			},
			adminStatus: http.StatusOK, ownerStatus: http.StatusOK,
		},
		{
			name:  "PUT key policy, bound to unbound",
			setup: func(f *shareFx) { f.seedTeam(); f.bindDanaKey("base") },
			call: func(f *shareFx) (string, string, any) {
				return http.MethodPut, "/share/keys/" + f.danaKeyID() + "/policy", map[string]any{"policy": ""}
			},
			adminStatus: http.StatusAccepted, ownerStatus: http.StatusOK, action: sharing.ActionKeyPolicyBind, benefits: true,
		},
		{
			name: "PUT key policy, rebound wider",
			setup: func(f *shareFx) {
				f.seedTeam()
				f.mustOwner(http.StatusCreated, http.MethodPost, "/share/policies", map[string]any{"document": widerDoc2})
				f.bindDanaKey("base")
			},
			call: func(f *shareFx) (string, string, any) {
				return http.MethodPut, "/share/keys/" + f.danaKeyID() + "/policy", map[string]any{"policy": "wide"}
			},
			adminStatus: http.StatusAccepted, ownerStatus: http.StatusOK, action: sharing.ActionKeyPolicyBind, benefits: true,
		},
		{
			name: "PUT key policy, rebound narrower",
			setup: func(f *shareFx) {
				f.seedTeam()
				f.mustOwner(http.StatusCreated, http.MethodPost, "/share/policies", map[string]any{"document": narrowDoc2})
				f.bindDanaKey("base")
			},
			call: func(f *shareFx) (string, string, any) {
				return http.MethodPut, "/share/keys/" + f.danaKeyID() + "/policy", map[string]any{"policy": "narrow"}
			},
			adminStatus: http.StatusOK, ownerStatus: http.StatusOK,
		},
	}
}

// Policies the key-rebind rows bind to.
const (
	widerDoc2  = `{"name":"wide","data":"type==contact","fields":{"contact":["title","org","email"]}}`
	narrowDoc2 = `{"name":"narrow","data":"type==contact","fields":{"contact":["title"]}}`
)
