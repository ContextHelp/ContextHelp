// Package contracttest holds one contract suite per provider role. Each
// suite states what every backend of the role must do, and is run against
// every backend the role has (see the table in the providers package's
// contract_test.go). A new backend joins its role's table and must pass
// the suite unchanged.
//
// Backends are exercised hermetically: HTTP through an in-memory
// RoundTripper, CLI tools through fixtures. Import from _test.go files
// only.
package contracttest

import (
	"context"
	"testing"
)

// Case is one backend under a role's suite.
type Case[P any] struct {
	// Name labels the backend in test output.
	Name string
	// New returns a ready provider, wired to its fixtures.
	New func(t testing.TB) P
	// Stub marks the placeholder backend, which has no I/O to cancel or
	// fail; assertions that need a real backend are skipped for it.
	Stub bool
}

// Run runs suite once per case, as subtests.
func Run[P any](t *testing.T, cases []Case[P], suite func(t *testing.T, c Case[P], p P)) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			suite(t, c, c.New(t))
		})
	}
}

// canceled returns an already-canceled context.
func canceled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func requireName(t *testing.T, name string) {
	t.Helper()
	if name == "" {
		t.Error("Name() is empty")
	}
}
