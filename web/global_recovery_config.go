package web

import (
	"context"
	"errors"
	"reflect"
	"sort"

	"quantmesh/config"
	"quantmesh/strategy"
)

// These failures are admission failures: no persistence has been attempted.
type globalRecoveryAdmissionError struct {
	code   string
	status int
}

func (e *globalRecoveryAdmissionError) Error() string { return e.code }

func globalRecoveryStateError(err error) error {
	if errors.Is(err, strategy.ErrRecoveryConfigRequired) {
		return &globalRecoveryAdmissionError{"bot_recovery_configuration_required", 409}
	}
	return &globalRecoveryAdmissionError{"bot_recovery_configuration_unverified", 503}
}

func globalRecoveryBots(cfg *config.Config) (map[string]config.BotConfig, error) {
	bots := cfg.Bots
	result := make(map[string]config.BotConfig, len(bots))
	for _, bot := range bots {
		id := config.BotIDOrGenerate(bot)
		if id == "" {
			return nil, &globalRecoveryAdmissionError{"bot_configuration_identity_unverified", 503}
		}
		if _, exists := result[id]; exists {
			return nil, &globalRecoveryAdmissionError{"bot_configuration_identity_unverified", 503}
		}
		result[id] = config.ColdBotConfiguration(bot)
	}
	return result, nil
}

func globalRecoverySymbolBots(cfg *config.Config) (map[string]config.BotConfig, error) {
	legacy := &config.Config{}
	for _, symbol := range cfg.Trading.Symbols {
		legacy.Bots = append(legacy.Bots, config.SymbolConfigToBotConfig(symbol, cfg.Exchanges[symbol.Exchange].Testnet))
	}
	return globalRecoveryBots(legacy)
}

func globalRecoveryAffectedBots(before, after *config.Config) ([]string, error) {
	oldBots, err := globalRecoveryBots(before)
	if err != nil {
		return nil, err
	}
	oldSymbols, err := globalRecoverySymbolBots(before)
	if err != nil {
		return nil, err
	}
	newSymbols, err := globalRecoverySymbolBots(after)
	if err != nil {
		return nil, err
	}
	newBots, err := globalRecoveryBots(after)
	if err != nil {
		return nil, err
	}
	// Global defaults and shared strategy/account dependencies can affect a
	// secondary financial leg. Conservatively protect every configured owner.
	oldTrading, newTrading := before.Trading, after.Trading
	oldTrading.Symbols, newTrading.Symbols = nil, nil
	oldTrading.PriceInterval, newTrading.PriceInterval = 0, 0
	oldTrading.ProfitSpread, newTrading.ProfitSpread = 0, 0
	oldTrading.OrderQuantity, newTrading.OrderQuantity = 0, 0
	oldTrading.BuyWindowSize, newTrading.BuyWindowSize = 0, 0
	oldTrading.SellWindowSize, newTrading.SellWindowSize = 0, 0
	sharedChanged := !reflect.DeepEqual(oldSymbols, newSymbols) ||
		!reflect.DeepEqual(before.Exchanges, after.Exchanges) ||
		!reflect.DeepEqual(before.BotGroups, after.BotGroups) ||
		!reflect.DeepEqual(before.Strategies, after.Strategies) ||
		!reflect.DeepEqual(oldTrading, newTrading)
	ids := make(map[string]bool)
	for id, bot := range oldBots {
		other, exists := newBots[id]
		if sharedChanged || !exists || !reflect.DeepEqual(bot, other) {
			ids[id] = true
		}
	}
	for id := range newBots {
		if _, exists := oldBots[id]; !exists || sharedChanged {
			ids[id] = true
		}
	}
	// Keep independent Bot membership separate from legacy mirrors: leaving a
	// symbols entry cannot disguise deletion of the primary recovery owner.
	if sharedChanged {
		for id := range oldSymbols {
			ids[id] = true
		}
		for id := range newSymbols {
			ids[id] = true
		}
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	return ordered, nil
}

func withGlobalRecoveryConfiguration(ctx context.Context, before, after *config.Config, persist func() error) error {
	ids, err := globalRecoveryAffectedBots(before, after)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return persist()
	}
	coordinator, ok := botManagerProvider().(BotStrategyConfigurationCoordinator)
	if !ok {
		return &globalRecoveryAdmissionError{"bot_config_lifecycle_coordination_unavailable", 503}
	}
	var acquire func(int) error
	acquire = func(index int) error {
		if index == len(ids) {
			return persist()
		}
		id := ids[index]
		entered := false
		err := withBotStrategyPersistenceContext(ctx, coordinator, id, func(managed bool) error {
			entered = true
			if managed {
				return &globalRecoveryAdmissionError{"bot_running", 409}
			}
			reserved, err := botHasAccountWalletCapitalReservation(ctx, id)
			if err != nil {
				return &globalRecoveryAdmissionError{"bot_capital_reservation_verification_unavailable", 503}
			}
			if reserved {
				return &globalRecoveryAdmissionError{"bot_capital_reservation_not_released", 409}
			}
			if err := verifyBotRecoveryConfiguration(ctx, id); err != nil {
				return globalRecoveryStateError(err)
			}
			return acquire(index + 1)
		})
		if err != nil && !entered {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return errors.Join(errConfigSaveCancelledBeforePersistence, err)
			}
			return &globalRecoveryAdmissionError{"bot_config_lifecycle_coordination_unavailable", 503}
		}
		return err
	}
	return acquire(0)
}
