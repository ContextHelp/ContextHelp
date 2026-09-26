package cmd

import (
	"errors"
	"net"
	"net/url"
	"testing"

	"hop.top/kit/go/console/output"
)

// A request nothing answered is PREREQUISITE whatever the dial error's
// text; anything past the dial is left to the central classifier.
func TestContactError(t *testing.T) {
	get := func(inner error) error {
		return &url.Error{Op: "Get", URL: "http://h/healthz", Err: inner}
	}
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"dial timeout", get(&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("i/o timeout")}), output.ExitPrerequisite},
		{"dial refused", get(&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}), output.ExitPrerequisite},
		{"unknown host", get(&net.DNSError{Err: "lookup failed", Name: "h", IsNotFound: true}), output.ExitPrerequisite},
		{"read after connect", get(&net.OpError{Op: "read", Net: "tcp", Err: errors.New("reset by peer")}), output.ExitGeneric},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExitCodeFor(contactError("http://h", tc.err)); got != tc.want {
				t.Fatalf("exit %d, want %d", got, tc.want)
			}
		})
	}
}
