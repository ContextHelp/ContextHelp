package bosobligations_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/bosaccess"
	"github.com/ideacrafterslabs/ctxt/internal/bosobligations"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

func newTestReceiver(t *testing.T) (*bosobligations.Receiver, *sqlite.Driver) {
	t.Helper()
	ctx := context.Background()
	bs, err := local.New(t.TempDir())
	require.NoError(t, err)
	drv, err := sqlite.New(":memory:", bs)
	require.NoError(t, err)
	require.NoError(t, drv.Init(ctx))
	t.Cleanup(func() { _ = drv.Close(ctx) })
	svc := service.New(drv, nil, nil, nil, "", nil)
	return bosobligations.NewReceiver(svc), drv
}

func TestObligationsConformance_ProfileHoldEraseReleaseIdempotency(t *testing.T) {
	ctx := context.Background()
	rcv, drv := newTestReceiver(t)
	now := time.Now().UTC().Truncate(time.Second)

	// 1. Profile advertises bos.obligations/v1 and capabilities accurately.
	prof := bosaccess.DeclaredProfile("1.0.0")
	require.True(t, prof.Can.Erase)
	require.True(t, prof.Can.Hold)
	require.False(t, prof.Can.Anonymize)
	require.True(t, prof.Can.EraseDerived)
	foundContract := false
	for _, c := range prof.Contracts {
		if c.ID == bosaccess.ObligationsContract && c.Level == "complete" {
			foundContract = true
		}
	}
	require.True(t, foundContract, "profile must advertise bos.obligations/v1 complete")

	// Seed root + derived object for subject 1; subject 2 is absent.
	const (
		subj1   = "bos://core.documents/doc_1"
		subjAbs = "bos://core.documents/doc_absent"
	)
	require.NoError(t, drv.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "o-bos-1", Type: "document", RawContent: "sensitive record", TextContent: "sensitive record",
		Source: "bos", SourceKey: subj1, Status: storage.ObjectStatusActive, CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, drv.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "o-bos-derived", Type: "summary", RawContent: "derived summary", TextContent: "derived summary",
		Status: storage.ObjectStatusActive, CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, drv.Edges().Create(ctx, &storage.Edge{
		ID: "e-der-1", FromType: "object", FromID: "o-bos-derived", ToType: "object", ToID: "o-bos-1", EdgeType: "derives_from", CreatedAt: now,
	}))

	// 2. anonymize -> 400 kind_unsupported.
	_, code, err := rcv.Apply(ctx, bosobligations.Request{
		ID: "ob-anon-1", Kind: "anonymize", Subjects: []string{subj1}, EffectiveAt: now.Format(time.RFC3339),
	})
	require.Equal(t, 400, code)
	var oe *bosobligations.Error
	require.ErrorAs(t, err, &oe)
	require.Equal(t, bosobligations.CodeKindUnsupported, oe.Code)

	// 3. hold places active hold and returns receipt with matching SHA-256 subject digest.
	holdReq := bosobligations.Request{
		ID: "ob-hold-1", Kind: "hold", Hold: "hold-litigation-1",
		Subjects: []string{subj1, subjAbs}, EffectiveAt: now.Format(time.RFC3339),
	}
	st, code, err := rcv.Apply(ctx, holdReq)
	require.NoError(t, err)
	require.Equal(t, 200, code)
	require.Equal(t, bosobligations.StateApplied, st.State)
	require.NotNil(t, st.Receipt)
	require.Equal(t, bosobligations.SubjectDigest(holdReq.Subjects), st.Receipt.Digest)
	require.Equal(t, int64(1), st.Receipt.Counts.Objects)
	require.Equal(t, []string{subjAbs}, st.Receipt.Absent)

	// 4. Idempotent replay returns identical receipt; body reuse with different payload -> 409 id_reused.
	stReplay, code, err := rcv.Apply(ctx, holdReq)
	require.NoError(t, err)
	require.Equal(t, 200, code)
	require.Equal(t, st.Receipt.CompletedAt, stReplay.Receipt.CompletedAt)

	diffReq := holdReq
	diffReq.Hold = "hold-different"
	_, code, err = rcv.Apply(ctx, diffReq)
	require.Equal(t, 409, code)
	require.ErrorAs(t, err, &oe)
	require.Equal(t, bosobligations.CodeIDReused, oe.Code)

	// 5. erase while held -> 409 hold_in_force and object remains intact!
	_, code, err = rcv.Apply(ctx, bosobligations.Request{
		ID: "ob-erase-blocked", Kind: "erase", Subjects: []string{subj1}, EffectiveAt: now.Format(time.RFC3339), Derived: true, Tombstone: true,
	})
	require.Equal(t, 409, code)
	require.ErrorAs(t, err, &oe)
	require.Equal(t, bosobligations.CodeHoldInForce, oe.Code)
	_, err = drv.Objects().Get(ctx, "o-bos-1")
	require.NoError(t, err)

	// 6. release lifts hold -> erase succeeds with derived count & tombstone!
	_, code, err = rcv.Apply(ctx, bosobligations.Request{
		ID: "ob-rel-1", Kind: "release", Hold: "hold-litigation-1", Subjects: []string{subj1, subjAbs}, EffectiveAt: now.Format(time.RFC3339),
	})
	require.NoError(t, err)
	require.Equal(t, 200, code)

	eraseReq := bosobligations.Request{
		ID: "ob-erase-1", Kind: "erase", Subjects: []string{subj1, subjAbs},
		EffectiveAt: now.Format(time.RFC3339), Derived: true, Tombstone: true,
	}
	stErase, code, err := rcv.Apply(ctx, eraseReq)
	require.NoError(t, err)
	require.Equal(t, 200, code)
	require.Equal(t, bosobligations.StateApplied, stErase.State)
	require.Equal(t, bosobligations.SubjectDigest(eraseReq.Subjects), stErase.Receipt.Digest)
	require.Equal(t, int64(1), stErase.Receipt.Counts.Objects)
	require.Equal(t, int64(1), stErase.Receipt.Counts.Derived["objects"])
	require.Equal(t, []string{subjAbs}, stErase.Receipt.Absent)

	// Status lookup returns stored receipt; unknown ID returns obligation_unknown.
	gotSt, err := rcv.Status(ctx, "ob-erase-1")
	require.NoError(t, err)
	require.Equal(t, stErase.Receipt.Digest, gotSt.Receipt.Digest)
	_, err = rcv.Status(ctx, "ob-missing")
	require.ErrorAs(t, err, &oe)
	require.Equal(t, bosobligations.CodeObligationUnknown, oe.Code)

	// Tombstone blocks resurrection.
	err = drv.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "o-bos-resurrect", Type: "document", RawContent: "x", SourceKey: subj1,
		Status: storage.ObjectStatusActive, CreatedAt: now, UpdatedAt: now,
	})
	require.ErrorIs(t, err, storage.ErrSourceErased)
}
