package position

import (
	"testing"

	"quantmesh/config"
)

func TestOpeningPausePublishesSharedGate(t *testing.T) {
	spm, _ := newStateTestSPM("LONG", "futures")
	spm.openingPauseMu.Lock()
	spm.setOpeningPausedLocked("risk")
	spm.openingPauseMu.Unlock()
	if !spm.OpeningGate().Blocked() || !spm.IsOpeningPaused() {
		t.Fatal("runtime pause was not visible to shared execution")
	}
	spm.OpeningGate().Block("independent_risk")
	spm.ResumeOpening()
	if !spm.OpeningGate().Blocked() || !spm.IsOpeningPaused() {
		t.Fatal("manual resume cleared an independent risk block")
	}
	spm.OpeningGate().Unblock("independent_risk")
	if spm.IsOpeningPaused() {
		t.Fatal("gate remained paused after all owners recovered")
	}
}

func TestConfiguredOpeningPauseIsEffectiveBeforeStart(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.OpenPositionControl.PauseOpening = true
	spm := NewSuperPositionManager(cfg, &MockExecutor{}, &MockExchange{}, 2, 4)
	if !spm.OpeningGate().Blocked() {
		t.Fatal("configured pause needs to apply before strategy StartAll")
	}
	spm.ResumeOpening()
	if !spm.OpeningGate().HasBlock("manual") || !spm.IsOpeningPaused() {
		t.Fatal("risk-source resume cleared a configured manual pause")
	}
	if err := spm.ReleaseManualOpeningPause(); err != nil || spm.IsOpeningPaused() {
		t.Fatalf("explicit manual resume did not clear configured pause: err=%v", err)
	}
}

func TestMarketOpeningGateKeepsInventoryProtectionRunning(t *testing.T) {
	spm, exec := newR5bSPM(t, "LONG", 4)
	fillSlot(spm, 100, 1, 100, "")
	spm.SetMarketRiskPaused(true)
	orders := adjustAt(t, spm, exec, 100)
	if len(orders) == 0 {
		t.Fatal("market pause suppressed inventory close maintenance")
	}
	for _, req := range orders {
		if !req.ReduceOnly || req.Side != "SELL" {
			t.Fatalf("market pause permitted new exposure: %+v", req)
		}
	}
	spm.openingPauseMu.Lock()
	spm.setOpeningPausedLocked("manual")
	spm.openingPauseMu.Unlock()
	spm.SetMarketRiskPaused(false)
	if !spm.IsOpeningPaused() {
		t.Fatal("market recovery cleared manual pause")
	}
}

func TestPriceFeedRecoveryDoesNotClearOtherOpeningBlocks(t *testing.T) {
	spm, _ := newStateTestSPM("LONG", "futures")
	spm.SetPriceFeedStale(true)
	spm.SetMarketRiskPaused(true)
	spm.SetPriceFeedStale(false)
	if !spm.OpeningGate().HasBlock("market_risk") || !spm.IsOpeningPaused() {
		t.Fatal("price-feed recovery cleared an unrelated market-risk block")
	}
	spm.SetMarketRiskPaused(false)
	if spm.IsOpeningPaused() {
		t.Fatal("opening gate remained blocked after both sources recovered")
	}
}
