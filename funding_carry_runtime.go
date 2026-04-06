package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/lock"
	"quantmesh/logger"
	"quantmesh/monitor"
	"quantmesh/storage"
	"quantmesh/strategy"
)

// startFundingCarrySymbolRuntime 資金費套利專用運行時（現貨+合約雙連線，不跑網格）
func startFundingCarrySymbolRuntime(
	ctx context.Context,
	baseCfg *config.Config,
	symCfg config.SymbolConfig,
	eventBus *event.EventBus,
	storageService *storage.StorageService,
	distributedLock lock.DistributedLock,
	onRequestStop func(botID string),
) (*SymbolRuntime, error) {
	if strings.ToLower(symCfg.Exchange) != "binance" {
		return nil, fmt.Errorf("資金費套利當前僅支援 binance，當前: %s", symCfg.Exchange)
	}

	permCtx, cancelPerm := context.WithTimeout(ctx, 15*time.Second)
	defer cancelPerm()
	perm, err := exchange.CheckFundingCarrySetup(permCtx, baseCfg, symCfg.Exchange, symCfg.Symbol)
	if err != nil {
		return nil, fmt.Errorf("資金費套利預檢失敗: %w", err)
	}
	if perm == nil {
		return nil, fmt.Errorf("資金費套利預檢結果為空")
	}
	if !perm.OK {
		return nil, fmt.Errorf("資金費套利 API 未就緒: 現貨=%v 合約=%v (%s / %s)",
			perm.SpotOK, perm.FuturesOK, perm.SpotMessage, perm.FuturesMessage)
	}

	localCfg := *baseCfg
	botID := symCfg.ID
	if botID == "" {
		botID = config.GenerateBotID(symCfg.Exchange, symCfg.Symbol, symCfg.GetMarketType())
	}
	localCfg.Trading.BotID = botID
	localCfg.Trading.Symbol = symCfg.Symbol
	localCfg.Trading.MarketType = config.MarketTypeFundingCarry
	mergeFundingCarryStrategyConfig(&localCfg, symCfg)

	futEx, err := exchange.NewExchange(&localCfg, symCfg.Exchange, symCfg.Symbol, "futures")
	if err != nil {
		return nil, fmt.Errorf("創建合約連線失敗: %w", err)
	}
	spotEx, err := exchange.NewExchange(&localCfg, symCfg.Exchange, symCfg.Symbol, "spot")
	if err != nil {
		return nil, fmt.Errorf("創建現貨連線失敗: %w", err)
	}

	priceMonitor := monitor.NewPriceMonitor(
		futEx,
		symCfg.Symbol,
		localCfg.Timing.PriceSendInterval,
	)
	if err := priceMonitor.Start(); err != nil {
		return nil, fmt.Errorf("價格流: %w", err)
	}
	pollInterval := time.Duration(localCfg.Timing.PricePollInterval) * time.Millisecond
	for i := 0; i < 10; i++ {
		if priceMonitor.GetLastPrice() > 0 {
			break
		}
		time.Sleep(pollInterval)
	}
	if priceMonitor.GetLastPrice() <= 0 {
		return nil, fmt.Errorf("無法獲取初始價格")
	}

	totalCap := symCfg.TotalAllocatedCapital
	if totalCap <= 0 {
		totalCap = symCfg.OrderQuantity
	}
	if totalCap <= 0 {
		return nil, fmt.Errorf("total_allocated_capital 必須大於 0")
	}

	strategyManager := strategy.NewStrategyManager(&localCfg, totalCap)
	if eventBus != nil {
		strategyManager.SetEventBus(eventBus)
	}
	var fcCfg map[string]interface{}
	for _, si := range symCfg.Strategies {
		if si.Type == "funding_carry" {
			fcCfg = si.Config
			break
		}
	}
	fc := strategy.NewFundingCarryStrategy("funding_carry", &localCfg, symCfg, futEx, spotEx, fcCfg)
	strategyManager.RegisterStrategy("funding_carry", fc, 1.0, 0)
	if err := strategyManager.StartAll(); err != nil {
		return nil, err
	}

	accountID := ""
	if exCfg, ok := baseCfg.Exchanges[symCfg.Exchange]; ok && len(exCfg.APIKey) > 0 {
		if len(exCfg.APIKey) > 8 {
			accountID = exCfg.APIKey[:8]
		} else {
			accountID = exCfg.APIKey
		}
	}

	rt := &SymbolRuntime{
		Config:           symCfg,
		Exchange:         futEx,
		PriceMonitor:     priceMonitor,
		StrategyManager:  strategyManager,
		EventBus:         eventBus,
		StorageService:   storageService,
		AccountID:        accountID,
		SuperPositionManager: nil,
		ExchangeExecutor:     nil,
		ExecutorAdapter:      nil,
		ExchangeAdapter:      nil,
	}

	rt.Stop = func() {
		logger.Info("⏹️ [%s] 停止資金費套利運行時（策略 Stop 會自動嘗試平倉）", symCfg.Symbol)
		if strategyManager != nil {
			strategyManager.StopAll()
		}
		if priceMonitor != nil {
			priceMonitor.Stop()
		}
		futEx.StopOrderStream()
		spotEx.StopOrderStream()
	}

	return rt, nil
}

func mergeFundingCarryStrategyConfig(localCfg *config.Config, symCfg config.SymbolConfig) {
	localCfg.Strategies.Enabled = true
	if localCfg.Strategies.Configs == nil {
		localCfg.Strategies.Configs = make(map[string]config.StrategyConfig)
	}
	var cfg map[string]interface{}
	weight := 1.0
	for _, si := range symCfg.Strategies {
		if si.Type == "funding_carry" {
			cfg = si.Config
			if si.Weight > 0 {
				weight = si.Weight
			}
			break
		}
	}
	localCfg.Strategies.Configs["funding_carry"] = config.StrategyConfig{
		Enabled: true,
		Type:    "funding_carry",
		Weight:  weight,
		Config:  cfg,
	}
}
