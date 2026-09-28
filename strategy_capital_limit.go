package main

import (
	"fmt"
	"math"

	"quantmesh/config"
)

// capStrategyCapitalLimit constrains a Bot's gross order budget to the
// exchange-confirmed available quote balance.
func capStrategyCapitalLimit(configured, available float64) (float64, error) {
	if math.IsNaN(configured) || math.IsInf(configured, 0) || configured < 0 {
		return 0, fmt.Errorf("configured strategy capital is invalid")
	}
	if math.IsNaN(available) || math.IsInf(available, 0) || available <= 0 {
		return 0, fmt.Errorf("exchange available quote balance is invalid")
	}
	if configured == 0 || configured > available {
		return available, nil
	}
	return configured, nil
}

func applyBotCapitalLimit(control *config.OpenPositionControl, budget float64) error {
	if control == nil || math.IsNaN(budget) || math.IsInf(budget, 0) || budget <= 0 {
		return fmt.Errorf("positive verified Bot capital budget required")
	}
	limit := func(value float64) (float64, error) {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return 0, fmt.Errorf("configured position notional limit is invalid")
		}
		if value == 0 || value > budget {
			return budget, nil
		}
		return value, nil
	}
	maxPositionValue, err := limit(control.MaxPositionValue)
	if err != nil {
		return err
	}
	control.MaxPositionValue = maxPositionValue
	if botRisk := control.BotRiskControl; botRisk != nil && botRisk.Enabled {
		botMaxPositionValue, err := limit(botRisk.MaxPositionValue)
		if err != nil {
			return err
		}
		botRisk.MaxPositionValue = botMaxPositionValue
	}
	return nil
}
