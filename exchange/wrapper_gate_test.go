package exchange

import "testing"

func TestGateWrapperAdvertisesFundingIncomeHistory(t *testing.T) {
	if !(&gateWrapper{}).SupportsFundingIncomeHistory() {
		t.Fatal("Gate wrapper must enable authenticated funding-income synchronization")
	}
}
