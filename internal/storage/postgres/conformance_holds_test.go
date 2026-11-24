//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

func TestConformance_Governance(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.RunGovernanceConformance(t, drv)
}
