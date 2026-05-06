package caldavvdir

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/kit/go/runtime/bus"
	"hop.top/kit/go/runtime/domain"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/adapter/calendar"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
	ctxtpolicy "github.com/ideacrafterslabs/ctxt/internal/policy"
)

// fakeRepo is a minimal EventRepository for Gate-4 tests.
type fakeRepo struct {
	mu      sync.Mutex
	created []eventEntity
}

func (r *fakeRepo) Create(_ context.Context, e *eventEntity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.created = append(r.created, *e)
	return nil
}
func (r *fakeRepo) Get(_ context.Context, id string) (*eventEntity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.created {
		if r.created[i].GetID() == id {
			return &r.created[i], nil
		}
	}
	return nil, domain.ErrNotFound
}
func (r *fakeRepo) List(_ context.Context, _ domain.Query) ([]eventEntity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]eventEntity, len(r.created))
	copy(out, r.created)
	return out, nil
}
func (r *fakeRepo) Update(_ context.Context, _ *eventEntity) error { return nil }
func (r *fakeRepo) Delete(_ context.Context, _ string) error       { return nil }

// sampleICS is a minimal valid VEVENT string used across tests.
const sampleICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//ctxt//caldav-vdir//EN
BEGIN:VEVENT
UID:event-001
SUMMARY:Sample
DTSTART:20260601T100000Z
DTEND:20260601T110000Z
END:VEVENT
END:VCALENDAR
`

// TestAdapterIdentity locks Protocol/Backend.
func TestAdapterIdentity(t *testing.T) {
	a := New(Config{VDir: t.TempDir()})
	assert.Equal(t, calendar.Protocol, a.Protocol())
	assert.Equal(t, "caldav-vdir", a.Backend())
}

// TestCapabilitiesAllFour confirms full bidirectional declaration.
func TestCapabilitiesAllFour(t *testing.T) {
	a := New(Config{VDir: t.TempDir()})
	require.True(t, adapter.HasCapability(a, adapter.CapFetch))
	require.True(t, adapter.HasCapability(a, adapter.CapServe))
	require.True(t, adapter.HasCapability(a, adapter.CapSubmit))
	require.True(t, adapter.HasCapability(a, adapter.CapEmitEvents))
}

// TestStartCreatesVDir verifies Start creates a missing vdir.
func TestStartCreatesVDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "subdir")
	a := New(Config{VDir: dir})
	require.NoError(t, a.Start(context.Background(), nil))
	defer func() { _ = a.Stop(context.Background()) }()
	st, err := os.Stat(dir)
	require.NoError(t, err)
	require.True(t, st.IsDir())
}

// TestStartRequiresVDir surfaces a clean error when VDir is empty.
func TestStartRequiresVDir(t *testing.T) {
	a := New(Config{})
	err := a.Start(context.Background(), nil)
	require.Error(t, err)
	assert.False(t, a.Ready())
}

// TestSubmitWritesICS — happy-path Submit lands as a file on disk.
func TestSubmitWritesICS(t *testing.T) {
	dir := t.TempDir()
	a := New(Config{VDir: dir})
	require.NoError(t, a.Start(context.Background(), nil))
	defer func() { _ = a.Stop(context.Background()) }()

	obj := ingest.Object{Content: sampleICS}
	require.NoError(t, a.Submit(context.Background(), obj))

	body, err := os.ReadFile(filepath.Join(dir, "event-001.ics"))
	require.NoError(t, err)
	assert.Contains(t, string(body), "UID:event-001")
}

// TestSubmitRejectsInvalidICS — missing UID/VEVENT short-circuits.
func TestSubmitRejectsInvalidICS(t *testing.T) {
	dir := t.TempDir()
	a := New(Config{VDir: dir})
	require.NoError(t, a.Start(context.Background(), nil))
	defer func() { _ = a.Stop(context.Background()) }()

	err := a.Submit(context.Background(), ingest.Object{Content: "not an ics"})
	require.True(t, errors.Is(err, ErrInvalidICS))
}

// TestSubmitAfterDrainRefuses.
func TestSubmitAfterDrainRefuses(t *testing.T) {
	dir := t.TempDir()
	a := New(Config{VDir: dir})
	require.NoError(t, a.Start(context.Background(), nil))
	require.NoError(t, a.Drain(context.Background()))
	defer func() { _ = a.Stop(context.Background()) }()

	err := a.Submit(context.Background(), ingest.Object{Content: sampleICS})
	require.Error(t, err)
}

// TestFetchRoundTrip — Submit + Fetch returns the same event.
func TestFetchRoundTrip(t *testing.T) {
	dir := t.TempDir()
	a := New(Config{VDir: dir})
	require.NoError(t, a.Start(context.Background(), nil))
	defer func() { _ = a.Stop(context.Background()) }()

	require.NoError(t, a.Submit(context.Background(), ingest.Object{Content: sampleICS}))
	got, err := a.Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "event-001", got[0].ID)
	assert.Equal(t, EventEntityKind, got[0].Type)
}

// TestFetchPrePopulatedVDir — Fetch over a vdir seeded outside the
// adapter (fixture .ics file).
func TestFetchPrePopulatedVDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fixture.ics"), []byte(sampleICS), 0o600))

	a := New(Config{VDir: dir})
	require.NoError(t, a.Start(context.Background(), nil))
	defer func() { _ = a.Stop(context.Background()) }()

	got, err := a.Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "event-001", got[0].ID)
}

// TestServeServesPROPFIND — bring the listener up, fire PROPFIND, get
// a multistatus.
func TestServeServesPROPFIND(t *testing.T) {
	dir := t.TempDir()
	a := New(Config{VDir: dir, CalendarName: "Test"})
	require.NoError(t, a.Start(context.Background(), nil))
	defer func() { _ = a.Stop(context.Background()) }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serveDone := make(chan error, 1)
	go func() { serveDone <- a.Serve(ctx, ln) }()
	// give the server a moment to start
	time.Sleep(50 * time.Millisecond)

	url := "http://" + ln.Addr().String() + "/"
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	assert.Equal(t, http.StatusMultiStatus, resp.StatusCode)
	assert.Contains(t, string(body), "<d:displayname>Test</d:displayname>")
	assert.Contains(t, string(body), "<c:calendar/>")

	cancel()
	select {
	case err := <-serveDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Serve did not return after ctx cancel")
	}
}

// TestServeServesGETandPUT — round-trip an .ics through HTTP.
func TestServeServesGETandPUT(t *testing.T) {
	dir := t.TempDir()
	a := New(Config{VDir: dir})
	require.NoError(t, a.Start(context.Background(), nil))
	defer func() { _ = a.Stop(context.Background()) }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveDone := make(chan error, 1)
	go func() { serveDone <- a.Serve(ctx, ln) }()
	time.Sleep(50 * time.Millisecond)

	base := "http://" + ln.Addr().String()

	// PUT
	req, err := http.NewRequestWithContext(ctx, "PUT", base+"/event-001.ics", bytes.NewReader([]byte(sampleICS)))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	// GET
	req, err = http.NewRequestWithContext(ctx, "GET", base+"/event-001.ics", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	got, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, string(got), "UID:event-001")

	cancel()
	<-serveDone
}

// TestServeREPORTReturnsAllEvents seeds the vdir and verifies REPORT
// includes every event.
func TestServeREPORTReturnsAllEvents(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "event-001.ics"), []byte(sampleICS), 0o600))

	a := New(Config{VDir: dir})
	require.NoError(t, a.Start(context.Background(), nil))
	defer func() { _ = a.Stop(context.Background()) }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveDone := make(chan error, 1)
	go func() { serveDone <- a.Serve(ctx, ln) }()
	time.Sleep(50 * time.Millisecond)

	req, err := http.NewRequestWithContext(ctx, "REPORT", "http://"+ln.Addr().String()+"/", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	assert.Equal(t, http.StatusMultiStatus, resp.StatusCode)
	assert.Contains(t, string(body), "<d:href>/event-001.ics</d:href>")

	cancel()
	<-serveDone
}

// TestStopClosesServerCleanly — Stop tears down the HTTP server.
func TestStopClosesServerCleanly(t *testing.T) {
	dir := t.TempDir()
	a := New(Config{VDir: dir})
	require.NoError(t, a.Start(context.Background(), nil))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	serveDone := make(chan error, 1)
	go func() { serveDone <- a.Serve(context.Background(), ln) }()
	time.Sleep(50 * time.Millisecond)

	require.NoError(t, a.Drain(context.Background()))
	require.NoError(t, a.Stop(context.Background()))

	select {
	case err := <-serveDone:
		require.NoError(t, err, "Serve should return cleanly after Stop")
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after Stop")
	}
}

// TestSanitizeUIDStripsPathTraversal — adversarial UID can't escape.
func TestSanitizeUIDStripsPathTraversal(t *testing.T) {
	cases := map[string]string{
		"normal":          "normal",
		"with/slash":      "with_slash",
		"../../etc/host":  ".._.._etc_host",
		"":                "",
		"valid_chars-9.0": "valid_chars-9.0",
	}
	for in, want := range cases {
		assert.Equal(t, want, sanitizeUID(in), "input=%q", in)
	}
}

// TestGate4_SubmitVetoedByPolicy is the Acceptance Gate 4 proof. A
// CEL rule on kit.runtime.entity.pre_persisted vetoes calendar events
// whose Kind discriminator is calendar.event AND whose UID is empty.
// Submit returns the policy error and the .ics file is NOT written.
func TestGate4_SubmitVetoedByPolicy(t *testing.T) {
	dir := t.TempDir()
	policyDir := t.TempDir()
	policyFile := filepath.Join(policyDir, "ctxt.yaml")
	require.NoError(t, os.WriteFile(policyFile, []byte(`policies:
  - name: deny-calendar-events
    on: kit.runtime.entity.pre_persisted
    when: 'resource.fields.Kind != "calendar.event"'
    effect: allow
    otherwise: deny
    message: "calendar events vetoed in this test"
`), 0o600))
	t.Setenv("CTXT_POLICY_FILE", policyFile)

	b := bus.New()
	t.Cleanup(func() { _ = b.Close(context.Background()) })

	pol, err := ctxtpolicy.Init(b)
	require.NoError(t, err)
	t.Cleanup(pol.Close)

	repo := &fakeRepo{}
	a := New(Config{VDir: dir, Repo: repo, Publisher: pol.Publisher()})
	require.NoError(t, a.Start(context.Background(), nil))
	defer func() { _ = a.Stop(context.Background()) }()

	err = a.Submit(context.Background(), ingest.Object{Content: sampleICS})
	require.Error(t, err, "policy must veto Submit")
	assert.Empty(t, repo.created, "vetoed event must NOT reach the repo")

	// And NO .ics file was written, since the veto fires before disk.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".ics") {
			t.Errorf("vetoed event landed on disk: %s", e.Name())
		}
	}
}

// TestGate4_SubmitAllowedWhenPolicyAllows — sanity check: with a
// permissive ruleset the same Submit succeeds end-to-end.
func TestGate4_SubmitAllowedWhenPolicyAllows(t *testing.T) {
	dir := t.TempDir()
	policyDir := t.TempDir()
	policyFile := filepath.Join(policyDir, "ctxt.yaml")
	require.NoError(t, os.WriteFile(policyFile, []byte("policies: []\n"), 0o600))
	t.Setenv("CTXT_POLICY_FILE", policyFile)

	b := bus.New()
	t.Cleanup(func() { _ = b.Close(context.Background()) })

	pol, err := ctxtpolicy.Init(b)
	require.NoError(t, err)
	t.Cleanup(pol.Close)

	repo := &fakeRepo{}
	a := New(Config{VDir: dir, Repo: repo, Publisher: pol.Publisher()})
	require.NoError(t, a.Start(context.Background(), nil))
	defer func() { _ = a.Stop(context.Background()) }()

	require.NoError(t, a.Submit(context.Background(), ingest.Object{Content: sampleICS}))
	assert.Len(t, repo.created, 1)
	assert.Equal(t, "event-001", repo.created[0].GetID())
}
