package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/execution"
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
	baseA, baseB := legAEx.GetBaseAsset(), legBEx.GetBaseAsset()
	if err := validateFundingPerpSpreadLegBases(baseA, baseB); err != nil {
		return nil, err
	}
	const spreadCapitalAsset = "USDT"
	quoteA := strings.ToUpper(strings.TrimSpace(legAEx.GetQuoteAsset()))
	quoteB := strings.ToUpper(strings.TrimSpace(legBEx.GetQuoteAsset()))
	if quoteA != spreadCapitalAsset || quoteB != spreadCapitalAsset {
		return nil, fmt.Errorf("funding_perp_spread currently requires USDT-quoted legs; got %q and %q", quoteA, quoteB)
	}
	requestedCapital := symCfg.TotalAllocatedCapital
	if requestedCapital <= 0 {
		requestedCapital = symCfg.OrderQuantity
	}
	balanceCtx, cancelBalance := context.WithTimeout(ctx, 10*time.Second)
	legABalance, balanceErr := legAEx.GetBalance(balanceCtx, spreadCapitalAsset)
	if balanceErr == nil {
		var legBBalance float64
		legBBalance, balanceErr = legBEx.GetBalance(balanceCtx, spreadCapitalAsset)
		if balanceErr == nil {
			verifiedCapital, capErr := capTwoLegStrategyCapitalLimit(requestedCapital, legABalance, legBBalance)
			if capErr != nil {
				balanceErr = capErr
			} else {
				walletCandidate := symCfg
				walletCandidate.TotalAllocatedCapital = verifiedCapital
				for _, wallet := range []struct {
					exchange string
					balance  float64
				}{{exchange: fp.LegA.Exchange, balance: legABalance}, {exchange: fp.LegB.Exchange, balance: legBBalance}} {
					allocated, allocationErr := configuredAccountWalletCapitalForQuote(baseCfg, walletCandidate, wallet.exchange, "futures", spreadCapitalAsset)
					if allocationErr != nil {
						balanceErr = fmt.Errorf("calculate configured futures-wallet capital for %s: %w", wallet.exchange, allocationErr)
						break
					}
					if allocated > wallet.balance {
						balanceErr = fmt.Errorf("configured futures-wallet capital %.2f USDT exceeds available balance %.2f USDT on %s", allocated, wallet.balance, wallet.exchange)
						break
					}
				}
			}
			if balanceErr == nil {
				if verifiedCapital < requestedCapital {
					logger.WarnCtx(ctx, "funding_perp_spread budget capped from %.2f to %.2f USDT by verified leg balances", requestedCapital, verifiedCapital)
				}
				symCfg.TotalAllocatedCapital = verifiedCapital
			}
		}
	}
	cancelBalance()
	if balanceErr != nil {
		return nil, fmt.Errorf("verify funding_perp_spread USDT balance on both legs: %w", balanceErr)
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
	openingGate := &execution.OpeningGate{}
	if localCfg.Trading.OpenPositionControl.PauseOpening || (localCfg.Trading.OpenPositionControl.BotRiskControl != nil && localCfg.Trading.OpenPositionControl.BotRiskControl.PauseOpening) {
		openingGate.Block("manual")
	}
	st := strategy.NewFundingPerpSpreadStrategy("funding_perp_spread", &localCfg, symCfg, legAEx, legBEx, fp, stratCfg)
	st.SetOpeningGate(openingGate)
	stateBotID := fundingPerpSpreadStateScope(botID, baseCfg, fp)
	st.SetRuntimeStateStore(&strategyRuntimeStateAdapter{storageService: storageService, botID: stateBotID})
	st.SetCoordinationLock(distributedLock)
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
		AccountScope:         stateBotID,
		AccountMarketType:    "futures",
		OpeningGate:          openingGate,
		SuperPositionManager: nil,
		ExchangeExecutor:     nil,
		ExecutorAdapter:      nil,
		ExchangeAdapter:      nil,
	}
	rt.PrepareShutdown = func(shutdownCtx context.Context, _ bool) error {
		return st.PrepareShutdown(shutdownCtx)
	}
	rt.CloseForShutdown = st.CloseForShutdown
	rt.VerifyShutdownClose = st.VerifyFlat

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

func validateFundingPerpSpreadLegBases(baseA, baseB string) error {
	baseA = strings.ToUpper(strings.TrimSpace(baseA))
	baseB = strings.ToUpper(strings.TrimSpace(baseB))
	if baseA == "" || baseB == "" || baseA != baseB {
		return fmt.Errorf("funding_perp_spread legs must use the same verified base asset; got %q and %q", baseA, baseB)
	}
	return nil
}

// fundingPerpSpreadStateScope isolates durable strategy state by bot, both legs,
// and credential identity without persisting or logging credential material.
func fundingPerpSpreadStateScope(botID string, cfg *config.Config, fp *config.FundingPerpSpreadConfig) string {
	h := sha256.New()
	write := func(value string) {
		_, _ = h.Write([]byte(value))
		_, _ = h.Write([]byte{0})
	}
	write(botID)
	for _, leg := range []config.FundingPerpLeg{fp.LegA, fp.LegB} {
		write(strings.ToLower(strings.TrimSpace(leg.Exchange)))
		write(strings.ToUpper(strings.TrimSpace(leg.Symbol)))
		exCfg, ok := cfg.Exchanges[strings.TrimSpace(leg.Exchange)]
		if !ok {
			write("missing-exchange-config")
			continue
		}
		write(exCfg.APIKey)
		write(exCfg.SecretKey)
		write(exCfg.Passphrase)
		write(fmt.Sprintf("testnet=%t", exCfg.Testnet))
	}
	return "funding_perp_spread:" + hex.EncodeToString(h.Sum(nil))
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
