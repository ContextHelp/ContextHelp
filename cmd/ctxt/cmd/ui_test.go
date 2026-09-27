package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/kit/go/console/output"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/browser/launch"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

const uiTestToken = "tok-ui-open"

// uiDpkms is a real dpkms router with web UI sign-in on, counting the
// login-code requests it gets.
type uiDpkms struct {
	*httptest.Server
	mints atomic.Int32
}

func newUIDpkms(t *testing.T, private bool) *uiDpkms {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	svc := service.New(driver, jobs.NewQueue(driver.Jobs()), builtins.Registry(), search.NewEngine(driver), "", nil)
	rc := httpserver.RouterConfig{}
	if !private {
		static, err := authn.NewStatic([]authn.StaticToken{{Token: uiTestToken, Principal: "ops", Roles: []string{authn.RoleReader}}})
		require.NoError(t, err)
		sessions, err := authn.NewSessions(driver.UISessions(), static, authn.SessionOptions{})
		require.NoError(t, err)
		rc.Auth, rc.Sessions = sessions.Provider(static), sessions
	}
	router := httpserver.NewRouterWithConfig(svc, rc)
	d := &uiDpkms{}
	d.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/ui/login-codes" {
			d.mints.Add(1)
		}
		router.ServeHTTP(w, r)
	}))
	t.Cleanup(d.Close)
	return d
}

// exchange trades the code in a sign-in URL's fragment the way the web
// UI does, and returns the status.
func (d *uiDpkms) exchange(t *testing.T, link string) int {
	t.Helper()
	_, code, ok := strings.Cut(link, "#code=")
	require.True(t, ok, "link %q has no code fragment", link)
	req, err := http.NewRequest(http.MethodPost, d.URL+"/ui/auth/session", strings.NewReader(`{"code":"`+code+`"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set(httpserver.HeaderCSRF, "1")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode
}

func swapUISeams(t *testing.T, op launch.Opener, terminal func(io.Writer) bool) {
	t.Helper()
	origOpener, origTerminal := uiOpener, uiTerminal
	t.Cleanup(func() { uiOpener, uiTerminal = origOpener, origTerminal })
	uiOpener, uiTerminal = op, terminal
}

func uiConfigured(t *testing.T, serverURL, token string) *testDB {
	t.Helper()
	db := setupTestDB(t)
	cfg := "server:\n  url: " + serverURL + "\n"
	if token != "" {
		cfg += "  token: " + token + "\n"
	}
	appendConfig(t, db, cfg)
	return db
}

var signInLink = regexp.MustCompile(`http://127\.0\.0\.1:\d+/ui/auth#code=[A-Za-z0-9_-]{43}`)

func TestUIOpen_NoBrowserPrintsLink(t *testing.T) {
	d := newUIDpkms(t, false)
	op := &fakeOpener{}
	swapUISeams(t, op, func(io.Writer) bool { return true })
	db := uiConfigured(t, d.URL, uiTestToken)

	out, err := db.exec("ui", "open", "--no-browser")
	require.NoError(t, err, out)
	link := signInLink.FindString(out)
	require.NotEmpty(t, link, "no sign-in link in:\n%s", out)
	assert.True(t, strings.HasPrefix(link, d.URL+"/ui/auth#code="), "link on the configured server, code in the fragment")
	assert.Empty(t, op.opened(), "--no-browser never opens a browser, even on a terminal")
	assert.Equal(t, http.StatusOK, d.exchange(t, link), "the printed code signs in")
	assert.Equal(t, http.StatusUnauthorized, d.exchange(t, link), "once")
}

func TestUIOpen_TerminalOpensBrowser(t *testing.T) {
	d := newUIDpkms(t, false)
	op := &fakeOpener{}
	swapUISeams(t, op, func(io.Writer) bool { return true })
	db := uiConfigured(t, d.URL, uiTestToken)

	out, err := db.exec("ui", "open")
	require.NoError(t, err, out)
	got := op.opened()
	require.Len(t, got, 1)
	assert.Regexp(t, signInLink, got[0])
	assert.NotContains(t, out, "#code=", "a link handed to the browser is not printed too")
	assert.Contains(t, out, "Opened the web UI sign-in")
	assert.Equal(t, http.StatusOK, d.exchange(t, got[0]))
}

func TestUIOpen_NonTerminalPrintsLink(t *testing.T) {
	d := newUIDpkms(t, false)
	op := &fakeOpener{}
	swapUISeams(t, op, func(io.Writer) bool { return false })
	db := uiConfigured(t, d.URL, uiTestToken)

	out, err := db.exec("ui", "open")
	require.NoError(t, err, out)
	assert.Regexp(t, signInLink, out)
	assert.Empty(t, op.opened(), "no browser without a terminal")
}

func TestUIOpen_BrowserFailureFallsBackToLink(t *testing.T) {
	d := newUIDpkms(t, false)
	op := &fakeOpener{err: io.ErrUnexpectedEOF}
	swapUISeams(t, op, func(io.Writer) bool { return true })
	db := uiConfigured(t, d.URL, uiTestToken)

	out, err := db.exec("ui", "open")
	require.NoError(t, err, out)
	assert.Contains(t, out, "could not open a browser")
	assert.Regexp(t, signInLink, out)
}

func TestUIOpen_JSON(t *testing.T) {
	d := newUIDpkms(t, false)
	swapUISeams(t, &fakeOpener{}, func(io.Writer) bool { return false })
	db := uiConfigured(t, d.URL, uiTestToken)

	out, err := db.exec("ui", "open", "--no-browser", "--format", "json")
	require.NoError(t, err, out)
	var link uiLink
	require.NoError(t, json.Unmarshal([]byte(firstJSON(out)), &link), out)
	assert.True(t, link.SessionRequired)
	assert.False(t, link.Opened)
	assert.Regexp(t, signInLink, link.URL)
	assert.False(t, link.ExpiresAt.IsZero())
}

func TestUIOpen_PrivateInstanceOpensUI(t *testing.T) {
	d := newUIDpkms(t, true)
	swapUISeams(t, &fakeOpener{}, func(io.Writer) bool { return false })
	db := uiConfigured(t, d.URL, "")

	out, err := db.exec("ui", "open", "--no-browser")
	require.NoError(t, err, out)
	assert.Contains(t, out, d.URL+"/ui/\n")
	assert.NotContains(t, out, "#code=")
	assert.Contains(t, out, "no sign-in needed")
}

func TestUIOpen_WrongTokenIsUnauthorized(t *testing.T) {
	d := newUIDpkms(t, false)
	op := &fakeOpener{}
	swapUISeams(t, op, func(io.Writer) bool { return true })
	db := uiConfigured(t, d.URL, "tok-wrong")

	out, err := db.exec("ui", "open")
	require.Error(t, err, out)
	assert.Equal(t, output.ExitUnauthorized, ExitCodeFor(err))
	assert.Empty(t, op.opened())
}

func TestUIOpen_ServerFlagPinsInstance(t *testing.T) {
	configured := newUIDpkms(t, false)
	pinned := newUIDpkms(t, false)
	swapUISeams(t, &fakeOpener{}, func(io.Writer) bool { return false })
	db := uiConfigured(t, configured.URL, uiTestToken)

	out, err := db.exec("ui", "open", "--server", pinned.URL, "--no-browser")
	require.NoError(t, err, out)
	assert.Contains(t, out, pinned.URL+"/ui/auth#code=")
	assert.Zero(t, configured.mints.Load())
	assert.Equal(t, int32(1), pinned.mints.Load())
}

func TestUIOpen_DryRunMintsNothing(t *testing.T) {
	d := newUIDpkms(t, false)
	op := &fakeOpener{}
	swapUISeams(t, op, func(io.Writer) bool { return true })
	db := uiConfigured(t, d.URL, uiTestToken)

	out, err := db.exec("ui", "open", "--dry-run")
	require.NoError(t, err, out)
	assert.Contains(t, out, "Would request")
	assert.Zero(t, d.mints.Load())
	assert.Empty(t, op.opened())
}

func TestRedactLoginCode(t *testing.T) {
	assert.Equal(t, "http://h/ui/auth", redactLoginCode("http://h/ui/auth#code=abc"))
	assert.Equal(t, "http://h/ui/", redactLoginCode("http://h/ui/"))
}

// A link the browser already got is spent: the JSON document reports
// it opened and leaves the code out.
func TestUIOpen_JSONAfterOpeningOmitsCode(t *testing.T) {
	d := newUIDpkms(t, false)
	op := &fakeOpener{}
	swapUISeams(t, op, func(io.Writer) bool { return true })
	db := uiConfigured(t, d.URL, uiTestToken)

	out, err := db.exec("ui", "open", "--format", "json")
	require.NoError(t, err, out)
	var link uiLink
	require.NoError(t, json.Unmarshal([]byte(firstJSON(out)), &link), out)
	assert.True(t, link.Opened)
	assert.Equal(t, d.URL+"/ui/auth", link.URL)
	assert.NotContains(t, out, "#code=")
	require.Len(t, op.opened(), 1)
}
