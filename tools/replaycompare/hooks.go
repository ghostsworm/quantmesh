package main

import (
	"context"
	"fmt"
	"time"

	"quantmesh/backtest/replay"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/position"
	"quantmesh/strategy/regime"
)

// 在模擬時間上重現實盤後台循環（symbol_manager_regime.go / RunRegimeControlLoop / FundingRateMonitor）：
//   - regime.Detector.Refresh：首個 tick 一次，之後每 Detector.PollInterval（實盤 Detector.run 的牆鐘 ticker）；
//   - spm.RefreshRegimeInterval：首個 tick 一次，之後每 regimeControlCheckInterval，或檢測器狀態變化後的下一個 tick
//     （實盤 RunRegimeControlLoop 的 ticker 與 NotifyRegimeChanged 觸發）；
//   - 資金費監控器：replay.SimFundingMonitor（上一期已結算費率，無未來函數）。
// 全部在引擎主循環內同步調用，結果確定、可重複；不啟動任何 goroutine。

// regimeControlCheckInterval 與 position.regimeControlCheckInterval（30s）保持一致
const regimeControlCheckInterval = 30 * time.Second

// hookStats 注入功能的運行統計（寫入 RunSummary）
type hookStats struct {
	regimeEnabled   bool
	refreshErrors   int
	lastRefreshErr  string
	intervalChanges int
	// 以下按 regime 控制檢查點（每 30s 模擬時間）採樣
	samples        int
	intervalSum    float64 // 當前間隔 / base 之和
	regimeSamples  map[string]int
	fundingEnabled bool
}

// needsRegime 與 symbol_manager_regime.gridRegimeNeeded 一致
func needsRegime(bot *config.Config) bool {
	t := bot.Trading
	return t.RegimeFilter.Enabled || t.AdaptiveInterval.Enabled || t.UpperBoundFreeze.Enabled
}

// regimeOptions 與 symbol_manager_regime.newGridRegimeRuntime 一致
func regimeOptions(bot *config.Config) (position.RegimeControlOptions, error) {
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

// installHooks 為需要 K 線 regime 或資金費監控的變體設置 cfg.Setup / cfg.OnTick。
// hourly 為包含段前歷史的 1h K 線（ClosedKlineSource 只返回模擬時間前已收盤的部分）。
func installHooks(cfg *replay.Config, symbol string, hourly []*exchange.Candle, funding []replay.FundingPoint) *hookStats {
	bot := cfg.Bot
	st := &hookStats{regimeEnabled: needsRegime(bot), fundingEnabled: bot.FundingRate.Enabled, regimeSamples: map[string]int{}}
	if !st.regimeEnabled && !st.fundingEnabled {
		return st
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
		if st.fundingEnabled {
			// 序列之前（無已結算費率）按 0 處理：不偏移
			spm.SetFundingMonitor(replay.NewSimFundingMonitor(bot.FundingRate, funding, 0, clock))
		}
		if !st.regimeEnabled {
			return nil
		}
		opts, err := regimeOptions(bot)
		if err != nil {
			return err
		}
		src, err := NewClosedKlineSource(symbol, position.RegimeConfigFromConfig(bot.Trading.RegimeFilter).WithDefaults().KlineInterval, hourly, clock.Now)
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
		spm.ConfigureRegimeControl(det, opts)
		return nil
	}
	if !st.regimeEnabled {
		return st
	}
	cfg.OnTick = func(now time.Time, _ float64) {
		if !started || !now.Before(nextPoll) {
			if err := det.Refresh(ctx); err != nil {
				// 段首歷史不足時 bootstrap 失敗屬預期（實盤同樣記錄日誌並在下個輪詢重試）
				st.refreshErrors++
				st.lastRefreshErr = err.Error()
			}
			nextPoll = advancePast(nextPoll, now, pollInterval, !started)
		}
		if !started || changed || !now.Before(nextControl) {
			changed = false
			if spm.RefreshRegimeInterval() {
				st.intervalChanges++
			}
			if !started || !now.Before(nextControl) {
				nextControl = advancePast(nextControl, now, regimeControlCheckInterval, !started)
				st.samples++
				if base > 0 {
					st.intervalSum += spm.GetPriceInterval() / base
				}
				st.regimeSamples[det.Snapshot().Effective().String()]++
			}
		}
		started = true
	}
	return st
}

// advancePast 周期調度：首次從 now 起算；之後跳過已錯過的周期，返回嚴格晚於 now 的下一個觸發點
func advancePast(next, now time.Time, every time.Duration, first bool) time.Time {
	if first || next.IsZero() {
		return now.Add(every)
	}
	for !next.After(now) {
		next = next.Add(every)
	}
	return next
}
