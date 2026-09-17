package replay

import (
	"context"
	"fmt"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/position"
	"quantmesh/strategy/regime"
)

// 在模擬時間上重現實盤後台循環（symbol_manager_regime.go / RunRegimeControlLoop / FundingRateMonitor），
// 使 regime_filter、adaptive_interval、upper_bound_freeze、funding_rate.pricing_enabled 在回放中生效：
//   - regime.Detector.Refresh：首個 tick 一次，之後每 Detector.PollInterval（實盤 Detector.run 的牆鐘 ticker）；
//   - spm.RefreshRegimeInterval：首個 tick 一次，之後每 RegimeControlCheckInterval，或檢測器狀態變化後的下一個 tick
//     （實盤 RunRegimeControlLoop 的 ticker 與 NotifyRegimeChanged 觸發）；
//   - 資金費監控器：SimFundingMonitor（上一期已結算費率，無未來函數）。
// 全部在引擎主循環內同步調用（Config.Setup / Config.OnTick），結果確定、可重複；不啟動任何 goroutine。
// inventory_skew 與 fee_aware_spread 由倉位管理器直接讀取 Bot 配置，不需要注入。
// 使用方：tools/replaycompare（校準工具）與 RunGridTask（Web 回測任務 engine=replay）。

// RegimeControlCheckInterval 與 position.regimeControlCheckInterval（30s）保持一致
const RegimeControlCheckInterval = 30 * time.Second

// FeatureOptions 注入功能所需的外部數據
type FeatureOptions struct {
	// Symbol 交易對（與 Bot.Trading.Symbol 一致）
	Symbol string
	// RegimeKlines regime 檢測器使用的 K 線，周期必須等於 regime_filter.kline_interval（未配置時為 regime.DefaultKlineInterval）。
	// 可包含回放開始前的歷史用於預熱；K 線源只返回模擬時間前已收盤的部分。
	RegimeKlines []*exchange.Candle
	// FundingSeries 資金費率序列（按時間升序），供資金費監控器使用；為空時始終使用 FundingFallbackRate
	FundingSeries []FundingPoint
	// FundingFallbackRate 序列之前（或沒有序列）時的每 8h 費率
	FundingFallbackRate float64
}

// FeatureStats 注入功能的運行統計
type FeatureStats struct {
	// RegimeEnabled 是否注入了 regime 檢測器（regime_filter / adaptive_interval / upper_bound_freeze 任一開啟）
	RegimeEnabled bool `json:"regime_enabled"`
	// FundingMonitorEnabled 是否注入了資金費監控器（funding_rate.enabled）
	FundingMonitorEnabled bool `json:"funding_monitor_enabled"`
	// RefreshErrors 檢測器刷新失敗次數（歷史 K 線不足以完成 bootstrap 時屬預期）
	RefreshErrors int `json:"regime_refresh_errors"`
	// LastRefreshError 最後一次刷新錯誤
	LastRefreshError string `json:"last_regime_refresh_error,omitempty"`
	// IntervalChanges 自適應間隔實際改變網格間隔的次數
	IntervalChanges int `json:"interval_changes"`
	// Samples 按 regime 控制檢查點（每 30s 模擬時間）採樣的次數
	Samples int `json:"regime_samples"`
	// IntervalMultipleSum 各採樣點「當前間隔 / 基礎間隔」之和
	IntervalMultipleSum float64 `json:"-"`
	// RegimeCounts 各採樣點的有效行情狀態計數
	RegimeCounts map[string]int `json:"regime_counts,omitempty"`
}

// MeanIntervalMultiple 採樣期間平均「當前間隔 / 基礎間隔」；無採樣時返回 0
func (s *FeatureStats) MeanIntervalMultiple() float64 {
	if s == nil || s.Samples == 0 {
		return 0
	}
	return s.IntervalMultipleSum / float64(s.Samples)
}

// RegimeSharePct 各行情狀態佔採樣點的百分比；無採樣時返回 nil
func (s *FeatureStats) RegimeSharePct() map[string]float64 {
	if s == nil || s.Samples == 0 {
		return nil
	}
	out := make(map[string]float64, len(s.RegimeCounts))
	for k, n := range s.RegimeCounts {
		out[k] = float64(n) / float64(s.Samples) * percentScale
	}
	return out
}

// percentScale 比例轉百分比
const percentScale = 100.0

// NeedsRegimeDetector 與 symbol_manager_regime.gridRegimeNeeded 一致
func NeedsRegimeDetector(bot *config.Config) bool {
	if bot == nil {
		return false
	}
	t := bot.Trading
	return t.RegimeFilter.Enabled || t.AdaptiveInterval.Enabled || t.UpperBoundFreeze.Enabled
}

// RegimeKlineInterval Bot 配置下 regime 檢測器使用的 K 線周期（補默認值後）
func RegimeKlineInterval(bot *config.Config) string {
	return position.RegimeConfigFromConfig(bot.Trading.RegimeFilter).WithDefaults().KlineInterval
}

// regimeControlOptions 與 symbol_manager_regime.newGridRegimeRuntime 一致
func regimeControlOptions(bot *config.Config) (position.RegimeControlOptions, error) {
	t := bot.Trading
	opts := position.RegimeControlOptions{
		FilterEnabled: t.RegimeFilter.Enabled,
		Adaptive:      position.AdaptiveIntervalConfigFromConfig(t.AdaptiveInterval),
		AutoBound:     t.UpperBoundFreeze.WithDefaults(),
	}
	if err := opts.Adaptive.Validate(); err != nil {
		return opts, fmt.Errorf("trading.adaptive_interval: %w", err)
	}
	return opts, nil
}

// InstallFeatureHooks 按 cfg.Bot 的開關設置 cfg.Setup / cfg.OnTick，注入 regime 檢測器、自適應間隔控制與資金費監控器。
// 兩類功能都未開啟時不修改 cfg。cfg.Setup/OnTick 已被設置時返回錯誤（避免靜默覆蓋調用方的鉤子）。
// 返回的統計在 Engine.Run 期間更新，Run 結束後讀取。
func InstallFeatureHooks(cfg *Config, opts FeatureOptions) (*FeatureStats, error) {
	if cfg == nil || cfg.Bot == nil {
		return nil, fmt.Errorf("install feature hooks: bot config is nil")
	}
	bot := cfg.Bot
	st := &FeatureStats{RegimeEnabled: NeedsRegimeDetector(bot), FundingMonitorEnabled: bot.FundingRate.Enabled, RegimeCounts: map[string]int{}}
	if !st.RegimeEnabled && !st.FundingMonitorEnabled {
		return st, nil
	}
	if cfg.Setup != nil || (st.RegimeEnabled && cfg.OnTick != nil) {
		return nil, fmt.Errorf("install feature hooks: Config.Setup/OnTick already set")
	}
	symbol := opts.Symbol
	if symbol == "" {
		symbol = bot.Trading.Symbol
	}
	var (
		spm          *position.SuperPositionManager
		det          *regime.Detector
		base         float64
		nextPoll     time.Time
		nextControl  time.Time
		changed      bool
		started      bool
		ctx          = context.Background()
		pollInterval time.Duration
	)
	cfg.Setup = func(s *position.SuperPositionManager, clock position.Clock) error {
		spm = s
		if st.FundingMonitorEnabled {
			spm.SetFundingMonitor(NewSimFundingMonitor(bot.FundingRate, opts.FundingSeries, opts.FundingFallbackRate, clock))
		}
		if !st.RegimeEnabled {
			return nil
		}
		ctrl, err := regimeControlOptions(bot)
		if err != nil {
			return err
		}
		src, err := NewClosedKlineSource(symbol, RegimeKlineInterval(bot), opts.RegimeKlines, clock.Now)
		if err != nil {
			return err
		}
		det, err = regime.NewDetector(symbol, position.RegimeConfigFromConfig(bot.Trading.RegimeFilter), src,
			regime.WithClock(clock.Now), regime.WithOnChange(func(regime.Change) { changed = true }))
		if err != nil {
			return fmt.Errorf("regime detector %s: %w", symbol, err)
		}
		pollInterval = det.PollInterval()
		base = s.GetPriceInterval()
		// 與實盤一致：Initialize 之後才啟動檢測器與間隔控制循環（在首個 OnTick 中完成）
		spm.ConfigureRegimeControl(det, ctrl)
		return nil
	}
	if !st.RegimeEnabled {
		return st, nil
	}
	cfg.OnTick = func(now time.Time, _ float64) {
		if !started || !now.Before(nextPoll) {
			if err := det.Refresh(ctx); err != nil {
				// 段首歷史不足時 bootstrap 失敗屬預期（實盤同樣記錄日誌並在下個輪詢重試）
				st.RefreshErrors++
				st.LastRefreshError = err.Error()
			}
			nextPoll = AdvancePast(nextPoll, now, pollInterval, !started)
		}
		if !started || changed || !now.Before(nextControl) {
			changed = false
			if spm.RefreshRegimeInterval() {
				st.IntervalChanges++
			}
			if !started || !now.Before(nextControl) {
				nextControl = AdvancePast(nextControl, now, RegimeControlCheckInterval, !started)
				st.Samples++
				if base > 0 {
					st.IntervalMultipleSum += spm.GetPriceInterval() / base
				}
				st.RegimeCounts[det.Snapshot().Effective().String()]++
			}
		}
		started = true
	}
	return st, nil
}

// AdvancePast 周期調度：首次從 now 起算；之後跳過已錯過的周期，返回嚴格晚於 now 的下一個觸發點
func AdvancePast(next, now time.Time, every time.Duration, first bool) time.Time {
	if first || next.IsZero() {
		return now.Add(every)
	}
	for !next.After(now) {
		next = next.Add(every)
	}
	return next
}
