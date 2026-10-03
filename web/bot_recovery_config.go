package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/storage"
	"quantmesh/strategy"
)

// Caller holds the Bot lifecycle lock. Do not acquire it recursively here.
func verifyBotRecoveryConfiguration(parent context.Context, botID string) error {
	if parent == nil {
		return fmt.Errorf("%w: request context unavailable", strategy.ErrRecoveryConfigUnverified)
	}
	var store storage.Storage
	if primaryStorageForAppConfig != nil {
		store = primaryStorageForAppConfig
	} else if storageServiceProvider != nil {
		store = storageServiceProvider.GetStorage()
	}
	reader, ok := store.(storage.BotStrategyRuntimeStateContextLister)
	if !ok {
		return fmt.Errorf("%w: complete Bot state storage unavailable", strategy.ErrRecoveryConfigUnverified)
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	states, err := reader.ListBotStrategyRuntimeStatesContext(ctx, botID)
	if err != nil {
		return fmt.Errorf("%w: %w", strategy.ErrRecoveryConfigUnverified, err)
	}
	if states == nil {
		return fmt.Errorf("%w: complete Bot state read unavailable", strategy.ErrRecoveryConfigUnverified)
	}
	if len(states) == 0 {
		return ctx.Err()
	}
	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		return fmt.Errorf("%w: configuration read unavailable", strategy.ErrRecoveryConfigUnverified)
	}
	bot := botCfgByID(cfg, botID)
	if bot == nil {
		return fmt.Errorf("%w: independent Bot configuration unavailable", strategy.ErrRecoveryConfigUnverified)
	}
	matches := 0
	for _, candidate := range cfg.Bots {
		id := candidate.ID
		if id == "" {
			id = config.GenerateBotID(candidate.Exchange, candidate.Symbol, candidate.GetMarketType())
		}
		if id == botID {
			matches++
		}
	}
	if matches != 1 {
		return fmt.Errorf("%w: ambiguous independent Bot configuration", strategy.ErrRecoveryConfigUnverified)
	}
	owner := *bot
	owner.ID = botID
	bindings, err := strategy.ResolveRecoveryConfigBindings(cfg, owner, states)
	if err != nil {
		return err
	}
	return strategy.VerifyBotRecoveryConfigStates(ctx, botID, states, bindings)
}

func respondRecoveryConfigurationError(c *gin.Context, err error) {
	if errors.Is(err, strategy.ErrRecoveryConfigRequired) {
		c.JSON(http.StatusConflict, gin.H{"error": "bot_recovery_configuration_required"})
		return
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": "bot_recovery_configuration_unverified"})
}
