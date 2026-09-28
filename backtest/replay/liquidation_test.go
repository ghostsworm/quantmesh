package replay

import (
	"math"
	"reflect"
	"testing"

	"quantmesh/position"
)

func TestReplayProtectiveLiquidationDeterministicAndPausesReentry(t *testing.T) {
	var reference *Result
	for run := 0; run < 12; run++ {
		cfg := testConfig(MatchingConfig{})
		cfg.Bot.Trading.GridRiskControl.Enabled = true
		cfg.Bot.Trading.GridRiskControl.StopLossRatio = 0.002
		eng := NewEngine(cfg)
		result, err := eng.Run(ticksFromPrices([]float64{2000, 1985, 1975, 1960, 1900, 2100}, 10))
		if err != nil {
			t.Fatal(err)
		}
		status := eng.spm.GetProtectiveLiquidationStatus()
		if status.State != "completed" || status.Reason != "stop_loss" || !eng.spm.IsOpeningPaused() || math.Abs(eng.spm.GetNetPositionQty()) > testFloatDelta || math.Abs(eng.ex.snapshot().netQty) > testFloatDelta {
			t.Fatalf("unsettled/reopened protective flow: %+v", status)
		}
		if eng.ex.openOrderCount() != 0 {
			t.Fatal("replay retained orders after protective close")
		}
		if result.Metrics.TakerFills == 0 || result.Metrics.FeesTaker <= 0 {
			t.Fatal("protective taker fees missing")
		}
		if reference != nil && (!reflect.DeepEqual(reference.Backtest.Trades, result.Backtest.Trades) || !reflect.DeepEqual(reference.Metrics, result.Metrics)) {
			t.Fatal("protective replay depends on goroutine scheduling")
		}
		reference = result
	}
}

func TestReplayMarketCloseContextUsesTakerAndStoresTerminal(t *testing.T) {
	ex, executor := newTestExchange(t, MatchingConfig{})
	ex.setMarket(testBaseTs, 2000)
	_, err := executor.PlaceOrderContext(t.Context(), &position.OrderRequest{Symbol: testSymbol, Side: "BUY", Price: 2000, Quantity: 1, ClientOrderID: "entry"})
	if err != nil {
		t.Fatal(err)
	}
	ex.setMarket(testBaseTs+1, 1900)
	ord, err := executor.PlaceOrderContext(t.Context(), &position.OrderRequest{Symbol: testSymbol, Side: "SELL", Type: "MARKET", Quantity: 1, ReduceOnly: true, ClientOrderID: "market-close"})
	if err != nil {
		t.Fatal(err)
	}
	ex.mu.Lock()
	terminal := ex.orderStates[ord.OrderID]
	ex.mu.Unlock()
	if terminal.Status != "FILLED" || terminal.ExecutedQty != 1 || terminal.AvgPrice != 1900 || terminal.Commission <= 0 || ex.snapshot().netQty != 0 {
		t.Fatalf("invalid market terminal: %+v", terminal)
	}
}
