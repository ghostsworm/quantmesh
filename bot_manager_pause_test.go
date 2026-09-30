package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/execution"
	"quantmesh/position"
	"quantmesh/risk"
	"quantmesh/utils"
)

// pauseTestExecutor 記錄撤單請求
type pauseTestExecutor struct {
	mu        sync.Mutex
	cancelled []int64
}

type pauseOwnedTestExecutor struct {
	*pauseTestExecutor
	called chan struct{}
}

func (e *pauseOwnedTestExecutor) CancelOwnedOpeningOrders(context.Context) error {
	select {
	case e.called <- struct{}{}:
	default:
	}
	return nil
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

func TestRiskPauseCoordinatorCannotBeBypassedByBotAutoResume(t *testing.T) {
	gate := &execution.OpeningGate{}
	bot := &BotRuntime{
		BotID: "coordinated-risk-pause",
		Config: config.BotConfig{OpenPositionControl: config.OpenPositionControl{
			BotRiskControl: &config.BotRiskControl{AutoResumeAfter: 1},
		}},
		Inner: &SymbolRuntime{OpeningGate: gate},
	}
	coordinator := risk.NewOpeningPauseCoordinator()
	bots := []risk.BotController{bot}

	coordinator.Pause("global_circuit_breaker", "circuit_breaker:daily_loss", bots)
	time.Sleep(1100 * time.Millisecond)
	if !coordinator.IsHeldBy("global_circuit_breaker") || !gate.HasBlock("global_circuit_breaker") {
		t.Fatal("configured Bot auto-resume bypassed an active coordinated risk hold")
	}
	if !coordinator.Release("global_circuit_breaker", bots) || gate.HasBlock("global_circuit_breaker") {
		t.Fatal("explicit coordinator release did not release its own hold")
	}
}

func TestCoordinatorReleasePreservesExplicitManualPause(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100
	spm := position.NewSuperPositionManager(cfg, &pauseTestExecutor{}, pauseTestExchange{}, 2, 3)
	bot := &BotRuntime{BotID: "manual-pause-owner", Inner: &SymbolRuntime{SuperPositionManager: spm}}
	coordinator := risk.NewOpeningPauseCoordinator()
	bots := []risk.BotController{bot}

	bot.PauseOpeningManually("operator_pause")
	coordinator.Pause("global_circuit_breaker", "daily_loss", bots)
	if !coordinator.Release("global_circuit_breaker", bots) {
		t.Fatal("coordinator did not release its own risk hold")
	}
	if !spm.OpeningGate().HasBlock("manual") || spm.OpeningGate().HasBlock("global_circuit_breaker") {
		t.Fatalf("risk release changed wrong pause owner: manual=%v risk=%v", spm.OpeningGate().HasBlock("manual"), spm.OpeningGate().HasBlock("global_circuit_breaker"))
	}
	if !bot.Config.OpenPositionControl.PauseOpening {
		t.Fatal("coordinator release cleared persisted/runtime-visible manual pause state")
	}
	if err := bot.ResumeOpeningManually(); err != nil {
		t.Fatalf("explicit manual resume failed: %v", err)
	}
	if spm.OpeningGate().Blocked() {
		t.Fatalf("manual resume left gate blocked: manual=%v risk=%v", spm.OpeningGate().HasBlock("manual"), spm.OpeningGate().HasBlock("global_circuit_breaker"))
	}
	if bot.Config.OpenPositionControl.PauseOpening {
		t.Fatal("explicit manual resume left runtime config paused")
	}
}

func TestCoordinatorReleasePreservesOpeningControllerLimitPause(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100
	spm := position.NewSuperPositionManager(cfg, &pauseTestExecutor{}, pauseTestExchange{}, 2, 3)
	bot := &BotRuntime{BotID: "position-limit-owner", Inner: &SymbolRuntime{SuperPositionManager: spm}}
	coordinator := risk.NewOpeningPauseCoordinator()
	bots := []risk.BotController{bot}
	controllerOwns := func(reason string) bool { return reason == "position_limit" }

	if !spm.PauseOpeningUnlessHeld("position_limit", controllerOwns) {
		t.Fatal("opening controller could not apply its position-limit gate")
	}
	coordinator.Pause("global_circuit_breaker", "daily_loss", bots)
	if !coordinator.Release("global_circuit_breaker", bots) {
		t.Fatal("coordinator did not release its own risk hold")
	}
	if !spm.OpeningGate().HasBlock("opening_controller:position_limit") || !spm.IsOpeningPaused() {
		t.Fatal("coordinator release cleared the active position-limit gate")
	}
	if !spm.ResumeOpeningIfOwned(controllerOwns) || spm.IsOpeningPaused() {
		t.Fatal("position-limit recovery did not release its own gate")
	}
}

func TestManualPauseUsesOwnedCancellationInsteadOfImmediateSweep(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100
	base := &pauseTestExecutor{}
	owned := &pauseOwnedTestExecutor{pauseTestExecutor: base, called: make(chan struct{}, 1)}
	spm := position.NewSuperPositionManager(cfg, owned, pauseTestExchange{}, 2, 3)
	spm.PauseOpeningManually("operator_pause")
	if len(base.cancelledIDs()) != 0 {
		t.Fatal("manual pause performed an immediate unverified slot sweep before draining admitted opens")
	}
	select {
	case <-owned.called:
	case <-time.After(3 * time.Second):
		t.Fatal("manual pause did not invoke owned cancellation after its drain delay")
	}
}

func TestExplicitPauseAutoResumeUsesRequestedDuration(t *testing.T) {
	gate := &execution.OpeningGate{}
	bot := &BotRuntime{
		BotID: "explicit-auto-resume",
		Config: config.BotConfig{OpenPositionControl: config.OpenPositionControl{
			BotRiskControl: &config.BotRiskControl{AutoResumeAfter: 1},
		}},
		Inner: &SymbolRuntime{OpeningGate: gate},
	}
	bot.PauseOpeningWithAutoResume("user_requested_timed_pause", 1)
	time.Sleep(1100 * time.Millisecond)
	if gate.HasBlock("manual") {
		t.Fatal("explicitly requested automatic resume did not release the pause")
	}
}

func TestTimedManualPauseExpiryPreservesCoordinatedRiskHold(t *testing.T) {
	gate := &execution.OpeningGate{}
	bot := &BotRuntime{
		BotID: "timed-manual-risk-overlap",
		Inner: &SymbolRuntime{OpeningGate: gate},
	}
	coordinator := risk.NewOpeningPauseCoordinator()
	bots := []risk.BotController{bot}

	bot.PauseOpeningManuallyWithAutoResume("operator_pause", 1)
	coordinator.Pause("global_circuit_breaker", "daily_loss", bots)
	time.Sleep(1100 * time.Millisecond)
	if !gate.HasBlock("global_circuit_breaker") || gate.HasBlock("manual") {
		t.Fatalf("manual timer cleared or retained the wrong source: risk=%v manual=%v", gate.HasBlock("global_circuit_breaker"), gate.HasBlock("manual"))
	}
	if !coordinator.IsHeldBy("global_circuit_breaker") {
		t.Fatal("manual timer released the coordinated risk source")
	}
	if bot.Config.OpenPositionControl.PauseOpening || bot.GetPositionStatus()["paused"] != true {
		t.Fatal("manual timer must release only its persisted manual state while the risk gate remains active")
	}
	if !coordinator.Release("global_circuit_breaker", bots) || gate.Blocked() {
		t.Fatal("explicit risk-source release did not finally clear its own hold")
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
	br.PauseOpeningManually("manual")
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
