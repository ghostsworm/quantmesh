package position

import (
	"math"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/strategy/regime"
)

// fakeRegimeProvider 可變的 regime 快照
type fakeRegimeProvider struct {
	mu   sync.Mutex
	snap regime.Snapshot
}

func (f *fakeRegimeProvider) Snapshot() regime.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap
}

func (f *fakeRegimeProvider) set(s regime.Snapshot) {
	f.mu.Lock()
	f.snap = s
	f.mu.Unlock()
}

var r5bBarTime = time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC)

func readySnap(r regime.Regime) regime.Snapshot {
	return regime.Snapshot{Regime: r, Ready: true, BarOpenTime: r5bBarTime, EMA: 100, ATR: 2}
}

func newR5bSPM(t *testing.T, direction string, window int) (*SuperPositionManager, *MockExecutor) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Trading.Symbol = "ETHUSDT"
	cfg.Trading.Direction = direction
	cfg.Trading.MarketType = "futures"
	cfg.Trading.PriceInterval = 1
	cfg.Trading.ProfitSpread = 1
	cfg.Trading.BuyWindowSize = window
	cfg.Trading.SellWindowSize = 10
	cfg.Trading.ShortOpenWindowSize = window
	cfg.Trading.OrderQuantity = 100
	cfg.Trading.OrderCleanupThreshold = 100
	exec := &MockExecutor{}
	spm := NewSuperPositionManager(cfg, exec, &MockExchange{}, 2, 3)
	spm.setAnchorPrice(100)
	return spm, exec
}

// adjustAt 強制全量重算一輪並返回本輪新下的訂單
func adjustAt(t *testing.T, spm *SuperPositionManager, exec *MockExecutor, price float64) []*OrderRequest {
	t.Helper()
	exec.PlacedOrders = nil
	spm.markAdjustDirty()
	if err := spm.AdjustOrders(price); err != nil {
		t.Fatalf("AdjustOrders(%v): %v", price, err)
	}
	return exec.PlacedOrders
}

func ordersBy(orders []*OrderRequest, side string, reduceOnly bool) []*OrderRequest {
	var out []*OrderRequest
	for _, o := range orders {
		if o.Side == side && o.ReduceOnly == reduceOnly {
			out = append(out, o)
		}
	}
	return out
}

func TestRegimeConfigMirrorsYAMLTags(t *testing.T) {
	pairs := []struct{ a, b reflect.Type }{
		{reflect.TypeOf(config.RegimeFilterConfig{}), reflect.TypeOf(regime.RegimeConfig{})},
		{reflect.TypeOf(config.AdaptiveIntervalConfig{}), reflect.TypeOf(regime.AdaptiveIntervalConfig{})},
	}
	for _, p := range pairs {
		if p.a.NumField() != p.b.NumField() {
			t.Fatalf("%s has %d fields, %s has %d", p.a, p.a.NumField(), p.b, p.b.NumField())
		}
		for i := 0; i < p.a.NumField(); i++ {
			fa, fb := p.a.Field(i), p.b.Field(i)
			if fa.Name != fb.Name || fa.Tag.Get("yaml") != fb.Tag.Get("yaml") {
				t.Fatalf("field %d mismatch: %s(%q) vs %s(%q)", i, fa.Name, fa.Tag.Get("yaml"), fb.Name, fb.Tag.Get("yaml"))
			}
		}
	}
	a := AdaptiveIntervalConfigFromConfig(config.AdaptiveIntervalConfig{Enabled: true})
	if a.ATRMultiplier != regime.DefaultATRMultiplier || a.ChangeThresholdRatio != regime.DefaultChangeThresholdRatio {
		t.Fatalf("adaptive defaults not applied: %+v", a)
	}
}

func TestPlanOpeningLegRegimePolicy(t *testing.T) {
	stale := readySnap(regime.TrendDown)
	stale.Stale = true
	tests := []struct {
		name       string
		filter     bool
		snap       regime.Snapshot
		dir        regime.Direction
		wantWindow int
		wantFreeze bool
	}{
		{name: "未啟用過濾保持原樣", filter: false, snap: readySnap(regime.TrendDown), dir: regime.DirectionLong, wantWindow: 7},
		{name: "LONG 逆勢減半向下取整", filter: true, snap: readySnap(regime.TrendDown), dir: regime.DirectionLong, wantWindow: 3},
		{name: "LONG 順勢冻结上沿", filter: true, snap: readySnap(regime.TrendUp), dir: regime.DirectionLong, wantWindow: 7, wantFreeze: true},
		{name: "SHORT 逆勢減半", filter: true, snap: readySnap(regime.TrendUp), dir: regime.DirectionShort, wantWindow: 3},
		{name: "SHORT 順勢冻结下沿", filter: true, snap: readySnap(regime.TrendDown), dir: regime.DirectionShort, wantWindow: 7, wantFreeze: true},
		{name: "震盪滿鋪", filter: true, snap: readySnap(regime.Range), dir: regime.DirectionLong, wantWindow: 7},
		{name: "未就緒保持原樣", filter: true, snap: regime.Snapshot{Regime: regime.TrendDown}, dir: regime.DirectionLong, wantWindow: 7},
		{name: "過期保持原樣", filter: true, snap: stale, dir: regime.DirectionLong, wantWindow: 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spm, _ := newR5bSPM(t, "LONG", 7)
			spm.ConfigureRegimeControl(&fakeRegimeProvider{snap: tt.snap}, RegimeControlOptions{FilterEnabled: tt.filter})
			plan := spm.planOpeningLeg(tt.dir, 7, 100, spm.loadRegimeTick(), time.Now())
			if plan.Window != tt.wantWindow || plan.FreezeBound != tt.wantFreeze || plan.StopOpening {
				t.Fatalf("plan = %+v, want window=%d freeze=%v", plan, tt.wantWindow, tt.wantFreeze)
			}
		})
	}
	// 窗口 1 逆勢仍至少保留 1 檔
	spm, _ := newR5bSPM(t, "LONG", 1)
	spm.ConfigureRegimeControl(&fakeRegimeProvider{snap: readySnap(regime.TrendDown)}, RegimeControlOptions{FilterEnabled: true})
	if plan := spm.planOpeningLeg(regime.DirectionLong, 1, 100, spm.loadRegimeTick(), time.Now()); plan.Window != 1 {
		t.Fatalf("window 1 adverse = %d, want 1", plan.Window)
	}
}

func TestAdjustOrdersRegimeTrendDownHalvesLongWindow(t *testing.T) {
	spm, exec := newR5bSPM(t, "LONG", 6)
	if got := len(ordersBy(adjustAt(t, spm, exec, 100), "BUY", false)); got != 5 {
		t.Fatalf("baseline buys = %d, want 5 (slots 99..95)", got)
	}

	spm2, exec2 := newR5bSPM(t, "LONG", 6)
	spm2.ConfigureRegimeControl(&fakeRegimeProvider{snap: readySnap(regime.TrendDown)}, RegimeControlOptions{FilterEnabled: true})
	buys := ordersBy(adjustAt(t, spm2, exec2, 100), "BUY", false)
	if len(buys) != 2 {
		t.Fatalf("trend_down buys = %d, want 2 (window 3: slots 100 skipped, 99, 98)", len(buys))
	}
}

func TestAdjustOrdersFreezeUpperBoundLong(t *testing.T) {
	spm, exec := newR5bSPM(t, "LONG", 6)
	provider := &fakeRegimeProvider{snap: readySnap(regime.TrendUp)}
	spm.ConfigureRegimeControl(provider, RegimeControlOptions{FilterEnabled: true})

	if got := len(ordersBy(adjustAt(t, spm, exec, 100), "BUY", false)); got != 5 {
		t.Fatalf("initial buys = %d, want 5", got)
	}
	// 價格上漲：不得在凍結上沿 100 之上重建倉
	for _, o := range ordersBy(adjustAt(t, spm, exec, 105), "BUY", false) {
		if o.Price > 100 {
			t.Fatalf("buy placed above frozen bound 100: %+v", o)
		}
	}
	// 已有持倉的平倉單照常掛出
	fillSlot(spm, 104, 1, 104, PositionLegNone)
	if got := len(ordersBy(adjustAt(t, spm, exec, 105.5), "SELL", true)); got != 1 {
		t.Fatalf("close orders while frozen = %d, want 1", got)
	}
	// 離開 TrendUp 後解除冻结，恢復跟隨現價
	provider.set(readySnap(regime.Range))
	var above int
	for _, o := range ordersBy(adjustAt(t, spm, exec, 105), "BUY", false) {
		if o.Price > 100 {
			above++
		}
	}
	if above == 0 {
		t.Fatalf("expected buys above old bound after leaving trend_up")
	}
}

func TestAdjustOrdersFreezeLowerBoundShort(t *testing.T) {
	spm, exec := newR5bSPM(t, "SHORT", 6)
	spm.ConfigureRegimeControl(&fakeRegimeProvider{snap: readySnap(regime.TrendDown)}, RegimeControlOptions{FilterEnabled: true})
	if got := len(ordersBy(adjustAt(t, spm, exec, 100), "SELL", false)); got != 5 {
		t.Fatalf("initial short opens = %d, want 5", got)
	}
	for _, o := range ordersBy(adjustAt(t, spm, exec, 95), "SELL", false) {
		if o.Price < 100 {
			t.Fatalf("short open placed below frozen bound 100: %+v", o)
		}
	}
}

func TestAdjustOrdersAutoUpperBoundFromATR(t *testing.T) {
	spm, exec := newR5bSPM(t, "LONG", 6)
	snap := readySnap(regime.Range) // EMA 100, ATR 2, k=1 → 自動上沿 102
	spm.ConfigureRegimeControl(&fakeRegimeProvider{snap: snap}, RegimeControlOptions{
		AutoBound: config.UpperBoundFreezeConfig{Enabled: true, ATRMultiplier: 1},
	})
	if got := len(ordersBy(adjustAt(t, spm, exec, 103), "BUY", false)); got != 0 {
		t.Fatalf("price above auto price_high should pause opening, got %d buys", got)
	}
	buys := ordersBy(adjustAt(t, spm, exec, 101.5), "BUY", false)
	if len(buys) == 0 {
		t.Fatalf("expected buys below auto price_high")
	}
	for _, o := range buys {
		if o.Price > 102 {
			t.Fatalf("buy above auto price_high: %+v", o)
		}
	}
	// 手動 price_high 更嚴格時以手動為準
	spm.config.Trading.PriceHigh = 101
	if low, high := spm.effectivePriceBounds(spm.loadRegimeTick()); high != 101 || low != 0 {
		t.Fatalf("bounds = (%v, %v), want (0, 101)", low, high)
	}
	// 過期快照不產生自動邊界
	snap.Stale = true
	spm.ConfigureRegimeControl(&fakeRegimeProvider{snap: snap}, RegimeControlOptions{AutoBound: config.UpperBoundFreezeConfig{Enabled: true}})
	spm.config.Trading.PriceHigh = 0
	if _, high := spm.effectivePriceBounds(spm.loadRegimeTick()); high != 0 {
		t.Fatalf("stale snapshot should not set auto bound, got %v", high)
	}
}

func TestRefreshRegimeIntervalAdaptiveAndPolicy(t *testing.T) {
	spm, _ := newR5bSPM(t, "LONG", 6)
	provider := &fakeRegimeProvider{}
	snap := readySnap(regime.Range)
	snap.ATR = 3.2 // k=1 → round(3.2)=3
	provider.set(snap)
	spm.ConfigureRegimeControl(provider, RegimeControlOptions{
		FilterEnabled: true,
		Adaptive:      AdaptiveIntervalConfigFromConfig(config.AdaptiveIntervalConfig{Enabled: true, ATRMultiplier: 1}),
	})

	if !spm.RefreshRegimeInterval() || spm.config.Trading.PriceInterval != 3 || spm.config.Trading.ProfitSpread != 3 {
		t.Fatalf("adaptive: interval=%v spread=%v, want 3/3", spm.config.Trading.PriceInterval, spm.config.Trading.ProfitSpread)
	}
	if spm.RefreshRegimeInterval() {
		t.Fatalf("same bar and regime must not re-evaluate")
	}

	// 逆勢：3 × 1.5 = 4.5 → 量化為 base 的整數倍；自適應當前值不回寫放大後的值
	snap.Regime = regime.TrendDown
	provider.set(snap)
	spm.RefreshRegimeInterval()
	got := spm.config.Trading.PriceInterval
	if got != math.Round(4.5) {
		t.Fatalf("trend_down interval = %v, want 5", got)
	}
	spm.regimeCtl.mu.Lock()
	adaptiveCurrent := spm.regimeCtl.adaptiveCurrent
	spm.regimeCtl.mu.Unlock()
	if adaptiveCurrent != 3 {
		t.Fatalf("adaptiveCurrent = %v, want unscaled 3", adaptiveCurrent)
	}

	// 過期 → 回到 base
	snap.Stale = true
	provider.set(snap)
	spm.RefreshRegimeInterval()
	if spm.config.Trading.PriceInterval != 1 || spm.config.Trading.ProfitSpread != 1 {
		t.Fatalf("stale: interval=%v spread=%v, want base 1/1", spm.config.Trading.PriceInterval, spm.config.Trading.ProfitSpread)
	}

	// 外部熱更新間隔 → 以新值為基準
	spm.UpdateTradingParams(2, 2, 0, 0, 0)
	snap.Stale = false
	snap.Regime = regime.Range
	snap.BarOpenTime = r5bBarTime.Add(time.Hour)
	snap.ATR = 6.1 // k=1 → round(6.1/2)=3 倍 → 6
	provider.set(snap)
	spm.RefreshRegimeInterval()
	if spm.config.Trading.PriceInterval != 6 {
		t.Fatalf("rebased interval = %v, want 6 (base 2)", spm.config.Trading.PriceInterval)
	}
}

func TestRefreshRegimeIntervalSkipsUnsupportedModes(t *testing.T) {
	spm, _ := newR5bSPM(t, "LONG", 6)
	spm.config.Trading.GridMode = gridModeGeometric
	spm.config.Trading.PriceInterval = 0.01
	snap := readySnap(regime.TrendDown)
	spm.ConfigureRegimeControl(&fakeRegimeProvider{snap: snap}, RegimeControlOptions{FilterEnabled: true})
	if spm.RefreshRegimeInterval() || spm.config.Trading.PriceInterval != 0.01 {
		t.Fatalf("geometric grid interval must not change: %v", spm.config.Trading.PriceInterval)
	}
	// 未注入提供者：不做任何事
	spm2, _ := newR5bSPM(t, "LONG", 6)
	if spm2.RefreshRegimeInterval() {
		t.Fatalf("no provider must be a no-op")
	}
}

// 自適應放大間隔後，所有開倉槽位仍落在 anchor + n×base 上，舊間隔下的持倉槽位照常平倉
func TestAdaptiveIntervalKeepsSlotsAlignedToAnchor(t *testing.T) {
	spm, exec := newR5bSPM(t, "LONG", 6)
	fillSlot(spm, 106, 1, 106, PositionLegNone) // 舊間隔 1 下成交的槽位
	snap := readySnap(regime.Range)
	snap.ATR = 3
	spm.ConfigureRegimeControl(&fakeRegimeProvider{snap: snap}, RegimeControlOptions{
		Adaptive: AdaptiveIntervalConfigFromConfig(config.AdaptiveIntervalConfig{Enabled: true, ATRMultiplier: 1}),
	})
	spm.RefreshRegimeInterval()
	if spm.config.Trading.PriceInterval != 3 {
		t.Fatalf("interval = %v, want 3", spm.config.Trading.PriceInterval)
	}

	const base, anchor = 1.0, 100.0
	orders := adjustAt(t, spm, exec, 107.3)
	buys := ordersBy(orders, "BUY", false)
	if len(buys) == 0 {
		t.Fatalf("expected opening orders")
	}
	var slotPrices []float64
	for _, o := range orders {
		p, _, ok := spm.parseClientOrderID(o.ClientOrderID)
		if !ok {
			t.Fatalf("unparsable client order id %q", o.ClientOrderID)
		}
		n := (p - anchor) / base
		if math.Abs(n-math.Round(n)) > 1e-6 {
			t.Fatalf("slot %v not aligned to anchor %v + n×%v", p, anchor, base)
		}
		if !o.ReduceOnly {
			if m := (p - anchor) / 3; math.Abs(m-math.Round(m)) > 1e-6 {
				t.Fatalf("new opening slot %v not on the 3×base lattice", p)
			}
		}
		slotPrices = append(slotPrices, p)
	}
	sort.Float64s(slotPrices)
	if len(ordersBy(orders, "SELL", true)) != 1 {
		t.Fatalf("filled slot 106 should still get its close order, slots=%v", slotPrices)
	}
}

func TestInventorySkewHelpers(t *testing.T) {
	s := computeInventorySkew(0.5, 1)
	if s.windowFactor != 0.5 || s.priceShiftIntervals != 0.5 || s.closeSpreadFactor != 0.75 {
		t.Fatalf("skew(0.5,1) = %+v", s)
	}
	if s := computeInventorySkew(2, 5); s.windowFactor != 0 || s.closeSpreadFactor != 0.5 {
		t.Fatalf("inputs should clamp to [0,1]: %+v", s)
	}
	windowTests := []struct {
		w    int
		f    float64
		want int
	}{{6, 0.5, 3}, {6, 0.1, 1}, {6, 0, 0}, {6, -1, 0}, {0, 0.5, 0}, {5, 1, 5}}
	for _, tt := range windowTests {
		if got := scaleWindow(tt.w, tt.f); got != tt.want {
			t.Fatalf("scaleWindow(%d, %v) = %d, want %d", tt.w, tt.f, got, tt.want)
		}
	}
	if inventoryRatio(3, 0) != 0 || inventoryRatio(9, 4) != 1 || inventoryRatio(1, 4) != 0.25 {
		t.Fatalf("inventoryRatio mismatch")
	}
}

func TestAdjustOrdersInventorySkewLong(t *testing.T) {
	spm, exec := newR5bSPM(t, "LONG", 6)
	spm.config.Trading.OpenPositionControl.MaxPositionLayers = 4
	spm.config.Trading.InventorySkew = config.InventorySkewConfig{Enabled: true, Strength: 1}
	fillSlot(spm, 102, 1, 102, PositionLegNone)
	fillSlot(spm, 103, 1, 103, PositionLegNone) // inv = 2/4 = 0.5

	orders := adjustAt(t, spm, exec, 100)
	buys := ordersBy(orders, "BUY", false)
	// 窗口 6×0.5=3 → 槽位 100(跳過)/99/98；買價再下移 0.5×間隔
	if len(buys) != 2 {
		t.Fatalf("skewed buys = %d, want 2", len(buys))
	}
	for _, o := range buys {
		slot, _, _ := spm.parseClientOrderID(o.ClientOrderID)
		if math.Abs(o.Price-(slot-0.5)) > 1e-9 {
			t.Fatalf("buy price %v, want slot %v - 0.5", o.Price, slot)
		}
	}
	// 平倉利差 1×0.75（未注入費率時無下界）
	for _, o := range ordersBy(orders, "SELL", true) {
		slot, _, _ := spm.parseClientOrderID(o.ClientOrderID)
		if math.Abs(o.Price-(slot+0.75)) > 1e-9 {
			t.Fatalf("close price %v, want slot %v + 0.75", o.Price, slot)
		}
	}

	// 手續費下界高於偏斜後利差時取下界：102 × (2×0.0002 + 0.008) = 0.8568 > 0.75
	spm.SetFeeRates(0.0002, 0.0005)
	spm.config.Trading.FeeAwareSpread.SafetyMarginRatio = 0.008
	floor := spm.feeAwareSpreadFloor(102, gridOrdersPostOnly)
	if got := spm.skewedCloseSpread(102, 100, 102, 0.75); math.Abs(got-floor) > 1e-9 || floor <= 0.75 {
		t.Fatalf("skewed spread = %v, want fee floor %v", got, floor)
	}

	// 滿倉 + 強度 1 → 停止開倉
	fillSlot(spm, 104, 1, 104, PositionLegNone)
	fillSlot(spm, 105, 1, 105, PositionLegNone)
	spm.config.Trading.OpenPositionControl.MaxPositionLayers = 0
	spm.config.Trading.GridRiskControl = config.GridRiskControl{Enabled: false}
	spm.config.Trading.OpenPositionControl.BotRiskControl = &config.BotRiskControl{Enabled: true, MaxPositionLayers: 4}
	plan := spm.planOpeningLeg(regime.DirectionLong, 6, 100, spm.loadRegimeTick(), time.Now())
	if plan.Window != 0 || !plan.StopOpening {
		t.Fatalf("full inventory plan = %+v, want stop opening", plan)
	}
}

func TestPlanOpeningLegInventorySkewShortAndBothLegs(t *testing.T) {
	spm, _ := newR5bSPM(t, "BOTH", 8)
	spm.config.Trading.OpenPositionControl.MaxPositionLayers = 4
	spm.config.Trading.InventorySkew = config.InventorySkewConfig{Enabled: true} // 默認強度 0.5
	fillSlot(spm, 105, 1, 105, PositionLegShort)
	fillSlot(spm, 106, 1, 106, PositionLegShort)
	fillSlot(spm, 107, 1, 107, PositionLegShort)
	fillSlot(spm, 108, 1, 108, PositionLegShort) // 空腿 inv=1，多腿 inv=0

	tick := spm.loadRegimeTick()
	short := spm.planOpeningLeg(regime.DirectionShort, 8, 100, tick, time.Now())
	long := spm.planOpeningLeg(regime.DirectionLong, 8, 100, tick, time.Now())
	if short.Window != 4 || math.Abs(short.PriceShift-0.5) > 1e-9 || short.CloseSpreadFactor != 0.75 {
		t.Fatalf("short leg plan = %+v", short)
	}
	if long.Window != 8 || long.PriceShift != 0 || long.CloseSpreadFactor != 1 {
		t.Fatalf("long leg plan = %+v", long)
	}
	if got := spm.shiftedOpenPrice(101, short.PriceShift, "SELL"); got != 101.5 {
		t.Fatalf("short open price = %v, want 101.5", got)
	}
}

// fakeFundingMonitor 資金費率監控假實現（含下次結算時間）
type fakeFundingMonitor struct {
	rate float64
	next time.Time
}

func (f *fakeFundingMonitor) GetBuyBias() float64           { return 1 }
func (f *fakeFundingMonitor) GetSellBias() float64          { return 1 }
func (f *fakeFundingMonitor) IsHighRate() bool              { return false }
func (f *fakeFundingMonitor) GetCurrentRate() float64       { return f.rate }
func (f *fakeFundingMonitor) ShouldPauseBuying() bool       { return false }
func (f *fakeFundingMonitor) GetNextFundingTime() time.Time { return f.next }

func TestFundingPriceOffset(t *testing.T) {
	tests := []struct {
		name                         string
		rate, price, hours, interval float64
		want                         float64
	}{
		{name: "半個周期", rate: 0.001, price: 1000, hours: 4, interval: 10, want: 0.5},
		{name: "封頂 0.5×間隔", rate: 0.01, price: 1000, hours: 8, interval: 10, want: 5},
		{name: "小時數截斷到 8", rate: 0.001, price: 1000, hours: 20, interval: 10, want: 1},
		{name: "收費方不偏移", rate: -0.001, price: 1000, hours: 8, interval: 10, want: 0},
		{name: "間隔無效", rate: 0.001, price: 1000, hours: 8, interval: 0, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fundingPriceOffset(tt.rate, tt.price, tt.hours, tt.interval); math.Abs(got-tt.want) > 1e-9 {
				t.Fatalf("offset = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFundingPlanPricingAndPreSettlementPause(t *testing.T) {
	now := time.Date(2026, 9, 17, 4, 0, 0, 0, time.UTC)
	spm, _ := newR5bSPM(t, "LONG", 6)
	fm := &fakeFundingMonitor{rate: 0.0001, next: now.Add(4 * time.Hour)}
	spm.SetFundingMonitor(fm)

	if shift, pause := spm.fundingPlan(regime.DirectionLong, 1000, 10, now); shift != 0 || pause {
		t.Fatalf("disabled: shift=%v pause=%v", shift, pause)
	}
	spm.config.FundingRate.PricingEnabled = true
	// LONG 付正費率：0.0001×1000×4/8 = 0.05
	if shift, pause := spm.fundingPlan(regime.DirectionLong, 1000, 10, now); math.Abs(shift-0.05) > 1e-9 || pause {
		t.Fatalf("long paying: shift=%v pause=%v", shift, pause)
	}
	// SHORT 收正費率：不偏移
	if shift, _ := spm.fundingPlan(regime.DirectionShort, 1000, 10, now); shift != 0 {
		t.Fatalf("short receiving: shift=%v", shift)
	}
	// 負費率：SHORT 付費
	fm.rate = -0.0002
	if shift, _ := spm.fundingPlan(regime.DirectionShort, 1000, 10, now); math.Abs(shift-0.1) > 1e-9 {
		t.Fatalf("short paying: shift=%v", shift)
	}
	// 結算時間未知 → 按整周期
	fm.next = time.Time{}
	if shift, pause := spm.fundingPlan(regime.DirectionShort, 1000, 10, now); math.Abs(shift-0.2) > 1e-9 || pause {
		t.Fatalf("unknown settlement: shift=%v pause=%v", shift, pause)
	}
	// 結算前暫停：只暫停付費方
	spm.config.FundingRate.PreSettlementPauseMinutes = 10
	fm.next = now.Add(5 * time.Minute)
	if _, pause := spm.fundingPlan(regime.DirectionShort, 1000, 10, now); !pause {
		t.Fatalf("paying side should pause before settlement")
	}
	if _, pause := spm.fundingPlan(regime.DirectionLong, 1000, 10, now); pause {
		t.Fatalf("receiving side must not pause")
	}
	fm.next = now.Add(30 * time.Minute)
	if _, pause := spm.fundingPlan(regime.DirectionShort, 1000, 10, now); pause {
		t.Fatalf("outside pause window must not pause")
	}
}

func TestAdjustOrdersFundingPricingLong(t *testing.T) {
	spm, exec := newR5bSPM(t, "LONG", 3)
	spm.config.FundingRate.PricingEnabled = true
	// rate×price×hours/8 = 0.004×100×1 = 0.4（< 0.5×間隔）
	spm.SetFundingMonitor(&fakeFundingMonitor{rate: 0.004, next: time.Now().Add(8 * time.Hour)})
	fillSlot(spm, 101, 1, 101, PositionLegNone)

	orders := adjustAt(t, spm, exec, 100)
	buys := ordersBy(orders, "BUY", false)
	if len(buys) == 0 {
		t.Fatalf("expected buys")
	}
	for _, o := range buys {
		slot, _, _ := spm.parseClientOrderID(o.ClientOrderID)
		if math.Abs(o.Price-(slot-0.4)) > 0.011 { // 距結算時間在測試執行中略小於 8h，允許一個 tick 的誤差
			t.Fatalf("buy price %v, want about slot %v - 0.4", o.Price, slot)
		}
	}
	closes := ordersBy(orders, "SELL", true)
	if len(closes) != 1 || math.Abs(closes[0].Price-102.4) > 0.011 {
		t.Fatalf("close orders = %+v, want one at about 102.4", closes)
	}
}

func TestAdjustOrdersBothRegimeTrendUpPerLeg(t *testing.T) {
	spm, exec := newR5bSPM(t, "BOTH", 6)
	spm.ConfigureRegimeControl(&fakeRegimeProvider{snap: readySnap(regime.TrendUp)}, RegimeControlOptions{FilterEnabled: true})
	orders := adjustAt(t, spm, exec, 100)
	// 多腿順勢：窗口不變（槽位 100 被安全緩衝跳過 → 5 單）；空腿逆勢：窗口 3（槽位 100 跳過 → 2 單）
	if got := len(ordersBy(orders, "BUY", false)); got != 5 {
		t.Fatalf("both long opens = %d, want 5", got)
	}
	if got := len(ordersBy(orders, "SELL", false)); got != 2 {
		t.Fatalf("both short opens = %d, want 2", got)
	}
	for _, o := range ordersBy(adjustAt(t, spm, exec, 104), "BUY", false) {
		if o.Price > 100 {
			t.Fatalf("both long leg bought above frozen bound: %+v", o)
		}
	}
}

func TestSyncTrendSource(t *testing.T) {
	spm, _ := newR5bSPM(t, "LONG", 6)
	if _, ok := spm.syncTrend(spm.loadRegimeTick()); ok {
		t.Fatalf("no trend source should report !ok")
	}
	provider := &fakeRegimeProvider{snap: readySnap(regime.TrendDown)}
	spm.ConfigureRegimeControl(provider, RegimeControlOptions{FilterEnabled: true})
	if trend, ok := spm.syncTrend(spm.loadRegimeTick()); !ok || trend != "down" {
		t.Fatalf("regime trend = %q %v, want down", trend, ok)
	}
	provider.set(regime.Snapshot{})
	if _, ok := spm.syncTrend(spm.loadRegimeTick()); ok {
		t.Fatalf("unknown regime should report !ok")
	}
}
