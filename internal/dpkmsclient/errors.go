package dpkmsclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"hop.top/kit/go/console/output"
)

// maxErrorBody caps how much of a non-2xx body is read and kept.
const maxErrorBody = 64 << 10

// maxMessageBody caps a non-envelope body quoted in an error message.
const maxMessageBody = 512

// RemoteError is a non-2xx answer from dpkms. The envelope Do returns
// retains it, so callers can branch on the dpkms error code (for example
// POLICY_DENIED or ENTITLEMENT_REQUIRED) or decode Body (a 503 from
// /healthz still carries the health report).
type RemoteError struct {
	// URL is the endpoint's base URL.
	URL string
	// StatusCode is the HTTP status.
	StatusCode int
	// Code and Message come from the dpkms error envelope
	// ({"error":{"code","message"}}); both are empty when the body is not
	// one.
	Code    string
	Message string
	// Body is the raw response body, capped at 64 KiB.
	Body []byte
}

// Error renders the status with the dpkms code and message.
func (e *RemoteError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "dpkms at %s returned %d", e.URL, e.StatusCode)
	if e.Code != "" {
		b.WriteString(" " + e.Code)
	}
	msg := e.Message
	if msg == "" && e.Code == "" {
		msg = strings.TrimSpace(string(e.Body))
		if len(msg) > maxMessageBody {
			msg = msg[:maxMessageBody] + "…"
		}
	}
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	b.WriteString(": " + msg)
	return b.String()
}

// statusClass maps a dpkms HTTP status to kit's class and exit code.
func statusClass(status int) (code string, exit int) {
	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return output.CodeUnauthorized, output.ExitUnauthorized
	case status == http.StatusNotFound:
		return output.CodeNotFound, output.ExitNotFound
	case status == http.StatusConflict:
		return output.CodeConflict, output.ExitConflict
	case status == http.StatusBadRequest, status == http.StatusUnprocessableEntity:
		return output.CodeUsage, output.ExitUsage
	case status == http.StatusTooManyRequests:
		return output.CodeRateLimited, output.ExitRateLimited
	case status >= http.StatusInternalServerError:
		return output.CodeTransient, output.ExitTransient
	default:
		return output.CodeGeneric, output.ExitGeneric
	}
}

// statusError builds the envelope for a non-2xx answer.
func (c *Client) statusError(status int, body []byte) error {
	re := &RemoteError{URL: c.base, StatusCode: status, Body: body}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &env) == nil {
		re.Code, re.Message = env.Error.Code, env.Error.Message
	}
	code, exit := statusClass(status)
	e := output.WrapError(re, code, exit)
	if code == output.CodeUnauthorized {
		e.SuggestedFix = "check the token configured for " + c.base +
			" (server.token, or the token of its server.urls entry), and that it grants this operation"
	}
	return e
}

// transportError classifies a request that got no HTTP answer. Without a
// connection nothing answered at the endpoint: PREREQUISITE. Once a
// connection was up, the request may have reached dpkms and the failure
// (timeout, dropped connection) is TRANSIENT.
func (c *Client) transportError(ctx context.Context, err error, connected bool) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return err
	}
	if tlsFailure(err) {
		e := output.WrapError(fmt.Errorf("dpkms at %s: TLS failed: %w", c.base, err),
			output.CodePrerequisite, output.ExitPrerequisite)
		e.SuggestedFix = "check the URL scheme (http or https) and that the host's certificate is trusted"
		return e
	}
	if !connected || dialFailure(err) {
		e := output.WrapError(fmt.Errorf("dpkms at %s unreachable: %w", c.base, err),
			output.CodePrerequisite, output.ExitPrerequisite)
		e.SuggestedFix = "start dpkms (`dpkms serve`) or check the host is reachable; " +
			"ctxt never falls back to another instance"
		return e
	}
	return output.WrapError(fmt.Errorf("request to dpkms at %s failed after connecting: %w", c.base, err),
		output.CodeTransient, output.ExitTransient)
}

// dialFailure reports a failure while connecting: refused, unroutable or
// an unknown host. It covers transports that skip the httptrace hooks.
func dialFailure(err error) bool {
	var opErr *net.OpError
	var dnsErr *net.DNSError
	return (errors.As(err, &opErr) && opErr.Op == "dial") || errors.As(err, &dnsErr)
}

// tlsFailure reports a failed handshake or certificate check.
func tlsFailure(err error) bool {
	var verifyErr *tls.CertificateVerificationError
	var recordErr tls.RecordHeaderError
	var authorityErr x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var invalidErr x509.CertificateInvalidError
	return errors.As(err, &verifyErr) || errors.As(err, &recordErr) ||
		errors.As(err, &authorityErr) || errors.As(err, &hostErr) || errors.As(err, &invalidErr)
}
