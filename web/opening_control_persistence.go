package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/config"
)

var errOpeningTarget = errors.New("opening control target missing, mismatched or ambiguous")

func validateOpeningControl(req config.OpenPositionControl) error {
	if math.IsNaN(req.MaxPositionValue) || math.IsInf(req.MaxPositionValue, 0) || req.MaxPositionValue < 0 || req.MaxPositionLayers < 0 {
		return fmt.Errorf("invalid position limit")
	}
	for _, rule := range req.ScheduleRules {
		if rule.Action != "pause" && rule.Action != "resume" {
			return fmt.Errorf("invalid schedule action")
		}
		if len(rule.Time) != 5 {
			return fmt.Errorf("invalid schedule time")
		}
		if _, err := time.Parse("15:04", rule.Time); err != nil {
			return err
		}
		for _, day := range rule.Weekdays {
			if day < 0 || day > 6 {
				return fmt.Errorf("invalid weekday")
			}
		}
	}
	if p := req.PeriodicRule; p != nil && p.Enabled {
		const maxMinutes = int64(math.MaxInt64) / int64(time.Minute)
		if p.OpenDurationMin <= 0 || p.CloseDurationMin <= 0 || int64(p.OpenDurationMin) > maxMinutes || int64(p.CloseDurationMin) > maxMinutes-int64(p.OpenDurationMin) {
			return fmt.Errorf("invalid or overflowing periodic duration")
		}
	}
	return nil
}

// Explicit IDs never fall back to another bot. Without an ID, exactly one
// authoritative target must match; multiple strategies can share a symbol.
func openingControlTarget(cfg *config.Config, id, exchange, symbol, market string) (*config.OpenPositionControl, string, error) {
	var found *config.OpenPositionControl
	var foundID string
	count := 0
	match := func(ex, sym, mt string) bool {
		if mt == "spot_margin" {
			mt = "spot"
		}
		return strings.EqualFold(ex, exchange) && strings.EqualFold(sym, symbol) && mt == market
	}
	for i := range cfg.Bots {
		b := &cfg.Bots[i]
		bid := b.ID
		if bid == "" {
			bid = config.GenerateBotID(b.Exchange, b.Symbol, b.GetMarketType())
		}
		if id != "" && bid != id {
			continue
		}
		if !match(b.Exchange, b.Symbol, b.GetMarketType()) {
			continue
		}
		found, foundID, count = &b.OpenPositionControl, bid, count+1
	}
	if id == "" && count == 0 {
		for i := range cfg.Trading.Symbols {
			s := &cfg.Trading.Symbols[i]
			if match(s.Exchange, s.Symbol, s.GetMarketType()) {
				found, count = &s.OpenPositionControl, count+1
			}
		}
	}
	if count != 1 {
		return nil, "", errOpeningTarget
	}
	return found, foundID, nil
}

// Copy, resolve and persist under the manager lock. Failure never changes the
// currently published snapshot, including nested schedule slices.
func persistOpeningControl(id, exchange, symbol, market string, req config.OpenPositionControl, clamp func(config.OpenPositionControl) (config.OpenPositionControl, error)) (config.OpenPositionControl, string, error) {
	var zero config.OpenPositionControl
	fcm := fileConfigManager
	if fcm == nil {
		return zero, "", fmt.Errorf("configuration persistence unavailable")
	}
	if _, err := fcm.GetConfig(); err != nil {
		return zero, "", err
	}
	fcm.mu.Lock()
	defer fcm.mu.Unlock()
	data, err := json.Marshal(fcm.currentConfig)
	if err != nil {
		return zero, "", err
	}
	var next config.Config
	if err = json.Unmarshal(data, &next); err != nil {
		return zero, "", err
	}
	target, resolvedID, err := openingControlTarget(&next, id, exchange, symbol, market)
	if err != nil {
		return zero, "", err
	}
	target.MaxPositionValue = req.MaxPositionValue
	target.MaxPositionLayers = req.MaxPositionLayers
	target.ScheduleRules = req.ScheduleRules
	target.PeriodicRule = req.PeriodicRule
	if clamp != nil {
		effective, clampErr := clamp(config.CloneOpenPositionControl(*target))
		if clampErr != nil {
			return zero, "", fmt.Errorf("apply verified opening-control capital ceiling: %w", clampErr)
		}
		*target = effective
	}
	*target = config.CloneOpenPositionControl(*target)
	if err = next.Validate(); err != nil {
		return zero, "", err
	}
	if err = persistAppConfigToDB(&next, "web", "opening_control_update", "put_opening_control"); err != nil {
		return zero, "", err
	}
	fcm.currentConfig = &next
	return config.CloneOpenPositionControl(*target), resolvedID, nil
}

// persistOpeningPause writes the manual pause state to the exact configured Bot
// before the caller changes its runtime gate. A restart therefore cannot silently
// turn a user pause into permission to open new exposure.
func persistOpeningPause(id, exchange, symbol, market string, paused bool) error {
	fcm := fileConfigManager
	if fcm == nil {
		return fmt.Errorf("configuration persistence unavailable")
	}
	if _, err := fcm.GetConfig(); err != nil {
		return err
	}
	fcm.mu.Lock()
	defer fcm.mu.Unlock()
	data, err := json.Marshal(fcm.currentConfig)
	if err != nil {
		return err
	}
	var next config.Config
	if err = json.Unmarshal(data, &next); err != nil {
		return err
	}
	target, _, err := openingControlTarget(&next, id, exchange, symbol, market)
	if err != nil {
		return err
	}
	target.PauseOpening = paused
	if target.BotRiskControl != nil {
		target.BotRiskControl.PauseOpening = paused
		if paused {
			target.BotRiskControl.PauseOpeningReason = "manual"
		} else {
			target.BotRiskControl.PauseOpeningReason = ""
		}
	}
	*target = config.CloneOpenPositionControl(*target)
	if err = next.Validate(); err != nil {
		return err
	}
	if err = persistAppConfigToDB(&next, "web", "opening_control_pause", ""); err != nil {
		return err
	}
	fcm.currentConfig = &next
	return nil
}
