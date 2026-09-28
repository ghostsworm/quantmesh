package position

import (
	"math"
	"quantmesh/config"
	"quantmesh/storage"
	"quantmesh/strategy/regime"
	"testing"
	"time"
)

type auditTradeRecorder struct {
	pnl  []float64
	keys []string
}

func (s *auditTradeRecorder) SaveTradeIdempotent(trade *storage.Trade) error {
	s.pnl = append(s.pnl, trade.PnL)
	s.keys = append(s.keys, trade.ExecutionKey)
	return nil
}

func (s *auditTradeRecorder) SaveTrade(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, fee float64, feeAsset string, createdAt time.Time, botID string) error {
	s.pnl = append(s.pnl, pnl)
	return nil
}
func (s *auditTradeRecorder) SaveTradeWithDeviation(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, fee float64, feeAsset string, buyDev, sellDev float64, createdAt time.Time, botID string) error {
	return s.SaveTrade(buyOrderID, sellOrderID, exchange, symbol, buyPrice, sellPrice, quantity, pnl, fee, feeAsset, createdAt, botID)
}
func (s *auditTradeRecorder) SaveTradeWithExchangePnL(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, exchangePnL, fee float64, feeAsset string, buyDev, sellDev float64, createdAt time.Time, botID string) error {
	return s.SaveTrade(buyOrderID, sellOrderID, exchange, symbol, buyPrice, sellPrice, quantity, pnl, fee, feeAsset, createdAt, botID)
}

func TestAuditShortRealizedProfitMustHaveCorrectSign(t *testing.T) {
	spm := newDirectionTestSPM(t, "SHORT", nil)
	st := &auditTradeRecorder{}
	spm.SetTradeStorage(st)
	fillSlot(spm, 100, 1, 100, "")
	cid := spm.generateClientOrderID(100, "BUY", "")
	spm.OnOrderUpdate(OrderUpdate{OrderID: 201, ClientOrderID: cid, Symbol: "BTCUSDT", Status: "FILLED", Side: "BUY", ExecutedQty: 1, AvgPrice: 90, Commission: 0.01, CommissionAsset: "USDT", RealizedPnL: 10})
	if len(st.pnl) != 1 || st.pnl[0] != 10 || len(st.keys) != 1 || st.keys[0] == "" {
		t.Fatalf("SHORT entry=100, close=90, qty=1, actual gain=10; saved pnl=%v", st.pnl)
	}
}

func TestAuditCumulativeAverageFillPriceMustNotBeAveragedAgain(t *testing.T) {
	spm := newFillFeeSPM(t, "futures", nil)
	cid := openBuy(spm, 101)
	spm.OnOrderUpdate(OrderUpdate{OrderID: 101, ClientOrderID: cid, Symbol: "ETHUSDT", Status: "PARTIALLY_FILLED", Side: "BUY", ExecutedQty: 0.5, AvgPrice: 100, Commission: 0.01, CommissionAsset: "USDT"})
	spm.OnOrderUpdate(OrderUpdate{OrderID: 101, ClientOrderID: cid, Symbol: "ETHUSDT", Status: "FILLED", Side: "BUY", ExecutedQty: 1, AvgPrice: 110, Commission: 0.01, CommissionAsset: "USDT"})
	slot := spm.getOrCreateSlot(fillFeeTestPrice)
	slot.mu.RLock()
	defer slot.mu.RUnlock()
	if math.Abs(slot.AvgBuyPrice-110) > 1e-9 {
		t.Fatalf("exchange cumulative avg=110 for 1 unit; local avg=%v", slot.AvgBuyPrice)
	}
}

func TestAuditTriggerPriceMustNotSkipExistingShortStopLoss(t *testing.T) {
	exec := newLiqFakeVenue(-1)
	exec.limitFillRatio = 1
	spm, _ := newLiqTestSPM(t, "SHORT", exec)
	configureTestProtective(t, spm, exec, nil)
	spm.config.Trading.TriggerPrice = 50000
	spm.config.Trading.GridRiskControl = config.GridRiskControl{Enabled: true, StopLossRatio: 0.05}
	fillSlot(spm, 40000, 1, 40000, "")
	if err := spm.AdjustOrders(45000); err != nil {
		t.Fatal(err)
	}
	waitTestProtective(t, spm)
	if len(exec.limitReqs) == 0 {
		t.Fatal("existing SHORT loses 12.5%, but trigger-price guard skips 5% stop loss")
	}
}

func TestAuditInternalStopLossMustCrossCurrentBook(t *testing.T) {
	exec := newLiqFakeVenue(1)
	exec.bid, exec.ask, exec.limitFillRatio = 47000, 47001, 1
	spm, _ := newLiqTestSPM(t, "LONG", exec)
	configureTestProtective(t, spm, exec, nil)
	spm.config.Trading.GridRiskControl = config.GridRiskControl{Enabled: true, StopLossRatio: 0.05}
	fillSlot(spm, 60000, 1, 60000, "")
	if err := spm.AdjustOrders(50000); err != nil {
		t.Fatal(err)
	}
	waitTestProtective(t, spm)
	if len(exec.limitReqs) != 1 {
		t.Fatalf("expected one close, got %d", len(exec.limitReqs))
	}
	if got := exec.limitReqs[0].Price; got > 47000 {
		t.Fatalf("SELL stop-loss price %.2f is above best bid 47000; limit can remain unfilled", got)
	}
	if !exec.flat() || spm.GetNetPositionQty() != 0 {
		t.Fatal("stop-loss only submitted without settling exposure")
	}
}

type auditFavorableFunding struct{ fakeFundingMonitor }

func (f *auditFavorableFunding) GetBuyBias() float64  { return 1.2 }
func (f *auditFavorableFunding) GetSellBias() float64 { return 1.2 }

func TestAuditFundingTrendMustNotOverrideHardPause(t *testing.T) {
	spm, exec := newR5bSPM(t, "LONG", 6)
	spm.config.Trading.OpenPositionControl.PauseOpening = true
	spm.isOpeningPaused.Store(true)
	spm.config.FundingRate.BiasEnabled = true
	spm.config.FundingRate.TrendSyncEnabled = true
	spm.SetFundingMonitor(&auditFavorableFunding{})
	spm.ConfigureRegimeControl(&fakeRegimeProvider{snap: readySnap(regime.TrendUp)}, RegimeControlOptions{FilterEnabled: true})
	orders := adjustAt(t, spm, exec, 100)
	if n := len(ordersBy(orders, "BUY", false)); n > 0 {
		t.Fatalf("hard opening pause=true, favorable funding+trend still submitted %d opens", n)
	}
}
