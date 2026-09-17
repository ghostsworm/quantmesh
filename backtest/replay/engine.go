package replay

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"quantmesh/backtest"
	"quantmesh/exchange"
	"quantmesh/position"
)

// Result 回放結果
type Result struct {
	// Backtest 與舊引擎相同結構的結果（權益曲線、成交、通用指標），可直接用於報告
	Backtest *backtest.BacktestResult `json:"backtest"`
	// Metrics 回放專有指標
	Metrics Metrics `json:"metrics"`
}

// Engine 回放引擎（單次使用：一個 Engine 只能 Run 一次）
type Engine struct {
	cfg  Config
	ex   *simExchange
	exec *simExecutor
	spm  *position.SuperPositionManager
	// clock 由 tick 時間戳驅動的模擬時鐘（注入倉位管理器）
	clock *SimClock
	ran   bool

	// 權益/敞口統計
	peakEquity       float64
	maxDrawdownAbs   float64
	maxDrawdownPct   float64
	equityCurve      []backtest.EquityPoint
	exposure         ExposureSummary
	weightedNotional float64
	inMarketMs       int64
	lastSampleTs     int64
	lastStatTs       int64
	lastNotional     float64
	lastQty          float64
	adjustCalls      int
}

// NewEngine 創建回放引擎；配置在 Run 時按首個 tick 價格補全默認值（如推斷精度）。
func NewEngine(cfg Config) *Engine {
	return &Engine{cfg: cfg}
}

// SPM 返回被驅動的倉位管理器（Run 之後可用於檢查槽位狀態；Run 之前為 nil）
func (e *Engine) SPM() *position.SuperPositionManager { return e.spm }

// Clock 返回注入倉位管理器的模擬時鐘（Run 之前為 nil）
func (e *Engine) Clock() *SimClock { return e.clock }

// Run 按時間順序回放成交序列：
//  1. 首個 tick：設置最新價並 Initialize(price)（與實盤啟動一致，從空倉開始）；
//  2. 每個 tick：結算跨越的資金費 → 用該筆成交撮合掛單並投遞成交回報（OnOrderUpdate）→
//     距上次調用 ≥ AdjustIntervalMs 時調用 AdjustOrders(price) → 投遞新下單的 NEW / 撤單 CANCELED / 立即成交回報；
//  3. 記錄權益與敞口。
//
// 回報在倉位管理器的調用返回後才投遞，模擬 WebSocket 推送的異步性（同時避免持鎖回調死鎖）。
func (e *Engine) Run(ticks []Tick) (*Result, error) {
	if e.ran {
		return nil, fmt.Errorf("replay engine: Run called twice")
	}
	e.ran = true
	if len(ticks) == 0 {
		return nil, fmt.Errorf("replay engine: no ticks")
	}
	for i := 1; i < len(ticks); i++ {
		if ticks[i].Timestamp < ticks[i-1].Timestamp {
			return nil, fmt.Errorf("replay engine: ticks not sorted at index %d (%d < %d)", i, ticks[i].Timestamp, ticks[i-1].Timestamp)
		}
	}
	first := ticks[0]
	if first.Price <= 0 {
		return nil, fmt.Errorf("replay engine: first tick has non-positive price %.8f", first.Price)
	}
	cfg, err := e.cfg.normalized(first.Price)
	if err != nil {
		return nil, err
	}
	e.cfg = cfg
	e.ex = newSimExchange(cfg)
	e.exec = newSimExecutor(e.ex, cfg.Bot)
	e.spm = position.NewSuperPositionManager(cfg.Bot, e.exec, e.ex, cfg.PriceDecimals, cfg.QuantityDecimals)
	// 模擬時鐘：保證金鎖、reduce-only 冷卻、去抖兜底、緩存 TTL、撤單等待均按 tick 時間生效
	e.clock = NewSimClock(time.UnixMilli(first.Timestamp))
	e.spm.SetClock(e.clock)
	// 與實盤 symbol_manager 注入真實費率一致：費率感知最小利差使用回放的 maker/taker
	if cfg.Matching.TakerFeeRate > 0 {
		e.spm.SetFeeRates(cfg.Matching.MakerFeeRate, cfg.Matching.TakerFeeRate)
	}

	e.ex.setMarket(first.Timestamp, first.Price)
	if err := e.spm.Initialize(first.Price, strconv.FormatFloat(first.Price, 'f', cfg.PriceDecimals, 64)); err != nil {
		return nil, fmt.Errorf("replay engine: initialize position manager at price %.8f: %w", first.Price, err)
	}
	e.deliver()
	e.peakEquity = cfg.InitialCapital
	e.lastStatTs = first.Timestamp
	e.lastSampleTs = first.Timestamp
	e.equityCurve = append(e.equityCurve, backtest.EquityPoint{Timestamp: first.Timestamp, Equity: cfg.InitialCapital})
	e.exposure.Samples = append(e.exposure.Samples, ExposurePoint{Timestamp: first.Timestamp, Equity: cfg.InitialCapital})

	lastAdjust := int64(math.MinInt64)
	prevTs := first.Timestamp
	for i, t := range ticks {
		if t.Price <= 0 {
			continue
		}
		if cfg.FundingEnabled && i > 0 {
			e.ex.settleFunding(prevTs, t.Timestamp, e.fundingRateAt)
		}
		prevTs = t.Timestamp
		e.clock.AdvanceToMillis(t.Timestamp)
		if i > 0 {
			e.ex.matchTrade(t)
			e.deliver()
		}
		if lastAdjust == math.MinInt64 || t.Timestamp-lastAdjust >= cfg.AdjustIntervalMs {
			lastAdjust = t.Timestamp
			if err := e.spm.AdjustOrders(t.Price); err != nil {
				return nil, fmt.Errorf("replay engine: AdjustOrders at ts=%d price=%.8f: %w", t.Timestamp, t.Price, err)
			}
			e.adjustCalls++
			e.deliver()
		}
		e.record(t.Timestamp, false)
	}
	last := ticks[len(ticks)-1]
	e.record(last.Timestamp, true)
	return e.buildResult(ticks), nil
}

// deliver 投遞所有待處理回報；OnOrderUpdate 不會觸發新下單，但循環直到隊列清空以防萬一
func (e *Engine) deliver() {
	for {
		updates := e.ex.drainUpdates()
		if len(updates) == 0 {
			return
		}
		for _, u := range updates {
			e.spm.OnOrderUpdate(u)
		}
	}
}

// fundingRateAt 結算時刻的費率：序列中最後一個生效時間 ≤ ts 的點；無序列時用固定費率
func (e *Engine) fundingRateAt(ts int64) float64 {
	series := e.cfg.FundingSeries
	if len(series) == 0 {
		return e.cfg.FundingRate
	}
	rate := e.cfg.FundingRate
	for _, p := range series {
		if p.Timestamp > ts {
			break
		}
		rate = p.Rate
	}
	return rate
}

// record 更新回撤、時間加權敞口，並按採樣間隔記錄權益/敞口點
func (e *Engine) record(ts int64, final bool) {
	snap := e.ex.snapshot()
	if dt := ts - e.lastStatTs; dt > 0 {
		e.weightedNotional += math.Abs(e.lastNotional) * float64(dt)
		if e.lastQty != 0 {
			e.inMarketMs += dt
		}
		e.lastStatTs = ts
	}
	notional := snap.netQty * snap.lastPrice
	e.lastNotional = notional
	e.lastQty = snap.netQty
	if a := math.Abs(snap.netQty); a > e.exposure.MaxAbsQty {
		e.exposure.MaxAbsQty = a
	}
	if a := math.Abs(notional); a > e.exposure.MaxAbsNotional {
		e.exposure.MaxAbsNotional = a
	}
	if snap.equity > e.peakEquity {
		e.peakEquity = snap.equity
	}
	if dd := e.peakEquity - snap.equity; dd > e.maxDrawdownAbs {
		e.maxDrawdownAbs = dd
	}
	if e.peakEquity > 0 {
		if pct := (e.peakEquity - snap.equity) / e.peakEquity * 100; pct > e.maxDrawdownPct {
			e.maxDrawdownPct = pct
		}
	}
	if final || ts-e.lastSampleTs >= e.cfg.EquitySampleMs {
		e.lastSampleTs = ts
		e.equityCurve = append(e.equityCurve, backtest.EquityPoint{Timestamp: ts, Equity: snap.equity})
		e.exposure.Samples = append(e.exposure.Samples, ExposurePoint{Timestamp: ts, NetQty: snap.netQty, Notional: notional, Equity: snap.equity})
	}
}

func (e *Engine) buildResult(ticks []Tick) *Result {
	first, last := ticks[0], ticks[len(ticks)-1]
	e.ex.mu.Lock()
	stats := e.ex.stats
	trades := append([]backtest.Trade(nil), e.ex.trades...)
	realized := e.ex.realized
	feesMaker, feesTaker, funding := e.ex.feesMaker, e.ex.feesTaker, e.ex.fundingPaid
	netQty, avgEntry, lastPrice := e.ex.netQty, e.ex.avgEntry, e.ex.lastPrice
	equity := e.ex.equityLocked()
	e.ex.mu.Unlock()

	unrealized := 0.0
	if netQty != 0 {
		unrealized = netQty * (lastPrice - avgEntry)
	}
	fees := feesMaker + feesTaker
	m := Metrics{
		InitialCapital:       e.cfg.InitialCapital,
		FinalEquity:          equity,
		NetPnL:               equity - e.cfg.InitialCapital,
		RealizedPnL:          realized,
		UnrealizedPnL:        unrealized,
		FeesMaker:            feesMaker,
		FeesTaker:            feesTaker,
		FeesTotal:            fees,
		FundingPaid:          funding,
		MaxDrawdownPct:       e.maxDrawdownPct,
		MaxDrawdownAbs:       e.maxDrawdownAbs,
		Fills:                stats.fills,
		MakerFills:           stats.makerFills,
		TakerFills:           stats.takerFills,
		PartialFills:         stats.partialFills,
		OrdersPlaced:         stats.ordersPlaced,
		OrdersCanceled:       stats.ordersCanceled,
		PostOnlyRejects:      stats.postOnlyRejects,
		PostOnlyRepriced:     stats.postOnlyRepriced,
		PostOnlyFinalRejects: stats.postOnlyFinal,
		ReduceOnlyRejects:    stats.reduceOnlyRejects,
		MarginRejects:        stats.marginRejects,
		ClosedGrids:          stats.closedGrids,
		TicksProcessed:       len(ticks),
		AdjustCalls:          e.adjustCalls,
		StartTime:            first.Timestamp,
		EndTime:              last.Timestamp,
	}
	if stats.totalVolume > 0 {
		m.MakerRatio = stats.makerVolume / stats.totalVolume
	}
	if stats.closedGrids > 0 {
		m.NetProfitPerGrid = (realized - fees) / float64(stats.closedGrids)
		m.FeePerGrid = fees / float64(stats.closedGrids)
	}
	if fees > 0 {
		m.GridNetProfitToFeeRatio = (realized - fees) / fees
	}
	e.exposure.FinalNetQty = netQty
	if span := last.Timestamp - first.Timestamp; span > 0 {
		e.exposure.TimeWeightedAvgAbsNotional = e.weightedNotional / float64(span)
		e.exposure.TimeInMarketPct = float64(e.inMarketMs) / float64(span) * 100
	}
	e.exposure.Samples = thinExposure(e.exposure.Samples, maxExposureSamples)
	m.Exposure = e.exposure

	btMetrics := backtest.CalculateMetricsWithPrice(e.equityCurve, trades, e.cfg.InitialCapital, 0, lastPrice)
	btMetrics.MaxPosition = e.exposure.MaxAbsQty
	res := &backtest.BacktestResult{
		Symbol:         e.cfg.Bot.Trading.Symbol,
		Strategy:       "grid_replay",
		StartTime:      time.UnixMilli(first.Timestamp),
		EndTime:        time.UnixMilli(last.Timestamp),
		InitialCapital: e.cfg.InitialCapital,
		FinalCapital:   equity,
		Equity:         e.equityCurve,
		Trades:         trades,
		Metrics:        btMetrics,
		RiskMetrics:    backtest.CalculateRiskMetrics(e.equityCurve),
	}
	return &Result{Backtest: res, Metrics: m}
}

// thinExposure 均勻抽稀並保留首尾
func thinExposure(samples []ExposurePoint, max int) []ExposurePoint {
	if len(samples) <= max || max < 2 {
		return samples
	}
	out := make([]ExposurePoint, 0, max)
	step := float64(len(samples)-1) / float64(max-1)
	for i := 0; i < max; i++ {
		out = append(out, samples[int(math.Round(float64(i)*step))])
	}
	return out
}

// RunTicks 便捷函數：用 tick 序列回放
func RunTicks(cfg Config, ticks []Tick) (*Result, error) {
	return NewEngine(cfg).Run(ticks)
}

// RunAggTrades 便捷函數：用 aggTrade 數據回放（首選數據源）
func RunAggTrades(cfg Config, rows []backtest.AggTradeRow) (*Result, error) {
	return NewEngine(cfg).Run(AggTradesToTicks(rows))
}

// RunCandles 便捷函數：無 tick 數據時用 K 線內路徑回退
func RunCandles(cfg Config, candles []*exchange.Candle, opt IntrabarOptions) (*Result, error) {
	ticks, err := CandlesToTicks(candles, opt)
	if err != nil {
		return nil, err
	}
	return NewEngine(cfg).Run(ticks)
}
