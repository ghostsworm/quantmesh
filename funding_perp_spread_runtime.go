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

// startFundingPerpSpreadSymbolRuntime 雙永续跨所資金費差專用運行時
func startFundingPerpSpreadSymbolRuntime(
	ctx context.Context,
	baseCfg *config.Config,
	symCfg config.SymbolConfig,
	eventBus *event.EventBus,
	storageService *storage.StorageService,
	distributedLock lock.DistributedLock,
	onRequestStop func(botID string),
) (*SymbolRuntime, error) {
	fp := symCfg.FundingPerpSpread
	if fp == nil {
		return nil, fmt.Errorf("funding_perp_spread 缺少 funding_perp_spread 配置")
	}
	if err := config.ValidateFundingPerpSpread(fp); err != nil {
		return nil, err
	}

	localCfg := *baseCfg
	botID := symCfg.ID
	if botID == "" {
		botID = config.GenerateBotIDFundingPerpSpread(fp)
	}
	localCfg.Trading.BotID = botID
	ctx = logger.WithBotID(ctx, botID)
	localCfg.Trading.Symbol = symCfg.Symbol
	localCfg.Trading.MarketType = config.MarketTypeFundingPerpSpread
	mergeFundingPerpSpreadStrategyConfig(&localCfg, symCfg)

	legAEx, err := exchange.NewExchange(&localCfg, strings.TrimSpace(fp.LegA.Exchange), strings.TrimSpace(fp.LegA.Symbol), "futures")
	if err != nil {
		return nil, fmt.Errorf("創建 leg_a 合約連線失敗: %w", err)
	}
	legBEx, err := exchange.NewExchange(&localCfg, strings.TrimSpace(fp.LegB.Exchange), strings.TrimSpace(fp.LegB.Symbol), "futures")
	if err != nil {
		return nil, fmt.Errorf("創建 leg_b 合約連線失敗: %w", err)
	}

	// 價格監控掛在 leg_a（主顯示）
	priceMonitor := monitor.NewPriceMonitor(
		legAEx,
		fp.LegA.Symbol,
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
		return nil, fmt.Errorf("無法獲取初始價格（leg_a）")
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
	var stratCfg map[string]interface{}
	for _, si := range symCfg.Strategies {
		if si.Type == "funding_perp_spread" {
			stratCfg = si.Config
			break
		}
	}
	st := strategy.NewFundingPerpSpreadStrategy("funding_perp_spread", &localCfg, symCfg, legAEx, legBEx, fp, stratCfg)
	strategyManager.RegisterStrategy("funding_perp_spread", st, 1.0, 0)
	if err := strategyManager.StartAll(); err != nil {
		return nil, err
	}

	accountID := ""
	if fp != nil {
		if exCfg, ok := baseCfg.Exchanges[strings.TrimSpace(fp.LegA.Exchange)]; ok && len(exCfg.APIKey) > 0 {
			if len(exCfg.APIKey) > 8 {
				accountID = exCfg.APIKey[:8]
			} else {
				accountID = exCfg.APIKey
			}
		}
	}

	rt := &SymbolRuntime{
		Config:               symCfg,
		Exchange:             legAEx,
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
		logger.InfoCtx(ctx, "⏹️ [%s] 停止雙永续跨所資金費運行時", botID)
		if strategyManager != nil {
			strategyManager.StopAll()
		}
		if priceMonitor != nil {
			priceMonitor.Stop()
		}
		legAEx.StopOrderStream()
		legBEx.StopOrderStream()
	}

	return rt, nil
}

func mergeFundingPerpSpreadStrategyConfig(localCfg *config.Config, symCfg config.SymbolConfig) {
	localCfg.Strategies.Enabled = true
	if localCfg.Strategies.Configs == nil {
		localCfg.Strategies.Configs = make(map[string]config.StrategyConfig)
	}
	var cfg map[string]interface{}
	weight := 1.0
	for _, si := range symCfg.Strategies {
		if si.Type == "funding_perp_spread" {
			cfg = si.Config
			if si.Weight > 0 {
				weight = si.Weight
			}
			break
		}
	}
	localCfg.Strategies.Configs["funding_perp_spread"] = config.StrategyConfig{
		Enabled: true,
		Type:    "funding_perp_spread",
		Weight:  weight,
		Config:  cfg,
	}
}
