package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/position"
	"quantmesh/strategy/regime"
)

// gridRegimeRuntime 單個網格 Bot 的 K 線 regime 檢測器與間隔控制循環
// （trading.regime_filter / adaptive_interval / upper_bound_freeze 任一啟用時創建）
type gridRegimeRuntime struct {
	symbol   string
	detector *regime.Detector
	opts     position.RegimeControlOptions
	spm      atomic.Pointer[position.SuperPositionManager]

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// gridRegimeNeeded 是否需要 K 線檢測器
func gridRegimeNeeded(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	t := cfg.Trading
	return t.RegimeFilter.Enabled || t.AdaptiveInterval.Enabled || t.UpperBoundFreeze.Enabled
}

// logLegacyTrendFilterDeprecation 舊 tick 級趨勢過濾的廢棄提示
func logLegacyTrendFilterDeprecation(ctx context.Context, cfg *config.Config, symbol string) {
	if cfg == nil || !cfg.Trading.GridRiskControl.TrendFilterEnabled {
		return
	}
	if cfg.Trading.RegimeFilter.Enabled {
		logger.InfoCtx(ctx, "ℹ️ [%s] 已啟用 trading.regime_filter，舊 grid_risk_control.trend_filter_enabled（50ms tick 均線）不再參與開倉判斷", symbol)
		return
	}
	logger.WarnCtx(ctx, "⚠️ [%s] grid_risk_control.trend_filter_enabled 基於 50ms tick 均線（約 1.5 秒窗口），已廢棄；建議改用 trading.regime_filter（K 線 ADX/EMA 斜率）", symbol)
}

// newGridRegimeRuntime 校驗配置並創建檢測器（不啟動任何 goroutine）；無需檢測器時返回 nil, nil
func newGridRegimeRuntime(cfg *config.Config, symbol string, src regime.KlineSource) (*gridRegimeRuntime, error) {
	if !gridRegimeNeeded(cfg) {
		return nil, nil
	}
	if src == nil {
		return nil, fmt.Errorf("regime detector for %s: kline source is nil", symbol)
	}
	t := cfg.Trading
	opts := position.RegimeControlOptions{
		FilterEnabled: t.RegimeFilter.Enabled,
		Adaptive:      position.AdaptiveIntervalConfigFromConfig(t.AdaptiveInterval),
		AutoBound:     t.UpperBoundFreeze.WithDefaults(),
	}
	if err := opts.Adaptive.Validate(); err != nil {
		return nil, fmt.Errorf("trading.adaptive_interval (%s): %w", symbol, err)
	}
	if opts.Adaptive.Enabled && t.DynamicAdjustment.Enabled && t.DynamicAdjustment.PriceInterval.Enabled {
		logger.Warn("⚠️ [%s] trading.adaptive_interval 與 dynamic_adjustment.price_interval 同時啟用會互相覆蓋間隔，已停用 adaptive_interval", symbol)
		opts.Adaptive.Enabled = false
	}

	rt := &gridRegimeRuntime{symbol: symbol, opts: opts}
	rcfg := position.RegimeConfigFromConfig(t.RegimeFilter)
	detector, err := regime.NewDetector(symbol, rcfg, src, regime.WithOnChange(rt.onChange))
	if err != nil {
		return nil, fmt.Errorf("trading.regime_filter (%s): %w", symbol, err)
	}
	rt.detector = detector
	return rt, nil
}

func (r *gridRegimeRuntime) onChange(c regime.Change) {
	logger.Info("🧭 [%s] [Regime] %s → %s (ADX=%.1f slope=%.3f ATR=%.6f)",
		r.symbol, c.From, c.To, c.Snapshot.ADX, c.Snapshot.EMASlope, c.Snapshot.ATR)
	if spm := r.spm.Load(); spm != nil {
		spm.NotifyRegimeChanged()
	}
}

// start 注入倉位管理器並啟動檢測器與間隔控制循環
func (r *gridRegimeRuntime) start(ctx context.Context, spm *position.SuperPositionManager) error {
	if r == nil || spm == nil {
		return nil
	}
	r.spm.Store(spm)
	spm.ConfigureRegimeControl(r.detector, r.opts)
	if err := r.detector.Start(ctx); err != nil {
		spm.ConfigureRegimeControl(nil, position.RegimeControlOptions{})
		return fmt.Errorf("start regime detector %s: %w", r.symbol, err)
	}
	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	r.mu.Lock()
	r.cancel, r.done = cancel, done
	r.mu.Unlock()
	go func() {
		defer close(done)
		spm.RunRegimeControlLoop(loopCtx)
	}()
	logger.InfoCtx(ctx, "🧭 [%s] K 線 regime 已啟動 (周期=%s 輪詢=%s filter=%v adaptive_interval=%v upper_bound_freeze=%v)",
		r.symbol, r.detector.Config().KlineInterval, r.detector.PollInterval(),
		r.opts.FilterEnabled, r.opts.Adaptive.Enabled, r.opts.AutoBound.Enabled)
	return nil
}

// stop 停止間隔控制循環與檢測器並等待退出（冪等，nil 安全）
func (r *gridRegimeRuntime) stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.cancel, r.done = nil, nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	r.detector.Stop()
}
