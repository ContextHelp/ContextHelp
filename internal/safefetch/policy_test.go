package safefetch

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func TestPolicyCheck_BlockedRanges(t *testing.T) {
	cases := []struct {
		addr, rangeName string
	}{
		// IPv4
		{"0.0.0.0", "unspecified"},
		{"0.1.2.3", "unspecified"},
		{"10.0.0.1", "private"},
		{"10.255.255.255", "private"},
		{"100.64.0.1", "carrier-grade NAT"},
		{"100.127.255.254", "carrier-grade NAT"},
		{"127.0.0.1", "loopback"},
		{"127.255.255.254", "loopback"},
		{"169.254.169.254", "link-local"},
		{"169.254.0.1", "link-local"},
		{"172.16.0.1", "private"},
		{"172.31.255.255", "private"},
		{"192.168.1.1", "private"},
		{"224.0.0.1", "multicast"},
		{"239.255.255.250", "multicast"},
		{"240.0.0.1", "reserved"},
		{"255.255.255.255", "reserved"},
		// IPv6
		{"::", "unspecified"},
		{"::1", "loopback"},
		{"fc00::1", "unique-local"},
		{"fd12:3456::1", "unique-local"},
		{"fe80::1", "link-local"},
		{"fe80::1%en0", "link-local"},
		{"febf::1", "link-local"},
		{"fec0::1", "site-local"},
		{"ff02::1", "multicast"},
		{"ff05::1:3", "multicast"},
		// IPv4-mapped IPv6
		{"::ffff:127.0.0.1", "loopback"},
		{"::ffff:10.0.0.1", "private"},
		{"::ffff:169.254.169.254", "link-local"},
		{"::ffff:192.168.0.1", "private"},
		{"::ffff:100.64.0.1", "carrier-grade NAT"},
		{"::ffff:0.0.0.0", "unspecified"},
		// Other IPv6 forms that carry an IPv4 address
		{"::127.0.0.1", "loopback"},                // IPv4-compatible
		{"64:ff9b::a9fe:a9fe", "link-local"},       // NAT64 of 169.254.169.254
		{"64:ff9b::7f00:1", "loopback"},            // NAT64 of 127.0.0.1
		{"2002:7f00:1::1", "loopback"},             // 6to4 of 127.0.0.1
		{"2002:a9fe:a9fe::1", "link-local"},        // 6to4 of 169.254.169.254
		{"2002:c0a8:101::1", "private (RFC 1918)"}, // 6to4 of 192.168.1.1
	}
	var p Policy
	for _, tc := range cases {
		t.Run(tc.addr, func(t *testing.T) {
			err := p.Check(netip.MustParseAddr(tc.addr))
			if !errors.Is(err, ErrBlocked) {
				t.Fatalf("Check(%s) = %v, want ErrBlocked", tc.addr, err)
			}
			var be *BlockedError
			if !errors.As(err, &be) || !strings.HasPrefix(be.Range, tc.rangeName) {
				t.Fatalf("Check(%s) range = %+v, want %q", tc.addr, be, tc.rangeName)
			}
		})
	}
}

func TestPolicyCheck_PublicAllowed(t *testing.T) {
	for _, a := range []string{
		"1.1.1.1", "8.8.8.8", "93.184.216.34",
		"100.63.255.255", "100.128.0.0", // either side of CGNAT
		"172.15.255.255", "172.32.0.0", // either side of 172.16/12
		"169.253.255.255", "169.255.0.0", // either side of link-local
		"192.167.255.255", "192.169.0.0",
		"223.255.255.255",
		"2606:4700:4700::1111", "2001:4860:4860::8888",
		"::ffff:8.8.8.8", "64:ff9b::808:808", "2002:808:808::1",
	} {
		if err := (Policy{}).Check(netip.MustParseAddr(a)); err != nil {
			t.Errorf("Check(%s) = %v, want nil", a, err)
		}
	}
}

func TestPolicyCheck_Allowlists(t *testing.T) {
	p, err := ParsePolicy("fetch", false, []string{"Wiki.Corp.Example."}, []string{"10.20.0.0/16", "169.254.169.254", "fd00:abcd::/32"})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{"10.20.1.1", "::ffff:10.20.1.1", "169.254.169.254", "fd00:abcd::5"} {
		if err := p.Check(netip.MustParseAddr(a)); err != nil {
			t.Errorf("allowlisted %s: %v", a, err)
		}
	}
	for _, a := range []string{"10.21.0.1", "169.254.169.253", "fd00:abce::5", "127.0.0.1"} {
		if err := p.Check(netip.MustParseAddr(a)); !errors.Is(err, ErrBlocked) {
			t.Errorf("not allowlisted %s: got %v, want ErrBlocked", a, err)
		}
	}
	if !p.hostAllowed("wiki.corp.example") || !p.hostAllowed("WIKI.corp.example.") {
		t.Error("allowed host not matched case-insensitively")
	}
	if p.hostAllowed("corp.example") || p.hostAllowed("evil.wiki.corp.example") {
		t.Error("allowed host matched a different name")
	}

	all := Policy{AllowPrivate: true}
	if err := all.Check(netip.MustParseAddr("169.254.169.254")); err != nil {
		t.Errorf("allow_private: %v", err)
	}
}

func TestParsePolicy_RejectsMalformedEntries(t *testing.T) {
	for _, tc := range []struct {
		hosts, cidrs []string
		want         string
	}{
		{hosts: []string{""}, want: "fetch.allowed_hosts[0]"},
		{hosts: []string{"ok.example", "http://x.example"}, want: "fetch.allowed_hosts[1]"},
		{hosts: []string{"x.example:8080"}, want: "fetch.allowed_hosts[0]"},
		{hosts: []string{"*.example"}, want: "fetch.allowed_hosts[0]"},
		{cidrs: []string{"10.0.0.0/33"}, want: "fetch.allowed_cidrs[0]"},
		{cidrs: []string{"intranet"}, want: "fetch.allowed_cidrs[0]"},
	} {
		_, err := ParsePolicy("fetch", false, tc.hosts, tc.cidrs)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ParsePolicy(%v, %v) = %v, want error naming %s", tc.hosts, tc.cidrs, err, tc.want)
		}
	}
}

func TestBlockedError_NamesTheFix(t *testing.T) {
	err := Policy{}.check(defaultBlocked, "metadata.example", netip.MustParseAddr("169.254.169.254"))
	msg := err.Error()
	for _, want := range []string{"metadata.example", "169.254.169.254", "link-local", "fetch.allow_private", "fetch.allowed_cidrs"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q lacks %q", msg, want)
		}
	}
}
