package fetcher

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTP_Fetch_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != defaultUserAgent {
			t.Errorf("User-Agent = %q; want %q", r.Header.Get("User-Agent"), defaultUserAgent)
		}
		w.Write([]byte("hello"))
	}))
	defer ts.Close()

	f := NewHTTP(Options{})
	body, err := f.Fetch(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("Fetch err = %v", err)
	}
	if body != "hello" {
		t.Errorf("body = %q; want hello", body)
	}
}

func TestHTTP_Fetch_NonOKStatusErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("missing"))
	}))
	defer ts.Close()

	f := NewHTTP(Options{})
	body, err := f.Fetch(context.Background(), ts.URL)
	if err == nil {
		t.Fatal("expected error on 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error missing status: %v", err)
	}
	// Body still returned for diagnostics.
	if body != "missing" {
		t.Errorf("body = %q; want missing", body)
	}
}

func TestHTTP_Get_PreservesStatusAndContentType(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"err":"rate"}`))
	}))
	defer ts.Close()

	f := NewHTTP(Options{})
	res, err := f.Get(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("Get err = %v", err)
	}
	if res.Status != http.StatusForbidden {
		t.Errorf("Status = %d; want 403", res.Status)
	}
	if res.ContentType != "application/json" {
		t.Errorf("ContentType = %q", res.ContentType)
	}
	if !strings.Contains(string(res.Body), "rate") {
		t.Errorf("body = %q", res.Body)
	}
}

func TestHTTP_RespectsCustomUserAgent(t *testing.T) {
	got := ""
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		w.WriteHeader(200)
	}))
	defer ts.Close()

	f := NewHTTP(Options{UserAgent: "my-bot/2.0"})
	if _, err := f.Fetch(context.Background(), ts.URL); err != nil {
		t.Fatalf("Fetch err = %v", err)
	}
	if got != "my-bot/2.0" {
		t.Errorf("User-Agent = %q; want my-bot/2.0", got)
	}
}

func TestHTTP_RespectsContextCancel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer ts.Close()

	f := NewHTTP(Options{Timeout: 50 * time.Millisecond})
	_, err := f.Fetch(context.Background(), ts.URL)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// IBR adapter — exec stub.
func TestIBR_Snap_StubbedRunFuncSuccess(t *testing.T) {
	f := &IBRFetcher{
		opts:   IBROptions{Timeout: time.Second},
		binary: "/usr/bin/true",
		runFunc: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "/usr/bin/true" {
				t.Errorf("binary = %q", name)
			}
			if len(args) < 2 || args[0] != "snap" {
				t.Errorf("args = %v; want snap <url> ...", args)
			}
			return []byte(`{"dom":"snap"}`), nil
		},
	}
	body, err := f.Fetch(context.Background(), "https://example.test/")
	if err != nil {
		t.Fatalf("Fetch err = %v", err)
	}
	if !strings.Contains(body, "snap") {
		t.Errorf("body = %q", body)
	}

	res, err := f.Get(context.Background(), "https://example.test/")
	if err != nil {
		t.Fatalf("Get err = %v", err)
	}
	if res.Status != http.StatusOK {
		t.Errorf("Status = %d", res.Status)
	}
	if res.ContentType != "application/json" {
		t.Errorf("ContentType = %q", res.ContentType)
	}
}

func TestIBR_Snap_StubbedRunFuncError(t *testing.T) {
	f := &IBRFetcher{
		opts:   IBROptions{Timeout: time.Second},
		binary: "/usr/bin/true",
		runFunc: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return []byte("boom"), errors.New("exit 1")
		},
	}
	_, err := f.Fetch(context.Background(), "https://example.test/")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error missing output preview: %v", err)
	}
}

func TestIBR_Snap_AppliesExtraArgs(t *testing.T) {
	gotArgs := []string{}
	f := &IBRFetcher{
		opts:   IBROptions{Timeout: time.Second, ExtraArgs: []string{"--mode", "aria"}},
		binary: "/usr/bin/true",
		runFunc: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			gotArgs = args
			return []byte("ok"), nil
		},
	}
	if _, err := f.Fetch(context.Background(), "https://example.test/"); err != nil {
		t.Fatalf("Fetch err = %v", err)
	}
	want := []string{"snap", "https://example.test/", "--mode", "aria"}
	if !strSlicesEqual(gotArgs, want) {
		t.Errorf("args = %v; want %v", gotArgs, want)
	}
}

func TestNewIBR_LookPathFailureReturnsError(t *testing.T) {
	if _, err := NewIBR(IBROptions{Binary: "definitely-not-a-binary-9f8a3"}); err == nil {
		t.Fatal("expected NewIBR to fail when binary missing")
	}
}

func strSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
