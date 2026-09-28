package main

import (
	"fmt"
	"math"
)

// capStrategyCapitalLimit constrains each strategy runtime's internal order
// budget to the exchange-confirmed available quote balance.
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
