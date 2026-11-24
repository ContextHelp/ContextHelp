package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

func TestConformance_Governance(t *testing.T) {
	storagetest.RunGovernanceConformance(t, newTestDriver(t))
}
