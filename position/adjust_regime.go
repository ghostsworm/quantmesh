package position

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/strategy/regime"
)

// ========== K 線級 regime 過濾 / ATR 自適應間隔 / 自動邊界（R5 第五節第 4、5、6 條）==========
//
// 設計見 docs/decisions/2026-09-17-kline-regime-filter.md 與 docs/decisions/2026-09-17-grid-regime-wiring.md。
// 所有行為默認關閉：未注入 RegimeProvider 時 AdjustOrders 與舊邏輯完全一致。

const (
	// regimeControlCheckInterval 間隔控制循環檢查快照的周期；只有「新收盤 K 線」或「有效狀態變化」才會重新計算間隔
	regimeControlCheckInterval = 30 * time.Second
	// intervalCompareEps 判斷間隔是否被外部修改的相對容差
	intervalCompareEps = 1e-9
	// gridModeGeometric 等比網格（PriceInterval 為比例，不支持自適應間隔）
	gridModeGeometric = "geometric"
)

// RegimeProvider 市場狀態快照提供者（regime.Detector 天然滿足；接口定義在使用方）
type RegimeProvider interface {
	Snapshot() regime.Snapshot
}

// RegimeControlOptions regime 相關功能開關（均來自 trading.* 配置）
type RegimeControlOptions struct {
	// FilterEnabled trading.regime_filter.enabled：按 PolicyFor 縮放開倉窗口、放大間隔、冻结順勢邊界，並替代舊 tick 級趨勢過濾
	FilterEnabled bool
	// Adaptive trading.adaptive_interval（已轉換為 regime 類型；Enabled=false 時 Next 返回 base）
	Adaptive regime.AdaptiveIntervalConfig
	// AutoBound trading.upper_bound_freeze：EMA ± k×ATR 自動邊界
	AutoBound config.UpperBoundFreezeConfig
}

// RegimeConfigFromConfig 把 config 鏡像類型轉為 regime.RegimeConfig（字段不一致時編譯失敗，防止漂移）
func RegimeConfigFromConfig(c config.RegimeFilterConfig) regime.RegimeConfig {
	return regime.RegimeConfig(c)
}

// AdaptiveIntervalConfigFromConfig 把 config 鏡像類型轉為 regime.AdaptiveIntervalConfig 並補默認值
func AdaptiveIntervalConfigFromConfig(c config.AdaptiveIntervalConfig) regime.AdaptiveIntervalConfig {
	return regime.AdaptiveIntervalConfig(c).WithDefaults()
}

// frozenBound 順勢邊界冻结狀態
type frozenBound struct {
	active bool
	price  float64
}

// regimeControl regime 接入狀態
type regimeControl struct {
	// mu 保護 provider/opts 與間隔控制字段；鎖順序：spm.mu → regimeControl.mu（持有本鎖時不得獲取 spm.mu）
	mu       sync.Mutex
	provider RegimeProvider
	opts     RegimeControlOptions
	trigger  chan struct{}

	// 間隔控制（mu 保護）
	baseInterval     float64 // 自適應基準（配置的 price_interval）
	baseProfitSpread float64 // 配置的 profit_spread（>0 時隨間隔等比縮放）
	lastApplied      float64 // 最近一次寫入的間隔
	adaptiveCurrent  float64 // 自適應部分的當前值（未乘 policy 縮放，避免自激，見 ADR）
	lastBar          time.Time
	lastEffective    regime.Regime
	evaluated        bool

	// 以下僅在持有 spm.mu 時讀寫（AdjustOrders 路徑）
	freezeLong        frozenBound
	freezeShort       frozenBound
	lastLoggedRegime  regime.Regime
	lastLoggedApplied bool

	// 每個 Bot 只告警一次的標記
	unsupportedModeWarned atomic.Bool
	skewNoMaxLayersWarned atomic.Bool
}

// ConfigureRegimeControl 注入 regime 快照提供者與功能開關。provider 為 nil 時關閉全部 regime 行為。
// 基準間隔取調用時的 price_interval。
func (spm *SuperPositionManager) ConfigureRegimeControl(provider RegimeProvider, opts RegimeControlOptions) {
	spm.mu.Lock()
	defer spm.mu.Unlock()
	rc := &spm.regimeCtl
	rc.mu.Lock()
	rc.provider = provider
	rc.opts = opts
	rc.opts.AutoBound = opts.AutoBound.WithDefaults()
	rc.baseInterval = spm.config.Trading.PriceInterval
	rc.baseProfitSpread = spm.config.Trading.ProfitSpread
	rc.lastApplied = spm.config.Trading.PriceInterval
	rc.adaptiveCurrent = 0
	rc.evaluated = false
	if rc.trigger == nil {
		rc.trigger = make(chan struct{}, 1)
	}
	rc.mu.Unlock()
	rc.freezeLong = frozenBound{}
	rc.freezeShort = frozenBound{}
	spm.markAdjustDirty()
}

// SetRegimeProvider 只注入提供者（等價於 ConfigureRegimeControl(provider, {FilterEnabled: true})）
func (spm *SuperPositionManager) SetRegimeProvider(provider RegimeProvider) {
	spm.ConfigureRegimeControl(provider, RegimeControlOptions{FilterEnabled: provider != nil})
}

// NotifyRegimeChanged 通知間隔控制循環盡快重新評估（非阻塞，可在 Detector 的 OnChange 回調中調用）
func (spm *SuperPositionManager) NotifyRegimeChanged() {
	spm.regimeCtl.mu.Lock()
	ch := spm.regimeCtl.trigger
	spm.regimeCtl.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// regimeTick 單輪 AdjustOrders 使用的 regime 視圖（值類型）
type regimeTick struct {
	hasProvider bool
	opts        RegimeControlOptions
	snap        regime.Snapshot
	effective   regime.Regime
}

// loadRegimeTick 讀取一次快照（調用方可持有 spm.mu）
func (spm *SuperPositionManager) loadRegimeTick() regimeTick {
	rc := &spm.regimeCtl
	rc.mu.Lock()
	provider, opts := rc.provider, rc.opts
	rc.mu.Unlock()
	if provider == nil {
		return regimeTick{effective: regime.Unknown}
	}
	snap := provider.Snapshot()
	return regimeTick{hasProvider: true, opts: opts, snap: snap, effective: snap.Effective()}
}

// filterActive regime 過濾已啟用（此時舊 tick 級趨勢過濾退役）
func (t regimeTick) filterActive() bool {
	return t.hasProvider && t.opts.FilterEnabled
}

// policyActive 本輪應用 PolicyFor：過濾啟用且狀態有效。Unknown/過期時保持原有行為。
func (t regimeTick) policyActive() bool {
	return t.filterActive() && t.effective != regime.Unknown
}

// snapshotUsable 快照已就緒且未過期
func (t regimeTick) snapshotUsable() bool {
	return t.hasProvider && t.snap.Ready && !t.snap.Stale
}

// autoBounds ATR 自動邊界：high = EMA + k×ATR，low = EMA − k×ATR；不可用時返回 0
func (t regimeTick) autoBounds() (high, low float64) {
	if !t.opts.AutoBound.Enabled || !t.snapshotUsable() || t.snap.EMA <= 0 || t.snap.ATR <= 0 {
		return 0, 0
	}
	k := t.opts.AutoBound.WithDefaults().ATRMultiplier
	high = t.snap.EMA + k*t.snap.ATR
	low = t.snap.EMA - k*t.snap.ATR
	if low < 0 {
		low = 0
	}
	return high, low
}

// syncTrend 資金費率與趨勢聯動使用的趨勢："up"/"down"/"side"。
// regime 過濾啟用時用 K 線 regime（Unknown 時不可用），否則沿用舊趨勢檢測器。
func (spm *SuperPositionManager) syncTrend(t regimeTick) (string, bool) {
	if t.filterActive() {
		switch t.effective {
		case regime.TrendUp:
			return "up", true
		case regime.TrendDown:
			return "down", true
		case regime.Range:
			return "side", true
		default:
			return "", false
		}
	}
	if spm.trendDetector != nil && spm.gridRiskControl().TrendFilterEnabled {
		return spm.trendDetector.GetCurrentTrend(), true
	}
	return "", false
}

// logRegimeTransition 有效狀態變化時記錄一次日誌（調用方持有 spm.mu）
func (spm *SuperPositionManager) logRegimeTransition(t regimeTick) {
	if !t.hasProvider {
		return
	}
	rc := &spm.regimeCtl
	if rc.lastLoggedApplied && rc.lastLoggedRegime == t.effective {
		return
	}
	rc.lastLoggedApplied = true
	rc.lastLoggedRegime = t.effective
	logger.Info("🧭 [%s] [Regime] 有效狀態 → %s (ADX=%.1f slope=%.3f ATR=%.4f stale=%v ready=%v)",
		spm.logPrefix(), t.effective, t.snap.ADX, t.snap.EMASlope, t.snap.ATR, t.snap.Stale, t.snap.Ready)
}

// effectivePriceBounds 合併手動 price_low/price_high 與 ATR 自動邊界（取更嚴格者）。
// 自動上沿作用於做多腿（LONG/BOTH），自動下沿作用於做空腿（SHORT/BOTH）。
func (spm *SuperPositionManager) effectivePriceBounds(t regimeTick) (low, high float64) {
	low, high = spm.config.Trading.PriceLow, spm.config.Trading.PriceHigh
	autoHigh, autoLow := t.autoBounds()
	if autoHigh > 0 && !spm.isShort() && (high <= 0 || autoHigh < high) {
		high = autoHigh
	}
	if autoLow > 0 && !spm.isLong() && (low <= 0 || autoLow > low) {
		low = autoLow
	}
	return low, high
}

// applyFreezeBound 順勢邊界冻结（調用方持有 spm.mu）：
// freeze=true 且剛進入時記錄當前開倉窗口的邊界（LONG=最高槽位，SHORT=最低槽位），之後過濾掉越界槽位；
// freeze=false 時解除。只影響開倉槽位，平倉單不受影響。
func (spm *SuperPositionManager) applyFreezeBound(dir regime.Direction, freeze bool, slotPrices []float64, gridPrice float64) []float64 {
	fb := &spm.regimeCtl.freezeLong
	if dir == regime.DirectionShort {
		fb = &spm.regimeCtl.freezeShort
	}
	if !freeze {
		if fb.active {
			logger.Info("🧊 [%s] [Regime] %s 邊界冻结解除（原邊界 %s）", spm.logPrefix(), dir, formatPrice(fb.price, spm.priceDecimals))
		}
		*fb = frozenBound{}
		return slotPrices
	}
	if !fb.active {
		bound := gridPrice
		for i, p := range slotPrices {
			if i == 0 || (dir == regime.DirectionLong && p > bound) || (dir == regime.DirectionShort && p < bound) {
				bound = p
			}
		}
		*fb = frozenBound{active: true, price: bound}
		logger.Info("🧊 [%s] [Regime] %s 順勢趨勢，冻结開倉邊界於 %s（不追價重建倉，平倉單照常）",
			spm.logPrefix(), dir, formatPrice(bound, spm.priceDecimals))
	}
	eps := spm.priceTick() / 2
	out := slotPrices[:0:0]
	for _, p := range slotPrices {
		if dir == regime.DirectionLong && p > fb.price+eps {
			continue
		}
		if dir == regime.DirectionShort && p < fb.price-eps {
			continue
		}
		out = append(out, p)
	}
	return out
}

// intervalScaleFor 當前方向在給定狀態下的間隔放大倍數；BOTH 取兩腿較大者（兩腿共用一個間隔）
func (spm *SuperPositionManager) intervalScaleFor(r regime.Regime, direction string) float64 {
	long := regime.PolicyFor(r, regime.DirectionLong).IntervalScale
	short := regime.PolicyFor(r, regime.DirectionShort).IntervalScale
	switch direction {
	case "SHORT":
		return short
	case "BOTH":
		return math.Max(long, short)
	default:
		return long
	}
}

// RefreshRegimeInterval 按最新快照重新計算網格間隔並通過 UpdateTradingParams 應用。
// 只在「新收盤 K 線」或「有效狀態變化」時計算；返回是否修改了間隔。調用方不得持有 spm.mu。
//
//	adaptive = Adaptive.Next(adaptiveCurrent, ATR, base)   （未啟用時 = base）
//	target   = QuantizeInterval(adaptive × PolicyFor.IntervalScale, base)
//
// 目標間隔恆為 base 的整數倍，槽位仍落在 anchor + n×base 上，無需移動錨點。
// Unknown/過期 → 回到 base（原有行為）。等比網格與三級火箭網格不支持，跳過並告警一次。
func (spm *SuperPositionManager) RefreshRegimeInterval() bool {
	spm.mu.RLock()
	cur := spm.config.Trading.PriceInterval
	curSpread := spm.config.Trading.ProfitSpread
	gridMode := spm.config.Trading.GridMode
	rocket := spm.config.Trading.RocketTieredGrid != nil && spm.config.Trading.RocketTieredGrid.Enabled
	direction := spm.config.Trading.Direction
	spm.mu.RUnlock()

	rc := &spm.regimeCtl
	rc.mu.Lock()
	provider, opts := rc.provider, rc.opts
	if provider == nil || (!opts.FilterEnabled && !opts.Adaptive.Enabled) {
		rc.mu.Unlock()
		return false
	}
	if gridMode == gridModeGeometric || rocket {
		rc.mu.Unlock()
		if rc.unsupportedModeWarned.CompareAndSwap(false, true) {
			logger.Warn("⚠️ [%s] [Regime] 等比網格/三級火箭網格不支持自適應間隔與 regime 間隔縮放，已跳過（開倉窗口與邊界冻结仍生效）", spm.logPrefix())
		}
		return false
	}
	if rc.lastApplied > 0 && math.Abs(cur-rc.lastApplied) > intervalCompareEps*math.Max(1, rc.lastApplied) {
		logger.Info("ℹ️ [%s] [Regime] price_interval 被外部修改 %.8f → %.8f，以新值作為自適應基準", spm.logPrefix(), rc.lastApplied, cur)
		rc.baseInterval = cur
		rc.baseProfitSpread = curSpread
		rc.adaptiveCurrent = 0
		rc.evaluated = false
	}
	if rc.baseInterval <= 0 {
		rc.baseInterval = cur
		rc.baseProfitSpread = curSpread
	}
	base := rc.baseInterval
	rc.mu.Unlock()
	if base <= 0 {
		return false
	}

	snap := provider.Snapshot()
	eff := snap.Effective()

	rc.mu.Lock()
	if rc.evaluated && snap.BarOpenTime.Equal(rc.lastBar) && eff == rc.lastEffective {
		rc.mu.Unlock()
		return false
	}
	rc.evaluated = true
	rc.lastBar = snap.BarOpenTime
	rc.lastEffective = eff

	target := base
	if eff != regime.Unknown {
		adaptive := opts.Adaptive.Next(rc.adaptiveCurrent, snap.ATR, base)
		if opts.Adaptive.Enabled {
			rc.adaptiveCurrent = adaptive
		}
		scale := 1.0
		if opts.FilterEnabled {
			scale = spm.intervalScaleFor(eff, direction)
		}
		if q := regime.QuantizeInterval(adaptive*scale, base); q > 0 {
			target = q
		}
	}
	newSpread := -1.0 // 不修改
	if rc.baseProfitSpread > 0 {
		newSpread = rc.baseProfitSpread * target / base
	}
	rc.lastApplied = target
	rc.mu.Unlock()

	if math.Abs(target-cur) <= intervalCompareEps*math.Max(1, cur) {
		return false
	}
	changed := spm.UpdateTradingParams(target, newSpread, 0, 0, 0)
	if changed {
		logger.Info("📐 [%s] [Regime] 網格間隔 %.8f → %.8f（base=%.8f, regime=%s, ATR=%.8f）",
			spm.logPrefix(), cur, target, base, eff, snap.ATR)
	}
	return changed
}

// RunRegimeControlLoop 間隔控制循環：啟動時評估一次，之後每 regimeControlCheckInterval 或收到 NotifyRegimeChanged 時評估。
// ctx 取消時返回。
func (spm *SuperPositionManager) RunRegimeControlLoop(ctx context.Context) {
	spm.regimeCtl.mu.Lock()
	if spm.regimeCtl.trigger == nil {
		spm.regimeCtl.trigger = make(chan struct{}, 1)
	}
	trigger := spm.regimeCtl.trigger
	spm.regimeCtl.mu.Unlock()

	spm.RefreshRegimeInterval()
	ticker := spm.Clock().NewTicker(regimeControlCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			spm.RefreshRegimeInterval()
		case <-trigger:
			spm.RefreshRegimeInterval()
		}
	}
}
