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
	ctx = logger.WithBotID(ctx, botID)
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

	// 嘗試建立保證金帳戶連線（反向套利用，失敗不阻塞啟動）
	var marginEx exchange.ISpotMarginExchange
	marginRaw, marginErr := exchange.NewExchange(&localCfg, symCfg.Exchange, symCfg.Symbol, "spot_margin")
	if marginErr == nil {
		if me, ok := marginRaw.(exchange.ISpotMarginExchange); ok {
			marginEx = me
			logger.InfoCtx(ctx, "✅ [%s] 保證金帳戶已連線（支援反向套利）", symCfg.Symbol)
		}
	} else {
		logger.InfoCtx(ctx, "ℹ️ [%s] 保證金帳戶不可用（%v），反向套利已禁用", symCfg.Symbol, marginErr)
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
	if pollInterval <= 0 {
		// 零值保护：避免配置未经校验时 time.Sleep(0) 退化为 CPU 空转
		pollInterval = 500 * time.Millisecond
	}
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
	fc := strategy.NewFundingCarryStrategy("funding_carry", &localCfg, symCfg, futEx, spotEx, marginEx, fcCfg)
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

	// 每個 funding_carry bot 獨立同步資金費收入
	if storageService != nil {
		go startFundingIncomeSync(ctx, storageService.GetStorage(), futEx,
			symCfg.Exchange, symCfg.Symbol, accountID)
	}

	rt := &SymbolRuntime{
		Config:               symCfg,
		Exchange:             futEx,
		PriceMonitor:         priceMonitor,
		StrategyManager:      strategyManager,
		EventBus:             eventBus,
		StorageService:       storageService,
		AccountID:            accountID,
		SuperPositionManager: nil,
		ExchangeExecutor:     nil,
		ExecutorAdapter:      nil,
		ExchangeAdapter:      nil,
	}

	rt.Stop = func() {
		logger.InfoCtx(ctx, "⏹️ [%s] 停止資金費套利運行時（策略 Stop 會自動嘗試平倉）", symCfg.Symbol)
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
