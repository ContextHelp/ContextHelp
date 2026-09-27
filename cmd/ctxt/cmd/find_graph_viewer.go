package cmd

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/browser/launch"
	"github.com/ideacrafterslabs/ctxt/internal/searchgraph/viewer"
	"golang.org/x/term"
)

// defaultGraphViewerIdle is how long the viewer server waits without a
// request before it stops. The page loads everything it needs up front,
// so an idle server only matters for a reload; ten minutes covers a
// reload after a look away without leaving a forgotten server running.
const defaultGraphViewerIdle = 10 * time.Minute

// graphViewerShutdownGrace bounds how long a stopping server waits for
// in-flight requests.
const graphViewerShutdownGrace = 5 * time.Second

// viewerCSP is the Content-Security-Policy sent with every response. It
// matches the policy index.html declares in its own meta tag, plus
// frame-ancestors, which only a header can carry.
const viewerCSP = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"connect-src 'self'; img-src 'self' data: blob:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// viewerContentTypes maps the extensions the viewer serves to their
// content types. It is a fixed table rather than mime.TypeByExtension,
// which consults the host's mime database and so varies by machine. A
// file with any other extension is not served.
var viewerContentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".txt":  "text/plain; charset=utf-8",
	".json": "application/json",
}

type viewerFile struct {
	body        []byte
	contentType string
}

// viewerHandler serves the search-graph viewer and one graph document,
// and nothing else, under a secret path prefix: /<token>/.
//
// The token is the access control. The server binds loopback only, but
// any local process or any page open in the browser can reach
// loopback; without the token they cannot name a path that answers.
// Every miss is the same 404, whether the token, the file or the Host
// header was wrong, so a probe learns nothing.
type viewerHandler struct {
	token []byte
	hosts map[string]bool
	files map[string]viewerFile
	touch func()
}

// newViewerHandler builds the handler for token, serving doc as the
// viewer's data file. hosts lists the Host header values accepted;
// checking it defeats DNS rebinding, where a hostile page resolves its
// own name to 127.0.0.1 to read the server as same-origin.
func newViewerHandler(token string, doc []byte, hosts []string) (*viewerHandler, error) {
	if token == "" {
		return nil, errors.New("viewer: empty token")
	}
	files := map[string]viewerFile{}
	assets := viewer.Assets()
	entries, err := fs.ReadDir(assets, ".")
	if err != nil {
		return nil, fmt.Errorf("viewer: list assets: %w", err)
	}
	for _, e := range entries {
		ct, ok := viewerContentTypes[path.Ext(e.Name())]
		if !ok || !e.Type().IsRegular() {
			continue
		}
		body, err := fs.ReadFile(assets, e.Name())
		if err != nil {
			return nil, fmt.Errorf("viewer: read %s: %w", e.Name(), err)
		}
		files[e.Name()] = viewerFile{body: body, contentType: ct}
	}
	if _, ok := files[viewer.IndexFile]; !ok {
		return nil, fmt.Errorf("viewer: embedded assets lack %s", viewer.IndexFile)
	}
	files[viewer.DataFile] = viewerFile{body: doc, contentType: viewerContentTypes[".json"]}

	allowed := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		allowed[strings.ToLower(h)] = true
	}
	return &viewerHandler{token: []byte(token), hosts: allowed, files: files, touch: func() {}}, nil
}

func (h *viewerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.touch()
	hdr := w.Header()
	hdr.Set("Cache-Control", "no-store")
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Referrer-Policy", "no-referrer")
	hdr.Set("Content-Security-Policy", viewerCSP)
	hdr.Set("Cross-Origin-Resource-Policy", "same-origin")
	hdr.Set("X-Frame-Options", "DENY")

	if !h.hosts[strings.ToLower(r.Host)] {
		http.NotFound(w, r)
		return
	}
	seg, rest, hasSlash := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if subtle.ConstantTimeCompare([]byte(seg), h.token) != 1 {
		http.NotFound(w, r)
		return
	}
	if !hasSlash {
		// The page loads its assets by relative URL, so it needs the
		// trailing slash to resolve them under the token.
		http.Redirect(w, r, "/"+seg+"/", http.StatusFound)
		return
	}
	if rest == "" {
		rest = viewer.IndexFile
	}
	f, ok := h.files[rest]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		hdr.Set("Allow", "GET, HEAD")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	hdr.Set("Content-Type", f.contentType)
	hdr.Set("Content-Length", fmt.Sprint(len(f.body)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(f.body)
}

// newViewerToken returns 256 random bits, URL-safe.
func newViewerToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("viewer: token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// isTerminal reports whether w is a terminal.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// graphViewerServer runs the ephemeral local viewer for one document.
type graphViewerServer struct {
	// Log receives the URL and lifecycle messages (stderr).
	Log io.Writer
	// Opener opens the URL when OpenBrowser is set.
	Opener launch.Opener
	// OpenBrowser asks for the browser to be opened: the caller sets it
	// only for an interactive terminal session without --no-browser.
	OpenBrowser bool
	// Idle stops the server after this long without a request.
	Idle time.Duration
	// Addr is the listen address; empty means 127.0.0.1:0.
	Addr string
	// ready, when set, receives the URL once the server accepts
	// connections. Tests use it; the CLI prints the URL instead.
	ready func(url string)
}

// Run serves doc until ctx is cancelled or the server has been idle for
// s.Idle. Either is a normal stop and returns nil.
func (s graphViewerServer) Run(ctx context.Context, doc []byte) error {
	addr := s.Addr
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("graph viewer: listen: %w", err)
	}
	host := ln.Addr().String()
	_, port, _ := net.SplitHostPort(host)

	token, err := newViewerToken()
	if err != nil {
		_ = ln.Close()
		return err
	}
	h, err := newViewerHandler(token, doc, []string{host, net.JoinHostPort("localhost", port)})
	if err != nil {
		_ = ln.Close()
		return err
	}
	activity := make(chan struct{}, 1)
	h.touch = func() {
		select {
		case activity <- struct{}{}:
		default:
		}
	}

	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	url := "http://" + host + "/" + token + "/"
	fmt.Fprintf(s.Log, "Search graph viewer: %s\n", url)
	fmt.Fprintf(s.Log, "Serving on loopback only; stops after %s without a request. Press Ctrl-C to stop.\n", s.Idle)
	if s.ready != nil {
		s.ready(url)
	}
	if s.OpenBrowser && s.Opener != nil {
		if err := s.Opener.Open(ctx, url); err != nil {
			fmt.Fprintf(s.Log, "Warning: could not open a browser (%v); open the URL above.\n", err)
		}
	}

	idle := time.NewTimer(s.Idle)
	defer idle.Stop()
	var reason string
	for reason == "" {
		select {
		case <-ctx.Done():
			reason = "interrupted"
		case <-idle.C:
			reason = fmt.Sprintf("idle for %s", s.Idle)
		case <-activity:
			idle.Reset(s.Idle)
		case err := <-served:
			return fmt.Errorf("graph viewer: serve: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), graphViewerShutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
	}
	if err := <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("graph viewer: serve: %w", err)
	}
	fmt.Fprintf(s.Log, "Search graph viewer stopped (%s).\n", reason)
	return nil
}
