package investigator_test

import (
	"testing"
	"time"

	"github.com/transactx/backend/internal/investigator"
	"github.com/transactx/backend/internal/investigator/storetest"
)

func TestMemoryInvestigationStoreConformanceHarness(t *testing.T) {
	storetest.Run(t, func(t *testing.T, limits investigator.UsageAllowance) investigator.InvestigationStore {
		t.Helper()
		store := investigator.NewMemoryInvestigationStore(time.Now)
		store.SetUsageLimits(limits.InputTokens, limits.OutputTokens, limits.CostMicrousd)
		return store
	})
}
