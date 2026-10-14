package security

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hop.top/kit/go/core/netpolicy"
)

// hangingWebhook is an httptest server that accepts each request and
// never answers it. entered gets one value per request; gone gets one
// when the client abandons the request (its context ends server side).
type hangingWebhook struct {
	*httptest.Server
	entered chan struct{}
	gone    chan struct{}
}

func newHangingWebhook(t *testing.T) *hangingWebhook {
	t.Helper()
	h := &hangingWebhook{entered: make(chan struct{}, 8), gone: make(chan struct{}, 8)}
	release := make(chan struct{})
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The server notices a dropped connection only once the body is read.
		_, _ = io.Copy(io.Discard, r.Body)
		h.entered <- struct{}{}
		select {
		case <-r.Context().Done():
			h.gone <- struct{}{}
		case <-release:
		}
	}))
	t.Cleanup(func() {
		close(release)
		h.Close()
	})
	return h
}

func waitSignal(t *testing.T, ch <-chan struct{}, d time.Duration, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(d):
		t.Fatal(what)
	}
}

// Close's deadline cancels a webhook send still in flight: Close
// returns at the deadline and the request is abandoned, so no
// goroutine stays parked on the hanging endpoint.
func TestWebhook_CloseCancelsHangingSend(t *testing.T) {
	hook := newHangingWebhook(t)
	cfg := immediateCfg(4)
	cfg.WebhookURL = hook.URL
	e := New(cfg, nil)

	e.RecordAuthFailure(context.Background(), "user:hang")
	waitSignal(t, hook.entered, 2*time.Second, "webhook never called")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := e.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close = %v, want DeadlineExceeded", err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("Close took %s, want about its 100ms deadline", took)
	}
	waitSignal(t, hook.gone, time.Second, "webhook request still in flight after Close returned")
}

// A webhook nobody answers is abandoned after the delivery timeout,
// without waiting for Close.
func TestWebhook_DeliveryTimeoutBoundsHangingSend(t *testing.T) {
	hook := newHangingWebhook(t)
	cfg := immediateCfg(4)
	cfg.WebhookURL = hook.URL
	cfg.DeliveryTimeout = 50 * time.Millisecond
	e := New(cfg, nil)
	closeOnCleanup(t, e)

	e.RecordAuthFailure(context.Background(), "user:hang")
	waitSignal(t, hook.entered, 2*time.Second, "webhook never called")
	waitSignal(t, hook.gone, time.Second, "webhook request outlived the delivery timeout")
}

// The webhook is posted to the configured URL only: a redirect is
// reported, never followed, so the alert body never reaches another
// endpoint.
func TestWebhook_DoesNotFollowRedirects(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(other.Close)
	var hits atomic.Int32
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, other.URL+"/hook", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(hook.Close)

	cfg := immediateCfg(4)
	cfg.WebhookURL = hook.URL
	e := New(cfg, nil)
	e.RecordAuthFailure(context.Background(), "user:redirect")
	if err := e.Close(context.Background()); err != nil { // waits for the send
		t.Fatalf("Close: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("webhook hits = %d, want 1", hits.Load())
	}
	if n := elsewhere.Load(); n != 0 {
		t.Fatalf("redirect target hit %d time(s), want 0", n)
	}
}

// recordingTransport answers 204 and counts the requests that reached it.
type recordingTransport struct{ calls atomic.Int32 }

func (rt *recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rt.calls.Add(1)
	return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Request: r}, nil
}

// The webhook client enforces kit's offline policy beneath its own
// transport: an offline-marked context never reaches the wire.
func TestWebhook_OfflineContextRefused(t *testing.T) {
	rt := &recordingTransport{}
	send := webhookHandler("https://alerts.example/hook", newWebhookClient(rt))
	a := Alert{Kind: EventAuthFailure}

	send(offlineCtx(), a)
	if n := rt.calls.Load(); n != 0 {
		t.Fatalf("offline send reached the transport %d time(s), want 0", n)
	}
	send(context.Background(), a)
	if n := rt.calls.Load(); n != 1 {
		t.Fatalf("online send reached the transport %d time(s), want 1", n)
	}
}

// fakeSMTP is a loopback SMTP server. hang makes it accept and never
// greet; otherwise it runs a minimal session and records the DATA body.
type fakeSMTP struct {
	ln     net.Listener
	hang   bool
	conns  chan struct{} // one per accepted connection
	closed chan struct{} // one per connection the client closed
	data   chan string   // one per message received
}

func newFakeSMTP(t *testing.T, hang bool) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeSMTP{ln: ln, hang: hang, conns: make(chan struct{}, 8), closed: make(chan struct{}, 8), data: make(chan string, 8)}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakeSMTP) cfg() SMTPConfig {
	host, port, _ := net.SplitHostPort(s.ln.Addr().String())
	p, _ := strconv.Atoi(port)
	return SMTPConfig{Host: host, Port: p, From: "dpkms@example.net", To: "ops@example.net"}
}

func (s *fakeSMTP) serve(c net.Conn) {
	defer c.Close()
	s.conns <- struct{}{}
	r := bufio.NewReader(c)
	if s.hang {
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, err := r.ReadByte(); err != nil && !errors.Is(err, os.ErrDeadlineExceeded) {
			s.closed <- struct{}{}
		}
		return
	}
	reply := func(line string) { _, _ = c.Write([]byte(line + "\r\n")) }
	reply("220 fake ESMTP")
	var body []byte
	inData := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		if inData {
			if line == ".\r\n" {
				inData = false
				s.data <- string(body)
				reply("250 queued")
				continue
			}
			body = append(body, line...)
			continue
		}
		switch {
		case len(line) >= 4 && (line[:4] == "EHLO" || line[:4] == "HELO"):
			reply("250 fake")
		case len(line) >= 4 && line[:4] == "DATA":
			inData = true
			reply("354 go ahead")
		case len(line) >= 4 && line[:4] == "QUIT":
			reply("221 bye")
			return
		default:
			reply("250 ok")
		}
	}
}

// The SMTP handler delivers the alert through a full SMTP session.
func TestSMTP_DeliversAlert(t *testing.T) {
	srv := newFakeSMTP(t, false)
	cfg := immediateCfg(4)
	cfg.SMTP = srv.cfg()
	e := New(cfg, nil)
	e.RecordAuthFailure(context.Background(), "user:mail")
	if err := e.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case body := <-srv.data:
		if want := "Subject: [Security Alert] " + EventAuthFailure; !strings.Contains(body, want) {
			t.Fatalf("message lacks %q:\n%s", want, body)
		}
	default:
		t.Fatal("no message delivered")
	}
}

// Close's deadline cancels an SMTP conversation the server never
// answers: the connection is closed, not left waiting for a greeting.
func TestSMTP_CloseCancelsHangingSend(t *testing.T) {
	srv := newFakeSMTP(t, true)
	cfg := immediateCfg(4)
	cfg.SMTP = srv.cfg()
	e := New(cfg, nil)

	e.RecordAuthFailure(context.Background(), "user:hang")
	waitSignal(t, srv.conns, 2*time.Second, "SMTP server never dialed")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := e.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close = %v, want DeadlineExceeded", err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("Close took %s, want about its 100ms deadline", took)
	}
	waitSignal(t, srv.closed, time.Second, "SMTP connection still open after Close returned")
}

// An SMTP server that never answers is abandoned after the delivery
// timeout, without waiting for Close.
func TestSMTP_DeliveryTimeoutBoundsHangingSend(t *testing.T) {
	srv := newFakeSMTP(t, true)
	cfg := immediateCfg(4)
	cfg.SMTP = srv.cfg()
	cfg.DeliveryTimeout = 50 * time.Millisecond
	e := New(cfg, nil)
	closeOnCleanup(t, e)

	e.RecordAuthFailure(context.Background(), "user:hang")
	waitSignal(t, srv.conns, 2*time.Second, "SMTP server never dialed")
	waitSignal(t, srv.closed, time.Second, "SMTP connection outlived the delivery timeout")
}

// The SMTP dial goes through kit's offline guard: an offline-marked
// context never opens a socket.
func TestSMTP_OfflineContextRefused(t *testing.T) {
	var dials atomic.Int32
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("test dialer: no network")
	}
	send := smtpHandler(SMTPConfig{Host: "smtp.example.net", Port: 587, From: "a@example.net", To: "b@example.net"}, dial)
	a := Alert{Kind: EventAuthFailure}

	send(offlineCtx(), a)
	if n := dials.Load(); n != 0 {
		t.Fatalf("offline send dialed %d time(s), want 0", n)
	}
	send(context.Background(), a)
	if n := dials.Load(); n != 1 {
		t.Fatalf("online send dialed %d time(s), want 1", n)
	}
}

// offlineCtx is a context carrying kit's --offline mark.
func offlineCtx() context.Context {
	return netpolicy.WithOffline(context.Background(), true)
}

// closeOnCleanup closes e when the test ends, bounded so a send that
// ignores its context fails the test instead of hanging it.
func closeOnCleanup(t *testing.T, e *Emitter) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = e.Close(ctx)
	})
}
