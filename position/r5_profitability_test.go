package position

import (
	"context"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/config"
)

const r5Eps = 1e-9

func newR5SPM(t *testing.T, ex IExchange, exec OrderExecutorInterface) *SuperPositionManager {
	t.Helper()
	cfg := &config.Config{}
	cfg.Trading.Symbol = "ETHUSDT"
	cfg.Trading.Direction = "LONG"
	cfg.Trading.MarketType = "futures"
	cfg.Trading.PriceInterval = 1
	cfg.Trading.ProfitSpread = 1
	cfg.Trading.SellWindowSize = 5
	cfg.Trading.OrderQuantity = 100
	if ex == nil {
		ex = &MockExchange{}
	}
	if exec == nil {
		exec = &MockExecutor{}
	}
	return NewSuperPositionManager(cfg, exec, ex, 2, 3)
}

func boolPtr(b bool) *bool { return &b }

func TestFeeAwareSpreadFloor(t *testing.T) {
	tests := []struct {
		name     string
		enabled  *bool
		margin   float64
		setFees  bool
		maker    float64
		taker    float64
		postOnly bool
		price    float64
		want     float64
	}{
		{name: "未注入費率不生效", price: 3000, postOnly: true, want: 0},
		{name: "預設啟用 maker", setFees: true, maker: 0.0002, taker: 0.0005, postOnly: true, price: 3000, want: 3000 * (2*0.0002 + config.DefaultFeeAwareSafetyMarginRatio)},
		{name: "非 PostOnly 用 taker", setFees: true, maker: 0.0002, taker: 0.0005, postOnly: false, price: 3000, want: 3000 * (2*0.0005 + config.DefaultFeeAwareSafetyMarginRatio)},
		{name: "自定義安全邊際", setFees: true, maker: 0.0002, taker: 0.0005, margin: 0.001, postOnly: true, price: 1000, want: 1000 * (2*0.0002 + 0.001)},
		{name: "maker 返佣按 0 計", setFees: true, maker: -0.0001, taker: 0.0005, postOnly: true, price: 1000, want: 1000 * config.DefaultFeeAwareSafetyMarginRatio},
		{name: "顯式關閉", enabled: boolPtr(false), setFees: true, maker: 0.0002, taker: 0.0005, postOnly: true, price: 3000, want: 0},
		{name: "價格無效", setFees: true, maker: 0.0002, taker: 0.0005, postOnly: true, price: 0, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spm := newR5SPM(t, nil, nil)
			spm.config.Trading.FeeAwareSpread = config.FeeAwareSpreadConfig{Enabled: tt.enabled, SafetyMarginRatio: tt.margin}
			if tt.setFees {
				spm.SetFeeRates(tt.maker, tt.taker)
			}
			if got := spm.feeAwareSpreadFloor(tt.price, tt.postOnly); math.Abs(got-tt.want) > r5Eps {
				t.Fatalf("floor = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetFeeRatesRejectsNonFiniteAndOutOfRangeValues(t *testing.T) {
	spm := newR5SPM(t, nil, nil)
	spm.SetFeeRates(0.0002, 0.0005)
	invalid := [][2]float64{
		{math.NaN(), 0.001}, {math.Inf(1), 0.001}, {-1.01, 0.001},
		{0.001, math.NaN()}, {0.001, math.Inf(1)}, {0.001, 0}, {0.001, 1.01},
	}
	for _, rates := range invalid {
		spm.SetFeeRates(rates[0], rates[1])
		maker, taker, ok := spm.GetFeeRates()
		if !ok || maker != 0.0002 || taker != 0.0005 {
			t.Fatalf("invalid rates %v replaced trusted rates: maker=%v taker=%v ok=%v", rates, maker, taker, ok)
		}
	}
}

func TestGetEffectiveProfitSpreadRaisesToFeeFloorAndWarnsOnce(t *testing.T) {
	spm := newR5SPM(t, nil, nil)
	spm.lastMarketPrice.Store(3000.0)
	if got := spm.getEffectiveProfitSpread(); got != 1 {
		t.Fatalf("no fees: spread = %v, want configured 1", got)
	}
	spm.SetFeeRates(0.0002, 0.0005)
	want := 3000 * (2*0.0002 + config.DefaultFeeAwareSafetyMarginRatio) // 1.8
	for i := 0; i < 3; i++ {
		if got := spm.getEffectiveProfitSpread(); math.Abs(got-want) > r5Eps {
			t.Fatalf("spread = %v, want fee floor %v", got, want)
		}
	}
	if !spm.fees.belowFloorWarned.Load() {
		t.Fatalf("below-floor warning flag should be set")
	}
	spm.config.Trading.ProfitSpread = 5
	if got := spm.getEffectiveProfitSpread(); got != 5 {
		t.Fatalf("configured above floor: spread = %v, want 5", got)
	}
	// 無效 taker 被忽略，保持原費率
	spm.SetFeeRates(0.0001, 0)
	if m, tk, ok := spm.GetFeeRates(); !ok || m != 0.0002 || tk != 0.0005 {
		t.Fatalf("invalid SetFeeRates changed state: %v %v %v", m, tk, ok)
	}
}

func TestMakerSafeClosePrice(t *testing.T) {
	tests := []struct {
		name      string
		close     float64
		current   float64
		side      string
		failCount int
		want      float64
	}{
		{name: "SELL 平倉價在盤口外不變", close: 3002, current: 3000, side: "SELL", want: 3002},
		{name: "SELL 價格已越過平倉價", close: 3001.8, current: 3005, side: "SELL", want: 3005.01},
		{name: "SELL 連續被拒逐 tick 外移", close: 3001.8, current: 3005, side: "SELL", failCount: 2, want: 3005.03},
		{name: "SELL 外移封頂重定價次數", close: 3001.8, current: 3005, side: "SELL", failCount: 99, want: 3005.04},
		{name: "BUY 價格已跌破平倉價", close: 2998, current: 2990, side: "BUY", want: 2989.99},
		{name: "BUY 平倉價在盤口外不變", close: 2998, current: 3000, side: "BUY", want: 2998},
		{name: "無現價不調整", close: 2998, current: 0, side: "BUY", want: 2998},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spm := newR5SPM(t, nil, nil)
			if got := spm.makerSafeClosePrice(tt.close, tt.current, tt.side, tt.failCount); math.Abs(got-tt.want) > r5Eps {
				t.Fatalf("makerSafeClosePrice = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestAdjustOrdersCloseOrderFeeAwareAndPostOnly 平倉單價格含手續費下界、越價時掛到盤口外，且始終 PostOnly
func TestAdjustOrdersCloseOrderFeeAwareAndPostOnly(t *testing.T) {
	tests := []struct {
		name      string
		current   float64
		failCount int
		wantPrice float64
	}{
		{name: "費率下界抬高利差", current: 3000.4, wantPrice: 3001.8},
		{name: "價格越過平倉價且曾被拒", current: 3005, failCount: 5, wantPrice: 3005.04},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := &MockExecutor{}
			spm := newR5SPM(t, nil, exec)
			spm.setAnchorPrice(3000)
			spm.SetFeeRates(0.0002, 0.0005)
			slot := fillSlot(spm, 3000, 1, 0, "")
			slot.mu.Lock()
			slot.PostOnlyFailCount = tt.failCount
			slot.mu.Unlock()

			if err := spm.AdjustOrders(tt.current); err != nil {
				t.Fatalf("AdjustOrders: %v", err)
			}
			var found bool
			for _, req := range exec.PlacedOrders {
				if !req.PostOnly {
					t.Fatalf("grid order must stay PostOnly: %+v", req)
				}
				if req.Side == "SELL" {
					found = true
					if math.Abs(req.Price-tt.wantPrice) > r5Eps || !req.ReduceOnly {
						t.Fatalf("close order = %+v, want price %v reduceOnly", req, tt.wantPrice)
					}
				}
			}
			if !found {
				t.Fatalf("no close order placed: %+v", exec.PlacedOrders)
			}
		})
	}
}

func TestShouldSkipAdjust(t *testing.T) {
	spm := newR5SPM(t, nil, nil) // PriceInterval=1 → 分桶 0.1
	base := time.Unix(1_700_000_000, 0)
	steps := []struct {
		name  string
		price float64
		at    time.Duration
		dirty bool
		want  bool
	}{
		{name: "首次必須全量", price: 3000.01, want: false},
		{name: "同桶跳過", price: 3000.05, at: 50 * time.Millisecond, want: true},
		{name: "跨桶重算", price: 3000.15, at: 100 * time.Millisecond, want: false},
		{name: "同桶再次跳過", price: 3000.12, at: 150 * time.Millisecond, want: true},
		{name: "訂單事件標記 dirty", price: 3000.12, at: 200 * time.Millisecond, dirty: true, want: false},
		{name: "dirty 清除後跳過", price: 3000.12, at: 250 * time.Millisecond, want: true},
		{name: "超過兜底間隔強制重算", price: 3000.12, at: 250*time.Millisecond + adjustOrdersForceInterval, want: false},
	}
	for _, s := range steps {
		if s.dirty {
			spm.OnOrderUpdate(OrderUpdate{ClientOrderID: "unknown", Status: "NEW"})
		}
		if got := spm.shouldSkipAdjust(s.price, base.Add(s.at)); got != s.want {
			t.Fatalf("%s: shouldSkipAdjust = %v, want %v", s.name, got, s.want)
		}
	}
}

// TestAdjustOrdersDebounceStillRunsStopLoss 去抖跳過全量重算時，硬止損仍每個 tick 檢查
func TestAdjustOrdersDebounceStillRunsStopLoss(t *testing.T) {
	exec := newLiqFakeVenue(1)
	exec.bid, exec.ask = 2999.9, 3000.1
	spm := newR5SPM(t, exec, exec)
	configureTestProtective(t, spm, exec, nil)
	spm.setAnchorPrice(3000)
	slot := fillSlot(spm, 3000, 1, 0, "")
	spm.config.Trading.GridRiskControl = config.GridRiskControl{Enabled: true, StopLossRatio: 0.05}

	_ = spm.AdjustOrders(2999.95) // 全量一次（掛出平倉單）
	placed := len(exec.limitReqs)
	if placed == 0 {
		t.Fatalf("first run should place close order")
	}
	// 模擬平倉單被撤但不經 OnOrderUpdate（無 dirty）：同桶 tick 應跳過全量，不重掛
	slot.mu.Lock()
	_ = exec.CancelOrder(t.Context(), liqTestSymbol, slot.OrderID)
	slot.OrderID, slot.ClientOID, slot.OrderStatus, slot.SlotStatus = 0, "", OrderStatusCanceled, SlotStatusFree
	slot.mu.Unlock()
	_ = spm.AdjustOrders(2999.96)
	if len(exec.limitReqs) != placed {
		t.Fatalf("debounced tick should not place orders: %d -> %d", placed, len(exec.limitReqs))
	}
	// 同一分桶內浮虧超過止損線（均價 4000）：風控在去抖之前執行，仍須觸發止損
	slot.mu.Lock()
	slot.AvgBuyPrice = 4000
	slot.mu.Unlock()
	exec.limitFillRatio = 1
	if err := spm.AdjustOrders(2999.97); err != nil {
		t.Fatal(err)
	}
	waitTestProtective(t, spm)
	var stopLoss bool
	for _, req := range exec.limitReqs[placed:] {
		if req.OrderSource == "stop_loss" {
			stopLoss = true
		}
	}
	if !stopLoss {
		t.Fatalf("stop-loss not triggered: %+v", exec.limitReqs[placed:])
	}
}

type r5Account struct {
	TotalWalletBalance float64
	TotalMarginBalance float64
	AvailableBalance   float64
	AccountLeverage    int
}

// accountExchange GetAccount 計數並返回固定帳戶
type accountExchange struct {
	MockExchange
	calls   atomic.Int64
	account *r5Account
}

func (a *accountExchange) GetAccount(ctx context.Context) (interface{}, error) {
	a.calls.Add(1)
	return a.account, nil
}

func TestGetAccountCachedTTLAndInvalidate(t *testing.T) {
	ex := &accountExchange{account: &r5Account{AvailableBalance: 123}}
	spm := newR5SPM(t, ex, nil)
	for i := 0; i < 5; i++ {
		res, err := spm.getAccountCached(context.Background())
		if err != nil || accountAvailableBalance(res) != 123 {
			t.Fatalf("getAccountCached = %v, %v", res, err)
		}
	}
	if got := ex.calls.Load(); got != 1 {
		t.Fatalf("GetAccount calls = %d, want 1 within TTL", got)
	}
	spm.invalidateAccountCache()
	if _, err := spm.getAccountCached(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := ex.calls.Load(); got != 2 {
		t.Fatalf("GetAccount calls after invalidate = %d, want 2", got)
	}
}

func TestStopLossDenominator(t *testing.T) {
	t.Run("預設按持倉價值", func(t *testing.T) {
		ex := &accountExchange{account: &r5Account{TotalMarginBalance: 10000}}
		spm := newR5SPM(t, ex, nil)
		if d, basis := spm.stopLossDenominator(500); d != 500 || basis != config.StopLossBasisPosition {
			t.Fatalf("denominator = %v %s", d, basis)
		}
		if ex.calls.Load() != 0 {
			t.Fatalf("position basis must not query account")
		}
	})
	t.Run("權益緩存命中不發請求", func(t *testing.T) {
		ex := &accountExchange{account: &r5Account{TotalMarginBalance: 10000}}
		spm := newR5SPM(t, ex, nil)
		spm.config.Trading.GridRiskControl.StopLossBasis = "Equity"
		spm.storeAccountSnapshot(&r5Account{TotalMarginBalance: 8000, TotalWalletBalance: 9000}, time.Now())
		if d, basis := spm.stopLossDenominator(500); d != 8000 || basis != config.StopLossBasisEquity {
			t.Fatalf("denominator = %v %s", d, basis)
		}
		if ex.calls.Load() != 0 {
			t.Fatalf("fresh equity cache must not query account")
		}
	})
	t.Run("無權益數據回退持倉並後台刷新", func(t *testing.T) {
		ex := &accountExchange{account: &r5Account{TotalWalletBalance: 7000}}
		spm := newR5SPM(t, ex, nil)
		spm.config.Trading.GridRiskControl.StopLossBasis = config.StopLossBasisEquity
		if d, basis := spm.stopLossDenominator(500); d != 500 || basis != config.StopLossBasisPosition {
			t.Fatalf("fallback denominator = %v %s", d, basis)
		}
		deadline := time.Now().Add(2 * time.Second)
		for {
			if d, basis := spm.stopLossDenominator(500); d == 7000 && basis == config.StopLossBasisEquity {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("async equity refresh did not populate cache (calls=%d)", ex.calls.Load())
			}
			time.Sleep(5 * time.Millisecond)
		}
	})
}

func TestSweepPriceAndLiquidationLimitPrice(t *testing.T) {
	bids := []OrderBookLevel{{Price: 2999.9, Quantity: 1}, {Price: 2999.5, Quantity: 2}, {Price: 2990, Quantity: 5}}
	asks := []OrderBookLevel{{Price: 3000.1, Quantity: 1}, {Price: 3000.6, Quantity: 2}}
	tests := []struct {
		name string
		side string
		qty  float64
		last float64
		book []OrderBookLevel
		want float64
	}{
		{name: "SELL 數量不超過買一用買一", side: "SELL", qty: 0.5, last: 3000, book: bids, want: 2999.9},
		{name: "SELL 吃到第二檔", side: "SELL", qty: 2.5, last: 3000, book: bids, want: 2999.5},
		{name: "SELL 超過 1% 讓價邊界時封頂", side: "SELL", qty: 1000, last: 3000, book: []OrderBookLevel{{Price: 2900, Quantity: 1000}}, want: 2970},
		{name: "SELL 深度不足回退現價-1%", side: "SELL", qty: 100, last: 3000, book: bids, want: 2970},
		{name: "BUY 用賣一", side: "BUY", qty: 1, last: 3000, book: asks, want: 3000.1},
		{name: "BUY 無盤口回退現價+1%", side: "BUY", qty: 1, last: 3000, book: nil, want: 3030},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var prices liquidationBookPrices
			if tt.side == "SELL" {
				prices.sell = sweepPrice(tt.book, tt.qty)
			} else {
				prices.buy = sweepPrice(tt.book, tt.qty)
			}
			if got := liquidationLimitPrice(tt.side, tt.last, prices); math.Abs(got-tt.want) > 1e-6 {
				t.Fatalf("liquidationLimitPrice = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestLiquidateAllUsesOrderBook 全平倉優先使用盤口價（SELL 吃買盤、BUY 吃賣盤）
func TestLiquidateAllUsesOrderBook(t *testing.T) {
	tests := []struct {
		direction string
		wantSide  string
		wantPx    float64
	}{
		{"LONG", "SELL", 49990},
		{"SHORT", "BUY", 50010},
	}
	for _, tc := range tests {
		t.Run(tc.direction, func(t *testing.T) {
			exec := &MockExecutor{}
			ex := &orderBookExchange{orderBook: &OrderBook{
				Bids: []OrderBookLevel{{Price: 49990, Quantity: 1}},
				Asks: []OrderBookLevel{{Price: 50010, Quantity: 1}},
			}}
			spm := newR5SPM(t, ex, exec)
			spm.config.Trading.Direction = tc.direction
			spm.lastMarketPrice.Store(50000.0)
			fillSlot(spm, 50000, 0.5, 0, "")

			spm.LiquidateAll()

			if len(exec.PlacedOrders) != 1 {
				t.Fatalf("placed %d orders, want 1", len(exec.PlacedOrders))
			}
			req := exec.PlacedOrders[0]
			if req.Side != tc.wantSide || req.PostOnly || math.Abs(req.Price-tc.wantPx) > r5Eps {
				t.Fatalf("close order = %+v, want %s @ %v", req, tc.wantSide, tc.wantPx)
			}
		})
	}
}
