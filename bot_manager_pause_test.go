package main

import (
	"context"
	"sync"
	"testing"

	"quantmesh/config"
	"quantmesh/execution"
	"quantmesh/position"
	"quantmesh/utils"
)

// pauseTestExecutor 記錄撤單請求
type pauseTestExecutor struct {
	mu        sync.Mutex
	cancelled []int64
}

func (e *pauseTestExecutor) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	return &position.Order{ClientOrderID: req.ClientOrderID}, nil
}

func (e *pauseTestExecutor) BatchPlaceOrders(orders []*position.OrderRequest) ([]*position.Order, bool) {
	return nil, false
}

func (e *pauseTestExecutor) BatchPlaceOrdersWithDetails(orders []*position.OrderRequest) *position.BatchPlaceOrdersResult {
	return &position.BatchPlaceOrdersResult{}
}

func (e *pauseTestExecutor) BatchCancelOrders(orderIDs []int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cancelled = append(e.cancelled, orderIDs...)
	return nil
}

func (e *pauseTestExecutor) cancelledIDs() []int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]int64(nil), e.cancelled...)
}

// pauseTestExchange 最小化 position.IExchange 實現
type pauseTestExchange struct{}

func (pauseTestExchange) GetName() string { return "mock" }
func (pauseTestExchange) GetPositions(ctx context.Context, symbol string) (interface{}, error) {
	return nil, nil
}
func (pauseTestExchange) GetOpenOrders(ctx context.Context, symbol string) (interface{}, error) {
	return nil, nil
}
func (pauseTestExchange) GetOrder(ctx context.Context, symbol string, orderID int64) (interface{}, error) {
	return nil, nil
}
func (pauseTestExchange) GetBaseAsset() string                                     { return "BTC" }
func (pauseTestExchange) CancelAllOrders(ctx context.Context, symbol string) error { return nil }
func (pauseTestExchange) GetAccount(ctx context.Context) (interface{}, error)      { return nil, nil }
func (pauseTestExchange) GetPriceDecimals() int                                    { return 2 }
func (pauseTestExchange) GetQuantityDecimals() int                                 { return 3 }
func (pauseTestExchange) GetOrderBook(ctx context.Context, symbol string, limit int) (*position.OrderBook, error) {
	return nil, nil
}
func (pauseTestExchange) GetOrderFills(ctx context.Context, symbol string, orderID int64) (interface{}, error) {
	return nil, nil
}
func (pauseTestExchange) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return 50000, nil
}
func (pauseTestExchange) GetQuoteAsset() string { return "USDT" }
func (pauseTestExchange) GetBalance(ctx context.Context, asset string) (float64, error) {
	return 0, nil
}

// C2：BotRuntime.PauseOpening/ResumeOpening 必須作用到 SPM，而不只是修改展示用的 br.Config 拷貝
func TestBotRuntimePauseOpeningPropagatesToSuperPositionManager(t *testing.T) {
	const (
		openOrderID = int64(4242)
		slotPrice   = 50000.0
	)
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.Direction = "LONG"
	cfg.Trading.MarketType = "futures"
	cfg.Trading.PriceInterval = 100
	exec := &pauseTestExecutor{}
	spm := position.NewSuperPositionManager(cfg, exec, pauseTestExchange{}, 2, 3)

	// 掛一個開倉 BUY 單
	coid := utils.GenerateOrderIDWithSource(slotPrice, "BUY", 2, "")
	spm.OnOrderUpdate(position.OrderUpdate{OrderID: openOrderID, ClientOrderID: coid, Symbol: "BTCUSDT", Status: "NEW", Side: "BUY", Price: slotPrice})

	br := &BotRuntime{
		BotID:  "bot-c2",
		Config: config.BotConfig{ID: "bot-c2", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"},
		Inner:  &SymbolRuntime{SuperPositionManager: spm},
	}

	br.PauseOpening("circuit_breaker")
	if !spm.IsOpeningPaused() || spm.GetOpeningPauseReason() != "circuit_breaker" {
		t.Fatalf("SPM not paused: paused=%v reason=%q", spm.IsOpeningPaused(), spm.GetOpeningPauseReason())
	}
	if !br.Config.OpenPositionControl.PauseOpening || br.GetBotRiskControl().PauseOpeningReason != "circuit_breaker" {
		t.Fatalf("display config not synced: %+v", br.Config.OpenPositionControl)
	}
	found := false
	for _, id := range exec.cancelledIDs() {
		if id == openOrderID {
			found = true
		}
	}
	if !found {
		t.Fatalf("opening order %d not cancelled on pause, cancelled=%v", openOrderID, exec.cancelledIDs())
	}

	br.ResumeOpening()
	if spm.IsOpeningPaused() {
		t.Fatalf("SPM still paused after ResumeOpening")
	}
	if br.Config.OpenPositionControl.PauseOpening {
		t.Fatalf("display config still paused after ResumeOpening")
	}
}

func TestBotRuntimePauseOpeningWithoutSPM(t *testing.T) {
	br := &BotRuntime{BotID: "bot-no-spm"}
	br.PauseOpening("manual")
	if !br.Config.OpenPositionControl.PauseOpening {
		t.Fatalf("config not paused")
	}
	br.ResumeOpening()
	if br.Config.OpenPositionControl.PauseOpening {
		t.Fatalf("config not resumed")
	}
}

func TestBotRuntimePauseOpeningUsesSpecializedRuntimeGate(t *testing.T) {
	gate := &execution.OpeningGate{}
	br := &BotRuntime{BotID: "carry", Config: config.BotConfig{ID: "carry"}, Inner: &SymbolRuntime{OpeningGate: gate}}
	br.PauseOpening("manual")
	if !gate.HasBlock("manual") {
		t.Fatal("specialized runtime opening was not blocked")
	}
	status := br.GetPositionStatus()
	if status["paused"] != true || status["valuation_available"] != false {
		t.Fatalf("specialized strategy status claimed invalid exposure: %+v", status)
	}
	if err := br.ResumeOpeningManually(); err != nil {
		t.Fatal(err)
	}
	if gate.HasBlock("manual") {
		t.Fatal("manual resume did not clear the manual block")
	}
}

func TestBotRuntimeManualCloseRoutesThroughSpecializedOwner(t *testing.T) {
	called := false
	inner := &SymbolRuntime{CloseForManual: func(context.Context, config.ClosePositionConfig) (*position.ClosePositionRecord, error) {
		called = true
		return &position.ClosePositionRecord{RecordID: "carry-close-1", Status: position.CloseStatusFilled}, nil
	}}
	bot := &BotRuntime{BotID: "carry", Inner: inner}
	record, err := bot.ClosePositions(context.Background(), config.ClosePositionConfig{Method: "market"})
	if err != nil || !called || record == nil || record.RecordID != "carry-close-1" {
		t.Fatalf("specialized close not routed: record=%+v called=%v err=%v", record, called, err)
	}
	rows := bot.GetCloseRecords()
	if len(rows) != 1 || rows[0].RecordID != record.RecordID {
		t.Fatalf("specialized close not listed: %+v", rows)
	}
}
