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
	accountScope := equityAccountScopeID(exchangeName, exchangeCfg)
	marketType := candidate.GetMarketType()
	candidateID := candidate.ID
	if candidateID == "" {
		candidateID = config.GenerateBotID(exchangeName, candidate.Symbol, marketType)
	}
	seen := make(map[string]struct{})
	total := 0.0
	candidateFound := false
	add := func(id string, allocated float64) error {
		if id == candidateID {
			candidateFound = true
			allocated = candidate.TotalAllocatedCapital
		}
		if _, exists := seen[id]; exists {
			return nil
		}
		seen[id] = struct{}{}
		if allocated == 0 {
			allocated = cfg.Strategies.CapitalAllocation.TotalCapital
		}
		if math.IsNaN(allocated) || math.IsInf(allocated, 0) || allocated <= 0 {
			return fmt.Errorf("Bot %s has no valid capital allocation", id)
		}
		total += allocated
		if math.IsNaN(total) || math.IsInf(total, 0) {
			return fmt.Errorf("configured account capital total is invalid")
		}
		return nil
	}
	if len(cfg.Bots) > 0 {
		for _, bot := range cfg.Bots {
			botExchange := strings.TrimSpace(bot.Exchange)
			if botExchange == "" {
				botExchange = strings.TrimSpace(cfg.App.CurrentExchange)
			}
			botExchangeCfg, exists := cfg.Exchanges[botExchange]
			if !exists || equityAccountScopeID(botExchange, botExchangeCfg) != accountScope || bot.GetMarketType() != marketType {
				continue
			}
			id := config.BotIDOrGenerate(bot)
			if err := add(id, bot.TotalAllocatedCapital); err != nil {
				return 0, err
			}
		}
	} else {
		for _, symbol := range cfg.Trading.Symbols {
			botExchange := strings.TrimSpace(symbol.Exchange)
			if botExchange == "" {
				botExchange = strings.TrimSpace(cfg.App.CurrentExchange)
			}
			botExchangeCfg, exists := cfg.Exchanges[botExchange]
			if !exists || equityAccountScopeID(botExchange, botExchangeCfg) != accountScope || symbol.GetMarketType() != marketType {
				continue
			}
			id := symbol.ID
			if id == "" {
				id = config.GenerateBotID(botExchange, symbol.Symbol, symbol.GetMarketType())
			}
			if err := add(id, symbol.TotalAllocatedCapital); err != nil {
				return 0, err
			}
		}
	}
	if !candidateFound {
		if err := add(candidateID, candidate.TotalAllocatedCapital); err != nil {
			return 0, err
		}
	}
	return total, nil
}
