package main

// 本文件抽自 main.go，集中存放 Bot/策略相關的適配器與輔助函數。
// 拆分目的：避免單文件超過 3000 行硬上限。
// 行為與類型語意保持不變，僅做位置遷移。

import (
	"context"
	"strings"
	"time"

	"quantmesh/config"
	"quantmesh/risk"
	"quantmesh/web"
)

// convertStrategies 将配置中的策略列表转换为 API 返回的格式
func convertStrategies(strategies []config.StrategyInstance) []web.BotStrategyInfo {
	if len(strategies) == 0 {
		return nil
	}
	result := make([]web.BotStrategyInfo, len(strategies))
	for i, s := range strategies {
		result[i] = web.BotStrategyInfo{
			Type:   s.Type,
			Weight: s.Weight,
			Name:   getStrategyDisplayName(s.Type),
		}
	}
	return result
}

// getStrategyDisplayName 返回策略的显示名称
func getStrategyDisplayName(strategyType string) string {
	strategyNames := map[string]string{
		"grid":                "网格交易",
		"dca":                 "DCA定投",
		"dca_enhanced":        "增强DCA",
		"martingale":          "马丁格尔",
		"trend_following":     "趋势跟踪",
		"mean_reversion":      "均值回归",
		"combo":               "组合策略",
		"funding_carry":       "资金费率套利",
		"funding_perp_spread": "双永续跨所资金费套利",
	}
	if name, ok := strategyNames[strategyType]; ok {
		return name
	}
	return strategyType
}

// botManagerProviderAdapter 實現 web.BotManagerProvider
type botManagerProviderAdapter struct {
	manager *SymbolManager
}

func attachBotLastStartFailure(botMgr *BotManager, resp *web.BotResponse) {
	if botMgr == nil || resp == nil {
		return
	}
	if msg, at, ok := botMgr.GetLastStartFailure(resp.BotID); ok {
		resp.LastStartError = msg
		resp.LastStartErrorAt = at.UTC().Format(time.RFC3339)
	}
}

// attachBotRiskFields 填充 Bot API 的風控觸發狀態與說明（K 線風控 + 深度風控，與 symbol_manager 撤買單邏輯一致）
func attachBotRiskFields(inner *SymbolRuntime, resp *web.BotResponse) {
	if inner == nil || resp == nil {
		return
	}
	rmTriggered := inner.RiskMonitor != nil && inner.RiskMonitor.IsTriggered()
	dmTriggered := inner.DepthMonitor != nil && inner.DepthMonitor.IsTriggered()
	resp.RiskTriggered = rmTriggered || dmTriggered
	var parts []string
	if rmTriggered && inner.RiskMonitor != nil {
		if msg := inner.RiskMonitor.GetLastMsg(); msg != "" {
			parts = append(parts, msg)
		}
	}
	if dmTriggered && inner.DepthMonitor != nil {
		if msg := inner.DepthMonitor.GetLastMsg(); msg != "" {
			parts = append(parts, msg)
		}
	}
	resp.RiskTriggerMessage = strings.Join(parts, "; ")
}

func (a *botManagerProviderAdapter) ListBots() []web.BotResponse {
	cfg, err := web.GetLatestConfig()
	if err != nil || cfg == nil {
		return nil
	}
	botMgr := a.manager.GetBotManager()
	runningMap := make(map[string]*BotRuntime)
	for _, br := range botMgr.List() {
		if br != nil && br.BotID != "" {
			runningMap[br.BotID] = br
		}
	}
	var result []web.BotResponse
	for _, bc := range cfg.Bots {
		botID := bc.ID
		if botID == "" {
			botID = config.GenerateBotID(bc.Exchange, bc.Symbol, bc.GetMarketType())
		}
		name := bc.Name
		if name == "" {
			name = bc.Symbol + " (" + bc.GetMarketType() + ")"
		}
		resp := web.BotResponse{
			BotID:                 botID,
			Name:                  name,
			Exchange:              bc.Exchange,
			Symbol:                bc.Symbol,
			MarketType:            bc.GetMarketType(),
			Running:               false,
			PriceInterval:         bc.PriceInterval,
			ProfitSpread:          bc.ProfitSpread,
			OrderQuantity:         bc.OrderQuantity,
			TotalAllocatedCapital: bc.TotalAllocatedCapital,
			Strategies:            convertStrategies(bc.Strategies),
			BuyWindowSize:         bc.BuyWindowSize,
			CreatedAt:             bc.CreatedAt,
			HedgeGroupName:        web.FindGroupNameByBotID(cfg, botID),
			Direction:             bc.GetDirection(),
			Testnet:               cfg.EffectiveTestnetForExchange(bc.Exchange, bc.Testnet),
		}
		for _, strategy := range bc.Strategies {
			if strategy.Type == "grid" {
				if leverage, ok := strategy.Config["leverage"].(float64); ok {
					resp.Leverage = leverage
				}
				if maxCapitalRatio, ok := strategy.Config["max_capital_ratio"].(float64); ok {
					resp.MaxCapitalRatio = maxCapitalRatio
				}
				break
			}
		}
		if br, ok := runningMap[botID]; ok && br.Inner != nil {
			resp.Running = true
			if br.Inner.PriceMonitor != nil {
				resp.CurrentPrice = br.Inner.PriceMonitor.GetLastPrice()
			}
			if br.Inner.SuperPositionManager != nil {
				resp.TotalPnL = br.Inner.SuperPositionManager.GetUnrealizedPnL(resp.CurrentPrice)
			}
			attachBotRiskFields(br.Inner, &resp)
		} else if stoppedAt, ok := botMgr.GetStoppedAt(botID); ok {
			resp.StoppedAt = stoppedAt
		}
		attachBotLastStartFailure(botMgr, &resp)
		result = append(result, resp)
	}
	// 兼容：若 Bots 為空但 Symbols 有數據，從 Symbols 轉換
	if len(result) == 0 && len(cfg.Trading.Symbols) > 0 {
		for _, sc := range cfg.Trading.Symbols {
			exCfg, ok := cfg.Exchanges[sc.Exchange]
			testnet := false
			if ok {
				testnet = exCfg.Testnet
			}
			bc := config.SymbolConfigToBotConfig(sc, testnet)
			botID := bc.ID
			name := bc.Name
			if name == "" {
				name = bc.Symbol + " (" + bc.GetMarketType() + ")"
			}
			resp := web.BotResponse{
				BotID:                 botID,
				Name:                  name,
				Exchange:              bc.Exchange,
				Symbol:                bc.Symbol,
				MarketType:            bc.GetMarketType(),
				Running:               false,
				PriceInterval:         bc.PriceInterval,
				ProfitSpread:          bc.ProfitSpread,
				OrderQuantity:         bc.OrderQuantity,
				TotalAllocatedCapital: bc.TotalAllocatedCapital,
				CreatedAt:             bc.CreatedAt,
				HedgeGroupName:        web.FindGroupNameByBotID(cfg, botID),
				Direction:             bc.GetDirection(),
				Testnet:               testnet,
			}
			if br, ok := runningMap[botID]; ok && br.Inner != nil {
				resp.Running = true
				if br.Inner.PriceMonitor != nil {
					resp.CurrentPrice = br.Inner.PriceMonitor.GetLastPrice()
				}
				if br.Inner.SuperPositionManager != nil {
					resp.TotalPnL = br.Inner.SuperPositionManager.GetUnrealizedPnL(resp.CurrentPrice)
				}
				attachBotRiskFields(br.Inner, &resp)
			} else if stoppedAt, ok := botMgr.GetStoppedAt(botID); ok {
				resp.StoppedAt = stoppedAt
			}
			attachBotLastStartFailure(botMgr, &resp)
			result = append(result, resp)
		}
	}
	return result
}

func (a *botManagerProviderAdapter) GetBot(botID string) (*web.BotDetailResponse, bool) {
	botMgr := a.manager.GetBotManager()
	br, ok := botMgr.Get(botID)
	cfg, _ := web.GetLatestConfig()
	if ok && br != nil && br.Inner != nil {
		name := br.Config.Name
		if name == "" {
			name = br.Config.Symbol + " (" + br.Config.GetMarketType() + ")"
		}
		resp := &web.BotDetailResponse{
			BotResponse: web.BotResponse{
				BotID:                 br.BotID,
				Name:                  name,
				Exchange:              br.Config.Exchange,
				Symbol:                br.Config.Symbol,
				MarketType:            br.Config.GetMarketType(),
				Running:               true,
				PriceInterval:         br.Config.PriceInterval,
				ProfitSpread:          br.Config.ProfitSpread,
				OrderQuantity:         br.Config.OrderQuantity,
				TotalAllocatedCapital: br.Config.TotalAllocatedCapital,
				Strategies:            convertStrategies(br.Config.Strategies),
				BuyWindowSize:         br.Config.BuyWindowSize,
				CreatedAt:             br.Config.CreatedAt,
				HedgeGroupName:        web.FindGroupNameByBotID(cfg, br.BotID),
				Direction:             br.Config.GetDirection(),
				Testnet:               cfg.EffectiveTestnetForExchange(br.Config.Exchange, br.Config.Testnet),
			},
			Config: &br.Config,
		}
		for _, strategy := range br.Config.Strategies {
			if strategy.Type == "grid" {
				if leverage, ok := strategy.Config["leverage"].(float64); ok {
					resp.Leverage = leverage
				}
				if maxCapitalRatio, ok := strategy.Config["max_capital_ratio"].(float64); ok {
					resp.MaxCapitalRatio = maxCapitalRatio
				}
				break
			}
		}
		if br.Inner.PriceMonitor != nil {
			resp.CurrentPrice = br.Inner.PriceMonitor.GetLastPrice()
		}
		if br.Inner.SuperPositionManager != nil {
			resp.TotalPnL = br.Inner.SuperPositionManager.GetUnrealizedPnL(resp.CurrentPrice)
		}
		attachBotRiskFields(br.Inner, &resp.BotResponse)
		attachBotLastStartFailure(botMgr, &resp.BotResponse)
		return resp, true
	}
	if cfg == nil {
		return nil, false
	}
	for i := range cfg.Bots {
		id := cfg.Bots[i].ID
		if id == "" {
			id = config.GenerateBotID(cfg.Bots[i].Exchange, cfg.Bots[i].Symbol, cfg.Bots[i].GetMarketType())
		}
		if id == botID {
			bc := &cfg.Bots[i]
			name := bc.Name
			if name == "" {
				name = bc.Symbol + " (" + bc.GetMarketType() + ")"
			}
			resp := &web.BotDetailResponse{
				BotResponse: web.BotResponse{
					BotID:                 botID,
					Name:                  name,
					Exchange:              bc.Exchange,
					Symbol:                bc.Symbol,
					MarketType:            bc.GetMarketType(),
					Running:               false,
					PriceInterval:         bc.PriceInterval,
					ProfitSpread:          bc.ProfitSpread,
					OrderQuantity:         bc.OrderQuantity,
					TotalAllocatedCapital: bc.TotalAllocatedCapital,
					Strategies:            convertStrategies(bc.Strategies),
					BuyWindowSize:         bc.BuyWindowSize,
					CreatedAt:             bc.CreatedAt,
					HedgeGroupName:        web.FindGroupNameByBotID(cfg, botID),
					Direction:             bc.GetDirection(),
					Testnet:               cfg.EffectiveTestnetForExchange(bc.Exchange, bc.Testnet),
				},
				Config: bc,
			}
			for _, strategy := range bc.Strategies {
				if strategy.Type == "grid" {
					if leverage, ok := strategy.Config["leverage"].(float64); ok {
						resp.Leverage = leverage
					}
					if maxCapitalRatio, ok := strategy.Config["max_capital_ratio"].(float64); ok {
						resp.MaxCapitalRatio = maxCapitalRatio
					}
					break
				}
			}
			attachBotLastStartFailure(botMgr, &resp.BotResponse)
			return resp, true
		}
	}
	return nil, false
}

func (a *botManagerProviderAdapter) StartBot(ctx context.Context, botCfg config.BotConfig) error {
	_, err := a.manager.StartBot(ctx, botCfg)
	return err
}

func (a *botManagerProviderAdapter) StopBot(botID string) error {
	return a.manager.GetBotManager().StopBot(botID)
}

func (a *botManagerProviderAdapter) EnableBot(botID string) error {
	return a.manager.GetBotManager().EnableBot(botID)
}

// GetAllBots 實現 risk.BotProvider 接口
func (a *botManagerProviderAdapter) GetAllBots() []risk.BotController {
	botMgr := a.manager.GetBotManager()
	bots := botMgr.List()
	result := make([]risk.BotController, 0, len(bots))
	for _, bot := range bots {
		if bot != nil {
			result = append(result, bot)
		}
	}
	return result
}

// botExtendedProviderAdapter 實現 web.BotExtendedProvider
type botExtendedProviderAdapter struct {
	manager *SymbolManager
}

func (a *botExtendedProviderAdapter) GetBot(botID string) (web.BotExtended, bool) {
	botMgr := a.manager.GetBotManager()
	br, ok := botMgr.Get(botID)
	if !ok {
		return nil, false
	}
	return br, true
}
