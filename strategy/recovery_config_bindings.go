package strategy

import (
	"fmt"
	"strings"

	"quantmesh/config"
	"quantmesh/storage"
)

// ResolveRecoveryConfigBindings uses configuration and the fixed producer key
// contracts, never payload fields. Removed custom Combo definitions require
// restoration of their independent historical definition, not JSON guessing.
func ResolveRecoveryConfigBindings(cfg *config.Config, bot config.BotConfig, states []*storage.StrategyRuntimeState) ([]RecoveryConfigStateBinding, error) {
	if cfg == nil || !recoveryBindingComplete(bot.ID, bot.Symbol, bot.Exchange) {
		return nil, fmt.Errorf("%w: independent Bot configuration unavailable", ErrRecoveryConfigUnverified)
	}
	definitions := make(map[string]config.StrategyConfig)
	for name, definition := range cfg.Strategies.Configs {
		definitions[name] = definition
	}
	// Dedicated funding constructors bypass the general strategy expansion.
	// For ordinary Bots, use the very same normalization and duplicate checks
	// as startSymbolRuntime, on a local copy only.
	if bot.GetMarketType() != config.MarketTypeFundingCarry && bot.GetMarketType() != config.MarketTypeFundingPerpSpread {
		ordinary := make([]config.StrategyInstance, 0, len(bot.Strategies))
		hedges := make(map[string]bool)
		for _, instance := range bot.Strategies {
			switch instance.Type {
			case "spot_long", "spot_short", "futures_long", "futures_short":
				// These constructors read the exact Bot instance directly, not
				// the general strategy expansion's normalized map.
				if hedges[instance.Type] {
					return nil, fmt.Errorf("%w: duplicate independent hedge definition", ErrRecoveryConfigUnverified)
				}
				hedges[instance.Type] = true
				definitions[instance.Type] = config.StrategyConfig{Type: instance.Type, Config: instance.Config}
			default:
				ordinary = append(ordinary, instance)
			}
		}
		local := *cfg
		if err := config.ApplyBotStrategiesToLocalConfig(&local, &config.SymbolConfig{Strategies: ordinary}); err != nil {
			return nil, fmt.Errorf("%w: independent strategy configuration invalid", ErrRecoveryConfigUnverified)
		}
		if len(ordinary) > 0 {
			for name, definition := range local.Strategies.Configs {
				definitions[name] = definition
			}
		}
	}
	bindings := make(map[string]RecoveryConfigStateBinding)
	for _, name := range []string{"dca", "dca_enhanced", "martingale", "trend", "mean_reversion", "momentum", "spot_long", "spot_short", "futures_long", "futures_short", "combo", "funding_carry", "funding_perp_spread"} {
		binding := RecoveryConfigStateBinding{StateKey: name, StrategyType: name, SingleLeg: SingleLegRecoveryBinding{BotID: bot.ID, StrategyName: name, Symbol: bot.Symbol}}
		raw := definitions[name].Config
		if name == "martingale" {
			binding.SingleLeg.Direction = normalizeMartingaleDirection(name, parseMartingaleConfig(raw).Direction)
		}
		if name == "trend" || name == "mean_reversion" || name == "momentum" {
			if symbol, ok := raw["symbol"].(string); ok && symbol != "" {
				binding.SingleLeg.Symbol = symbol
			}
		}
		bindings[name] = binding
	}
	if err := resolveRecoveryComboBindings(bot, definitions["combo"].Config, bindings); err != nil {
		return nil, err
	}
	result := make([]RecoveryConfigStateBinding, 0, len(states))
	for _, state := range states {
		if state == nil {
			return nil, fmt.Errorf("%w: nil historical record", ErrRecoveryConfigUnverified)
		}
		binding, found := bindings[state.StrategyName]
		if !found {
			return nil, fmt.Errorf("%w: historical strategy definition unavailable", ErrRecoveryConfigUnverified)
		}
		if err := resolveRecoverySpecialBinding(cfg, bot, definitions[binding.StrategyType].Config, &binding); err != nil {
			return nil, err
		}
		result = append(result, binding)
	}
	return result, nil
}

func resolveRecoveryComboBindings(bot config.BotConfig, raw map[string]interface{}, bindings map[string]RecoveryConfigStateBinding) error {
	combo := parseComboConfig(raw)
	// The actual constructor's explicit symbol overrides raw Combo parameters.
	combo.Symbol = bot.Symbol
	parent := bindings["combo"]
	parent.SingleLeg.Symbol = combo.Symbol
	bindings["combo"] = parent
	for _, child := range combo.Strategies {
		key, err := comboChildRuntimeStateKey("combo", child.Name)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrRecoveryConfigUnverified, err)
		}
		if _, duplicate := bindings[key]; duplicate {
			return fmt.Errorf("%w: duplicate Combo definition", ErrRecoveryConfigUnverified)
		}
		binding := RecoveryConfigStateBinding{StateKey: key, StrategyType: child.Type, ComboParent: "combo", SingleLeg: SingleLegRecoveryBinding{BotID: bot.ID, StrategyName: child.Name, Symbol: combo.Symbol}}
		if child.Type == "martingale" {
			binding.SingleLeg.Direction = normalizeMartingaleDirection(child.Name, child.Direction)
		}
		bindings[key] = binding
	}
	return nil
}

func resolveRecoverySpecialBinding(cfg *config.Config, bot config.BotConfig, raw map[string]interface{}, binding *RecoveryConfigStateBinding) error {
	switch binding.StrategyType {
	case "funding_carry":
		account, found := cfg.Exchanges[bot.Exchange]
		if !found || strings.TrimSpace(account.APIKey) == "" {
			return fmt.Errorf("%w: independent account identity unavailable", ErrRecoveryConfigUnverified)
		}
		asset, err := recoveryConfiguredBaseAsset(bot.Exchange, bot.Symbol)
		if err != nil {
			return err
		}
		binding.Carry = FundingCarryRecoveryBinding{FuturesExchange: bot.Exchange, SpotExchange: bot.Exchange, Symbol: bot.Symbol, BaseAsset: asset, MarginAccountScope: config.AccountScopeID(bot.Exchange, account)}
	case "funding_perp_spread":
		if err := config.ValidateFundingPerpSpread(bot.FundingPerpSpread); err != nil {
			return fmt.Errorf("%w: independent spread legs unavailable", ErrRecoveryConfigUnverified)
		}
		legs := bot.FundingPerpSpread
		binding.Spread = FundingPerpSpreadRecoveryBinding{legs.LegA.Exchange, legs.LegA.Symbol, legs.LegB.Exchange, legs.LegB.Symbol}
	case "spot_long", "spot_short", "futures_long", "futures_short":
		symbol := bot.Symbol
		if override, ok := raw["symbol"].(string); ok && override != "" {
			symbol = override
		}
		group, _ := raw["group_id"].(string)
		binding.Hedge = HedgeRecoveryBinding{BotID: bot.ID, StrategyName: binding.StrategyType, GroupID: group, Symbol: symbol}
		if binding.StrategyType == "spot_long" || binding.StrategyType == "spot_short" {
			// The exchange adapter is constructed for the Bot symbol even
			// when the strategy has a symbol override.
			asset, err := recoveryConfiguredBaseAsset(bot.Exchange, bot.Symbol)
			if err != nil {
				return err
			}
			binding.Hedge.BaseAsset = asset
		}
	}
	return nil
}

// Only the explicit Binance concatenated stable-quote contract is supported
// offline here. Other asset formats need independent metadata, not a guess.
func recoveryConfiguredBaseAsset(venue, symbol string) (string, error) {
	if venue == "binance" {
		for _, quote := range []string{"USDT", "USDC", "FDUSD", "BUSD"} {
			if strings.HasSuffix(symbol, quote) && len(symbol) > len(quote) {
				base := strings.TrimSuffix(symbol, quote)
				if !strings.ContainsAny(base, " /-_: ") {
					return base, nil
				}
			}
		}
	}
	return "", fmt.Errorf("%w: independent asset binding unavailable for configured symbol", ErrRecoveryConfigUnverified)
}
