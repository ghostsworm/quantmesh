package main

import (
	"fmt"
	"math"
	"strings"

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

// capTwoLegStrategyCapitalLimit caps a 50/50 spread budget to the smaller
// verified quote balance on either exchange. It assumes no leverage credit.
func capTwoLegStrategyCapitalLimit(configured, legABalance, legBBalance float64) (float64, error) {
	if math.IsNaN(configured) || math.IsInf(configured, 0) || configured <= 0 {
		return 0, fmt.Errorf("configured two-leg strategy capital is invalid")
	}
	if math.IsNaN(legABalance) || math.IsInf(legABalance, 0) || legABalance <= 0 ||
		math.IsNaN(legBBalance) || math.IsInf(legBBalance, 0) || legBBalance <= 0 {
		return 0, fmt.Errorf("both spread legs require verified positive available quote balances")
	}
	legLimit := math.Min(legABalance, legBBalance)
	if legLimit > math.MaxFloat64/2 {
		return 0, fmt.Errorf("verified two-leg capital limit overflows")
	}
	maxCapital := 2 * legLimit
	if configured < maxCapital {
		return configured, nil
	}
	return maxCapital, nil
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

// configuredAccountCapitalTotal sums allocations for every configured Bot that
// uses the same exchange credential, environment and market account. A Bot's
// cap is a reservation even while stopped: it may still own live orders or lots.
func configuredAccountCapitalTotal(cfg *config.Config, candidate config.SymbolConfig) (float64, error) {
	if cfg == nil {
		return 0, fmt.Errorf("account capital configuration is unavailable")
	}
	exchangeName := strings.TrimSpace(candidate.Exchange)
	if exchangeName == "" {
		exchangeName = strings.TrimSpace(cfg.App.CurrentExchange)
	}
	exchangeCfg, ok := cfg.Exchanges[exchangeName]
	if !ok || strings.TrimSpace(exchangeCfg.APIKey) == "" {
		return 0, fmt.Errorf("account identity is unavailable")
	}
	return configuredAccountWalletCapitalTotal(cfg, candidate, exchangeName, candidate.GetMarketType())
}
