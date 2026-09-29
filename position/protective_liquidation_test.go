package position

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/config"
)

func configureTestProtective(t *testing.T, spm *SuperPositionManager, venue LiquidationVenue, schedule func(func())) {
	t.Helper()
	if err := spm.ConfigureProtectiveLiquidation(t.Context(), venue, schedule); err != nil {
		t.Fatal(err)
	}
}

func waitTestProtective(t *testing.T, spm *SuperPositionManager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := spm.WaitProtectiveLiquidation(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestProtectiveLiquidationAllTriggersUseVerification(t *testing.T) {
	for _, trigger := range []string{"stop_loss", "trailing_take_profit", "close_condition"} {
		t.Run(trigger, func(t *testing.T) {
			v := newLiqFakeVenue(1)
			v.limitFillRatio = 1
			spm, _ := newLiqTestSPM(t, "LONG", v)
			fillSlot(spm, liqTestLast, 1, 60000, "")
			grc := config.GridRiskControl{Enabled: true}
			switch trigger {
			case "stop_loss":
				grc.StopLossRatio = 0.05
			case "trailing_take_profit":
				grc.TakeProfitTriggerRatio = 0.05
				grc.TrailingTakeProfitRatio = 0.02
				spm.peakPnL = 0.1
			case "close_condition":
				grc.CloseConditionEnabled = true
				grc.CloseConditionLossLimit = 0.05
			}
			spm.config.Trading.GridRiskControl = grc
			var queue []func()
			configureTestProtective(t, spm, v, func(work func()) { queue = append(queue, work) })
			var stops atomic.Int32
			spm.SetRequestStopFunc(func() {
				if spm.GetNetPositionQty() != 0 || spm.GetProtectiveLiquidationStatus().State != "completed" {
					t.Error("stop callback preceded verified settlement")
				}
				stops.Add(1)
			})
			if err := spm.AdjustOrders(liqTestLast); err != nil {
				t.Fatal(err)
			}
			if len(queue) != 1 || len(v.limitReqs) != 0 || !spm.IsOpeningPaused() || spm.GetProtectiveLiquidationStatus().Reason != trigger {
				t.Fatal("trigger did not defer/gate verification")
			}
			spm.ResumeOpening()
			if err := spm.ResumeOpeningManually(); err == nil {
				t.Fatal("manual recovery released queued protection")
			}
			for i := 0; i < 10; i++ {
				if err := spm.AdjustOrders(liqTestLast); err != nil {
					t.Fatal(err)
				}
			}
			spm.LiquidateAll()
			if len(queue) != 1 || len(v.limitReqs) != 0 || !spm.IsOpeningPaused() {
				t.Fatal("queued protection was bypassed or duplicated")
			}
			queue[0]() // AdjustOrders has released spm.mu
			waitTestProtective(t, spm)
			if !v.flat() || spm.GetNetPositionQty() != 0 || spm.GetProtectiveLiquidationStatus().State != "completed" {
				t.Fatal("trigger did not verify and settle")
			}
			wantStops := int32(0)
			if trigger == "close_condition" {
				wantStops = 1
			}
			if stops.Load() != wantStops || len(v.limitReqs) != 1 {
				t.Fatal("wrong callback or submission count")
			}
			if !spm.IsOpeningPaused() {
				t.Fatal("protective close silently reopened risk")
			}
			spm.ResumeOpening()
			if !spm.IsOpeningPaused() {
				t.Fatal("automatic recovery released completed protection")
			}
			spm.SetMarketRiskPaused(true)
			if err := spm.ResumeOpeningManually(); err != nil {
				t.Fatal(err)
			}
			if !spm.IsOpeningPaused() {
				t.Fatal("manual recovery released independent market protection")
			}
			spm.SetMarketRiskPaused(false)
			if spm.IsOpeningPaused() {
				t.Fatal("explicit resume after verified success failed")
			}
		})
	}
}

func TestUnverifiedCostBasisDoesNotMasqueradeAsZeroPnLForRiskControls(t *testing.T) {
	for _, trigger := range []string{"stop_loss", "trailing_take_profit", "close_condition"} {
		t.Run(trigger, func(t *testing.T) {
			venue := newLiqFakeVenue(1)
			spm, _ := newLiqTestSPM(t, "LONG", venue)
			fillSlot(spm, liqTestLast, 1, 60000, "")
			slotRaw, ok := spm.slots.Load(liqTestLast)
			if !ok {
				t.Fatal("filled inventory slot is missing")
			}
			slot := slotRaw.(*InventorySlot)
			slot.mu.Lock()
			slot.AvgBuyPrice = 0
			slot.CostBasisUnverified = true
			slot.mu.Unlock()
			spm.refreshCostBasisOpeningGate()

			grc := config.GridRiskControl{Enabled: true}
			switch trigger {
			case "stop_loss":
				grc.StopLossRatio = 0.05
			case "trailing_take_profit":
				grc.TakeProfitTriggerRatio = 0.05
				grc.TrailingTakeProfitRatio = 0.02
				spm.peakPnL = 0.1
			case "close_condition":
				grc.CloseConditionEnabled = true
				grc.CloseConditionLossLimit = 0.05
			}
			spm.config.Trading.GridRiskControl = grc
			var queued []func()
			configureTestProtective(t, spm, venue, func(work func()) { queued = append(queued, work) })
			if err := spm.AdjustOrders(liqTestLast - 10000); err != nil {
				t.Fatal(err)
			}
			if len(queued) != 0 || spm.GetProtectiveLiquidationStatus().Reason != "" {
				t.Fatal("unverified zero PnL triggered an automatic PnL-based liquidation")
			}
			if trigger == "trailing_take_profit" && spm.peakPnL != 0.1 {
				t.Fatalf("unverified PnL changed trailing peak: got %v", spm.peakPnL)
			}
			if !spm.OpeningGate().HasBlock("grid_cost_basis_unverified") {
				t.Fatal("unverified position did not retain the opening gate")
			}
		})
	}
}

func TestManualPauseKeepsProtectivePositionManagementActive(t *testing.T) {
	venue := newLiqFakeVenue(1)
	venue.limitFillRatio = 1
	spm, _ := newLiqTestSPM(t, "LONG", venue)
	fillSlot(spm, liqTestLast, 1, 60000, "")
	spm.config.Trading.GridRiskControl = config.GridRiskControl{Enabled: true, StopLossRatio: 0.05}
	var work func()
	configureTestProtective(t, spm, venue, func(job func()) { work = job })

	spm.Pause()
	if !spm.IsOpeningPaused() {
		t.Fatal("manual pause must block new opening admission")
	}
	if err := spm.AdjustOrders(liqTestLast); err != nil {
		t.Fatal(err)
	}
	if work == nil || !spm.IsOpeningPaused() {
		t.Fatal("manual pause skipped stop-loss protection or released the opening gate")
	}
	work()
	waitTestProtective(t, spm)
	if !venue.flat() || spm.GetProtectiveLiquidationStatus().State != "completed" {
		t.Fatal("verified protective close did not run while manually paused")
	}
}

func TestProtectiveLiquidationFailureDoesNotStopOrResume(t *testing.T) {
	base := newLiqFakeVenue(1)
	venue := &uncertainLiquidationVenue{liqFakeVenue: base, ackOnly: true}
	spm, _ := newLiqTestSPM(t, "LONG", base)
	fillSlot(spm, liqTestLast, 1, 60000, "")
	spm.config.Trading.GridRiskControl = config.GridRiskControl{CloseConditionEnabled: true, CloseConditionLossLimit: 0.05}
	var stopped atomic.Bool
	spm.SetRequestStopFunc(func() { stopped.Store(true) })
	configureTestProtective(t, spm, venue, nil)
	if err := spm.AdjustOrders(liqTestLast); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := spm.WaitProtectiveLiquidation(ctx); err == nil {
		t.Fatal("unconfirmed cancellation reported complete")
	}
	spm.ResumeOpening()
	if err := spm.ResumeOpeningManually(); err == nil {
		t.Fatal("manual recovery released failed protection")
	}
	if stopped.Load() || !spm.IsOpeningPaused() || spm.GetProtectiveLiquidationStatus().State != "failed" || spm.GetProtectiveLiquidationStatus().Error == "" {
		t.Fatal("failed protection stopped the bot or released its hold")
	}
	if err := spm.AdjustOrders(liqTestLast); err != nil {
		t.Fatal(err)
	}
	if len(base.limitReqs) != 1 || len(base.marketReqs) != 0 {
		t.Fatal("failed protective task retried")
	}
}

func TestProtectiveLiquidationStopCancelsOwnedWorker(t *testing.T) {
	base := newLiqFakeVenue(1)
	venue := &blockingLiquidationVenue{liqFakeVenue: base, started: make(chan struct{}), resume: make(chan struct{})}
	spm, _ := newLiqTestSPM(t, "LONG", base)
	fillSlot(spm, liqTestLast, 1, 60000, "")
	spm.config.Trading.GridRiskControl = config.GridRiskControl{Enabled: true, StopLossRatio: 0.05}
	configureTestProtective(t, spm, venue, nil)
	if err := spm.AdjustOrders(liqTestLast); err != nil {
		t.Fatal(err)
	}
	select {
	case <-venue.started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start outside grid lock")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := spm.StopProtectiveLiquidation(ctx); err == nil {
		t.Fatal("interrupted protection cannot report success")
	}
	if spm.protective.queued.Load() || !spm.IsOpeningPaused() || !spm.liquidationNeedsReconciliation.Load() {
		t.Fatal("shutdown failed to settle worker/retain hold")
	}
	if len(base.marketReqs) != 0 {
		t.Fatal("cancelled worker submitted fallback")
	}
}

func TestProtectiveLiquidationMissingVenueFailsClosed(t *testing.T) {
	spm := newDirectionTestSPM(t, "LONG", &MockExecutor{})
	fillSlot(spm, liqTestLast, 1, 60000, "")
	spm.config.Trading.GridRiskControl = config.GridRiskControl{Enabled: true, StopLossRatio: 0.05}
	if err := spm.AdjustOrders(liqTestLast); err == nil || !strings.Contains(err.Error(), "verification venue") {
		t.Fatalf("missing wiring silently used legacy close: %v", err)
	}
	if !spm.IsOpeningPaused() || spm.GetProtectiveLiquidationStatus().State != "failed" {
		t.Fatal("missing capability released risk")
	}
}

func TestProtectiveLiquidationWaitCancellationDoesNotCancelWork(t *testing.T) {
	v := newLiqFakeVenue(1)
	v.limitFillRatio = 1
	spm, _ := newLiqTestSPM(t, "LONG", v)
	fillSlot(spm, liqTestLast, 1, 60000, "")
	spm.config.Trading.GridRiskControl = config.GridRiskControl{Enabled: true, StopLossRatio: 0.05}
	var work func()
	configureTestProtective(t, spm, v, func(job func()) { work = job })
	if err := spm.AdjustOrders(liqTestLast); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := spm.WaitProtectiveLiquidation(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	work()
	waitTestProtective(t, spm)
}
