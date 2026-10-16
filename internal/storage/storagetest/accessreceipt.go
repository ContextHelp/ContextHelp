package storagetest

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// accessDigest is a well-formed operation digest for the cases that do
// not look at it.
const accessDigest = storage.DigestPrefix + "0000000000000000000000000000000000000000000000000000000000000000"

// receiptTrail is a denied request's trail: it met authn, failed scope,
// and never reached the later checks.
func receiptTrail() []storage.AccessCheck {
	return []storage.AccessCheck{
		{Check: "authn.api_key", Outcome: storage.AccessOutcomePass, Reason: "authenticated"},
		{Check: "scope", Outcome: storage.AccessOutcomeFail, Reason: "scope_missing"},
	}
}

// sharingConfAccessLogReceipt pins that the decision, the ordered check
// trail and the digest read back as written, an empty trail reads as an
// empty list, and retention pruning leaves the receipt alone.
func sharingConfAccessLogReceipt(t *testing.T, drv storage.StorageDriver) {
	denied := &storage.AccessEntry{
		At: sharingT0, PrincipalID: "prn_rcpt_a", IdentityID: "idn_rcpt", Route: "GET /api/v1/share/audit",
		Decision: storage.AccessDecisionDenied, Checks: receiptTrail(), OperationDigest: accessDigest, PolicyVersions: "v1",
	}
	bare := &storage.AccessEntry{
		At: sharingT0.Add(time.Minute), PrincipalID: "prn_rcpt_a", Route: routeFind,
		Decision: storage.AccessDecisionAllowed, OperationDigest: accessDigest,
	}
	if err := drv.Sharing().AccessLog().Append(context.Background(), []*storage.AccessEntry{denied, bare}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got := queryAccessLog(t, drv, storage.AccessFilter{PrincipalID: "prn_rcpt_a"})
	if len(got) != 2 {
		t.Fatalf("Query = %+v, want both entries", got)
	}
	if got[1].Decision != storage.AccessDecisionDenied || got[1].OperationDigest != accessDigest {
		t.Fatalf("read back %+v, want the decision and digest", got[1])
	}
	if !slices.Equal(got[1].Checks, receiptTrail()) {
		t.Fatalf("checks = %+v, want the trail in the order it ran", got[1].Checks)
	}
	if got[0].Checks == nil || len(got[0].Checks) != 0 {
		t.Fatalf("empty trail read back as %#v, want an empty list", got[0].Checks)
	}
	if _, err := drv.Sharing().AccessLog().Prune(context.Background(), sharingT0.Add(time.Hour)); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	after := queryAccessLog(t, drv, storage.AccessFilter{PrincipalID: "prn_rcpt_a"})
	if len(after) != 2 || !slices.Equal(after[1].Checks, receiptTrail()) || after[1].Decision != storage.AccessDecisionDenied {
		t.Fatalf("after prune = %+v, want the receipt kept", after)
	}
}

// sharingConfAccessLogDecision pins the decision filter.
func sharingConfAccessLogDecision(t *testing.T, drv storage.StorageDriver) {
	mk := func(i int, d storage.AccessDecision, checks ...storage.AccessCheck) *storage.AccessEntry {
		return &storage.AccessEntry{
			At: sharingT0.Add(time.Duration(i) * time.Minute), PrincipalID: "prn_dec", Route: routeObjects,
			Decision: d, Checks: checks, OperationDigest: accessDigest,
		}
	}
	err := drv.Sharing().AccessLog().Append(context.Background(), []*storage.AccessEntry{
		mk(0, storage.AccessDecisionAllowed),
		mk(1, storage.AccessDecisionDenied, storage.AccessCheck{Check: "scope", Outcome: storage.AccessOutcomeFail, Reason: "scope_missing"}),
		mk(2, storage.AccessDecisionDeferred, storage.AccessCheck{Check: "backstop", Outcome: storage.AccessOutcomeUnknown, Reason: "response_unreadable"}),
		mk(3, storage.AccessDecisionDenied, storage.AccessCheck{Check: "route_access", Outcome: storage.AccessOutcomeFail, Reason: "owner_only"}),
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	denied := queryAccessLog(t, drv, storage.AccessFilter{PrincipalID: "prn_dec", Decision: storage.AccessDecisionDenied})
	if len(denied) != 2 || denied[0].Checks[0].Reason != "owner_only" || denied[1].Checks[0].Reason != "scope_missing" {
		t.Fatalf("Query(denied) = %+v, want the two refusals newest first", denied)
	}
	deferred := queryAccessLog(t, drv, storage.AccessFilter{PrincipalID: "prn_dec", Decision: storage.AccessDecisionDeferred})
	if len(deferred) != 1 || deferred[0].Checks[0].Outcome != storage.AccessOutcomeUnknown {
		t.Fatalf("Query(deferred) = %+v", deferred)
	}
	if all := queryAccessLog(t, drv, storage.AccessFilter{PrincipalID: "prn_dec"}); len(all) != 4 {
		t.Fatalf("Query(no decision) = %d entries, want all 4", len(all))
	}
}

// sharingConfAccessLogReceiptRefusals pins what Append refuses in a
// receipt, and that a refusal writes nothing.
func sharingConfAccessLogReceiptRefusals(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	st := drv.Sharing().AccessLog()
	pass := storage.AccessCheck{Check: "scope", Outcome: storage.AccessOutcomePass, Reason: "scope_held"}
	fail := storage.AccessCheck{Check: "scope", Outcome: storage.AccessOutcomeFail, Reason: "scope_missing"}
	unknown := storage.AccessCheck{Check: "backstop", Outcome: storage.AccessOutcomeUnknown, Reason: "response_unreadable"}
	base := func(d storage.AccessDecision, checks ...storage.AccessCheck) *storage.AccessEntry {
		return &storage.AccessEntry{PrincipalID: "prn_rcpt_bad", Route: routeObjects, Decision: d, Checks: checks, OperationDigest: accessDigest}
	}
	noDigest := base(storage.AccessDecisionAllowed, pass)
	noDigest.OperationDigest = ""
	for name, e := range map[string]*storage.AccessEntry{
		"no decision":                 base("", pass),
		"unknown decision":            base("maybe", pass),
		"no digest":                   noDigest,
		"denied without a FAIL":       base(storage.AccessDecisionDenied, pass),
		"deferred without an UNKNOWN": base(storage.AccessDecisionDeferred, fail),
		"check without id":            base(storage.AccessDecisionAllowed, storage.AccessCheck{Outcome: storage.AccessOutcomePass, Reason: "x"}),
		"check without reason":        base(storage.AccessDecisionAllowed, storage.AccessCheck{Check: "scope", Outcome: storage.AccessOutcomePass}),
		"check with bad outcome":      base(storage.AccessDecisionAllowed, storage.AccessCheck{Check: "scope", Outcome: "pass", Reason: "x"}),
	} {
		if err := st.Append(ctx, []*storage.AccessEntry{e}); err == nil {
			t.Fatalf("Append(%s) must fail", name)
		}
	}
	for name, e := range map[string]*storage.AccessEntry{
		"denied with a FAIL":       base(storage.AccessDecisionDenied, pass, fail),
		"deferred with an UNKNOWN": base(storage.AccessDecisionDeferred, pass, unknown),
	} {
		if err := st.Append(ctx, []*storage.AccessEntry{e}); err != nil {
			t.Fatalf("Append(%s) = %v, want it stored", name, err)
		}
	}
	if got := queryAccessLog(t, drv, storage.AccessFilter{PrincipalID: "prn_rcpt_bad"}); len(got) != 2 {
		t.Fatalf("refused entries wrote rows: %+v", got)
	}
}
