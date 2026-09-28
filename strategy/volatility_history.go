package strategy

import (
	"context"
	"fmt"
	"time"

	"quantmesh/indicators"
)

type VolatilityHistoryLoader func(context.Context, int, time.Time) ([]indicators.PricePoint, error)

const volatilityHistoryRetry = 30 * time.Second
const volatilityHistoryTimeout = 10 * time.Second

// Bind before Start. The owning runtime supplies the same venue/symbol used by
// its orders; the strategy never searches another account or a fallback symbol.
func (da *DynamicAdjuster) SetVolatilityHistoryLoader(loader VolatilityHistoryLoader) error {
	da.mu.Lock()
	defer da.mu.Unlock()
	if da.started || da.stopped {
		return fmt.Errorf("history loader must be bound before start")
	}
	da.historyLoader = loader
	return nil
}

func (da *DynamicAdjuster) historyNeededLocked() bool {
	if da.cfg.Trading.DynamicAdjustment.Enabled && da.cfg.Trading.DynamicAdjustment.VolatilityDetection.Enabled {
		return true
	}
	if da.manager == nil {
		return false
	}
	b := da.manager.GetRiskControls().Open.BotRiskControl
	return b != nil && b.Enabled && b.VolatilityPauseEnabled
}

func (da *DynamicAdjuster) volatilityHistoryLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var lastAttempt time.Time
	for {
		da.mu.Lock()
		now := da.now()
		needed := !da.stopped && da.historyLoader != nil && da.historyNeededLocked() &&
			(da.historyFailed || !da.historyThrough.Equal(now.UTC().Truncate(time.Hour))) &&
			(lastAttempt.IsZero() || now.Sub(lastAttempt) >= volatilityHistoryRetry)
		da.mu.Unlock()
		if needed {
			lastAttempt = now
			_ = da.reloadVolatilityHistory(da.ctx)
		}
		select {
		case <-da.ctx.Done():
			return
		case <-ticker.C:
		case <-da.historyWake:
		}
	}
}

// Called by the single lifecycle-owned loader worker. No network under mu.
func (da *DynamicAdjuster) reloadVolatilityHistory(ctx context.Context) error {
	da.mu.Lock()
	if da.stopped || da.historyLoader == nil {
		da.mu.Unlock()
		return fmt.Errorf("volatility history loader unavailable")
	}
	loader, asOf := da.historyLoader, da.now()
	count, err := da.volatilityAlert.RequiredHourlyHistory()
	da.mu.Unlock()
	var points []indicators.PricePoint
	if err == nil {
		qctx, cancel := context.WithTimeout(ctx, volatilityHistoryTimeout)
		points, err = loader(qctx, count, asOf)
		if qctx.Err() != nil {
			err = qctx.Err()
		}
		cancel()
	}
	da.mu.Lock()
	defer da.mu.Unlock()
	if da.stopped {
		return fmt.Errorf("volatility controller stopped during history load")
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil {
		// Validate again at application time: a fetch crossing the hour boundary
		// must not publish the previous hour as current evidence.
		err = da.volatilityAlert.ReplaceHourlyHistory(points, da.now())
	}
	da.historyFailed = err != nil
	if err == nil {
		da.historyThrough = points[len(points)-1].Timestamp
		da.updateHourlyObservationLocked()
	} else {
		da.observation = nil
	}
	da.refreshRiskControlsLocked()
	return err
}

// ReplaceVolatilityHistory also supports deterministic replay with explicit
// closed-hour evidence. It does not supply or fabricate fresh live quotes.
func (da *DynamicAdjuster) ReplaceVolatilityHistory(points []indicators.PricePoint) error {
	da.mu.Lock()
	defer da.mu.Unlock()
	if da.stopped {
		return fmt.Errorf("volatility controller stopped")
	}
	err := da.volatilityAlert.ReplaceHourlyHistory(points, da.now())
	da.historyFailed = err != nil
	if err == nil {
		da.historyThrough = points[len(points)-1].Timestamp
		da.updateHourlyObservationLocked()
	} else {
		da.observation = nil
	}
	da.refreshRiskControlsLocked()
	return err
}

func (da *DynamicAdjuster) updateHourlyObservationLocked() {
	history := da.volatilityAlert.GetVolatilityHistory(2)
	if len(history) < 2 || da.historyFailed {
		da.observation = nil
		return
	}
	previous, latest := history[0], history[1]
	severity := "info"
	if latest.ShortVolatility > previous.ShortVolatility && (previous.ShortVolatility == 0 || latest.ShortVolatility >= 2*previous.ShortVolatility) {
		severity = "critical"
	}
	da.observation = &indicators.VolatilityRegimeEvent{Timestamp: latest.Timestamp, NewRegime: latest.Regime, ShortVolatility: latest.ShortVolatility, Severity: severity}
}
