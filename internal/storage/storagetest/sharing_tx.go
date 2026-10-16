package storagetest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// sharingConfInTx pins the transactional seam of the sharing registry
// (ADR-083 §10 B): every registry read and write, and the audit log,
// called with the context InTx hands out, belong to its transaction.
// A change is classified and written in one transaction only if each of
// them joins it.
func sharingConfInTx(t *testing.T, drv storage.StorageDriver) {
	t.Run("commit", func(t *testing.T) { sharingTxCommit(t, drv) })
	t.Run("rollback", func(t *testing.T) { sharingTxRollback(t, drv) })
	t.Run("nested joins", func(t *testing.T) { sharingTxNested(t, drv) })
}

// sharingTxWrites performs every registry write in ctx's transaction
// and reads each back through it, returning the rows it created.
func sharingTxWrites(ctx context.Context, t *testing.T, drv storage.StorageDriver, tag string, f sharingFixture, identity string) (aud *storage.Audience, pol *storage.SharePolicy, grant *storage.ShareGrant, err error) {
	t.Helper()
	st := drv.Sharing()
	fail := func(what string, e error) error { return errors.New(what + ": " + e.Error()) }

	aud = &storage.Audience{Name: "txa-" + tag, CreatedAt: sharingT0}
	if err = st.Audiences().Create(ctx, aud); err != nil {
		return nil, nil, nil, fail("create audience", err)
	}
	if got, e := st.Audiences().Get(ctx, aud.ID); e != nil || got.Name != aud.Name {
		return nil, nil, nil, errors.New("the transaction does not read its own audience")
	}
	if err = st.Audiences().AddMember(ctx, &storage.AudienceMember{AudienceID: aud.ID, PrincipalID: f.principal.ID, AddedBy: storage.OwnerPrincipal, AddedAt: sharingT0}); err != nil {
		return nil, nil, nil, fail("add member", err)
	}
	if ms, e := st.Audiences().ListMembers(ctx, aud.ID); e != nil || len(ms) != 1 {
		return nil, nil, nil, errors.New("the transaction does not read its own membership")
	}
	pol = &storage.SharePolicy{Name: "txp-" + tag, Spec: json.RawMessage(`{"data":"type==note"}`), UpdatedAt: sharingT0}
	if err = st.Policies().Create(ctx, pol); err != nil {
		return nil, nil, nil, fail("create policy", err)
	}
	if upd, e := st.Policies().Update(ctx, pol.ID, json.RawMessage(`{"data":"type==note;tag==a"}`), sharingT0.Add(time.Minute)); e != nil || upd.Version != 2 {
		return nil, nil, nil, errors.New("the transaction does not read its own policy update")
	}
	grant = &storage.ShareGrant{AudienceID: aud.ID, PolicyID: pol.ID, CreatedBy: storage.OwnerPrincipal, CreatedAt: sharingT0}
	if err = st.Grants().Create(ctx, grant); err != nil {
		return nil, nil, nil, fail("create grant", err)
	}
	if gs, e := st.Grants().List(ctx, storage.GrantFilter{AudienceID: aud.ID}); e != nil || len(gs) != 1 {
		return nil, nil, nil, errors.New("the transaction does not read its own grant")
	}
	if err = st.SetDenyClasses(ctx, f.principal.ID, []string{storage.ClassPersonal}); err != nil {
		return nil, nil, nil, fail("set ceiling", err)
	}
	if got, e := st.DenyClasses(ctx, f.principal.ID); e != nil || len(got) != 1 {
		return nil, nil, nil, errors.New("the transaction does not read its own ceiling")
	}
	if err = st.BindIdentityPolicy(ctx, identity, f.policy.ID); err != nil {
		return nil, nil, nil, fail("bind key policy", err)
	}
	if got, e := st.IdentityPolicy(ctx, identity); e != nil || got != f.policy.ID {
		return nil, nil, nil, errors.New("the transaction does not read its own key binding")
	}
	if err = txAudit(ctx, drv, "conformance.share.tx."+tag); err != nil {
		return nil, nil, nil, fail("audit", err)
	}
	return aud, pol, grant, nil
}

func sharingTxCommit(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newSharingFixture(t, drv, "tx-commit")
	id := newIdentity(t, drv.Auth(), f.principal.ID, "key-tx-commit", sharingT0)
	st := drv.Sharing()
	var aud *storage.Audience
	var pol *storage.SharePolicy
	if err := st.InTx(ctx, func(ctx context.Context) error {
		var err error
		aud, pol, _, err = sharingTxWrites(ctx, t, drv, "commit", f, id.ID)
		return err
	}); err != nil {
		t.Fatalf("InTx: %v", err)
	}
	if _, err := st.Audiences().Get(ctx, aud.ID); err != nil {
		t.Errorf("committed audience: %v", err)
	}
	if ms, _ := st.Audiences().ListMembers(ctx, aud.ID); len(ms) != 1 {
		t.Errorf("committed members = %d, want 1", len(ms))
	}
	if got, _ := st.Policies().Get(ctx, pol.ID); got == nil || got.Version != 2 {
		t.Errorf("committed policy = %+v, want version 2", got)
	}
	if gs, _ := st.Grants().List(ctx, storage.GrantFilter{AudienceID: aud.ID}); len(gs) != 1 {
		t.Errorf("committed grants = %d, want 1", len(gs))
	}
	if got, _ := st.DenyClasses(ctx, f.principal.ID); len(got) != 1 {
		t.Errorf("committed ceiling = %v, want one class", got)
	}
	if got, _ := st.IdentityPolicy(ctx, id.ID); got != f.policy.ID {
		t.Errorf("committed key binding = %q, want %s", got, f.policy.ID)
	}
	if n := len(auditOf(t, drv, "conformance.share.tx.commit")); n != 1 {
		t.Errorf("committed audit entries: %d, want 1", n)
	}
}

// A transaction that fails leaves nothing behind: no row of the
// registry, no audit entry. A write that ignored the transaction would
// survive here (or, on SQLite, wait for the write lock the transaction
// holds and fail).
func sharingTxRollback(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newSharingFixture(t, drv, "tx-rollback")
	id := newIdentity(t, drv.Auth(), f.principal.ID, "key-tx-rollback", sharingT0)
	st := drv.Sharing()
	boom := errors.New("boom")
	var aud *storage.Audience
	var pol *storage.SharePolicy
	var grant *storage.ShareGrant
	err := st.InTx(ctx, func(ctx context.Context) error {
		var err error
		aud, pol, grant, err = sharingTxWrites(ctx, t, drv, "rollback", f, id.ID)
		if err != nil {
			return err
		}
		// A revocation inside the transaction is rolled back with it.
		if err := st.Grants().Revoke(ctx, grant.ID, sharingT0.Add(time.Hour)); err != nil {
			return err
		}
		if err := st.Audiences().RemoveMember(ctx, aud.ID, f.principal.ID); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("InTx = %v, want fn's error", err)
	}
	if _, err := st.Audiences().Get(ctx, aud.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("audience of a rolled-back transaction: %v, want ErrNotFound", err)
	}
	if _, err := st.Policies().Get(ctx, pol.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("policy of a rolled-back transaction: %v, want ErrNotFound", err)
	}
	if _, err := st.Grants().Get(ctx, grant.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("grant of a rolled-back transaction: %v, want ErrNotFound", err)
	}
	if got, _ := st.DenyClasses(ctx, f.principal.ID); len(got) != 0 {
		t.Errorf("ceiling after rollback = %v, want none", got)
	}
	if got, _ := st.IdentityPolicy(ctx, id.ID); got != "" {
		t.Errorf("key binding after rollback = %q, want unbound", got)
	}
	if n := len(auditOf(t, drv, "conformance.share.tx.rollback")); n != 0 {
		t.Errorf("audit entries of a rolled-back transaction: %d, want 0", n)
	}

	// A panic rolls back too.
	func() {
		defer func() { _ = recover() }()
		_ = st.InTx(ctx, func(ctx context.Context) error {
			if err := st.SetDenyClasses(ctx, f.principal.ID, []string{storage.ClassPersonal}); err != nil {
				return err
			}
			panic("boom")
		})
	}()
	if got, _ := st.DenyClasses(ctx, f.principal.ID); len(got) != 0 {
		t.Errorf("ceiling after a panic = %v, want none", got)
	}
}

// An InTx inside a transaction joins it, whichever store opened it, and
// the approval store's transactions are the registry's.
func sharingTxNested(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newSharingFixture(t, drv, "tx-nested")
	st := drv.Sharing()
	boom := errors.New("boom")
	err := st.InTx(ctx, func(ctx context.Context) error {
		if err := drv.Approvals().InTx(ctx, func(ctx context.Context) error {
			return st.SetDenyClasses(ctx, f.principal.ID, []string{storage.ClassPersonal})
		}); err != nil {
			return err
		}
		if got, err := st.DenyClasses(ctx, f.principal.ID); err != nil || len(got) != 1 {
			return errors.New("the joined transaction does not read its own write")
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("InTx = %v, want fn's error", err)
	}
	if got, _ := st.DenyClasses(ctx, f.principal.ID); len(got) != 0 {
		t.Errorf("a write in a joined transaction survived the outer rollback: %v", got)
	}
}
