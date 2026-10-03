package strategy

import (
	"context"
	"testing"

	"quantmesh/config"
)

type fundingCarryLegacyRuntimeView struct{ Strategy }

func TestFundingCarryManagerDoesNotReportUnstartedStrategyRunning(t *testing.T) {
	s, _, _ := newFundingCarryRepayIntentFixture()
	cfg := &config.Config{}
	cfg.Strategies.Configs = map[string]config.StrategyConfig{"funding_carry": {Enabled: true, Type: "funding_carry", Weight: 1}}
	manager := NewStrategyManager(cfg, 100)
	manager.RegisterStrategy("funding_carry", s, 1, 0)
	legacy := NewStrategyManager(cfg, 100)
	legacy.RegisterStrategy("funding_carry", fundingCarryLegacyRuntimeView{Strategy: s}, 1, 0)
	if status := legacy.GetStrategyStatus("funding_carry"); !status.IsEnabled || !status.IsRunning {
		t.Fatal("fixture did not reproduce legacy enabled-only manager fallback")
	}
	if status := manager.GetStrategyStatus("funding_carry"); !status.IsEnabled || status.IsRunning {
		t.Fatal("enabled but unstarted Funding Carry reported running")
	}
	_ = s.StopContext(context.Background())
}

func TestFundingCarryRunningReporterTracksActualLoop(t *testing.T) {
	s, margin, _ := newFundingCarryRepayIntentFixture()
	margin.positions = nil
	s.direction, s.marginDebt, s.marginBorrowTransferID, s.strategySpotKnown = DirectionNone, 0, 0, true
	if err := s.persistRuntimeStateLocked(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !s.IsRunning() {
		t.Fatal("successfully started loop reported stopped")
	}
	if err := s.StopContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.IsRunning() {
		t.Fatal("stopped loop reported running")
	}
}
