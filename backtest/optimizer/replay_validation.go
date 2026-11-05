package optimizer

import (
	"encoding/json"
	"fmt"
	"math"

	"quantmesh/backtest"
	"quantmesh/backtest/replay"
)

const (
	MaxReplayCandidates        = 32
	MaxReplayFolds             = 12
	MaxReplayCandles           = 100000
	MaxReplayCandleEvaluations = 2000000
)

var replayAxes = map[string]bool{
	"grid_spacing": false, "profit_spread": false, "order_quantity": false,
	"grid_count": true, "buy_window_size": true, "sell_window_size": true,
	"max_position_layers": true,
}

var replayFixedKeys = map[string]bool{
	"grid_spacing": true, "profit_spread": true, "order_quantity": true,
	"grid_count": true, "buy_window_size": true, "sell_window_size": true,
	"max_position_layers": true, "direction": true, "market_type": true,
	"price_low": true, "price_high": true, "maker_fee_rate": true, "taker_fee_rate": true,
	"participation_rate": true, "queue_factor": true, "fill_on_touch": true,
	"intrabar_path": true, "intrabar_steps_per_leg": true, "adjust_interval_ms": true,
	"funding_enabled": true, "funding_rate": true, "funding_series": true,
	"price_decimals": true, "quantity_decimals": true, "enforce_margin": true,
	"post_only_reprice_max_attempts": true, "order_cleaner": true,
	"order_cleanup_threshold": true, "cleanup_batch_size": true, "order_cleanup_interval": true,
	"regime_filter": true, "adaptive_interval": true, "upper_bound_freeze": true,
	"inventory_skew": true, "fee_aware_spread": true,
}

// ValidateReplaySearchSpace is shared by HTTP admission, task reload and Run.
// Unsupported axes are rejected: silently ignored legacy controls would make
// an apparent search produce identical replay configurations.
func ValidateReplaySearchSpace(space UniversalSearchSpace) error {
	cfg := space.ReplayWalkForward
	if cfg == nil {
		return nil
	}
	if space.Strategy != "grid" {
		return fmt.Errorf("replay walk-forward supports grid only")
	}
	count, err := CountUniversalParamCombos(space)
	if err != nil {
		return err
	}
	if count > MaxReplayCandidates {
		return fmt.Errorf("replay walk-forward permits at most %d candidates", MaxReplayCandidates)
	}
	if cfg.TrainDays <= 0 || cfg.TestDays <= 0 || cfg.StepDays < 0 {
		return fmt.Errorf("replay walk-forward requires positive train_days/test_days and nonnegative step_days")
	}
	if _, _, _, err := (WalkForwardConfig{TrainDays: cfg.TrainDays, TestDays: cfg.TestDays, StepDays: cfg.StepDays}).windowsMs(); err != nil {
		return err
	}
	if !finiteNumber(cfg.Leverage) || cfg.Leverage < 1 || cfg.Leverage > 125 || math.Trunc(cfg.Leverage) != cfg.Leverage {
		return fmt.Errorf("replay leverage must be an integer within [1,125]")
	}
	for key, r := range space.Ranges {
		integer, supported := replayAxes[key]
		if !supported {
			return fmt.Errorf("replay search parameter %q is not supported", key)
		}
		if r.Min < 0 || (key != "profit_spread" && r.Min == 0) {
			return fmt.Errorf("replay search parameter %s must be positive", key)
		}
		if integer && (math.Trunc(r.Min) != r.Min || math.Trunc(r.Max) != r.Max || math.Trunc(r.Step) != r.Step) {
			return fmt.Errorf("replay search parameter %s requires integers", key)
		}
		if integer && r.Max > 1000 {
			return fmt.Errorf("replay search parameter %s exceeds 1000", key)
		}
	}
	base, err := normalizedReplayParams(cfg.BaseParams)
	if err != nil {
		return err
	}
	// Validate every bounded candidate before scheduling or downloading data.
	for _, overrides := range (&UniversalOptimizer{}).EnumerateParamCombos(space) {
		p, err := cloneReplayParams(base)
		if err != nil {
			return err
		}
		for key, value := range overrides {
			p[key] = value
		}
		if err := validateReplayParams(p, cfg.Leverage); err != nil {
			return err
		}
	}
	return validateReplayParams(base, cfg.Leverage)
}

func cloneReplayParams(p map[string]interface{}) (map[string]interface{}, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("replay params: %w", err)
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("replay params: %w", err)
	}
	return out, nil
}

func normalizedReplayParams(input map[string]interface{}) (map[string]interface{}, error) {
	p, err := cloneReplayParams(input)
	if err != nil {
		return nil, err
	}
	if p == nil {
		p = map[string]interface{}{}
	}
	for key := range p {
		if !replayFixedKeys[key] {
			return nil, fmt.Errorf("replay fixed parameter %q is not supported", key)
		}
	}
	defaults := map[string]interface{}{
		"grid_spacing": 100.0, "order_quantity": 100.0, "profit_spread": 0.0,
		"direction": "LONG", "market_type": "futures", "maker_fee_rate": replay.DefaultMakerFeeRate,
		"taker_fee_rate": replay.DefaultTakerFeeRate, "participation_rate": replay.DefaultParticipationRate,
		"queue_factor": 0.0, "fill_on_touch": false, "intrabar_path": "auto",
		"intrabar_steps_per_leg": replay.DefaultIntrabarStepsPerLeg,
		"adjust_interval_ms":     replay.DefaultAdjustIntervalMs, "funding_enabled": false, "funding_rate": 0.0,
		"price_decimals": 2, "quantity_decimals": 6, "enforce_margin": true,
		"order_cleaner": true, "order_cleanup_interval": 60, "post_only_reprice_max_attempts": 3,
	}
	for key, value := range defaults {
		if _, ok := p[key]; !ok {
			if key == "grid_spacing" && canDeriveReplaySpacing(p) {
				continue
			}
			p[key] = value
		}
	}
	return cloneReplayParams(p)
}

func canDeriveReplaySpacing(p map[string]interface{}) bool {
	low, lowOK := p["price_low"].(float64)
	high, highOK := p["price_high"].(float64)
	count, countOK := p["grid_count"].(float64)
	return lowOK && highOK && countOK && low > 0 && high > low && count > 0
}

func validateReplayParams(p map[string]interface{}, leverage float64) error {
	// Strong root typing prevents permissive replay parsers falling back on a
	// typo (e.g. a string-valued fee). Feature objects remain strictly decoded
	// by ConfigFromTask, including rejection of unknown nested fields.
	boolKeys := map[string]bool{"fill_on_touch": true, "funding_enabled": true, "enforce_margin": true, "order_cleaner": true}
	featureKeys := map[string]bool{"regime_filter": true, "adaptive_interval": true, "upper_bound_freeze": true, "inventory_skew": true, "fee_aware_spread": true}
	for key, value := range p {
		if featureKeys[key] {
			continue
		}
		if boolKeys[key] {
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("params.%s must be boolean", key)
			}
			continue
		}
		switch key {
		case "direction":
			if value != "LONG" && value != "SHORT" {
				return fmt.Errorf("replay direction must be LONG or SHORT")
			}
			continue
		case "market_type":
			if value != "futures" {
				return fmt.Errorf("replay walk-forward supports futures only")
			}
			continue
		case "intrabar_path":
			s, ok := value.(string)
			if !ok {
				return fmt.Errorf("intrabar_path must be a string")
			}
			if _, err := replay.ParseIntrabarPath(s); err != nil {
				return err
			}
			continue
		case "funding_series":
			continue
		}
		v, ok := value.(float64)
		if !ok || !finiteNumber(v) {
			return fmt.Errorf("params.%s must be a finite number", key)
		}
		if key == "funding_rate" {
			if math.Abs(v) > 1 {
				return fmt.Errorf("funding_rate must be within [-1,1]")
			}
			continue
		}
		if v < 0 {
			return fmt.Errorf("params.%s must be nonnegative", key)
		}
		if key == "maker_fee_rate" || key == "taker_fee_rate" || key == "participation_rate" {
			if v > 1 {
				return fmt.Errorf("params.%s must be <= 1", key)
			}
		}
		if key == "grid_spacing" || key == "order_quantity" || key == "participation_rate" || key == "adjust_interval_ms" {
			if v <= 0 && !(key == "grid_spacing" && canDeriveReplaySpacing(p)) {
				return fmt.Errorf("params.%s must be positive", key)
			}
		}
		if key == "intrabar_steps_per_leg" && (v < 1 || v > 10 || math.Trunc(v) != v) {
			return fmt.Errorf("intrabar_steps_per_leg must be an integer within [1,10]")
		}
		if key == "price_decimals" || key == "quantity_decimals" {
			if v > 8 || math.Trunc(v) != v {
				return fmt.Errorf("params.%s must be an integer within [0,8]", key)
			}
		}
		if integer, ok := replayAxes[key]; ok && integer && (v < 1 || v > 1000 || math.Trunc(v) != v) {
			return fmt.Errorf("params.%s must be an integer within [1,1000]", key)
		}
	}
	if _, exists := p["funding_series"]; exists && p["funding_enabled"] != true {
		return fmt.Errorf("funding_series requires funding_enabled=true")
	}
	_, err := replay.ConfigFromTask(&backtest.BacktestTask{Symbol: "BTCUSDT", TotalCapital: 10000, Leverage: leverage, Params: p})
	return err
}
