package position

import "testing"

func TestEquityOpeningGatePreservesProtectiveClosesAndOtherOwners(t *testing.T) {
	spm, executor := newR5bSPM(t, "LONG", 4)
	fillSlot(spm, 100, 1, 100, "")
	if !spm.SetEquityRiskPaused(true) || spm.SetEquityRiskPaused(true) {
		t.Fatal("incorrect owned hold transition")
	}
	spm.ResumeOpening()
	if !spm.IsOpeningPaused() {
		t.Fatal("ordinary resume bypassed account data protection")
	}
	orders := adjustAt(t, spm, executor, 100)
	if len(orders) == 0 {
		t.Fatal("account data hold suppressed protective close maintenance")
	}
	for _, order := range orders {
		if !order.ReduceOnly || order.Side != "SELL" {
			t.Fatalf("new exposure while equity unavailable: %+v", order)
		}
	}
	spm.SetMarketRiskPaused(true)
	spm.SetEquityRiskPaused(false)
	if !spm.IsOpeningPaused() {
		t.Fatal("equity recovery cleared market risk")
	}
	spm.SetMarketRiskPaused(false)
	if spm.IsOpeningPaused() {
		t.Fatal("owned hold remained after all sources recovered")
	}
}
