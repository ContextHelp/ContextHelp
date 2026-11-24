package http_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func TestWatchEncryptionHTTP_PersistsEncryptAndScope(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	srv := dpkmstest.Start(t, driver, dpkmstest.WithKeys())

	resp, body := sendJSON(t, srv, http.MethodPost, "/api/v1/watches", dpkmstest.RoleAdmin, map[string]any{
		"path": t.TempDir(), "encrypt": true, "encrypt_scope": "emails and phone numbers", "paused": true,
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode, string(body))

	var out struct {
		ID           string `json:"id"`
		Encrypt      bool   `json:"encrypt"`
		EncryptScope string `json:"encrypt_scope"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	require.True(t, out.Encrypt)
	require.Equal(t, "emails and phone numbers", out.EncryptScope)

	got, err := driver.Watches().GetWatch(ownerCtx(), out.ID)
	require.NoError(t, err)
	require.True(t, got.Encrypt)
	require.Equal(t, "emails and phone numbers", got.EncryptScope)
}
