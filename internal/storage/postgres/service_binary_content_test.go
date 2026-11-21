//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/service/servicetest"
)

// TestPostgres_ServiceBinaryContent runs binary content through the
// service and worker write paths against a fresh Postgres database, where
// a NUL or invalid UTF-8 byte in a text column is an error.
func TestPostgres_ServiceBinaryContent(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	servicetest.RunBinaryContent(t, drv)
}
