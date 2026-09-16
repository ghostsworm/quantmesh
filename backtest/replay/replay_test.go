package replay

import (
	"errors"
	"math"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/position"
)

const (
	testSymbol     = "ETHUSDT"
	testBaseTs     = int64(1_700_000_000_000)
	testTickGapMs  = int64(1000)
	testInterval   = 10.0
	testOrderUSDT  = 100.0
	testCapital    = 10000.0
	testPriceDec   = 2
	testQtyDec     = 3
	testFloatDelta = 1e-9
)

func testBotConfig(window int) *config.Config {
	cfg := &config.Config{}
	cfg.Trading.Symbol = testSymbol
	cfg.Trading.PriceInterval = testInterval
	cfg.Trading.OrderQuantity = testOrderUSDT
	cfg.Trading.BuyWindowSize = window
	cfg.Trading.SellWindowSize = window
	return cfg
}

func testConfig(m MatchingConfig) Config {
	if m.MakerFeeRate == 0 && m.TakerFeeRate == 0 {
		m.MakerFeeRate = DefaultMakerFeeRate
		m.TakerFeeRate = DefaultTakerFeeRate
	}
	return Config{
		Bot:              testBotConfig(3),
		InitialCapital:   testCapital,
		Leverage:         1,
		PriceDecimals:    testPriceDec,
		QuantityDecimals: testQtyDec,
		Matching:         m,
	}
}

// ticksFromPrices 每秒一筆、每筆成交量 qty
func ticksFromPrices(prices []float64, qty float64) []Tick {
	out := make([]Tick, len(prices))
	for i, p := range prices {
		out[i] = Tick{Timestamp: testBaseTs + int64(i)*testTickGapMs, Price: p, Quantity: qty}
	}
	return out
}

// 價格多次觸及 1990 買單與 2000 賣單而不穿越，只有 1989 / 2001 真正穿價
var touchAndCrossPrices = []float64{2000, 1995, 1990, 1995, 1990, 2000, 1989, 1995, 2000, 2001}

func newTestExchange(t *testing.T, m MatchingConfig) (*simExchange, *simExecutor) {
	t.Helper()
	cfg, err := testConfig(m).normalized(2000)
	if err != nil {
		t.Fatalf("normalize config: %v", err)
	}
	ex := newSimExchange(cfg)
	return ex, newSimExecutor(ex, cfg.Bot)
}

func postOnlyBuy(price, qty float64, cid string) *position.OrderRequest {
	return &position.OrderRequest{Symbol: testSymbol, Side: sideBuy, Price: price, Quantity: qty, PriceDecimals: testPriceDec, PostOnly: true, ClientOrderID: cid}
}

func TestReplay_CrossingOnlyFillsFewerThanTouchFills(t *testing.T) {
	ticks := ticksFromPrices(touchAndCrossPrices, 10)

	crossRes, err := RunTicks(testConfig(MatchingConfig{}), ticks)
	if err != nil {
		t.Fatalf("crossing-only replay: %v", err)
	}
	touchRes, err := RunTicks(testConfig(MatchingConfig{FillOnTouch: true}), ticks)
	if err != nil {
		t.Fatalf("touch-fill replay: %v", err)
	}
	if crossRes.Metrics.Fills == 0 {
		t.Fatalf("crossing-only replay should still fill on real crosses, got 0 fills")
	}
	if crossRes.Metrics.Fills >= touchRes.Metrics.Fills {
		t.Fatalf("crossing-only fills=%d, want fewer than touch fills=%d", crossRes.Metrics.Fills, touchRes.Metrics.Fills)
	}
	t.Logf("crossing-only fills=%d closed=%d, touch fills=%d closed=%d",
		crossRes.Metrics.Fills, crossRes.Metrics.ClosedGrids, touchRes.Metrics.Fills, touchRes.Metrics.ClosedGrids)
}

func TestReplay_EndToEndMetricsConsistent(t *testing.T) {
	ticks := ticksFromPrices(touchAndCrossPrices, 10)
	res, err := RunTicks(testConfig(MatchingConfig{}), ticks)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	m := res.Metrics
	if m.Fills != 2 || m.ClosedGrids != 1 {
		t.Fatalf("want 1 buy + 1 sell fill and 1 closed grid, got fills=%d closed=%d", m.Fills, m.ClosedGrids)
	}
	if m.TakerFills != 0 || m.FeesTaker != 0 || math.Abs(m.MakerRatio-1) > testFloatDelta {
		t.Fatalf("all grid fills must be maker: taker=%d feesTaker=%v makerRatio=%v", m.TakerFills, m.FeesTaker, m.MakerRatio)
	}
	if m.RealizedPnL <= 0 || m.FeesMaker <= 0 {
		t.Fatalf("expected positive realized pnl and maker fees, got realized=%v fees=%v", m.RealizedPnL, m.FeesMaker)
	}
	wantNet := m.RealizedPnL + m.UnrealizedPnL - m.FeesTotal - m.FundingPaid
	if math.Abs(m.NetPnL-wantNet) > 1e-6 || math.Abs(m.FinalEquity-testCapital-m.NetPnL) > 1e-6 {
		t.Fatalf("pnl identity broken: net=%v want=%v final=%v", m.NetPnL, wantNet, m.FinalEquity)
	}
	if math.Abs(m.GridNetProfitToFeeRatio-(m.RealizedPnL-m.FeesTotal)/m.FeesTotal) > 1e-9 {
		t.Fatalf("grid net profit / fee ratio mismatch: %v", m.GridNetProfitToFeeRatio)
	}
	if res.Backtest == nil || len(res.Backtest.Trades) != 2 || len(res.Backtest.Equity) < 2 {
		t.Fatalf("backtest result incomplete: %+v", res.Backtest)
	}
	if m.Exposure.MaxAbsQty <= 0 || m.Exposure.TimeInMarketPct <= 0 {
		t.Fatalf("exposure summary not recorded: %+v", m.Exposure)
	}
	if n := res.Backtest.Metrics.TotalFees; math.Abs(n-m.FeesTotal) > 1e-9 {
		t.Fatalf("backtest metrics total fees %v != replay fees %v", n, m.FeesTotal)
	}
}

func TestSimExecutor_PostOnlyCrossingRejected(t *testing.T) {
	ex, exec := newTestExchange(t, MatchingConfig{})
	ex.setMarket(testBaseTs, 2000)

	// 遠高於最新價的 PostOnly 買單：重定價 3 次（每次 -0.01）仍交叉，最終被拒，不進掛單簿
	if _, err := exec.PlaceOrder(postOnlyBuy(2005, 0.05, "c1")); !errors.Is(err, errPostOnlyWouldCross) {
		t.Fatalf("want post-only rejection, got %v", err)
	}
	if n := ex.openOrderCount(); n != 0 {
		t.Fatalf("rejected post-only order must not rest, open=%d", n)
	}
	if ex.stats.postOnlyRejects != config.DefaultPostOnlyRepriceMaxAttempts+1 || ex.stats.postOnlyFinal != 1 {
		t.Fatalf("reject counters: rejects=%d final=%d", ex.stats.postOnlyRejects, ex.stats.postOnlyFinal)
	}

	// 等於最新價 +0.01 的 PostOnly 買單：2000.01 / 2000.00 被拒，重定價到 1999.99 成功
	ord, err := exec.PlaceOrder(postOnlyBuy(2000.01, 0.05, "c2"))
	if err != nil {
		t.Fatalf("repriced post-only order should be accepted: %v", err)
	}
	if math.Abs(ord.Price-1999.99) > testFloatDelta || ord.ClientOrderID != "c2" {
		t.Fatalf("want repriced order at 1999.99 with same client id, got price=%v cid=%s", ord.Price, ord.ClientOrderID)
	}
	if ex.stats.postOnlyRepriced != config.DefaultPostOnlyRepriceMaxAttempts+2 {
		t.Fatalf("repriced counter=%d", ex.stats.postOnlyRepriced)
	}

	// 非 PostOnly 交叉單：taker 立即成交
	taker := postOnlyBuy(2001, 0.05, "c3")
	taker.PostOnly = false
	if _, err := exec.PlaceOrder(taker); err != nil {
		t.Fatalf("non post-only order: %v", err)
	}
	if ex.stats.takerFills != 1 || ex.feesTaker <= 0 {
		t.Fatalf("crossing GTC order must fill as taker, takerFills=%d fee=%v", ex.stats.takerFills, ex.feesTaker)
	}
}

func TestSimExecutor_ReduceOnlyWithoutPositionRejected(t *testing.T) {
	ex, exec := newTestExchange(t, MatchingConfig{})
	ex.setMarket(testBaseTs, 2000)
	req := &position.OrderRequest{Symbol: testSymbol, Side: sideSell, Price: 2010, Quantity: 0.05, ReduceOnly: true, PostOnly: true, ClientOrderID: "r1"}
	res := exec.BatchPlaceOrdersWithDetails([]*position.OrderRequest{req})
	if len(res.PlacedOrders) != 0 || !res.ReduceOnlyErrors["r1"] {
		t.Fatalf("reduce-only without position must be reported in ReduceOnlyErrors: %+v", res)
	}
}

func TestSimExchange_PartialFillsByParticipation(t *testing.T) {
	ex, exec := newTestExchange(t, MatchingConfig{ParticipationRate: 0.5})
	ex.setMarket(testBaseTs, 2000)
	if _, err := exec.PlaceOrder(postOnlyBuy(1990, 1, "p1")); err != nil {
		t.Fatalf("place: %v", err)
	}
	ex.drainUpdates()

	ex.matchTrade(Tick{Timestamp: testBaseTs + 1, Price: 1985, Quantity: 0.6})
	ups := ex.drainUpdates()
	if len(ups) != 1 || ups[0].Status != statusPartiallyFilled || math.Abs(ups[0].ExecutedQty-0.3) > testFloatDelta {
		t.Fatalf("want partial fill 0.3 (participation 50%% of 0.6), got %+v", ups)
	}
	ex.matchTrade(Tick{Timestamp: testBaseTs + 2, Price: 1985, Quantity: 10})
	ups = ex.drainUpdates()
	if len(ups) != 1 || ups[0].Status != statusFilled || math.Abs(ups[0].ExecutedQty-1) > testFloatDelta {
		t.Fatalf("want final fill to 1.0, got %+v", ups)
	}
	if ups[0].Commission <= 0 || ups[0].AvgPrice != 1990 {
		t.Fatalf("fill must be at limit price with maker commission, got %+v", ups[0])
	}
}

func TestSimExchange_QueueModelFillsAtTouchAfterQueue(t *testing.T) {
	ex, exec := newTestExchange(t, MatchingConfig{QueueFactor: 2})
	ex.setMarket(testBaseTs, 2000)
	if _, err := exec.PlaceOrder(postOnlyBuy(1990, 1, "q1")); err != nil {
		t.Fatalf("place: %v", err)
	}
	ex.drainUpdates()

	// 排隊量 = 1 × 2；第一筆觸價 1.5 只消耗隊列
	ex.matchTrade(Tick{Timestamp: testBaseTs + 1, Price: 1990, Quantity: 1.5})
	if ups := ex.drainUpdates(); len(ups) != 0 {
		t.Fatalf("touch volume below queue must not fill, got %+v", ups)
	}
	// 第二筆觸價 1.0：先消耗剩餘隊列 0.5，剩餘 0.5 成交
	ex.matchTrade(Tick{Timestamp: testBaseTs + 2, Price: 1990, Quantity: 1})
	ups := ex.drainUpdates()
	if len(ups) != 1 || math.Abs(ups[0].ExecutedQty-0.5) > testFloatDelta {
		t.Fatalf("want 0.5 filled after queue consumed, got %+v", ups)
	}

	// 無排隊模型時觸價永不成交
	ex2, exec2 := newTestExchange(t, MatchingConfig{})
	ex2.setMarket(testBaseTs, 2000)
	if _, err := exec2.PlaceOrder(postOnlyBuy(1990, 1, "q2")); err != nil {
		t.Fatalf("place: %v", err)
	}
	ex2.drainUpdates()
	for i := 0; i < 5; i++ {
		ex2.matchTrade(Tick{Timestamp: testBaseTs + int64(i), Price: 1990, Quantity: 100})
	}
	if ups := ex2.drainUpdates(); len(ups) != 0 {
		t.Fatalf("touch without cross must not fill, got %+v", ups)
	}
}

func TestSimExchange_FundingSettlesEvery8h(t *testing.T) {
	const rate = 0.0001
	ex, _ := newTestExchange(t, MatchingConfig{})
	start := (testBaseTs/FundingIntervalMs)*FundingIntervalMs + 1
	ex.setMarket(start, 2000)
	ex.mu.Lock()
	ex.applyPositionLocked(sideBuy, 0.5, 2000)
	ex.mu.Unlock()

	// 跨越兩個結算點
	ex.settleFunding(start, start+2*FundingIntervalMs, func(int64) float64 { return rate })
	want := 2 * 0.5 * 2000 * rate
	if math.Abs(ex.fundingPaid-want) > 1e-9 {
		t.Fatalf("funding paid=%v want %v", ex.fundingPaid, want)
	}
}

func TestCandlesToTicks_PathByDirection(t *testing.T) {
	candles := []*exchange.Candle{
		{Timestamp: 0, Open: 100, High: 110, Low: 95, Close: 105, Volume: 8},    // 陽線 → O L H C
		{Timestamp: 60000, Open: 105, High: 108, Low: 90, Close: 92, Volume: 4}, // 陰線 → O H L C
	}
	ticks, err := CandlesToTicks(candles, IntrabarOptions{})
	if err != nil {
		t.Fatalf("candles to ticks: %v", err)
	}
	want := []float64{100, 95, 110, 105, 105, 108, 90, 92}
	if len(ticks) != len(want) {
		t.Fatalf("want %d ticks, got %d", len(want), len(ticks))
	}
	for i, p := range want {
		if ticks[i].Price != p {
			t.Fatalf("tick %d price=%v want %v", i, ticks[i].Price, p)
		}
	}
	if ticks[0].Quantity != 2 || ticks[4].Quantity != 1 {
		t.Fatalf("volume should be split evenly per candle: %v %v", ticks[0].Quantity, ticks[4].Quantity)
	}
	fixed, err := CandlesToTicks(candles[:1], IntrabarOptions{Path: IntrabarPathOHLC})
	if err != nil || fixed[1].Price != 110 {
		t.Fatalf("OHLC path must visit high first, got %+v err=%v", fixed, err)
	}
}

func TestEngine_RejectsUnsortedTicks(t *testing.T) {
	ticks := []Tick{{Timestamp: 2, Price: 2000}, {Timestamp: 1, Price: 1990}}
	if _, err := RunTicks(testConfig(MatchingConfig{}), ticks); err == nil {
		t.Fatal("unsorted ticks must be rejected")
	}
}
