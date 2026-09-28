package indicators

import (
	"fmt"
	"math"
	"time"
)

// PricePoint.Timestamp is the UTC close boundary of a completed one-hour bar.
// Volatility is the population standard deviation (percent) of hourly simple
// returns in the configured number of hours, with no annualization. Live ticks
// must not be appended here: neither sample count nor confirmation is tick based.
const MaxVolatilityWindowHours = 8760

func (vrd *VolatilityRegimeDetector) windowHours() int {
	c := vrd.config
	return max(c.ShortPeriod, c.MediumPeriod, c.LongPeriod, c.PriceRangePeriod)
}

func (vrd *VolatilityRegimeDetector) validateHourlyConfig() error {
	c := vrd.config
	for _, period := range []int{c.ShortPeriod, c.MediumPeriod, c.LongPeriod} {
		if period < 2 || period > MaxVolatilityWindowHours {
			return fmt.Errorf("volatility return windows must be 2..%d hours", MaxVolatilityWindowHours)
		}
	}
	if c.PriceRangePeriod < 1 || c.PriceRangePeriod > MaxVolatilityWindowHours || c.ConsecutivePeriods < 1 || c.ConsecutivePeriods > 24 {
		return fmt.Errorf("invalid hourly range/confirmation window")
	}
	for _, threshold := range []float64{c.LowThreshold, c.NormalThreshold, c.HighThreshold, c.ExtremeThreshold, c.PriceRangeThreshold, c.SuddenIncreaseThreshold, c.SuddenDecreaseThreshold} {
		if math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold < 0 {
			return fmt.Errorf("invalid volatility threshold")
		}
	}
	if c.HighThreshold <= c.LowThreshold || c.ExtremeThreshold <= c.HighThreshold {
		return fmt.Errorf("volatility thresholds must increase")
	}
	return nil
}

// RequiredHourlyHistory includes warmup, a prior comparison and confirmations.
func (vrd *VolatilityRegimeDetector) RequiredHourlyHistory() (int, error) {
	if err := vrd.validateHourlyConfig(); err != nil {
		return 0, err
	}
	return vrd.windowHours() + max(vrd.config.ConsecutivePeriods, 2) + 1, nil
}

// ReplaceHourlyHistory validates the entire evidence set before publication.
// A failed reload preserves the last accepted history, but the caller must keep
// its data-health hold until a fresh reload succeeds. Identical reloads are
// idempotent; overlapping closed bars cannot silently rewrite accepted history.
func (vrd *VolatilityRegimeDetector) ReplaceHourlyHistory(points []PricePoint, asOf time.Time) error {
	required, err := vrd.RequiredHourlyHistory()
	if err != nil {
		return err
	}
	if asOf.IsZero() || len(points) < required {
		return fmt.Errorf("insufficient closed hourly history: need %d", required)
	}
	end := asOf.UTC().Truncate(time.Hour)
	for i, p := range points {
		if p.Timestamp.IsZero() || !p.Timestamp.Equal(p.Timestamp.UTC().Truncate(time.Hour)) || p.Timestamp.After(end) {
			return fmt.Errorf("invalid or future hourly close boundary")
		}
		if i > 0 && p.Timestamp.Sub(points[i-1].Timestamp) != time.Hour {
			return fmt.Errorf("hourly history has a duplicate, gap or ordering error")
		}
		for _, v := range []float64{p.Price, p.High, p.Low} {
			if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("invalid hourly price")
			}
		}
		if p.High < p.Price || p.Low > p.Price || p.High < p.Low || p.Volume < 0 || math.IsNaN(p.Volume) || math.IsInf(p.Volume, 0) {
			return fmt.Errorf("invalid hourly OHLC/volume")
		}
	}
	if !points[len(points)-1].Timestamp.Equal(end) {
		return fmt.Errorf("latest completed hour is missing")
	}
	vrd.mu.Lock()
	defer vrd.mu.Unlock()
	oldByTime := make(map[int64]PricePoint, len(vrd.priceHistory))
	for _, p := range vrd.priceHistory {
		oldByTime[p.Timestamp.Unix()] = p
	}
	for _, p := range points {
		if old, ok := oldByTime[p.Timestamp.Unix()]; ok && (old.Price != p.Price || old.High != p.High || old.Low != p.Low || old.Volume != p.Volume) {
			return fmt.Errorf("closed hourly history changed; explicit reconciliation required")
		}
	}
	if len(vrd.priceHistory) > 0 && end.Before(vrd.priceHistory[len(vrd.priceHistory)-1].Timestamp) {
		return fmt.Errorf("hourly history regressed")
	}
	// Bound both replay work and retained evidence; keep enough complete periods
	// to rebuild candidate confirmation without using future prices.
	points = append([]PricePoint(nil), points[len(points)-required:]...)
	previousRegime := vrd.currentRegime
	previousEnd := time.Time{}
	if len(vrd.priceHistory) > 0 {
		previousEnd = vrd.priceHistory[len(vrd.priceHistory)-1].Timestamp
	}
	next := NewVolatilityRegimeDetector(vrd.config)
	if !previousEnd.IsZero() {
		next.currentRegime, next.previousRegime, next.consecutiveCount = vrd.currentRegime, vrd.previousRegime, vrd.consecutiveCount
		next.volatilityHistory = append([]VolatilityPoint(nil), vrd.volatilityHistory...)
	}
	for _, p := range points {
		next.priceHistory = append(next.priceHistory, p)
		if p.Timestamp.After(previousEnd) {
			next.detectRegime(p.Timestamp)
		}
	}
	vrd.priceHistory = next.priceHistory
	vrd.volatilityHistory = next.volatilityHistory
	vrd.currentRegime, vrd.previousRegime, vrd.consecutiveCount = next.currentRegime, next.previousRegime, next.consecutiveCount
	if end.After(previousEnd) && vrd.onRegimeChange != nil && previousRegime != vrd.currentRegime {
		latest := vrd.volatilityHistory[len(vrd.volatilityHistory)-1]
		// Publish one current transition, not historical startup notifications.
		vrd.currentRegime = previousRegime
		latest.Regime = next.currentRegime
		vrd.triggerRegimeChange(latest)
	}
	return nil
}
