package main

import (
	"context"
	"fmt"
	"sync"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/order"
	"quantmesh/position"
	"quantmesh/storage"
	"quantmesh/strategy"
)

type runtimeExposureBootstrapCoordinator struct {
	mu       sync.Mutex
	ready    bool
	complete bool
}

func (c *runtimeExposureBootstrapCoordinator) MarkReady() {
	c.mu.Lock()
	c.ready = true
	c.mu.Unlock()
}

func (c *runtimeExposureBootstrapCoordinator) MarkComplete() {
	c.mu.Lock()
	c.complete = true
	c.mu.Unlock()
}

func (c *runtimeExposureBootstrapCoordinator) Retry(attempt func() error) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.ready || c.complete {
		return false, nil
	}
	if err := attempt(); err != nil {
		return false, err
	}
	c.complete = true
	return true, nil
}

func loadRuntimeStrategyExposureInventory(cfg config.Config, ex position.IExchange, storageService *storage.StorageService, botID, symbol string) ([]execution.ExposurePosition, bool, error) {
	stateStore := &strategyRuntimeStateAdapter{storageService: storageService, botID: botID}
	var inventory []execution.ExposurePosition
	var restored bool
	var signalNames []string
	for _, name := range []string{"trend", "mean_reversion", "momentum"} {
		if strategyCfg, exists := cfg.Strategies.Configs[name]; exists && strategyCfg.Enabled {
			signalNames = append(signalNames, name)
		}
	}
	if len(signalNames) > 0 {
		positions, wasRestored, err := strategy.LoadSignalRuntimeExposureInventory(stateStore, &cfg, ex, symbol, signalNames...)
		if err != nil {
			return nil, false, fmt.Errorf("load signal-strategy exposure: %w", err)
		}
		inventory = append(inventory, positions...)
		restored = restored || wasRestored
	}
	loaders := []struct {
		name string
		load func() ([]execution.ExposurePosition, bool, error)
	}{
		{name: "dca", load: func() ([]execution.ExposurePosition, bool, error) {
			strategyCfg, exists := cfg.Strategies.Configs["dca"]
			if !exists || !strategyCfg.Enabled {
				return nil, false, nil
			}
			return strategy.LoadDCAExposureInventory(stateStore, &cfg, ex, "dca", symbol, strategyCfg.Config)
		}},
		{name: "dca_enhanced", load: func() ([]execution.ExposurePosition, bool, error) {
			strategyCfg, exists := cfg.Strategies.Configs["dca_enhanced"]
			if !exists || !strategyCfg.Enabled {
				return nil, false, nil
			}
			return strategy.LoadDCAExposureInventory(stateStore, &cfg, ex, "dca_enhanced", symbol, strategyCfg.Config)
		}},
		{name: "martingale", load: func() ([]execution.ExposurePosition, bool, error) {
			strategyCfg, exists := cfg.Strategies.Configs["martingale"]
			if !exists || !strategyCfg.Enabled {
				return nil, false, nil
			}
			return strategy.LoadMartingaleExposureInventory(stateStore, &cfg, ex, "martingale", symbol, strategyCfg.Config)
		}},
		{name: "combo", load: func() ([]execution.ExposurePosition, bool, error) {
			strategyCfg, exists := cfg.Strategies.Configs["combo"]
			if !exists || !strategyCfg.Enabled {
				return nil, false, nil
			}
			return strategy.LoadComboExposureInventory(stateStore, &cfg, ex, symbol, strategyCfg.Config)
		}},
	}
	for _, loader := range loaders {
		positions, wasRestored, err := loader.load()
		if err != nil {
			return nil, false, fmt.Errorf("load %s exposure: %w", loader.name, err)
		}
		inventory = append(inventory, positions...)
		restored = restored || wasRestored
	}
	return inventory, restored, nil
}

func retryRuntimeExposureBootstrapAfterStrategyRecovery(ctx context.Context, cfg config.Config, ex exchange.IExchange, exchangeAdapter position.IExchange,
	storageService *storage.StorageService, botID string, executor *order.ExchangeOrderExecutor, gate *execution.OpeningGate,
	backend runtimeIntentBackend, scope execution.IntentScope, book *execution.ExposureBook, spm *position.SuperPositionManager) error {
	inventory, restored, err := loadRuntimeStrategyExposureInventory(cfg, exchangeAdapter, storageService, botID, scope.Symbol)
	if err != nil {
		return err
	}
	if err := bootstrapRuntimeExposure(ctx, executor, gate, ex, backend, scope, book, spm, inventory, restored); err != nil {
		return err
	}
	spm.MarkGridRuntimeVenueFlatVerified()
	spm.CompleteReconciliation()
	return nil
}
