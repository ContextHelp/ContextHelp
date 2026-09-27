package dpkmsclient

import (
	"net/http"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// ObserveUpgrade returns a round tripper that sends each request through
// base (http.DefaultTransport, resolved per request, when nil) and hands
// fn the decoded upgrade state of every /api/v1 response (under any base
// path) that carries upgrade.HeaderName. dpkms sets that header while an
// upgrade is in progress or has failed; a response without it, or with a
// value that does not decode, calls nothing.
//
// fn runs on the requesting goroutine before the response is returned,
// possibly from several goroutines at once.
//
// A Client takes it through WithTransport. Installed on
// http.DefaultTransport it also covers every client built without a
// Transport, a Client included.
func ObserveUpgrade(base http.RoundTripper, fn func(upgrade.Status)) http.RoundTripper {
	return &upgradeObserver{base: base, fn: fn}
}

type upgradeObserver struct {
	base http.RoundTripper
	fn   func(upgrade.Status)
}

func (o *upgradeObserver) RoundTrip(req *http.Request) (*http.Response, error) {
	base := o.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil || o.fn == nil || !strings.Contains(req.URL.Path, "/api/v1/") {
		return resp, err
	}
	if v := resp.Header.Get(upgrade.HeaderName); v != "" {
		if st, derr := upgrade.DecodeHeader(v); derr == nil {
			o.fn(st)
		}
	}
	return resp, nil
}
