package cmd

import (
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
)

// portPolicy decides what serve does when a listener's port is taken.
type portPolicy int

const (
	// portExact binds the requested port or fails. Used for any port the
	// operator named: a client pointed at --port 8180 must never find a
	// different instance, or nothing, because serve drifted elsewhere.
	portExact portPolicy = iota
	// portPreferred binds the requested port, or an OS-assigned one when
	// it is in use. Used for built-in defaults only; the bound port is
	// announced and recorded in the pidfile.
	portPreferred
)

// policyFor returns portExact when the operator set the port and
// portPreferred when serve is using its built-in default.
func policyFor(explicit bool) portPolicy {
	if explicit {
		return portExact
	}
	return portPreferred
}

// listenTCP binds bind:port once and returns the live listener. The
// caller hands that listener to its server; the port is never released
// and re-bound, so two processes racing for one port cannot both be
// told it is free.
//
// Under portPreferred, only EADDRINUSE falls back to an OS-assigned port;
// any other bind error (bad address, permission) is returned as is.
func listenTCP(bind string, port int, policy portPolicy) (net.Listener, error) {
	addr := net.JoinHostPort(bind, fmt.Sprint(port))
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		return ln, nil
	}
	if policy == portExact || !errors.Is(err, syscall.EADDRINUSE) {
		return nil, err // *net.OpError already names "listen tcp <addr>"
	}
	ln, err = net.Listen("tcp", net.JoinHostPort(bind, "0"))
	if err != nil {
		return nil, fmt.Errorf("listen %s (fallback for busy port %d): %w", bind, port, err)
	}
	return ln, nil
}

// listenerPort returns the TCP port a listener is bound to.
func listenerPort(ln net.Listener) int {
	if a, ok := ln.Addr().(*net.TCPAddr); ok {
		return a.Port
	}
	return 0
}

// listenSpec names one serve listener and how to acquire it.
type listenSpec struct {
	name     string
	bind     string
	port     int
	explicit bool
}

// acquireListeners binds every spec, in order, and returns the live
// listeners in the same order. On any failure the listeners already
// bound are closed, so a refused start holds no port. A default port
// that was busy is reported on w with the port actually bound.
func acquireListeners(w io.Writer, specs []listenSpec) ([]net.Listener, error) {
	lns := make([]net.Listener, 0, len(specs))
	for _, s := range specs {
		ln, err := listenTCP(s.bind, s.port, policyFor(s.explicit))
		if err != nil {
			closeListeners(lns)
			if s.explicit {
				return nil, fmt.Errorf("%s port %d: %w", s.name, s.port, err)
			}
			return nil, fmt.Errorf("%s port: %w", s.name, err)
		}
		if got := listenerPort(ln); got != s.port {
			fmt.Fprintf(w, "note: %s default port %d is in use; bound %d instead\n", s.name, s.port, got)
		}
		lns = append(lns, ln)
	}
	return lns, nil
}

func closeListeners(lns []net.Listener) {
	for _, ln := range lns {
		_ = ln.Close()
	}
}
