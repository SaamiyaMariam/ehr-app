package integration

import "testing"

// Runs last (files execute alphabetically): after every scenario above has
// posted, voided and adjusted money in the shared schema, the whole ledger
// must still reconcile.
func TestZZGlobalReconciliationAfterAllScenarios(t *testing.T) {
	needDB(t)
	assertReconciled(t)
}
