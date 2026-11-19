package dpkmstest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/bosaccess"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

const (
	accBusiness = "business_scopes_00000000000000000000000001"
	accScope    = "bos://core.business_scopes/" + accBusiness
	accPerson   = "bos://core.principals/principals_00000000000000000000000001"
	accEng      = "bos://projects.engagements/engagements_00000000000000000000000001"
)

func accessSet(epoch int64, mode string, base int64) bosaccess.Set {
	docs := "core.documents"
	s := bosaccess.Set{
		Contract: bosaccess.Contract, Epoch: epoch, Mode: mode, BaseEpoch: base, Scope: accScope,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Policy:      bosaccess.Policy{MaxStalenessSeconds: 86400, ReconcileIntervalSeconds: 3600, RecordBasisMaxStalenessSeconds: 3600},
		Scopes:      []bosaccess.Scope{{ID: accScope}},
		Principals:  []bosaccess.Principal{{ID: accPerson, Kind: "person", Status: "active"}},
		RecordAccess: []bosaccess.RecordEntry{{ID: "rec_1", Subject: accPerson, Anchor: accEng, Scope: accScope,
			TypeKey: &docs, Via: bosaccess.ViaAssignment, Grant: "g"}},
	}
	s.Normalize()
	s.Digest = bosaccess.ContentDigest(s)
	return s
}

func send(t *testing.T, srv *dpkmstest.Server, token, method, path string, body any) (int, []byte) {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+path, rd)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.Bytes()
}

// The authority pushes a full set and a delta, reads the state, and is
// answered by the contract's codes; a caller that is not a peer principal is
// refused; every set applied or refused is in the audit log.
func TestAccessProvisioningRoutes(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	peer, err := bosaccess.Open(bosaccess.DeclaredProfile("test"), &bosaccess.MemStore{}, nil)
	require.NoError(t, err)
	srv := dpkmstest.Start(t, driver, dpkmstest.WithKeys(), dpkmstest.WithAuthority(peer))

	ctx := context.Background()
	require.NoError(t, driver.Auth().Principals().Create(ctx, &storage.Principal{
		ID: "prn_bos", Kind: storage.PrincipalKindPeer, DisplayName: "authority", Roles: []string{authn.RoleWriter}}))
	secret, _, err := authn.NewKeysOver(driver.Auth(), nil).Mint(ctx, authn.MintKey{PrincipalID: "prn_bos", Name: "authority"})
	require.NoError(t, err)
	path := "/bos/v1/access/" + accBusiness

	// not a peer: the reader key is refused
	code, body := send(t, srv, srv.Token(dpkmstest.RoleReader), http.MethodPut, path, accessSet(1, bosaccess.ModeFull, 0))
	assert.Equal(t, http.StatusForbidden, code, string(body))
	assert.Contains(t, string(body), `"forbidden"`)

	// full set: applied, acknowledged with the entry digest
	code, body = send(t, srv, secret, http.MethodPut, path, accessSet(1, bosaccess.ModeFull, 0))
	require.Equal(t, http.StatusOK, code, string(body))
	var ack bosaccess.Ack
	require.NoError(t, json.Unmarshal(body, &ack))
	assert.Equal(t, "applied", ack.State)
	require.NotNil(t, ack.RecordAccess)
	assert.Equal(t, 1, ack.RecordAccess.Count)
	require.NoError(t, bosaccess.Validate(bosaccess.SchemaAck, body))

	// replay is a confirmation; an older epoch is refused 409
	code, body = send(t, srv, secret, http.MethodPut, path, accessSet(1, bosaccess.ModeFull, 0))
	require.Equal(t, http.StatusOK, code, string(body))
	assert.Contains(t, string(body), "replayed")
	older := accessSet(2, bosaccess.ModeFull, 0)
	code, _ = send(t, srv, secret, http.MethodPut, path, older)
	require.Equal(t, http.StatusOK, code)
	code, body = send(t, srv, secret, http.MethodPut, path, accessSet(1, bosaccess.ModeFull, 0))
	assert.Equal(t, http.StatusConflict, code, string(body))
	assert.Contains(t, string(body), "epoch_stale")

	// a delta against the wrong base is 409; a bad digest 422
	code, body = send(t, srv, secret, http.MethodPatch, path, accessSet(3, bosaccess.ModeDelta, 1))
	assert.Equal(t, http.StatusConflict, code, string(body))
	assert.Contains(t, string(body), "base_epoch_mismatch")
	bad := accessSet(3, bosaccess.ModeFull, 0)
	bad.Digest = "sha256:" + string(bytes.Repeat([]byte("0"), 64))
	code, body = send(t, srv, secret, http.MethodPut, path, bad)
	assert.Equal(t, http.StatusUnprocessableEntity, code, string(body))

	// another scope is unknown; the method must match the mode
	code, _ = send(t, srv, secret, http.MethodPut, "/bos/v1/access/business_scopes_00000000000000000000000009", accessSet(3, bosaccess.ModeFull, 0))
	assert.Equal(t, http.StatusNotFound, code)
	code, body = send(t, srv, secret, http.MethodPatch, path, accessSet(3, bosaccess.ModeFull, 0))
	assert.Equal(t, http.StatusBadRequest, code, string(body))

	// the state
	code, body = send(t, srv, secret, http.MethodGet, path+"/state", nil)
	require.Equal(t, http.StatusOK, code, string(body))
	require.NoError(t, bosaccess.Validate(bosaccess.SchemaState, body))
	var rep bosaccess.Report
	require.NoError(t, json.Unmarshal(body, &rep))
	require.NotNil(t, rep.AppliedEpoch)
	assert.EqualValues(t, 2, *rep.AppliedEpoch)

	// audit: every applied and refused set left an entry
	entries, _, err := driver.AuditLog().List(ctx, storage.AuditFilter{EventTypes: []string{"access.applied", "access.refused"}})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(entries), 6)
}
