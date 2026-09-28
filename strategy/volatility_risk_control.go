package strategy

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/config"
	"quantmesh/indicators"
	"quantmesh/monitor"
)

const volatilityEvidenceMaxAge = 2 * time.Minute
const maxTrendHistoryMinutes = 1440

type trendPrice struct {
	at    time.Time
	price float64
}

func (da *DynamicAdjuster) now() time.Time {
	if da.manager != nil {
		return da.manager.Clock().Now()
	}
	return time.Now()
}

// OnPriceChange is invoked before strategy price delivery in the runtime. Risk
// admission is updated synchronously; it does not depend on an alert channel.
func (da *DynamicAdjuster) OnPriceChange(change monitor.PriceChange) {
	da.priceMu.Lock()
	defer da.priceMu.Unlock()
	da.mu.Lock()
	defer da.mu.Unlock()
	if da.stopped {
		return
	}
	now := da.now()
	at := change.Timestamp
	if at.IsZero() {
		at = now
	}
	if !validRiskPrice(change.NewPrice) || at.After(now) || now.Sub(at) > volatilityEvidenceMaxAge {
		da.quoteValid = false
		da.observation = nil
		da.refreshRiskControlsLocked()
		return
	}
	if !da.priceEvidenceAt.IsZero() && at.Before(da.priceEvidenceAt) {
		return
	}
	da.priceEvidenceAt = at
	da.priceHistory = append(da.priceHistory, change.NewPrice)
	window := da.cfg.Trading.DynamicAdjustment.PriceInterval.VolatilityWindow
	if window <= 0 {
		window = 50
	}
	if len(da.priceHistory) > 2*window {
		da.priceHistory = append([]float64(nil), da.priceHistory[len(da.priceHistory)-window:]...)
	}
	da.recordTrendLocked(at, change.NewPrice)
	high, low := change.HighPrice, change.LowPrice
	if high == 0 {
		high = change.NewPrice
	}
	if low == 0 {
		low = change.NewPrice
	}
	if !validRiskPrice(high) || !validRiskPrice(low) || high < low {
		da.quoteValid = false
		da.observation = nil
		da.refreshRiskControlsLocked()
		return
	}
	da.quoteValid = true
	// Hourly risk is derived exclusively from completed one-hour bars. Ticks
	// update mark/trend freshness, never the number of risk samples.
	da.updateHourlyObservationLocked()
	da.refreshRiskControlsLocked()
}

func validRiskPrice(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }

func (da *DynamicAdjuster) recordTrendLocked(at time.Time, price float64) {
	minute := at.Truncate(time.Minute)
	n := len(da.trendHistory)
	if n > 0 && da.trendHistory[n-1].at.Equal(minute) {
		da.trendHistory[n-1].price = price
	} else {
		da.trendHistory = append(da.trendHistory, trendPrice{minute, price})
	}
	if len(da.trendHistory) > maxTrendHistoryMinutes+1 {
		da.trendHistory = append([]trendPrice(nil), da.trendHistory[len(da.trendHistory)-maxTrendHistoryMinutes-1:]...)
	}
}

func (da *DynamicAdjuster) trendLocked(c config.VolatilityPauseConfig) string {
	period := c.TrendCheckPeriod
	if period <= 0 {
		period = 15
	}
	cutoff := da.priceEvidenceAt.Truncate(time.Minute).Add(-time.Duration(period) * time.Minute)
	var first float64
	for _, p := range da.trendHistory {
		if !p.at.After(cutoff) && cutoff.Sub(p.at) <= time.Minute {
			first = p.price
		}
	}
	if first == 0 || len(da.trendHistory) == 0 {
		return "unknown"
	}
	change := (da.trendHistory[len(da.trendHistory)-1].price/first - 1) * 100
	down, up := c.TrendDownThreshold, c.TrendUpThreshold
	if down == 0 {
		down = 2
	}
	if up == 0 {
		up = 2
	}
	if change <= -math.Abs(down) {
		return "down"
	}
	if change >= math.Abs(up) {
		return "up"
	}
	return "sideways"
}

// RefreshRiskControls re-evaluates a hot update even if the regime did not change.
func (da *DynamicAdjuster) RefreshRiskControls() {
	da.mu.Lock()
	defer da.mu.Unlock()
	if !da.stopped {
		da.refreshRiskControlsLocked()
		select {
		case da.historyWake <- struct{}{}:
		default:
		}
	}
}

// A manual request may release a historical latch only after current evidence
// passes the same policy. It is not an override for missing data or live risk.
func (da *DynamicAdjuster) ResumeVolatilityManually() error {
	da.mu.Lock()
	defer da.mu.Unlock()
	if da.stopped {
		return fmt.Errorf("volatility controller is stopped")
	}
	da.volatilityTriggered = false
	da.refreshRiskControlsLocked()
	if da.manager != nil && da.manager.IsVolatilityRiskPaused() {
		return fmt.Errorf("volatility risk or missing evidence still prevents opening")
	}
	return nil
}

func (da *DynamicAdjuster) refreshRiskControlsLocked() {
	if da.manager == nil {
		return
	}
	b := da.manager.GetRiskControls().Open.BotRiskControl
	if b == nil || !b.Enabled || !b.VolatilityPauseEnabled {
		da.volatilityTriggered = false
		da.manager.SetVolatilityRiskPause("")
		return
	}
	if !da.quoteValid || da.historyFailed || !da.historyThrough.Equal(da.now().UTC().Truncate(time.Hour)) || da.observation == nil || da.now().Sub(da.priceEvidenceAt) > volatilityEvidenceMaxAge {
		da.pauseVolatilityLocked("波动率暂停：行情证据不足或过期")
		return
	}
	da.checkVolatilityPauseLocked(*da.observation, b.VolatilityPauseConfig)
}

// Test/standalone event seam. Production uses synchronous observed prices above.
func (da *DynamicAdjuster) checkVolatilityPause(observation indicators.VolatilityRegimeEvent) {
	da.mu.Lock()
	defer da.mu.Unlock()
	if da.stopped || da.manager == nil {
		return
	}
	b := da.manager.GetRiskControls().Open.BotRiskControl
	if b == nil || !b.Enabled || !b.VolatilityPauseEnabled {
		da.volatilityTriggered = false
		da.manager.SetVolatilityRiskPause("")
		return
	}
	da.checkVolatilityPauseLocked(observation, b.VolatilityPauseConfig)
}

func (da *DynamicAdjuster) checkVolatilityPauseLocked(e indicators.VolatilityRegimeEvent, c config.VolatilityPauseConfig) {
	for _, threshold := range []float64{c.ResumeThreshold, c.TrendDownThreshold, c.TrendUpThreshold} {
		if math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold < 0 {
			da.pauseVolatilityLocked("波动率暂停：无效风控配置")
			return
		}
	}
	if c.TrendCheckPeriod < 0 || c.TrendCheckPeriod > maxTrendHistoryMinutes {
		da.pauseVolatilityLocked("波动率暂停：无效趋势周期")
		return
	}
	if math.IsNaN(e.ShortVolatility) || math.IsInf(e.ShortVolatility, 0) || e.ShortVolatility < 0 || e.NewRegime < indicators.RegimeLow || e.NewRegime > indicators.RegimeExtreme {
		da.pauseVolatilityLocked("波动率暂停：无效行情证据")
		return
	}
	da.currentTrend = da.trendLocked(c)
	direction := config.NormalizeDirection(da.cfg.Trading.Direction)
	high := e.NewRegime >= indicators.RegimeHigh
	triggered := (c.PauseOnHighVolatility && high) || (c.PauseOnExtremeVolatility && e.NewRegime == indicators.RegimeExtreme) || (c.PauseOnSuddenIncrease && e.Severity == "critical")
	long, short := direction != "SHORT", direction == "SHORT" || direction == "BOTH"
	trendNeeded := (long && c.PauseOnDowntrend) || (short && c.PauseOnUptrend)
	triggered = triggered || (high && trendNeeded && da.currentTrend == "unknown") ||
		(high && long && c.PauseOnDowntrend && da.currentTrend == "down") ||
		(high && short && c.PauseOnUptrend && da.currentTrend == "up")
	if triggered {
		da.volatilityTriggered = true
		da.pauseVolatilityLocked(volatilityPauseReasonPrefix + ": " + e.NewRegime.String())
		return
	}
	if !da.volatilityTriggered || (c.AutoResumeOnNormal && e.NewRegime <= indicators.RegimeNormal && (c.ResumeThreshold <= 0 || e.ShortVolatility < c.ResumeThreshold)) {
		da.volatilityTriggered = false
		da.manager.SetVolatilityRiskPause("")
	}
}

func (da *DynamicAdjuster) pauseVolatilityLocked(reason string) {
	if !da.manager.SetVolatilityRiskPause(reason) {
		return
	}
	// Admission is already closed. Drain/cancel only this executor's openings;
	// failure retains the executor's independent unverified cancellation hold.
	da.runWorker(func() {
		ctx, cancel := context.WithTimeout(da.ctx, 10*time.Second)
		defer cancel()
		da.manager.CancelVolatilityOpeningOrders(ctx)
	})
}

func (da *DynamicAdjuster) riskRefreshLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-da.ctx.Done():
			return
		case <-ticker.C:
			da.RefreshRiskControls()
		}
	}
}

func isVolatilityPauseReason(reason string) bool {
	return strings.HasPrefix(reason, volatilityPauseReasonPrefix)
}
