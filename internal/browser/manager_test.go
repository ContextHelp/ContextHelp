package browser

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

func TestManager_IsRunning_FalseWhenNoClient(t *testing.T) {
	m := NewManager(config.BrowserConfig{Enabled: true, Binary: "ibr"})
	assert.False(t, m.IsRunning())
}

func TestManager_IsRunning_TrueWhenHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"healthy"}`))
	}))
	defer srv.Close()

	m := NewManager(config.BrowserConfig{Enabled: true})
	m.client = NewClient(srv.URL, "tok")
	assert.True(t, m.IsRunning())
}

func TestManager_Client_ReturnsNilWhenDisabled(t *testing.T) {
	m := NewManager(config.BrowserConfig{Enabled: false})
	assert.Nil(t, m.Client())
}

func TestManager_Enabled(t *testing.T) {
	m := NewManager(config.BrowserConfig{Enabled: true})
	assert.True(t, m.Enabled())

	m2 := NewManager(config.BrowserConfig{Enabled: false})
	assert.False(t, m2.Enabled())
}

func TestManager_Port_ZeroWhenNotStarted(t *testing.T) {
	m := NewManager(config.BrowserConfig{Enabled: true})
	assert.Equal(t, 0, m.Port())
}

func TestManager_PID_ZeroWhenNotStarted(t *testing.T) {
	m := NewManager(config.BrowserConfig{Enabled: true})
	assert.Equal(t, 0, m.PID())
}
