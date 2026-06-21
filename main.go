package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	// "quantmesh/ai" // AI 功能已迁移到商业插件
	"quantmesh/ai"
	"quantmesh/ai/geminiusage"
	"quantmesh/ai/processor"
	"quantmesh/ai/service"
	"quantmesh/backtest"
	"quantmesh/backtest/optimrun"
	"quantmesh/cfgmgr"
	"quantmesh/config"
	"quantmesh/database"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/i18n"
	"quantmesh/inspector"
	"quantmesh/lock"
	"quantmesh/logger"
	"quantmesh/macro"
	"quantmesh/metrics"
	"quantmesh/monitor"
	"quantmesh/notify"
	"quantmesh/notify/aipipe"
	"quantmesh/plugin"
	"quantmesh/position"
	"quantmesh/profit"
	"quantmesh/risk"
	"quantmesh/storage"
	"quantmesh/utils"
	"quantmesh/web"
)

// Version 应用版本号
var Version = "3.108.1-rc20"

// 全局日志存儲實例（用於清理任務和 WebSocket 推送）
var globalLogStorage *storage.LogStorage

// 以下類型已抽取到 main_adapters_web.go：
//   capitalDataSourceAdapter, webAuthnLoggerAdapter,
//   reconciliationStorageAdapter, allocationManagerProviderAdapter,
//   planManagerProviderAdapter, polymarketSignalAdapter,
//   reconciliationRestoreAdapter, tradeStorageAdapter,
//   snapshotRuntimeAdapter
// 以及 helper buildBinanceConfigForBacktest。

type symbolManagerWebAdapter struct {
	manager         *SymbolManager
	ctx             context.Context
	cfg             *config.Config
	eventBus        *event.EventBus
	storageService  *storage.StorageService
	distributedLock lock.DistributedLock
}

func (a *symbolManagerWebAdapter) Get(exchange, symbol string) (interface{}, bool) {
	return a.GetEx(exchange, symbol, "")
}

func (a *symbolManagerWebAdapter) GetEx(exchange, symbol, marketType string) (interface{}, bool) {
	mt := strings.TrimSpace(strings.ToLower(marketType))
	if mt != "" {
		rt, ok := a.manager.Get(exchange, symbol, mt)
		if !ok {
			return nil, false
		}
		return rt, true
	}

	// 未指定 market_type 時，僅在唯一候選時返回，避免默認 futures 造成錯配
	spotRT, spotOK := a.manager.Get(exchange, symbol, "spot")
	futuresRT, futuresOK := a.manager.Get(exchange, symbol, "futures")
	if spotOK && !futuresOK {
		return spotRT, true
	}
	if futuresOK && !spotOK {
		return futuresRT, true
	}
	if spotOK && futuresOK {
		logger.Warn("⚠️ GetEx 命中 spot/futures 同名交易對，需顯式指定 market_type: %s:%s", exchange, symbol)
		return nil, false
	}
	return nil, false
}

// GetByBotID 按 Bot 主鍵（如 UUID）獲取運行時；與 GetEx(exchange,symbol,mt) 不同，後者僅適用於 ID 為 exchange:symbol:mt 的舊版 Bot
func (a *symbolManagerWebAdapter) GetByBotID(botID string) (interface{}, bool) {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return nil, false
	}
	br, ok := a.manager.GetBotManager().Get(botID)
	if !ok || br == nil || br.Inner == nil {
		return nil, false
	}
	return br.Inner, true
}

func (a *symbolManagerWebAdapter) List() []interface{} {
	runtimes := a.manager.List()
	result := make([]interface{}, len(runtimes))
	for i, rt := range runtimes {
		result[i] = rt
	}
	return result
}

func (a *symbolManagerWebAdapter) StartSymbol(exchange, symbol, requestedMarketType string) error {
	// 從配置管理器獲取最新配置（而不是使用啟动時的配置）
	cfg, err := web.GetLatestConfig()
	if err != nil {
		// 如果獲取最新配置失败，回退到使用啟动時的配置
		logger.Warn("⚠️ 獲取最新配置失败，使用啟动時的配置: %v", err)
		cfg = a.cfg
	}

	// 從配置中查找對应的 SymbolConfig（可能有多個同名交易對，如 BTCUSDT 合約 + BTCUSDT 現貨）
	// 若前端傳入 market_type，優先啟動與之匹配且尚未運行的；否則啟動首個尚未運行的
	var symCfg *config.SymbolConfig
	var candidates []*config.SymbolConfig
	for i := range cfg.Trading.Symbols {
		if strings.EqualFold(cfg.Trading.Symbols[i].Exchange, exchange) &&
			strings.EqualFold(cfg.Trading.Symbols[i].Symbol, symbol) {
			candidates = append(candidates, &cfg.Trading.Symbols[i])
		}
	}

	if len(candidates) == 0 {
		err := fmt.Errorf("未找到交易對配置: %s:%s", exchange, symbol)
		if a.eventBus != nil {
			a.eventBus.Publish(&event.Event{
				Type: event.EventTypeTradingStartFailed,
				Data: map[string]interface{}{
					"exchange": exchange,
					"symbol":   symbol,
					"error":    err.Error(),
					"message":  err.Error(),
				},
			})
		}
		return err
	}

	// 若指定了 market_type，優先匹配該類型且未運行的
	if requestedMarketType != "" {
		reqMT := strings.ToLower(strings.TrimSpace(requestedMarketType))
		if reqMT == "spot" || reqMT == "futures" {
			var hasMatching bool
			for _, c := range candidates {
				mt := strings.ToLower(c.GetMarketType())
				if mt == reqMT {
					hasMatching = true
					if _, running := a.manager.Get(exchange, symbol, c.GetMarketType()); !running {
						symCfg = c
						break
					}
				}
			}
			if symCfg == nil && hasMatching {
				err := fmt.Errorf("交易對 %s:%s (%s) 已在使用中，無需重複啟動", exchange, symbol, requestedMarketType)
				if a.eventBus != nil {
					a.eventBus.Publish(&event.Event{
						Type: event.EventTypeTradingStartFailed,
						Data: map[string]interface{}{
							"exchange": exchange, "symbol": symbol,
							"error": err.Error(), "message": err.Error(),
						},
					})
				}
				return err
			}
			if symCfg == nil && reqMT == "spot" {
				err := fmt.Errorf("未找到 %s:%s 的現貨配置，請先在配置中新增 market_type: spot 的交易對", exchange, symbol)
				if a.eventBus != nil {
					a.eventBus.Publish(&event.Event{
						Type: event.EventTypeTradingStartFailed,
						Data: map[string]interface{}{
							"exchange": exchange, "symbol": symbol,
							"error": err.Error(), "message": err.Error(),
						},
					})
				}
				return err
			}
		}
	}
	// 未指定或未匹配到：取首個尚未運行的
	if symCfg == nil {
		for _, c := range candidates {
			mt := c.GetMarketType()
			if _, running := a.manager.Get(exchange, symbol, mt); !running {
				symCfg = c
				break
			}
		}
	}

	if symCfg == nil {
		// 所有候選都已在運行
		err := fmt.Errorf("交易對 %s:%s 的所有配置（%d 個）均已在運行", exchange, symbol, len(candidates))
		if a.eventBus != nil {
			a.eventBus.Publish(&event.Event{
				Type: event.EventTypeTradingStartFailed,
				Data: map[string]interface{}{
					"exchange": exchange,
					"symbol":   symbol,
					"error":    err.Error(),
					"message":  err.Error(),
				},
			})
		}
		return err
	}

	// 已找到尚未運行的配置
	marketType := symCfg.GetMarketType()
	// 確認確實未運行（double check）
	if _, ok := a.manager.Get(exchange, symbol, marketType); ok {
		err := fmt.Errorf("交易對 %s:%s (%s) 已經在运行", exchange, symbol, marketType)
		if a.eventBus != nil {
			a.eventBus.Publish(&event.Event{
				Type: event.EventTypeTradingStartFailed,
				Data: map[string]interface{}{
					"exchange":    exchange,
					"symbol":      symbol,
					"market_type": marketType,
					"error":       err.Error(),
					"message":     err.Error(),
				},
			})
		}
		return err
	}

	// 持久化啟用状態：确保重啟后仍保持啟动（帶 market_type 精確匹配）
	if err := web.SetSymbolEnabled(exchange, symbol, true, marketType); err != nil {
		wrapped := fmt.Errorf("更新配置失败: %w", err)
		if a.eventBus != nil {
			a.eventBus.Publish(&event.Event{
				Type: event.EventTypeTradingStartFailed,
				Data: map[string]interface{}{
					"exchange": exchange,
					"symbol":   symbol,
					"error":    wrapped.Error(),
					"message":  wrapped.Error(),
				},
			})
		}
		return wrapped
	}

	// 重新獲取最新配置（保存時會 normalize，确保使用落盘后的最新值啟动）
	cfg, err = web.GetLatestConfig()
	if err != nil {
		logger.Warn("⚠️ 獲取最新配置失败，使用啟动時的配置: %v", err)
		cfg = a.cfg
	}
	symCfg = nil
	for i := range cfg.Trading.Symbols {
		if strings.EqualFold(cfg.Trading.Symbols[i].Exchange, exchange) &&
			strings.EqualFold(cfg.Trading.Symbols[i].Symbol, symbol) &&
			strings.EqualFold(cfg.Trading.Symbols[i].GetMarketType(), marketType) {
			symCfg = &cfg.Trading.Symbols[i]
			break
		}
	}
	if symCfg == nil {
		err := fmt.Errorf("未找到交易對配置: %s:%s (%s)", exchange, symbol, marketType)
		if a.eventBus != nil {
			a.eventBus.Publish(&event.Event{
				Type: event.EventTypeTradingStartFailed,
				Data: map[string]interface{}{
					"exchange": exchange,
					"symbol":   symbol,
					"error":    err.Error(),
					"message":  err.Error(),
				},
			})
		}
		return err
	}

	// 啟动 SymbolRuntime（使用最新配置）
	exCfg, _ := cfg.Exchanges[symCfg.Exchange]
	botCfg := config.SymbolConfigToBotConfig(*symCfg, exCfg.Testnet)
	botID := botCfg.ID
	if botID == "" {
		botID = config.GenerateBotID(botCfg.Exchange, botCfg.Symbol, botCfg.GetMarketType())
	}
	_ = a.manager.GetBotManager().EnableBot(botID)
	br, err := a.manager.StartBot(a.ctx, botCfg)
	if err != nil {
		wrapped := fmt.Errorf("啟动失败: %w", err)
		hint := ""
		// 常见的“無法啟动交易”原因提示（不影响主流程）
		if strings.Contains(wrapped.Error(), "每笔净利润為负或為零") {
			hint = "建议：增加 price_interval（價格間隔）或在配置中設置更低且准确的 fee_rate（手续费率）"
		}
		if a.eventBus != nil {
			data := map[string]interface{}{
				"exchange": exchange,
				"symbol":   symbol,
				"error":    wrapped.Error(),
				"message":  wrapped.Error(),
			}
			if hint != "" {
				data["hint"] = hint
			}
			a.eventBus.Publish(&event.Event{
				Type: event.EventTypeTradingStartFailed,
				Data: data,
			})
		}
		return wrapped
	}
	if br == nil || br.Inner == nil {
		return fmt.Errorf("啟動成功但未返回運行時")
	}
	rt := br.Inner

	// 注册到 Web API
	if a.storageService != nil {
		status := &web.SystemStatus{
			Running:       true,
			Exchange:      exchange,
			Symbol:        symbol,
			CurrentPrice:  0,
			TotalPnL:      0,
			TotalTrades:   0,
			RiskTriggered: false,
			Uptime:        0,
		}
		web.RegisterSymbolProviders(exchange, symbol, &web.SymbolScopedProviders{
			Status:   status,
			Price:    rt.PriceMonitor,
			Exchange: &exchangeProviderAdapter{exchange: rt.Exchange},
			Position: web.NewPositionManagerAdapter(rt.SuperPositionManager),
			Risk:     rt.RiskMonitor,
			Storage:  web.NewStorageServiceAdapter(a.storageService),
		})

		// 啟动后台 goroutine 来更新状態（Uptime、CurrentPrice 等）
		startTime := time.Now()
		go func(r *SymbolRuntime, st *web.SystemStatus, started time.Time, storage *storage.StorageService) {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			dbQueryCounter := 0
			for {
				select {
				case <-a.ctx.Done():
					st.Running = false
					return
				case <-ticker.C:
					// 如果交易對已停止，不再更新状態
					if !st.Running {
						return
					}

					if r.PriceMonitor != nil {
						st.CurrentPrice = r.PriceMonitor.GetLastPrice()
					}
					if r.RiskMonitor != nil {
						st.RiskTriggered = r.RiskMonitor.IsTriggered()
					}

					// 更新统计信息
					if r.SuperPositionManager != nil {
						dbQueryCounter++

						useEstimation := true
						if storage != nil && storage.GetStorage() != nil {
							if dbQueryCounter >= 5 || st.TotalPnL == 0 {
								dbQueryCounter = 0
								now := utils.NowUTC()
								allHistoryStart := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
								pnlSummary, err := storage.GetStorage().GetPnLBySymbol(r.Config.Symbol, r.AccountID, allHistoryStart, now)
								if err == nil {
									st.TotalPnL = pnlSummary.TotalPnL
									st.TotalTrades = pnlSummary.TotalTrades
									useEstimation = false
								}
							} else {
								useEstimation = false
							}
						}

						if useEstimation {
							totalBuyQty := r.SuperPositionManager.GetTotalBuyQty()
							totalSellQty := r.SuperPositionManager.GetTotalSellQty()
							profitSpread := r.SuperPositionManager.GetProfitSpread()
							st.TotalPnL = totalSellQty * profitSpread

							if st.CurrentPrice > 0 {
								orderQtyInBase := r.Config.OrderQuantity / st.CurrentPrice
								if orderQtyInBase > 0 {
									st.TotalTrades = int((totalBuyQty + totalSellQty) / (orderQtyInBase * 2))
								}
							}
						}
					}

					st.Uptime = int64(time.Since(started).Seconds())
				}
			}
		}(rt, status, startTime, a.storageService)
	}

	logger.Info("✅ [%s:%s] 交易已啟动", exchange, symbol)
	if a.eventBus != nil {
		a.eventBus.Publish(&event.Event{
			Type: event.EventTypeTradingStarted,
			Data: map[string]interface{}{
				"exchange": exchange,
				"symbol":   symbol,
				"message":  fmt.Sprintf("交易已啟动: %s:%s", exchange, symbol),
			},
		})
	}
	return nil
}

func (a *symbolManagerWebAdapter) StopSymbol(exchange, symbol string) error {
	// 嘗試從配置中獲取 market_type，再用精確 key 查找
	mt := a.resolveMarketType(exchange, symbol)
	rt, ok := a.manager.Get(exchange, symbol, mt)
	if !ok {
		err := fmt.Errorf("交易對 %s:%s (%s) 未运行", exchange, symbol, mt)
		if a.eventBus != nil {
			a.eventBus.Publish(&event.Event{
				Type: event.EventTypeTradingStopFailed,
				Data: map[string]interface{}{
					"exchange": exchange,
					"symbol":   symbol,
					"error":    err.Error(),
					"message":  err.Error(),
				},
			})
		}
		return err
	}

	// 持久化停用状態：确保重啟后不會自动再啟动（帶 market_type 精確匹配）
	if err := web.SetSymbolEnabled(exchange, symbol, false, mt); err != nil {
		wrapped := fmt.Errorf("更新配置失败: %w", err)
		if a.eventBus != nil {
			a.eventBus.Publish(&event.Event{
				Type: event.EventTypeTradingStopFailed,
				Data: map[string]interface{}{
					"exchange": exchange,
					"symbol":   symbol,
					"error":    wrapped.Error(),
					"message":  wrapped.Error(),
				},
			})
		}
		return wrapped
	}

	// 停止运行時
	if rt.Stop != nil {
		rt.Stop()
	}

	// 從管理器中移除，这样下次 StartSymbol 才不會误判為"已运行"
	a.manager.Remove(exchange, symbol, mt)

	logger.Info("⏹️ [%s:%s:%s] 交易已停止", exchange, symbol, mt)
	if a.eventBus != nil {
		a.eventBus.Publish(&event.Event{
			Type: event.EventTypeTradingStopped,
			Data: map[string]interface{}{
				"exchange": exchange,
				"symbol":   symbol,
				"message":  fmt.Sprintf("交易已停止: %s:%s", exchange, symbol),
			},
		})
	}
	return nil
}

// 以下類型/函數已抽取到 main_adapters_bot.go：
//   convertStrategies, getStrategyDisplayName,
//   botManagerProviderAdapter, botExtendedProviderAdapter,
//   attachBotLastStartFailure, attachBotRiskFields。

// resolveMarketType 從配置或运行時中推導 market_type
func (a *symbolManagerWebAdapter) resolveMarketType(exchange, symbol string) string {
	// 先從配置中查找
	cfg, err := web.GetLatestConfig()
	if err == nil {
		for _, sym := range cfg.Trading.Symbols {
			if strings.EqualFold(sym.Exchange, exchange) && strings.EqualFold(sym.Symbol, symbol) {
				return sym.GetMarketType()
			}
		}
	}
	// 再從运行時中遍歷查找
	for _, rt := range a.manager.List() {
		if rt != nil && strings.EqualFold(rt.Config.Exchange, exchange) && strings.EqualFold(rt.Config.Symbol, symbol) {
			return rt.Config.GetMarketType()
		}
	}
	return "futures" // 默認合約
}

// UpdateTradingParams 实现 TradingParamsUpdater 接口
// 将最新配置推送到所有运行中的 SymbolRuntime，解决配置修改后内存不同步问题
func (a *symbolManagerWebAdapter) UpdateTradingParams(latestConfig *config.Config) []string {
	return a.manager.UpdateRuntimeTradingParams(latestConfig)
}

func (a *symbolManagerWebAdapter) ClosePositions(exchange, symbol string) (*web.ClosePositionsResponse, error) {
	mt := a.resolveMarketType(exchange, symbol)
	rt, ok := a.manager.Get(exchange, symbol, mt)
	if !ok {
		return nil, fmt.Errorf("交易對 %s:%s (%s) 未找到", exchange, symbol, mt)
	}

	// 創建上下文（带超時）
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()

	// 調用平倉函數並獲取結果
	successCount, failCount, err := closeAllPositionsWithResult(ctx, rt.Exchange, symbol, rt.PriceMonitor)
	if err != nil {
		return nil, err
	}

	message := fmt.Sprintf("平倉完成: 成功 %d, 失败 %d", successCount, failCount)
	if successCount == 0 && failCount == 0 {
		message = "當前没有持倉需要平倉"
	}

	return &web.ClosePositionsResponse{
		SuccessCount: successCount,
		FailCount:    failCount,
		Message:      message,
	}, nil
}

// GetAllStrategyStatus 獲取所有策略的運行狀態
func (a *symbolManagerWebAdapter) GetAllStrategyStatus(exchange, symbol string) ([]web.StrategyRuntimeStatusResponse, error) {
	rt, ok := a.manager.Get(exchange, symbol)
	if !ok {
		return nil, fmt.Errorf("交易對 %s:%s 未找到", exchange, symbol)
	}

	if rt.StrategyManager == nil {
		return []web.StrategyRuntimeStatusResponse{}, nil
	}

	// 從 StrategyManager 獲取所有策略狀態
	statuses := rt.StrategyManager.GetAllStrategyStatus()

	// 轉換為 web 響應格式
	result := make([]web.StrategyRuntimeStatusResponse, 0, len(statuses))
	for _, s := range statuses {
		resp := web.StrategyRuntimeStatusResponse{
			Name:           s.Name,
			Type:           s.Type,
			IsEnabled:      s.IsEnabled,
			IsRunning:      s.IsRunning,
			Weight:         s.Weight,
			AllocatedFunds: s.AllocatedFunds,
			UsedFunds:      s.UsedFunds,
			AvailableFunds: s.AvailableFunds,
			PositionCount:  s.PositionCount,
			OrderCount:     s.OrderCount,
		}

		// 轉換統計數據
		if s.Statistics != nil {
			resp.Statistics = &web.StrategyStatsResponse{
				TotalTrades: s.Statistics.TotalTrades,
				WinRate:     s.Statistics.WinRate,
				TotalPnL:    s.Statistics.TotalPnL,
				TotalVolume: s.Statistics.TotalVolume,
			}
		}

		// 轉換持倉數據
		if s.Positions != nil {
			resp.Positions = make([]web.StrategyPositionResp, 0, len(s.Positions))
			for _, p := range s.Positions {
				resp.Positions = append(resp.Positions, web.StrategyPositionResp{
					Symbol:       p.Symbol,
					Size:         p.Size,
					EntryPrice:   p.EntryPrice,
					CurrentPrice: p.CurrentPrice,
					PnL:          p.PnL,
				})
			}
		}

		// 轉換訂單數據
		if s.Orders != nil {
			resp.Orders = make([]web.StrategyOrderResp, 0, len(s.Orders))
			for _, o := range s.Orders {
				resp.Orders = append(resp.Orders, web.StrategyOrderResp{
					OrderID:  o.OrderID,
					Symbol:   o.Symbol,
					Side:     o.Side,
					Price:    o.Price,
					Quantity: o.Quantity,
					Status:   o.Status,
				})
			}
		}

		// 轉換可视化數據
		if s.VisualizationData != nil {
			resp.VisualizationData = s.VisualizationData
		}

		result = append(result, resp)
	}

	return result, nil
}

// GetStrategyStatus 獲取單個策略的運行狀態
func (a *symbolManagerWebAdapter) GetStrategyStatus(exchange, symbol, strategyName string) (*web.StrategyRuntimeStatusResponse, error) {
	rt, ok := a.manager.Get(exchange, symbol)
	if !ok {
		return nil, fmt.Errorf("交易對 %s:%s 未找到", exchange, symbol)
	}

	if rt.StrategyManager == nil {
		return nil, nil
	}

	// 從 StrategyManager 獲取策略狀態
	s := rt.StrategyManager.GetStrategyStatus(strategyName)
	if s == nil {
		return nil, nil
	}

	// 轉換為 web 響應格式
	resp := &web.StrategyRuntimeStatusResponse{
		Name:           s.Name,
		Type:           s.Type,
		IsEnabled:      s.IsEnabled,
		IsRunning:      s.IsRunning,
		Weight:         s.Weight,
		AllocatedFunds: s.AllocatedFunds,
		UsedFunds:      s.UsedFunds,
		AvailableFunds: s.AvailableFunds,
		PositionCount:  s.PositionCount,
		OrderCount:     s.OrderCount,
	}

	// 轉換統計數據
	if s.Statistics != nil {
		resp.Statistics = &web.StrategyStatsResponse{
			TotalTrades: s.Statistics.TotalTrades,
			WinRate:     s.Statistics.WinRate,
			TotalPnL:    s.Statistics.TotalPnL,
			TotalVolume: s.Statistics.TotalVolume,
		}
	}

	// 轉換持倉數據
	if s.Positions != nil {
		resp.Positions = make([]web.StrategyPositionResp, 0, len(s.Positions))
		for _, p := range s.Positions {
			resp.Positions = append(resp.Positions, web.StrategyPositionResp{
				Symbol:       p.Symbol,
				Size:         p.Size,
				EntryPrice:   p.EntryPrice,
				CurrentPrice: p.CurrentPrice,
				PnL:          p.PnL,
			})
		}
	}

	// 轉換訂單數據
	if s.Orders != nil {
		resp.Orders = make([]web.StrategyOrderResp, 0, len(s.Orders))
		for _, o := range s.Orders {
			resp.Orders = append(resp.Orders, web.StrategyOrderResp{
				OrderID:  o.OrderID,
				Symbol:   o.Symbol,
				Side:     o.Side,
				Price:    o.Price,
				Quantity: o.Quantity,
				Status:   o.Status,
			})
		}
	}

	// 轉換可视化數據
	if s.VisualizationData != nil {
		resp.VisualizationData = s.VisualizationData
	}

	return resp, nil
}

// GetAllStrategyStatusAll 獲取所有幣種下所有策略的運行狀態（聚合）
func (a *symbolManagerWebAdapter) GetAllStrategyStatusAll() ([]web.SymbolStrategyRuntimeItem, error) {
	runtimes := a.manager.List()
	result := make([]web.SymbolStrategyRuntimeItem, 0, len(runtimes))
	for _, rt := range runtimes {
		if rt.StrategyManager == nil {
			continue
		}
		statuses := rt.StrategyManager.GetAllStrategyStatus()
		strategies := make([]web.StrategyRuntimeStatusResponse, 0, len(statuses))
		for _, s := range statuses {
			resp := web.StrategyRuntimeStatusResponse{
				Name:           s.Name,
				Type:           s.Type,
				IsEnabled:      s.IsEnabled,
				IsRunning:      s.IsRunning,
				Weight:         s.Weight,
				AllocatedFunds: s.AllocatedFunds,
				UsedFunds:      s.UsedFunds,
				AvailableFunds: s.AvailableFunds,
				PositionCount:  s.PositionCount,
				OrderCount:     s.OrderCount,
			}
			if s.Statistics != nil {
				resp.Statistics = &web.StrategyStatsResponse{
					TotalTrades: s.Statistics.TotalTrades,
					WinRate:     s.Statistics.WinRate,
					TotalPnL:    s.Statistics.TotalPnL,
					TotalVolume: s.Statistics.TotalVolume,
				}
			}
			if s.Positions != nil {
				resp.Positions = make([]web.StrategyPositionResp, 0, len(s.Positions))
				for _, p := range s.Positions {
					resp.Positions = append(resp.Positions, web.StrategyPositionResp{
						Symbol:       p.Symbol,
						Size:         p.Size,
						EntryPrice:   p.EntryPrice,
						CurrentPrice: p.CurrentPrice,
						PnL:          p.PnL,
					})
				}
			}
			if s.Orders != nil {
				resp.Orders = make([]web.StrategyOrderResp, 0, len(s.Orders))
				for _, o := range s.Orders {
					resp.Orders = append(resp.Orders, web.StrategyOrderResp{
						OrderID:  o.OrderID,
						Symbol:   o.Symbol,
						Side:     o.Side,
						Price:    o.Price,
						Quantity: o.Quantity,
						Status:   o.Status,
					})
				}
			}
			if s.VisualizationData != nil {
				resp.VisualizationData = s.VisualizationData
			}
			strategies = append(strategies, resp)
		}
		result = append(result, web.SymbolStrategyRuntimeItem{
			Exchange:   rt.Config.Exchange,
			Symbol:     rt.Config.Symbol,
			MarketType: rt.Config.GetMarketType(),
			Strategies: strategies,
		})
	}
	return result, nil
}

func init() {
	// 配置 GC 参數
	// 從环境变量读取 GOGC，如果没有则使用默认值 100
	if goGC := os.Getenv("GOGC"); goGC != "" {
		if val, err := strconv.Atoi(goGC); err == nil && val > 0 {
			debug.SetGCPercent(val)
			log.Printf("[INFO] GOGC 設置為: %d", val)
		}
	} else {
		// 默认設置為 100（標准值）
		debug.SetGCPercent(100)
	}
}

func main() {
	// 检查版本参數
	if len(os.Args) > 1 && (os.Args[1] == "-version" || os.Args[1] == "--version") {
		fmt.Printf("QuantMesh Market Maker\n")
		fmt.Printf("Version: %s\n", Version)
		os.Exit(0)
	}

	// 解析調試参數（-debug / --debug）與 --migrate-app-config、--repair-app-config-tables
	debugMode := false
	migrateAppCfg := false
	repairAppCfgTables := false
	filteredArgs := []string{os.Args[0]}
	for _, arg := range os.Args[1:] {
		switch arg {
		case "-debug", "--debug":
			debugMode = true
		case "--migrate-app-config":
			migrateAppCfg = true
		case "--repair-app-config-tables":
			repairAppCfgTables = true
		default:
			filteredArgs = append(filteredArgs, arg)
		}
	}
	if debugMode {
		log.Printf("[INFO] Debug 模式已啟用：Gin 將输出全量请求日志")
	}
	os.Args = filteredArgs
	_ = config.LoadDotEnvIfPresent(".")

	// 注意：不再設置 time.Local，避免竞態条件
	// 時区处理统一使用 utils.GlobalLocation（通過 init() 或 config 設置）
	// 所有時间操作应使用 utils.ToConfiguredTimezone()、utils.ToUTC()、utils.NowConfiguredTimezone() 等工具函數

	// 1. 最早初始化日志存儲（在配置加載之前，使用默认路径）
	// 注意：logs.db 放在 data 目錄下，與其他數據庫文件保持一致，方便 systemd 的 ReadWritePaths 配置
	logStoragePath := "./data/logs.db"
	if len(os.Args) > 2 && os.Args[1] == "--log-db" {
		logStoragePath = os.Args[2]
		os.Args = append(os.Args[:1], os.Args[3:]...)
	}

	logStorage, err := storage.NewLogStorage(logStoragePath)
	if err != nil {
		log.Printf("[WARN] 初始化日志存儲失败: %v，將继续运行但不保存日志到數據库", err)
		logStorage = nil
	} else {
		globalLogStorage = logStorage
		log.Printf("[DEBUG] logStorage 指針: %p", logStorage)
		logger.InitLogStorage(func(level, message, botID string) {
			if logStorage != nil {
				logStorage.WriteLog(level, message, botID)
			} else {
				log.Printf("[ERROR] logStorage 為 nil，無法写入日志")
			}
		})
		log.Printf("[INFO] 日志存儲已初始化: %s", logStoragePath)
	}

	logger.Info("🚀 QuantMesh 做市商系統啟动...")
	logger.Info("📦 版本号: %s", Version)

	// 诊断：注册按需 goroutine 栈快照处理器，用于排查偶发 CPU 100% busy-loop。
	// CPU 异常时执行 `kill -USR1 <pid>` 即可把全部 goroutine 栈写入 logs/，空转的 goroutine 会霸榜现形。
	monitor.StartGoroutineDumpHandler()

	// 可選：第一個命令行參數為一次性導入用主配置 YAML 路徑；省略則僅從主庫 app_config / 引導加載
	mainYAMLPath := ""
	if len(os.Args) > 1 {
		mainYAMLPath = os.Args[1]
	}

	sqlitePath := strings.TrimSpace(os.Getenv("QUANTMESH_SQLITE_PATH"))
	if sqlitePath == "" {
		sqlitePath = "./data/quantmesh.db"
	}

	var cfg *config.Config
	var configComplete bool
	var configFileExisted bool

	if mainYAMLPath != "" {
		_, err = os.Stat(mainYAMLPath)
		configFileExisted = err == nil
		if !configFileExisted {
			logger.Fatalf("❌ 找不到主配置 YAML: %s", mainYAMLPath)
		}
		cfg, err = config.LoadConfig(mainYAMLPath)
		if err != nil {
			logger.Fatalf("❌ 加載配置失败: %v", err)
		}
		configComplete = cfg.App.CurrentExchange != "" &&
			len(cfg.Exchanges) > 0 &&
			cfg.Exchanges[cfg.App.CurrentExchange].APIKey != "" &&
			cfg.Exchanges[cfg.App.CurrentExchange].SecretKey != "" &&
			len(cfg.Trading.Symbols) > 0 &&
			cfg.Trading.Symbols[0].Symbol != ""
		if !configComplete {
			logger.Info("ℹ️ 配置不完整，僅啟动 Web 服務，请通過引導页面完成配置")
		}
	} else {
		cfgFromDB, errDB := storage.LoadConfigFromAppConfigDBIfExists(sqlitePath)
		if errDB != nil {
			logger.Warn("⚠️ 嘗試從數據庫加載配置失敗: %v", errDB)
		} else if cfgFromDB != nil {
			cfg = cfgFromDB
			configComplete = cfg.App.CurrentExchange != "" &&
				len(cfg.Exchanges) > 0 &&
				cfg.Exchanges[cfg.App.CurrentExchange].APIKey != "" &&
				cfg.Exchanges[cfg.App.CurrentExchange].SecretKey != "" &&
				len(cfg.Trading.Symbols) > 0 &&
				cfg.Trading.Symbols[0].Symbol != ""
			logger.Info("ℹ️ 已從主庫 app_config 加載（未指定命令行 YAML 路徑）")
		}
		if cfg == nil {
			logger.Info("ℹ️ 主庫無快照，使用最小化配置（僅啟用 Web 服務）")
			cfg = config.CreateMinimalConfig()
			configComplete = false
		}
	}

	config.ApplyStoragePathFromEnv(cfg)
	config.ApplyDatabaseDSNFromEnv(cfg)

	if err := utils.SetLocation(cfg.System.Timezone); err != nil {
		logger.Warn("⚠️ 加載時区 %s 失败: %v，將使用默认時区 Asia/Shanghai", cfg.System.Timezone, err)
		utils.SetLocation("Asia/Shanghai")
	} else {
		logger.Info("✅ 系统時区設置為: %s", cfg.System.Timezone)
	}
	logger.SetLocation(utils.GlobalLocation)

	if debugMode {
		cfg.System.LogLevel = "debug"
	}

	logLevel := logger.ParseLogLevel(cfg.System.LogLevel)
	logger.SetLevel(logLevel)
	logger.Info("日誌級別設置為: %s", logLevel.String())

	// 初始化 i18n 系統
	logLang := cfg.System.LogLanguage
	if logLang == "" {
		logLang = "zh-CN" // 預設中文
	}
	if err := i18n.Init(logLang); err != nil {
		logger.Warn("⚠️ 初始化 i18n 失敗: %v，將使用預設語言", err)
	} else {
		logger.Info("✅ i18n 系統已初始化，日誌語言: %s", logLang)
	}

	// 日誌輸出使用繁體中文（當選擇中文時）
	logLangForLogger := logLang
	if logLang == "zh-CN" || logLang == "zh" {
		logLangForLogger = "zh-TW"
	}
	logger.SetLogLanguage(logLangForLogger)
	logger.SetTranslateFunc(func(key string, data ...interface{}) string {
		return i18n.TWithLang(logLangForLogger, key, data...)
	})

	logger.Info("✅ 配置加載成功: 交易對數量=%d, 當前預設交易所=%s",
		len(cfg.Trading.Symbols), cfg.App.CurrentExchange)

	// 啟動耗時追蹤（用於優化 Web API 就緒時間）
	startTime := time.Now()
	logger.Info("⏱️ [啟動] 開始初始化 (t=0)")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 啟動定期日誌清理任務（在 ctx 定義之後）
	if globalLogStorage != nil && cfg.System.LogCleanup.Enabled {
		lc := cfg.System.LogCleanup
		schedule := lc.Schedule
		if schedule == "" {
			schedule = "02:00"
		}
		retentionDays := lc.RetentionDays
		if retentionDays <= 0 {
			retentionDays = 7
		}
		levelsToClean := lc.LevelsToClean
		if len(levelsToClean) == 0 {
			levelsToClean = []string{"INFO", "WARN"}
		}

		go func() {
			// 解析執行時间 HH:MM
			hour, min := 2, 0
			if t, err := time.Parse("15:04", schedule); err == nil {
				hour, min = t.Hour(), t.Minute()
			}

			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()

			now := time.Now()
			nextCleanup := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, now.Location())
			if nextCleanup.Before(now) {
				nextCleanup = nextCleanup.Add(24 * time.Hour)
			}
			initialDelay := nextCleanup.Sub(now)
			logger.Info("🧹 日志清理任務已啟用，下次執行: %s (保留 %d 天前的 %v)", nextCleanup.Format("2006-01-02 15:04"), retentionDays, levelsToClean)

			initialTimer := time.NewTimer(initialDelay)
			defer initialTimer.Stop()

			runCleanup := func() {
				logger.Info("🧹 開始定期清理日誌...")
				rowsAffected, err := globalLogStorage.CleanOldLogsByLevel(retentionDays, levelsToClean)
				if err != nil {
					logger.Warn("⚠️ 清理日志失败: %v", err)
				} else {
					logger.Info("✅ 已清理 %d 条 %v 级别日志（%d天前）", rowsAffected, levelsToClean, retentionDays)
				}
				if err := globalLogStorage.Vacuum(); err != nil {
					logger.Warn("⚠️ 數據库优化失败: %v", err)
				} else {
					logger.Info("✅ 日志數據库优化完成")
				}
			}

			select {
			case <-ctx.Done():
				return
			case <-initialTimer.C:
				runCleanup()
			}

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					runCleanup()
				}
			}
		}()
	}

	// 事件總線 & 通知 & 存儲
	logger.Info("🔧 正在初始化事件總線...")
	// 增加缓冲区大小到5000，避免事件队列满
	eventBus := event.NewEventBus(5000)

	logger.Info("🔧 正在初始化存儲服務...")
	// 若已配置路徑與類型則強制視為啟用（與 config.Validate 一致，避免設置頁開關已開但進程未重啟或配置未寫入導致回測不可用）
	if cfg.Storage.Path != "" && cfg.Storage.Type != "" {
		cfg.Storage.Enabled = true
	}
	willAutoMigrate := !migrateAppCfg && os.Getenv("QUANTMESH_SKIP_AUTO_MIGRATE") != "1" &&
		configComplete && configFileExisted
	// 遷移主配置入庫時必須能打開主庫
	if migrateAppCfg || willAutoMigrate {
		cfg.Storage.Enabled = true
		if cfg.Storage.Type == "" {
			cfg.Storage.Type = "sqlite"
		}
		storageType := strings.ToLower(strings.TrimSpace(cfg.Storage.Type))
		if cfg.Storage.Path == "" && storageType != "mysql" && storageType != "postgres" && storageType != "postgresql" {
			cfg.Storage.Path = "./data/quantmesh.db"
		}
	}
	storageService, err := storage.NewStorageService(cfg, ctx)
	if err != nil {
		logger.Warn("⚠️ 初始化存儲服務失败: %v (將继续运行，但不保存數據)", err)
		storageService = nil
	} else if cfg.Storage.Enabled {
		storageService.Start()
	}
	if storageService != nil && storageService.GetStorage() == nil {
		logger.Warn("⚠️ 存儲服務已創建但 GetStorage() 為空（storage.enabled 可能為 false），回測等依賴存儲的功能將不可用")
	}
	logger.Info("✅ 存儲服務初始化完成 (enabled=%v, storage!=nil=%v)", cfg.Storage.Enabled, storageService != nil && storageService.GetStorage() != nil)
	logger.Info("⏱️ [啟動] 存儲服務完成 (耗時: %v)", time.Since(startTime))

	// 幂等補建 app_config / bot_configs 文檔表（舊部署可能缺少該遷移）
	if storageService != nil {
		if st := storageService.GetStorage(); st != nil {
			if ss, ok := st.(*storage.SQLStorage); ok {
				if err := ss.EnsureAppConfigDocumentTables(); err != nil {
					logger.Warn("⚠️ 確保 app_config 文檔表失敗: %v", err)
				}
			}
		}
	}
	if repairAppCfgTables {
		if storageService == nil || storageService.GetStorage() == nil {
			logger.Fatalf("❌ --repair-app-config-tables 需要 storage.enabled=true 且主庫可連接")
		}
		ss, ok := storageService.GetStorage().(*storage.SQLStorage)
		if !ok {
			logger.Fatalf("❌ --repair-app-config-tables 需要 SQLite 或 MySQL 主庫實例")
		}
		if err := ss.EnsureAppConfigDocumentTables(); err != nil {
			logger.Fatalf("❌ 修復 app_config 表失敗: %v", err)
		}
		logger.Info("✅ app_config / bot_configs 文檔表已確保存在（幂等）")
		os.Exit(0)
	}

	if migrateAppCfg {
		if storageService == nil || storageService.GetStorage() == nil {
			logger.Fatalf("❌ --migrate-app-config 需要可用的主庫存儲（請配置 storage.path 或檢查上述警告）")
		}
		botsDir := os.Getenv("QUANTMESH_BOTS_DIR")
		if botsDir == "" {
			botsDir = "./bots"
		}
		yamlImport := mainYAMLPath
		if yamlImport == "" {
			yamlImport = strings.TrimSpace(os.Getenv("QUANTMESH_IMPORT_YAML"))
		}
		if yamlImport == "" {
			if _, e := os.Stat("config.yaml"); e == nil {
				yamlImport = "config.yaml"
			}
		}
		if yamlImport == "" {
			logger.Fatalf("❌ --migrate-app-config 需要 YAML 路徑：命令行第一參數、環境變量 QUANTMESH_IMPORT_YAML，或當前目錄存在 config.yaml")
		}
		if _, err := storage.MigrateYAMLToAppConfigDB(ctx, storageService.GetStorage(), yamlImport, botsDir, storage.MigrateYAMLModeCLI); err != nil {
			logger.Fatalf("❌ 遷移失敗: %v", err)
		}
		logger.Info("✅ 已將主配置與 Bot 配置寫入數據庫（下次啟動將優先從 app_config 加載，可用 QUANTMESH_USE_APP_CONFIG=0 禁用）")
		os.Exit(0)
	}

	if willAutoMigrate && storageService != nil && storageService.GetStorage() != nil {
		if err := config.EnsureEnvFileIfMissing(".", cfg); err != nil {
			logger.Warn("⚠️ 自動生成 .env 失敗（可忽略）: %v", err)
		}
		botsDir := os.Getenv("QUANTMESH_BOTS_DIR")
		if botsDir == "" {
			botsDir = "./bots"
		}
		did, migErr := storage.MigrateYAMLToAppConfigDB(ctx, storageService.GetStorage(), mainYAMLPath, botsDir, storage.MigrateYAMLModeAuto)
		if migErr != nil {
			logger.Warn("⚠️ 自動遷移主配置入庫失敗（可稍後使用 --migrate-app-config）: %v", migErr)
		} else if did {
			logger.Info("✅ 已自動將主配置與 Bot 配置寫入數據庫（來源: 命令行指定的 YAML）")
			if mainYAMLPath != "" {
				if backup, err := config.RenameConfigYAMLToBackup(mainYAMLPath); err != nil {
					logger.Warn("⚠️ 已入庫但無法歸檔源 YAML（請手動改名或檢查目錄權限）: %v", err)
				} else {
					logger.Info("✅ 已歸檔源配置文件: %s", backup)
				}
			}
		}
	}

	if storageService != nil && storageService.GetStorage() != nil {
		if err := storage.ApplyAppConfigFromDBIfPresent(storageService.GetStorage(), &cfg); err != nil {
			logger.Fatalf("❌ %v", err)
		}
		if ss, ok := storageService.GetStorage().(*storage.SQLStorage); ok && ss != nil {
			doc, derr := ss.GetAppConfigDocument(context.Background())
			if derr != nil {
				logger.Warn("⚠️ 讀取 app_config 狀態失敗: %v", derr)
			} else if doc == nil || doc.Revision < 1 || strings.TrimSpace(doc.Content) == "" {
				if _, serr := storage.SaveAppConfigSnapshot(context.Background(), storageService.GetStorage(), cfg, "bootstrap", "main.go"); serr != nil {
					logger.Warn("⚠️ 寫入初始 app_config 失敗: %v", serr)
				} else {
					logger.Info("✅ 已將當前配置寫入主庫 app_config（首次快照）")
				}
			}
		}
		// app_config 覆蓋 YAML 後，仍允許 .env / 環境變量覆蓋 SQLite 路徑與主庫 DSN（密鑰不進庫）
		config.ApplyStoragePathFromEnv(cfg)
		config.ApplyDatabaseDSNFromEnv(cfg)
		// 若從數據庫覆蓋了配置，同步時区、日誌級別與 i18n
		if err := utils.SetLocation(cfg.System.Timezone); err != nil {
			logger.Warn("⚠️ 加載時区 %s 失败: %v，將使用默认時区 Asia/Shanghai", cfg.System.Timezone, err)
			utils.SetLocation("Asia/Shanghai")
		} else {
			logger.SetLocation(utils.GlobalLocation)
		}
		logLangDB := cfg.System.LogLanguage
		if logLangDB == "" {
			logLangDB = "zh-CN"
		}
		if err := i18n.Init(logLangDB); err != nil {
			logger.Warn("⚠️ i18n 重新初始化失敗: %v", err)
		}
		logLangForLoggerDB := logLangDB
		if logLangDB == "zh-CN" || logLangDB == "zh" {
			logLangForLoggerDB = "zh-TW"
		}
		logger.SetLogLanguage(logLangForLoggerDB)
		logger.SetTranslateFunc(func(key string, data ...interface{}) string {
			return i18n.TWithLang(logLangForLoggerDB, key, data...)
		})
		logger.SetLevel(logger.ParseLogLevel(cfg.System.LogLevel))
	}

	// 初始化配置管理器
	var configManager *cfgmgr.ConfigManager
	var configStorage storage.ConfigStorage

	// 根据配置的数据库类型初始化配置存储
	dbType := cfg.Database.Type
	if dbType == "" {
		dbType = "sqlite" // 默认使用 SQLite
	}

	logger.Info("🔧 正在初始化配置存儲 (数据库类型: %s)...", dbType)

	if dbType == "sqlite" {
		// SQLite 使用存储路径
		configDBPath := cfg.Storage.Path
		if configDBPath == "" {
			configDBPath = "./data/config.db"
		}
		configStorage, err = storage.NewConfigStorage(configDBPath)
		if err != nil {
			logger.Warn("⚠️ 初始化 SQLite 配置存儲失败: %v", err)
		}
	} else if dbType == "mysql" || dbType == "postgres" || dbType == "postgresql" {
		// 遠端 SQL 使用 DSN；Supabase 使用 postgres/postgresql DSN。
		dsn := cfg.Database.DSN
		if dsn == "" {
			dsn = storage.ResolveSQLStorageDSN(cfg, dbType)
		}
		configStorage, err = storage.NewConfigStorageByType(dbType, dsn)
		if err != nil {
			logger.Warn("⚠️ 初始化 %s 配置存儲失败: %v，將回退使用 SQLite", dbType, err)
			// 遠端庫失败时回退到 SQLite，避免服务无法启动
			configDBPath := cfg.Storage.Path
			if configDBPath == "" {
				configDBPath = "./data/config.db"
			}
			configStorage, err = storage.NewConfigStorage(configDBPath)
			if err != nil {
				logger.Warn("⚠️ SQLite 回退也失败: %v", err)
			} else {
				dbType = "sqlite"
				logger.Info("ℹ️ 已回退使用 SQLite 配置存儲: %s", configDBPath)
			}
		}
	} else {
		configStorage, err = storage.NewConfigStorageByType(dbType, cfg.Storage.Path)
		if err != nil {
			logger.Warn("⚠️ 初始化配置存儲失败: %v", err)
		}
	}

	if configStorage != nil {
		logger.Info("✅ 配置存儲已初始化 (类型: %s)", dbType)
		logger.Info("🔧 正在初始化配置管理器...")
		configManager = cfgmgr.NewConfigManager(cfg, configStorage)
		if err := configManager.Start(); err != nil {
			logger.Warn("⚠️ 初始化配置管理器失败: %v", err)
		} else {
			logger.Info("✅ 配置管理器已啟动")
		}
	}

	// 初始化通知服务（使用配置管理器）
	logger.Info("🔧 正在初始化通知服務...")
	var notifier *notify.NotificationService
	if configManager != nil {
		notifier = notify.NewNotificationService(cfg, configManager)
	} else {
		notifier = notify.NewNotificationService(cfg, nil)
	}

	// 运行 K 线文件迁移（一次性迁移现有文件到统一管理系统）
	if storageService != nil {
		if st := storageService.GetStorage(); st != nil {
			// 注意：现在使用 GormStorage，不再是 SQLStorage
			// 迁移逻辑需要更新
			logger.Info("✅ 存儲服務已啟用 (GORM)")
		}
	}

	// 合规：审计日志與 OSS 上傳
	var auditLogger *storage.AuditTradeLogger
	var ossUploader *storage.OSSUploader
	if cfg.Compliance.Enabled && cfg.Compliance.AuditLog.Enabled {
		auditLogger, err = storage.NewAuditTradeLogger(cfg.Compliance.AuditLog.Directory, cfg.Compliance.AuditLog.Format)
		if err != nil {
			logger.Warn("⚠️ 初始化合规审计日志失败: %v", err)
		} else {
			storage.SetGlobalAuditLogger(auditLogger)
			logger.Info("✅ 合规审计日志已啟用 (格式: %s)", cfg.Compliance.AuditLog.Format)
		}
	}
	if cfg.Compliance.Enabled && cfg.Compliance.OSS.Enabled && auditLogger != nil {
		ossUploader, err = storage.NewOSSUploader(storage.OSSConfig{
			Endpoint:        cfg.Compliance.OSS.Endpoint,
			Bucket:          cfg.Compliance.OSS.Bucket,
			AccessKeyID:     cfg.Compliance.OSS.AccessKeyID,
			AccessKeySecret: cfg.Compliance.OSS.AccessKeySecret,
			Prefix:          cfg.Compliance.OSS.Prefix,
			UploadTime:      cfg.Compliance.OSS.UploadTime,
			AuditDir:        cfg.Compliance.AuditLog.Directory,
		})
		if err != nil {
			logger.Warn("⚠️ 初始化 OSS 上傳器失败: %v", err)
		} else {
			ossUploader.Start()
		}
	}

	// 初始化數據库（可選，用於未来迁移）
	var db database.Database
	if cfg.Database.Type != "" && cfg.Database.DSN != "" {
		dbConfig := &database.Config{
			Type:            cfg.Database.Type,
			DSN:             cfg.Database.DSN,
			MaxOpenConns:    cfg.Database.MaxOpenConns,
			MaxIdleConns:    cfg.Database.MaxIdleConns,
			ConnMaxLifetime: time.Duration(cfg.Database.ConnMaxLifetime) * time.Second,
			LogLevel:        cfg.Database.LogLevel,
		}
		db, err = database.NewDatabase(dbConfig)
		if err != nil {
			logger.Warn("⚠️ 初始化數據库失败: %v (將继续使用現有存儲)", err)
			db = nil
		} else {
			defer db.Close()
			logger.Info("✅ 數據库已初始化 (類型: %s)", cfg.Database.Type)

			// 初始化 AI 异步任務系统
			logger.Info("🔧 正在初始化 AI 异步任務系统...")
			taskService := service.NewTaskService(db)
			aiService := service.NewAIService()
			taskProcessor := processor.NewTaskProcessor(taskService, aiService)

			// 設置全局任務服務，供 GeminiClient 使用
			ai.GlobalTaskService = taskService

			// 啟动任務处理器
			go taskProcessor.Start()
			logger.Info("✅ AI 异步任務系统已啟动")
		}
	}

	// 初始化事件中心
	logger.Info("🔧 正在初始化事件中心...")
	eventCenterConfig := &event.EventCenterConfig{
		Enabled:                  cfg.EventCenter.Enabled,
		PriceVolatilityThreshold: cfg.EventCenter.PriceVolatilityThreshold,
		MonitoredSymbols:         cfg.EventCenter.MonitoredSymbols,
		CleanupInterval:          cfg.EventCenter.CleanupInterval,
		Retention: event.RetentionConfig{
			CriticalDays:     cfg.EventCenter.Retention.CriticalDays,
			WarningDays:      cfg.EventCenter.Retention.WarningDays,
			InfoDays:         cfg.EventCenter.Retention.InfoDays,
			CriticalMaxCount: cfg.EventCenter.Retention.CriticalMaxCount,
			WarningMaxCount:  cfg.EventCenter.Retention.WarningMaxCount,
			InfoMaxCount:     cfg.EventCenter.Retention.InfoMaxCount,
		},
	}

	var eventCenter *event.EventCenter
	if db != nil {
		eventCenter = event.NewEventCenter(db, eventBus, notifier, eventCenterConfig)

		// 設置事件中心控制器，用於 Web API 动態控制
		web.SetEventCenterController(eventCenter)

		// 如果配置啟用，则啟动事件中心
		if eventCenterConfig.Enabled {
			if err := eventCenter.Start(); err != nil {
				logger.Warn("⚠️ 啟动事件中心失败: %v", err)
			}
		} else {
			logger.Info("⏸️ 事件中心未啟用（可通過 Web API 动態啟用）")
		}
		defer eventCenter.Stop()
	} else {
		logger.Warn("⚠️ 數據库未初始化，事件中心將不可用")
	}
	logger.Info("✅ 事件中心初始化完成")
	logger.Info("⏱️ [啟動] 事件中心完成 (耗時: %v)", time.Since(startTime))

	// Web 服務器（提前啟動，讓 API 盡快可用；交易相關接口在 SymbolManager 就緒前返回 503）
	var webServer *web.WebServer
	if cfg.Web.Enabled {
		logger.Info("🌐 开始初始化 Web 服務器（提前啟動優化）...")
		// 初始化密碼管理器
		passwordManager, err := web.NewPasswordManager("./data")
		if err != nil {
			logger.Error("❌ 初始化密碼管理器失败: %v", err)
		} else {
			web.SetPasswordManager(passwordManager)
			logger.Info("✅ 密碼管理器已初始化")
		}

		// 初始化 WebAuthn 管理器
		var rpID string
		var rpOrigin string
		if domain := os.Getenv("DOMAIN"); domain != "" {
			rpID = domain
			if cfg.Web.TLS != nil && cfg.Web.TLS.Enabled {
				rpOrigin = fmt.Sprintf("https://%s", domain)
			} else {
				rpOrigin = fmt.Sprintf("http://%s", domain)
			}
		} else if cfg.Web.Domain != "" {
			rpID = cfg.Web.Domain
			if cfg.Web.TLS != nil && cfg.Web.TLS.Enabled {
				rpOrigin = fmt.Sprintf("https://%s", cfg.Web.Domain)
			} else {
				rpOrigin = fmt.Sprintf("http://%s", cfg.Web.Domain)
			}
		} else {
			host := cfg.Web.Host
			if host == "" || host == "0.0.0.0" {
				host = "localhost"
			}
			rpID = host
			if cfg.Web.TLS != nil && cfg.Web.TLS.Enabled {
				if cfg.Web.Port == 443 {
					rpOrigin = fmt.Sprintf("https://%s", host)
				} else {
					rpOrigin = fmt.Sprintf("https://%s:%d", host, cfg.Web.Port)
				}
			} else {
				if cfg.Web.Port == 80 {
					rpOrigin = fmt.Sprintf("http://%s", host)
				} else {
					rpOrigin = fmt.Sprintf("http://%s:%d", host, cfg.Web.Port)
				}
			}
		}
		webauthnManager, err := web.NewWebAuthnManager(&webAuthnLoggerAdapter{}, "./data", rpID, rpOrigin)
		if err != nil {
			logger.Error("❌ 初始化 WebAuthn 管理器失败: %v", err)
		} else {
			web.SetWebAuthnManager(webauthnManager)
			logger.Info("✅ WebAuthn 管理器已初始化 (rpID=%s, rpOrigin=%s)", rpID, rpOrigin)
		}

		if storageService != nil && storageService.GetStorage() != nil {
			web.SetPrimaryStorageForAppConfig(storageService.GetStorage())
			geminiusage.SetPersist(func(e geminiusage.Entry) {
				st := web.GetPrimaryStorageForAppConfig()
				if st == nil {
					return
				}
				rec := &storage.GeminiUsageRecord{
					CalledAt:     e.At,
					Model:        e.Model,
					Source:       e.Source,
					InputTokens:  e.InputTokens,
					OutputTokens: e.OutputTokens,
					DurationMs:   e.DurationMs,
				}
				if err := st.SaveGeminiUsageRecord(rec); err != nil {
					logger.Debug("📊 Gemini 用量寫庫失敗: %v", err)
				}
			})
		}
		fileConfigManager := web.NewFileConfigManager("")
		fileConfigManager.SetRuntimeConfig(cfg)
		web.SetFileConfigManager(fileConfigManager)
		web.SetGlobalConfig(cfg)
		logger.Info("✅ 配置管理器已初始化")

		web.SetVersion(Version)
		logger.Info("✅ 版本号已設置: %s", Version)

		// 初始化 Bot 配置文件管理器
		if err := web.InitBotConfigManager("."); err != nil {
			logger.Warn("⚠️ 初始化 Bot 配置管理器失败: %v", err)
		}

		// 初始化策略模板管理器
		if err := web.InitStrategyTemplateManager("."); err != nil {
			logger.Warn("⚠️ 初始化策略模板管理器失败: %v", err)
		}

		hotReloader := config.NewHotReloader(cfg)
		web.SetConfigHotReloader(hotReloader)
		logger.Info("✅ 配置热更新器已初始化")

		if globalLogStorage != nil {
			logStorageAdapter := web.NewLogStorageAdapter(globalLogStorage)
			web.SetLogStorageProvider(logStorageAdapter)
			logger.Info("✅ 日志存儲提供者已設置")
		}

		logger.Info("🔧 正在創建 Web 服務器實例...")
		webServer = web.NewWebServer(cfg)
		if webServer == nil {
			logger.Warn("⚠️ Web 服務器未創建（可能配置中 Web.Enabled=false）")
		} else {
			logger.Info("🔧 正在啟动 Web 服務器...")
			if err := webServer.Start(ctx); err != nil {
				logger.Error("❌ 啟动Web服務器失败: %v", err)
			} else {
				logger.Info("✅ Web服務器已啟动，可通過 http://%s:%d 访问", cfg.Web.Host, cfg.Web.Port)
				logger.Info("⏱️ [啟動] Web API 已就緒 (耗時: %v)", time.Since(startTime))
				// 設置配置存儲和配置管理器到 Web（與 storageService 無關，避免 storage 初始化失敗時 bots/create 等 API 返回 503）
				if configStorage != nil {
					web.SetConfigStorage(configStorage)
				}
				if configManager != nil {
					web.SetConfigManager(configManager)
					logger.Info("✅ 配置管理器已設置到 Web 服務器")
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	} else {
		logger.Info("ℹ️ Web 服務未啟用（配置中 web.enabled=false）")
	}

	// 舊的事件处理器（保留用於存儲服務）
	// 使用 worker pool 模式，限制並发數量，避免 goroutine 泄漏
	// 重要：Subscribe() 必須在循環外調用一次，否則每次循環都會創建新 channel 導致嚴重內存泄漏
	eventCh := eventBus.Subscribe()
	eventWorkerPool := make(chan struct{}, 10) // 最多10個並发 worker
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-eventCh:
				if !ok {
					return // EventBus 已關閉
				}
				if evt == nil {
					continue
				}
				// 使用 worker pool 限制並发
				eventWorkerPool <- struct{}{}
				go func(e *event.Event) {
					defer func() { <-eventWorkerPool }()
					if storageService != nil {
						storageService.Save(string(e.Type), e.Data)
					}
				}(evt)
			}
		}
	}()

	// 初始化 Prometheus 系统指標采集器
	logger.Info("🔧 正在初始化 Prometheus 系统指標采集器...")
	systemMetricsCollector := metrics.NewSystemMetricsCollector(10 * time.Second)
	systemMetricsCollector.Start()
	logger.Info("✅ Prometheus 系统指標采集器已啟动")

	// 初始化分布式鎖（多實例模式）
	logger.Info("🔧 正在初始化分布式鎖...")
	var distributedLock lock.DistributedLock
	lockConfig := &lock.Config{
		Enabled:    cfg.DistributedLock.Enabled,
		Type:       cfg.DistributedLock.Type,
		Prefix:     cfg.DistributedLock.Prefix,
		DefaultTTL: time.Duration(cfg.DistributedLock.DefaultTTL) * time.Second,
		Redis: lock.RedisConfig{
			Addr:     cfg.DistributedLock.Redis.Addr,
			Password: cfg.DistributedLock.Redis.Password,
			DB:       cfg.DistributedLock.Redis.DB,
			PoolSize: cfg.DistributedLock.Redis.PoolSize,
		},
	}
	distributedLock, err = lock.NewDistributedLock(lockConfig)
	if err != nil {
		logger.Fatalf("❌ 初始化分布式鎖失败: %v", err)
	}
	defer distributedLock.Close()

	if cfg.DistributedLock.Enabled {
		logger.Info("✅ 分布式鎖已啟用 (類型: %s, 實例: %s)", cfg.DistributedLock.Type, cfg.Instance.ID)
	} else {
		logger.Info("ℹ️ 分布式鎖未啟用（單机模式）")
	}

	// 初始化記憶體管理器
	logger.Info("🔧 正在初始化記憶體管理器...")
	memoryManager := monitor.NewMemoryManager(cfg, ctx)
	memoryManager.Start()
	logger.Info("✅ 記憶體管理器已啟动")

	// 初始化 Watchdog（系统監控）
	logger.Info("🔧 正在初始化系统監控...")
	var watchdog *monitor.Watchdog
	if cfg.Watchdog.Enabled {
		watchdog = monitor.NewWatchdog(cfg, storageService, globalLogStorage, notifier)
		if err := watchdog.Start(ctx); err != nil {
			logger.Error("❌ 啟动 Watchdog 失败: %v", err)
		} else {
			logger.Info("✅ Watchdog 系统監控已啟动")
		}
	}

	// 初始化K线数据收集器（按需創建：僅為實際使用的交易所創建適配器）
	logger.Info("🔧 正在初始化K线数据收集器...")
	var klineCollector *monitor.KlineCollector
	if storageService != nil {
		// 收集實際使用的交易所（Bots 或 Trading.Symbols），避免為未使用的交易所創建客戶端
		usedExchanges := make(map[string]bool)
		for _, bot := range cfg.Bots {
			if bot.Exchange != "" {
				usedExchanges[strings.ToLower(bot.Exchange)] = true
			}
		}
		for _, sym := range cfg.Trading.Symbols {
			if sym.Exchange != "" {
				usedExchanges[strings.ToLower(sym.Exchange)] = true
			} else if cfg.App.CurrentExchange != "" {
				usedExchanges[strings.ToLower(cfg.App.CurrentExchange)] = true
			}
		}
		// 若無實際交易對，僅使用 binance（公開 K 線 API 無需認證）
		if len(usedExchanges) == 0 {
			if _, ok := cfg.Exchanges["binance"]; ok {
				usedExchanges["binance"] = true
			}
		}
		// 後備：若仍無，使用主要交易所中已配置的
		if len(usedExchanges) == 0 {
			for _, ex := range []string{"binance", "okx", "bybit", "bitget", "gate"} {
				if _, exists := cfg.Exchanges[ex]; exists {
					usedExchanges[ex] = true
					break
				}
			}
		}

		exchanges := make(map[string]exchange.IExchange)
		for exchangeName := range usedExchanges {
			if _, exists := cfg.Exchanges[exchangeName]; !exists {
				continue
			}
			ex, err := exchange.NewExchange(cfg, exchangeName, "BTCUSDT", "futures")
			if err != nil {
				logger.Warn("⚠️ 创建交易所实例失败 %s: %v", exchangeName, err)
				continue
			}
			exchanges[exchangeName] = ex
		}

		if len(exchanges) > 0 {
			klineCollector = monitor.NewKlineCollector(cfg, storageService, exchanges)
			if err := klineCollector.Start(); err != nil {
				logger.Error("❌ 啟动K线数据收集器失败: %v", err)
			} else {
				exchangeNames := make([]string, 0, len(exchanges))
				for k := range exchanges {
					exchangeNames = append(exchangeNames, k)
				}
				sort.Strings(exchangeNames)
				logger.Info("✅ K线数据收集器已啟动 (交易所: %v)", exchangeNames)
				logger.Info("⏱️ [啟動] K線收集器完成 (耗時: %v)", time.Since(startTime))
				// 注入到Web层
				web.SetKlineCollector(klineCollector)
				defer klineCollector.Stop()
			}
		} else {
			logger.Warn("⚠️ 没有可用的交易所实例，K线数据收集器将不可用")
		}
	} else {
		logger.Warn("⚠️ 存储服务未初始化，K线数据收集器将不可用")
	}

	// 初始化插件系统
	var pluginLoader *plugin.PluginLoader
	if cfg.Plugins.Enabled {
		logger.Info("🔌 开始加載插件系统...")
		pluginLoader = plugin.NewPluginLoader()

		// 從目錄加載所有插件
		pluginDir := cfg.Plugins.Directory
		if pluginDir == "" {
			pluginDir = "./plugins"
		}

		logger.Info("📂 插件目錄: %s", pluginDir)
		if err := pluginLoader.LoadPluginsFromDirectory(pluginDir, cfg.Plugins.Licenses); err != nil {
			logger.Warn("⚠️ 加載插件失败: %v", err)
		} else {
			// 初始化每個已加載的插件
			loadedPlugins := pluginLoader.ListPlugins()
			logger.Info("📦 已发現 %d 個插件", len(loadedPlugins))

			for _, p := range loadedPlugins {
				pluginConfig, exists := cfg.Plugins.Config[p.Name]
				if !exists {
					pluginConfig = make(map[string]interface{})
				}

				if err := pluginLoader.InitializePlugin(p.Name, pluginConfig); err != nil {
					logger.Warn("⚠️ 初始化插件 %s 失败: %v", p.Name, err)
				} else {
					logger.Info("✅ 插件 %s (版本 %s) 初始化成功", p.Name, p.Version)
				}
			}

			logger.Info("✅ 插件系统啟动完成")
		}

		// 在程序退出時卸載所有插件
		defer func() {
			if pluginLoader != nil {
				pluginLoader.UnloadAll()
				logger.Info("✅ 所有插件已卸載")
			}
		}()
	} else {
		logger.Info("ℹ️ 插件系统未啟用")
	}

	logger.Info("⏱️ [啟動] 插件系統完成 (耗時: %v)", time.Since(startTime))

	symbolManager := NewSymbolManager(cfg, eventBus, storageService, distributedLock, mainYAMLPath)
	symbolManager.GetBotManager().StartFeeRateRefreshLoop(ctx)
	SetCurrentSymbolManager(symbolManager) // 让 MCP 写工具能找到 BotManager

	// 創建 SymbolManager 适配器（用於 Web API）
	symbolManagerAdapter := &symbolManagerWebAdapter{
		manager:         symbolManager,
		ctx:             ctx,
		cfg:             cfg,
		eventBus:        eventBus,
		storageService:  storageService,
		distributedLock: distributedLock,
	}
	web.RegisterSymbolManager(symbolManagerAdapter)
	web.RegisterStrategyRuntimeProvider(symbolManagerAdapter) // 注册策略運行時提供者
	web.RegisterBotManagerProvider(&botManagerProviderAdapter{manager: symbolManager})
	web.RegisterBotExtendedProvider(&botExtendedProviderAdapter{manager: symbolManager}) // 註冊擴展的 Bot 提供者

	// 初始化全局熔斷器
	if cfg.CircuitBreaker.Enabled {
		logger.Info("🔧 正在初始化全局熔斷器...")
		circuitBreaker := risk.NewGlobalCircuitBreaker(
			&cfg.CircuitBreaker,
			eventBus,
			&botManagerProviderAdapter{manager: symbolManager},
		)
		web.SetGlobalCircuitBreaker(circuitBreaker)
		logger.Info("✅ 全局熔斷器已初始化並啟用")
	}

	// 初始化紧急操作中心
	if cfg.EmergencyCenter.Enabled {
		logger.Info("🚨 正在初始化紧急操作中心...")
		emergencyCenter := risk.NewEmergencyCenter(
			&cfg.EmergencyCenter,
			eventBus,
			&botManagerProviderAdapter{manager: symbolManager},
		)
		web.SetEmergencyCenter(emergencyCenter)
		logger.Info("✅ 紧急操作中心已初始化並啟用")
	}

	// 初始化动态止损管理器
	if cfg.DynamicStopLoss.Enabled {
		logger.Info("🎯 正在初始化动态止损管理器...")
		dynamicSLManager := risk.NewDynamicStopLossManager(
			&cfg.DynamicStopLoss,
			eventBus,
			&botManagerProviderAdapter{manager: symbolManager},
		)
		dynamicSLManager.Start()
		web.SetDynamicStopLossManager(dynamicSLManager)
		logger.Info("✅ 动态止损管理器已初始化並啟用")

		// 程序退出时停止
		defer dynamicSLManager.Stop()
	}

	// 只有在配置完整時才啟动交易系统（優先使用 Bots，兼容舊 Symbols）
	var firstRuntime *SymbolRuntime
	if configComplete {
		botCfgs := cfg.Bots
		if len(botCfgs) == 0 && len(cfg.Trading.Symbols) > 0 {
			// 兼容：若 Bots 為空但 Symbols 有數據，現場轉換
			for _, symCfg := range cfg.Trading.Symbols {
				exCfg, ok := cfg.Exchanges[symCfg.Exchange]
				testnet := false
				if ok {
					testnet = exCfg.Testnet
				}
				botCfgs = append(botCfgs, config.SymbolConfigToBotConfig(symCfg, testnet))
			}
		}
		for _, botCfg := range botCfgs {
			botID := botCfg.ID
			if botID == "" {
				botID = config.GenerateBotID(botCfg.Exchange, botCfg.Symbol, botCfg.GetMarketType())
			}
			if !botCfg.IsEnabled() {
				logger.Info("⏭️ [%s] 配置已禁用，跳過自动啟动", botID)
				continue
			}
			// 數據庫 bot_states 優先：若已停止則跳過，不執行持倉安全性檢查
			if enabled, reason := symbolManager.GetBotManager().IsBotEnabledInDB(botID); !enabled {
				logger.Info("⏭️ [%s] 數據庫中已停止，跳過自动啟动（原因: %s）", botID, reason)
				continue
			}
			br, err := symbolManager.StartBot(ctx, botCfg)
			if err != nil {
				logger.Error("❌ [%s] 啟动失败: %v", botID, err)
				continue
			}
			if br != nil && br.Inner != nil && firstRuntime == nil {
				firstRuntime = br.Inner
			}
		}

		if firstRuntime == nil {
			logger.Warn("⚠️ 所有交易對啟动失败，但 Web 服務將继续运行")
			configComplete = false // 標記為不完整，避免后续绑定數據
		}
	} else {
		logger.Info("ℹ️ 配置不完整，跳過交易系统啟动，僅运行 Web 服務")
	}

	// 初始化倉位计划管理器（用於目標倉位調整和通知）
	var planManager *position.PlanManager
	if db != nil {
		// AllocationManagerProvider：按交易對獲取 AllocationManager
		allocationManagerProvider := &allocationManagerProviderAdapter{symbolManager: symbolManager}
		planManager = position.NewPlanManager(db, eventBus, allocationManagerProvider)
		// 注入到 Web 层
		web.SetPlanManagerProvider(&planManagerProviderAdapter{planManager: planManager})
		logger.Info("✅ 倉位计划管理器已初始化")
	}

	// 啟动利润提取執行器（定時檢查規則並執行內部轉账）
	if storageService != nil && storageService.GetStorage() != nil {
		getExchange := func(exchangeID string) exchange.IExchange {
			for _, rt := range symbolManager.List() {
				if rt != nil && rt.Config.Exchange == exchangeID {
					return rt.Exchange
				}
			}
			return nil
		}
		withdrawExecutor := profit.NewWithdrawExecutor(ctx, storageService.GetStorage(), getExchange)
		withdrawExecutor.Start()
		if webServer != nil {
			web.SetExchangeGetter(getExchange)
		}
	}

	// 每日快照任務（小時權益記錄 + 日終未實現盈虧與日內最大回撤，90 天外小時數據清理）
	var dailySnapshotRunner *monitor.DailySnapshotRunner
	if storageService != nil && storageService.GetStorage() != nil {
		getRuntimes := func() []monitor.RuntimeSnapshotSource {
			list := symbolManager.List()
			out := make([]monitor.RuntimeSnapshotSource, 0, len(list))
			for _, rt := range list {
				if rt != nil {
					out = append(out, &snapshotRuntimeAdapter{rt: rt})
				}
			}
			return out
		}
		dailySnapshotRunner = monitor.NewDailySnapshotRunner(storageService.GetStorage(), getRuntimes, "23:59", 90)
		dailySnapshotRunner.Start()
	}

	// Web 绑定數據提供者（兼容舊前端：使用第一個运行時，同時注册多交易對）
	var newsMonitor *monitor.NewsMonitor
	var newsPriceRecorder *monitor.PriceHistoryRecorder
	var newsPredVerifier *monitor.PredictionVerifier
	if webServer != nil && configComplete && firstRuntime != nil {
		statusMap := make(map[string]*web.SystemStatus)
		for _, rt := range symbolManager.List() {
			if rt == nil {
				continue
			}
			marketType := rt.Config.GetMarketType()
			if marketType == "" {
				marketType = "futures"
			}
			mapKey := fmt.Sprintf("%s:%s:%s", rt.Config.Exchange, rt.Config.Symbol, marketType)
			// StartBot 已调用 registerWebSymbolProvidersForRuntime 時避免重複注册與雙重 goroutine
			if web.IsSymbolStatusRegistered(rt.Config.Exchange, rt.Config.Symbol, marketType) {
				if st, ok := web.GetRegisteredSystemStatus(rt.Config.Exchange, rt.Config.Symbol, marketType); ok {
					statusMap[mapKey] = st
				}
				continue
			}
			status := &web.SystemStatus{
				Running:       true,
				Exchange:      rt.Config.Exchange,
				Symbol:        rt.Config.Symbol,
				MarketType:    marketType,
				CurrentPrice:  0,
				TotalPnL:      0,
				TotalTrades:   0,
				RiskTriggered: false,
				Uptime:        0,
			}
			statusMap[mapKey] = status

			web.RegisterSymbolProviders(rt.Config.Exchange, rt.Config.Symbol, &web.SymbolScopedProviders{
				Status:   status,
				Price:    rt.PriceMonitor,
				Exchange: &exchangeProviderAdapter{exchange: rt.Exchange},
				Position: web.NewPositionManagerAdapter(rt.SuperPositionManager),
				Risk:     rt.RiskMonitor,
				Storage:  web.NewStorageServiceAdapter(storageService),
			}, marketType)

			startTime := time.Now()
			go func(r *SymbolRuntime, st *web.SystemStatus, started time.Time) {
				ticker := time.NewTicker(2 * time.Second)
				defer ticker.Stop()
				dbQueryCounter := 0
				for {
					select {
					case <-ctx.Done():
						st.Running = false
						web.SetStatusProvider(st)
						return
					case <-ticker.C:
						// 如果交易對已停止，不再更新状態
						if !st.Running {
							continue
						}

						if r.PriceMonitor != nil {
							st.CurrentPrice = r.PriceMonitor.GetLastPrice()
						}
						if r.RiskMonitor != nil {
							st.RiskTriggered = r.RiskMonitor.IsTriggered()
						}

						// 更新统计信息
						if r.SuperPositionManager != nil {
							// 增加计數器，每 10 秒（5個周期）從數據库同步一次真實數據
							dbQueryCounter++

							useEstimation := true
							if storageService != nil && storageService.GetStorage() != nil {
								// 每 10 秒更新一次，或者如果當前 PnL 还是 0 则更新
								if dbQueryCounter >= 5 || st.TotalPnL == 0 {
									dbQueryCounter = 0
									// 查詢所有历史累计盈利（而非僅今日）
									// 使用一個很早的時间作為起始時间，确保查詢所有历史數據
									now := utils.NowUTC()
									allHistoryStart := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)

									// 查詢所有历史累计盈亏
									pnlSummary, err := storageService.GetStorage().GetPnLBySymbol(r.Config.Symbol, r.AccountID, allHistoryStart, now)
									if err == nil {
										st.TotalPnL = pnlSummary.TotalPnL
										st.TotalTrades = pnlSummary.TotalTrades
										useEstimation = false
									}
								} else {
									// 在非更新周期，保持之前的值，不使用估算
									useEstimation = false
								}
							}

							// 如果無法從數據库獲取（或未啟用存儲），回退到估算逻辑
							if useEstimation {
								totalBuyQty := r.SuperPositionManager.GetTotalBuyQty()
								totalSellQty := r.SuperPositionManager.GetTotalSellQty()
								profitSpread := r.SuperPositionManager.GetProfitSpread()

								// 修正盈亏估算：僅作為参考
								st.TotalPnL = totalSellQty * profitSpread

								// 修正成交次數估算：數量之和 / (單笔數量 * 2)
								if st.CurrentPrice > 0 {
									orderQtyInBase := r.Config.OrderQuantity / st.CurrentPrice
									if orderQtyInBase > 0 {
										st.TotalTrades = int((totalBuyQty + totalSellQty) / (orderQtyInBase * 2))
									}
								}
							}

							// 检查倉位计划進度（每個 ticker 周期检查一次）
							if planManager != nil {
								currentValue := r.SuperPositionManager.GetTotalPositionValueUSDT()
								_ = planManager.CheckPlanProgress(context.Background(), r.Config.Exchange, r.Config.Symbol, currentValue)
							}
						}

						st.Uptime = int64(time.Since(started).Seconds())
						if r == firstRuntime {
							// 兼容舊接口
							web.SetStatusProvider(st)
						}
					}
				}
			}(rt, status, startTime)
		}

		if firstRuntime != nil {
			web.SetDefaultSymbolKey(firstRuntime.Config.Exchange, firstRuntime.Config.Symbol)
			mt := firstRuntime.Config.GetMarketType()
			if mt == "" {
				mt = "futures"
			}
			if st, ok := statusMap[fmt.Sprintf("%s:%s:%s", firstRuntime.Config.Exchange, firstRuntime.Config.Symbol, mt)]; ok && st != nil {
				web.SetStatusProvider(st)
			}
			web.SetOrderQuantityConfig(firstRuntime.Config.OrderQuantity)
		}

		// 资金费率監控（複用舊逻辑，默认主流交易對）
		if storageService != nil {
			symbols := []string{
				"BTCUSDT", "ETHUSDT", "BNBUSDT", "SOLUSDT", "XRPUSDT",
				"ADAUSDT", "DOGEUSDT", "DOTUSDT", "MATICUSDT", "AVAXUSDT",
			}
			fundingMonitor := monitor.NewFundingMonitor(
				storageService.GetStorage(),
				firstRuntime.Exchange,
				symbols,
				8,
			)
			fundingMonitor.Start()
			web.RegisterFundingProvider(firstRuntime.Config.Exchange, firstRuntime.Config.Symbol, fundingMonitor)
			web.SetFundingMonitorProvider(fundingMonitor)

			// 資金費用同步：定時從交易所拉取 FUNDING_FEE 並寫入 funding_payments
			go startFundingIncomeSync(ctx, storageService.GetStorage(), firstRuntime.Exchange,
				firstRuntime.Config.Exchange, firstRuntime.Config.Symbol, firstRuntime.AccountID)

			// 初始化價差監控（支持數據庫配置覆蓋 config.yaml，UI 可動態啟停）
			logger.Info("🔍 初始化價差監控...")
			getBasisExch := func() exchange.IExchange {
				if firstRuntime != nil && firstRuntime.Exchange != nil {
					return firstRuntime.Exchange
				}
				return nil
			}
			basisCtrl := web.NewBasisMonitorController(storageService.GetStorage(), getBasisExch, cfg)
			web.SetBasisMonitorController(basisCtrl)
			effectiveBasis := basisCtrl.GetEffectiveConfig(ctx)
			basisCtrl.ApplyConfig(ctx, effectiveBasis)

			// 初始化新聞監控（Gemini 分析 + NewsAPI 收集）
			// 始終創建 NewsMonitor 並設置 provider，以便「手動觸發分析」在未啟用時也可用
			logger.Info("📰 初始化新聞監控...")
			getPrice := func(symbol string) float64 {
				for _, rt := range symbolManager.List() {
					if rt.Config.Symbol == symbol && rt.PriceMonitor != nil {
						return rt.PriceMonitor.GetLastPrice()
					}
				}
				return 0
			}
			newsMonitor = monitor.NewNewsMonitor(cfg, storageService.GetStorage())
			newsMonitor.SetPriceGetter(getPrice)
			web.SetNewsMonitorProvider(newsMonitor)
			if cfg.NewsMonitor.Enabled {
				if err := newsMonitor.Start(); err != nil {
					logger.Warn("⚠️ 新聞監控啟动失败: %v", err)
				} else {
					// 將新聞監控注入各运行時的风控監視器
					for _, rt := range symbolManager.List() {
						if rt.RiskMonitor != nil {
							rt.RiskMonitor.SetNewsMonitor(newsMonitor)
						}
					}
					logger.Info("✅ 新聞監控已啟动")
					// 啟動價格历史記錄器（用於預测驗证）
					newsPriceRecorder = monitor.NewPriceHistoryRecorder(cfg, storageService.GetStorage(), getPrice)
					newsPriceRecorder.Start()
					// 啟动預测驗证器
					newsPredVerifier = monitor.NewPredictionVerifier(cfg, storageService.GetStorage())
					newsPredVerifier.Start()
				}
			} else {
				// 未啟用時僅初始化分析器，支援手動觸發
				newsMonitor.InitForManualTrigger()
				logger.Info("✅ 新聞監控已就緒（手動觸發可用）")
			}

			// Web 保存 app_config 後同步新聞運行時（避免仍持啟動時舊 *Config 指針）
			web.SetNewsMonitorRuntimeSync(func(newCfg *config.Config) {
				if newsMonitor == nil || newCfg == nil {
					return
				}
				if newsPriceRecorder != nil {
					newsPriceRecorder.Stop()
					newsPriceRecorder = nil
				}
				if newsPredVerifier != nil {
					newsPredVerifier.Stop()
					newsPredVerifier = nil
				}
				newsMonitor.ApplyRuntimeConfig(newCfg)
				if !newCfg.NewsMonitor.Enabled {
					return
				}
				getPrice := func(symbol string) float64 {
					for _, rt := range symbolManager.List() {
						if rt.Config.Symbol == symbol && rt.PriceMonitor != nil {
							return rt.PriceMonitor.GetLastPrice()
						}
					}
					return 0
				}
				newsPriceRecorder = monitor.NewPriceHistoryRecorder(newCfg, storageService.GetStorage(), getPrice)
				newsPriceRecorder.Start()
				newsPredVerifier = monitor.NewPredictionVerifier(newCfg, storageService.GetStorage())
				newsPredVerifier.Start()
				for _, rt := range symbolManager.List() {
					if rt.RiskMonitor != nil {
						rt.RiskMonitor.SetNewsMonitor(newsMonitor)
					}
				}
			})
		}

		// 初始化宏觀事件預測市場拉取（Polymarket Gamma API）
		if cfg.MacroEvent.Enabled {
			logger.Info("📊 初始化宏觀事件預測市場...")
			macroClassifier := macro.NewEventImpactClassifier(cfg)
			macroFetcher := macro.NewMacroEventFetcher(cfg, macroClassifier)
			macroFetcher.Start(ctx)
			web.SetMacroEventFetcher(macroFetcher)
			logger.Info("✅ 宏觀事件拉取已啟动")
		}

		// 設置系统監控數據提供者
		if watchdog != nil {
			systemMetricsProvider := web.NewSystemMetricsProvider(storageService, watchdog)
			web.SetSystemMetricsProvider(systemMetricsProvider)
			logger.Info("✅ 系统監控數據提供者已設置")
		}

		// 設置事件中心提供者
		if db != nil {
			web.SetEventProvider(db)
			web.SetTaskProvider(db)
			logger.Info("✅ 事件中心提供者已設置")
			logger.Info("✅ 任務提供者已設置")
		}

		// 設置资金數據源提供者
		web.SetCapitalDataSource(&capitalDataSourceAdapter{
			manager: symbolManager,
			cfg:     cfg,
		})
		logger.Info("✅ 资金數據源提供者已設置")

		// 設置策略资金分配提供者
		// 概覽頁「已用」以倉位層 AllocationManager 為準，與槽位矩陣一致（恢復持倉後策略層 CapitalAllocator 可能未同步）
		getAllocationFunc := func() map[string]web.StrategyCapitalInfo {
			result := make(map[string]web.StrategyCapitalInfo)
			runtimes := symbolManager.List()
			for _, rt := range runtimes {
				if rt.StrategyManager == nil {
					continue
				}
				allocator := rt.StrategyManager.GetCapitalAllocator()
				if allocator == nil {
					continue
				}
				// 該 runtime 的已用資金以倉位層為準，與槽位 FILLED 狀態一致
				// 始終使用 AllocationManager 的值（即使為 0），確保與槽位矩陣一致
				usedFromPosition := -1.0 // 使用 -1 作為標記，表示未獲取到 AllocationManager 的值
				if rt.SuperPositionManager != nil {
					am := rt.SuperPositionManager.GetAllocationManager()
					if am != nil {
						st := am.GetStatus(rt.Config.Exchange, rt.Config.Symbol)
						if st != nil {
							usedFromPosition = st.UsedAmount
						}
					}
				}
				strategies := allocator.GetAllStrategiesCapital()
				for name, capital := range strategies {
					used := capital.Used
					// 如果獲取到了 AllocationManager 的值（包括 0），優先使用它
					if usedFromPosition >= 0 {
						used = usedFromPosition
					}
					available := capital.Allocated - used
					if available < 0 {
						available = 0
					}
					// 如果策略名称已存在，合並數據（累加）
					if existing, ok := result[name]; ok {
						result[name] = web.StrategyCapitalInfo{
							Allocated: existing.Allocated + capital.Allocated,
							Used:      existing.Used + used,
							Available: existing.Available + available,
							Weight:    existing.Weight, // 权重保持不变（取第一個）
							FixedPool: existing.FixedPool + capital.FixedPool,
						}
					} else {
						result[name] = web.StrategyCapitalInfo{
							Allocated: capital.Allocated,
							Used:      used,
							Available: available,
							Weight:    capital.Weight,
							FixedPool: capital.FixedPool,
						}
					}
				}
			}
			return result
		}

		// 释放单个策略锁定资金
		releaseCapitalFunc := func(strategyName string) float64 {
			totalReleased := 0.0
			runtimes := symbolManager.List()
			for _, rt := range runtimes {
				if rt.StrategyManager == nil {
					continue
				}
				allocator := rt.StrategyManager.GetCapitalAllocator()
				if allocator == nil {
					continue
				}
				totalReleased += allocator.ReleaseAll(strategyName)
			}
			return totalReleased
		}

		// 释放所有策略锁定资金
		releaseAllCapitalFunc := func() map[string]float64 {
			result := make(map[string]float64)
			runtimes := symbolManager.List()
			for _, rt := range runtimes {
				if rt.StrategyManager == nil {
					continue
				}
				allocator := rt.StrategyManager.GetCapitalAllocator()
				if allocator == nil {
					continue
				}
				released := allocator.ReleaseAllStrategies()
				for name, amount := range released {
					result[name] += amount
				}
			}
			return result
		}

		strategyProvider := web.NewStrategyProviderAdapter(getAllocationFunc, releaseCapitalFunc, releaseAllCapitalFunc)
		web.SetStrategyProvider(strategyProvider)
		logger.Info("✅ 策略资金分配提供者已設置")

		// 智子巡檢（定時彙總 + 緊急事件通知）
		var sophonInspector *inspector.SophonInspector
		if cfg.Inspector.Enabled && storageService != nil && storageService.GetStorage() != nil {
			getPriceForInspector := func(symbol string) float64 {
				for _, rt := range symbolManager.List() {
					if rt != nil && rt.Config.Symbol == symbol && rt.PriceMonitor != nil {
						return rt.PriceMonitor.GetLastPrice()
					}
				}
				return 0
			}
			getRuntimesForInspector := func() []inspector.SnapshotSource {
				list := symbolManager.List()
				out := make([]inspector.SnapshotSource, 0, len(list))
				for _, rt := range list {
					if rt != nil {
						out = append(out, &snapshotRuntimeAdapter{rt: rt})
					}
				}
				return out
			}
			var getNewsRisk inspector.NewsRiskProvider
			if newsMonitor != nil {
				getNewsRisk = newsMonitor.GetRiskAssessmentBySymbol
			}
			isRiskTriggered := func() (bool, string) {
				for _, rt := range symbolManager.List() {
					if rt == nil {
						continue
					}
					if rt.RiskMonitor != nil && rt.RiskMonitor.IsTriggered() {
						if msg := rt.RiskMonitor.GetLastMsg(); msg != "" {
							return true, msg
						}
						return true, "風控已觸發"
					}
					if rt.DepthMonitor != nil && rt.DepthMonitor.IsTriggered() {
						if msg := rt.DepthMonitor.GetLastMsg(); msg != "" {
							return true, msg
						}
						return true, "深度風控已觸發"
					}
				}
				return false, ""
			}
			getExchangeForInspector := func(exchangeName string) exchange.IExchange {
				for _, rt := range symbolManager.List() {
					if rt != nil && rt.Config.Exchange == exchangeName {
						return rt.Exchange
					}
				}
				return nil
			}
			getAccountSummaryForInspector := func(ctx context.Context, exchangeName, accountID string) (inspector.AccountSummary, error) {
				ex := getExchangeForInspector(exchangeName)
				if ex == nil {
					return inspector.AccountSummary{}, fmt.Errorf("exchange not found: %s", exchangeName)
				}
				acc, err := ex.GetAccount(ctx)
				if err != nil || acc == nil {
					return inspector.AccountSummary{}, err
				}
				total := acc.TotalMarginBalance
				if total == 0 {
					total = acc.TotalWalletBalance
				}
				used := total - acc.AvailableBalance
				currency := ex.GetQuoteAsset()
				if currency == "" {
					currency = "USDT"
				}
				return inspector.AccountSummary{
					Exchange:         exchangeName,
					Account:          accountID,
					TotalBalance:     total,
					AvailableBalance: acc.AvailableBalance,
					UsedMargin:       used,
					Currency:         currency,
				}, nil
			}
			collector := &inspector.Collector{
				GetSnapshotSources: getRuntimesForInspector,
				Storage:            storageService.GetStorage(),
				GetNewsRisk:        getNewsRisk,
				IsRiskTriggered:    isRiskTriggered,
				GetPrice:           getPriceForInspector,
				GetAccountSummary:  getAccountSummaryForInspector,
			}
			goldAnalyzer := &inspector.GoldAnalyzer{
				GoldSymbol:  "PAXGUSDT",
				BTCSymbol:   "BTCUSDT",
				GetPrice:    getPriceForInspector,
				Storage:     storageService.GetStorage(),
				GetNewsRisk: getNewsRisk,
			}
			regularInterval, _ := time.ParseDuration(cfg.Inspector.Schedule.RegularInterval)
			if regularInterval <= 0 {
				regularInterval = time.Hour
			}
			quietInterval, _ := time.ParseDuration(cfg.Inspector.Schedule.QuietInterval)
			if quietInterval <= 0 {
				quietInterval = 4 * time.Hour
			}
			scheduler := inspector.NewScheduler(inspector.SchedulerConfig{
				RegularInterval: regularInterval,
				QuietHoursStart: cfg.Inspector.Schedule.QuietHoursStart,
				QuietHoursEnd:   cfg.Inspector.Schedule.QuietHoursEnd,
				QuietInterval:   quietInterval,
			})
			eventMonitor := inspector.NewEventMonitor(inspector.EventThresholds{
				PnLAlert:          cfg.Inspector.Thresholds.PnLAlert,
				RiskScoreChange:   cfg.Inspector.Thresholds.RiskScoreChange,
				FundingRateAlert:  cfg.Inspector.Thresholds.FundingRateAlert,
				CorrelationChange: cfg.Inspector.Thresholds.CorrelationChange,
				BalanceChangePct:  cfg.Inspector.Thresholds.BalanceChangePct,
			})
			var geminiClient inspector.GeminiContentGenerator
			if cfg.AI.Enabled {
				ins := config.ResolveInspectorAI(cfg)
				if ins.APIKey != "" {
					geminiClient = ai.NewClientFromUpstream(ins)
				}
			}
			reportCfg := inspector.DefaultReportConfig()
			reportCfg.Name = cfg.Inspector.Name
			reportCfg.IncludeAIInsights = cfg.Inspector.Report.IncludeAIInsights
			reportCfg.MaxNewsItems = cfg.Inspector.Report.MaxNewsItems
			sophonInspector = inspector.NewSophonInspector(&inspector.SophonInspectorOptions{
				Collector:    collector,
				Analyzer:     &inspector.Analyzer{Client: geminiClient},
				EventMonitor: eventMonitor,
				ReportGen:    &inspector.ReportGenerator{Config: reportCfg},
				Scheduler:    scheduler,
				GoldAnalyzer: goldAnalyzer,
				NotifyReport: func(report *inspector.InspectorReport) {
					if report == nil || notifier == nil {
						return
					}
					evt := &event.Event{
						Type:      event.EventTypeInspectorReport,
						Timestamp: report.GeneratedAt,
						Data:      map[string]interface{}{"title": report.Title, "body": report.Body},
					}
					eventBus.Publish(evt)
					notifier.Send(evt)
				},
				SaveReport: func(report *inspector.InspectorReport) error {
					if report == nil || storageService == nil {
						return nil
					}
					st := storageService.GetStorage()
					if st == nil {
						return nil
					}
					rec := &storage.InspectionReport{
						ReportType:  report.ReportType,
						Title:       report.Title,
						Body:        report.Body,
						EventType:   report.EventType,
						GeneratedAt: report.GeneratedAt,
						CreatedAt:   time.Now(),
					}
					if report.Snapshot != nil {
						if b, err := json.Marshal(report.Snapshot); err == nil {
							rec.SnapshotJSON = string(b)
						}
					}
					if report.Analysis != nil {
						if b, err := json.Marshal(report.Analysis); err == nil {
							rec.AnalysisJSON = string(b)
						}
					}
					if len(report.EventData) > 0 {
						if b, err := json.Marshal(report.EventData); err == nil {
							rec.EventDataJSON = string(b)
						}
					}
					return st.SaveInspectionReport(rec)
				},
			})
			sophonInspector.Start()
			defer sophonInspector.Stop()
			logger.Info("✅ 智子巡檢已啟動")
		}

		// 設置全局存儲服務提供者（用於不带 symbol 參數的 API，如提現规则管理）
		if storageService != nil {
			storageAdapter := web.NewStorageServiceAdapter(storageService)
			web.SetStorageServiceProvider(storageAdapter)
			storage.SetBotRiskControlStorageGetter(func() storage.Storage {
				if storageService == nil {
					return nil
				}
				return storageService.GetStorage()
			})
			if st := storageService.GetStorage(); st != nil {
				if ssAdapter := web.NewStorageSystemSettingsAdapter(st); ssAdapter != nil {
					web.SetSystemSettingsProvider(ssAdapter)
					logger.Info("✅ 系統設置提供者已設置")
					bootstrapAipipe(ssAdapter)
					bootstrapObservability(Version, ssAdapter)
					bootstrapMCP(Version, ssAdapter, st, globalLogStorage)
				}
			}
			logger.Info("✅ 全局存儲服務提供者已設置")

			// 設置配置存儲和配置管理器
			if configStorage != nil {
				web.SetConfigStorage(configStorage)
				if configManager != nil {
					web.SetConfigManager(configManager)
					logger.Info("✅ 配置管理器已設置到 Web 服務器")
				}
			}

			// 回测任務管理器（需要 TaskStore，即 SQLite）
			if st := storageService.GetStorage(); st != nil {
				if taskStore := st.GetBacktestTaskStore(); taskStore != nil {
					binanceConfig := buildBinanceConfigForBacktest(cfg)
					// 获取 K 线数据目录
					klineDataDir := "./data/kline" // 默认路径
					if klineCollector != nil {
						klineDataDir = klineCollector.GetDataDir()
					}
					taskManager := backtest.NewTaskManager(taskStore, binanceConfig, klineDataDir)
					web.SetBacktestTaskManager(taskManager)
					logger.Info("✅ 回测任務管理器已設置")

					if optimStore := st.GetOptimTaskStore(); optimStore != nil {
						optimTaskManager := optimrun.NewOptimTaskManager(optimStore, binanceConfig)
						web.SetOptimTaskManager(optimTaskManager)
						logger.Info("✅ 參數優化任務管理器已設置")
					}

					// 初始化智能參數推薦服務
					exchangeFactory := func(exchangeName, marketType string) (exchange.IExchange, error) {
						// 使用 exchange.NewExchange 創建適配器
						// 使用 BTCUSDT 作為默認交易對（僅用於獲取價格，不實際交易）
						return exchange.NewExchange(cfg, exchangeName, "BTCUSDT", marketType)
					}
					smartParamsService := backtest.NewSmartParamsService(exchangeFactory, backtest.SmartParamsConfig{})
					web.SetSmartParamsService(smartParamsService)
					logger.Info("✅ 智能參數推薦服務已設置")

					// 初始化自動回測調度器
					autoSchedulerConfig := backtest.AutoSchedulerConfig{
						Enabled:            cfg.AutoBacktest.Enabled,
						ScheduleInterval:   time.Duration(cfg.AutoBacktest.ScheduleIntervalHours) * time.Hour,
						ResultTTL:          24 * time.Hour,
						MaxConcurrentTasks: cfg.AutoBacktest.MaxConcurrentTasks,
						DefaultCapital:     cfg.AutoBacktest.DefaultCapital,
						DefaultExchange:    cfg.App.CurrentExchange,
						DefaultMarketType:  "futures",
					}
					// 解析配置中的交易對
					if len(cfg.AutoBacktest.Symbols) > 0 {
						autoSchedulerConfig.Symbols = make([]backtest.SymbolConfig, 0, len(cfg.AutoBacktest.Symbols))
						for _, symCfg := range cfg.AutoBacktest.Symbols {
							autoSchedulerConfig.Symbols = append(autoSchedulerConfig.Symbols, backtest.SymbolConfig{
								Symbol:     symCfg.Symbol,
								Exchange:   symCfg.Exchange,
								MarketType: symCfg.MarketType,
								Strategies: symCfg.Strategies,
							})
						}
					}
					autoScheduler := backtest.NewAutoBacktestScheduler(taskManager, smartParamsService, autoSchedulerConfig)
					web.SetAutoBacktestScheduler(autoScheduler)
					autoScheduler.Start()
					logger.Info("✅ 自動回測調度器已設置（啟用: %v）", cfg.AutoBacktest.Enabled)
				} else {
					// 無任務存儲時仍提供智能參數推薦（使用 Binance 公開 API 獲取價格）
					smartParamsService := backtest.NewSmartParamsService(nil, backtest.SmartParamsConfig{})
					web.SetSmartParamsService(smartParamsService)
					logger.Info("✅ 智能參數推薦服務已設置（僅推薦，無任務存儲）")
				}
			} else {
				// 有存儲服務但 GetStorage() 為空時，仍提供智能參數推薦
				smartParamsService := backtest.NewSmartParamsService(nil, backtest.SmartParamsConfig{})
				web.SetSmartParamsService(smartParamsService)
				logger.Info("✅ 智能參數推薦服務已設置（僅推薦）")
			}
		}

		logger.Info("✅ 所有交易對已初始化，進入运行状態")
	} else if webServer != nil {
		// 配置不完整，只設置存儲服務提供者
		if storageService != nil {
			storageAdapter := web.NewStorageServiceAdapter(storageService)
			web.SetStorageServiceProvider(storageAdapter)
			storage.SetBotRiskControlStorageGetter(func() storage.Storage {
				if storageService == nil {
					return nil
				}
				return storageService.GetStorage()
			})
			if st := storageService.GetStorage(); st != nil {
				if ssAdapter := web.NewStorageSystemSettingsAdapter(st); ssAdapter != nil {
					web.SetSystemSettingsProvider(ssAdapter)
					bootstrapAipipe(ssAdapter)
					bootstrapObservability(Version, ssAdapter)
					bootstrapMCP(Version, ssAdapter, st, globalLogStorage)
				}
			}
			if st := storageService.GetStorage(); st != nil {
				if taskStore := st.GetBacktestTaskStore(); taskStore != nil {
					binanceConfig := buildBinanceConfigForBacktest(cfg)
					// 获取 K 线数据目录
					klineDataDir := "./data/kline" // 默认路径
					if klineCollector != nil {
						klineDataDir = klineCollector.GetDataDir()
					}
					taskManager := backtest.NewTaskManager(taskStore, binanceConfig, klineDataDir)
					web.SetBacktestTaskManager(taskManager)
					logger.Info("✅ 回测任務管理器已設置")

					if optimStore := st.GetOptimTaskStore(); optimStore != nil {
						optimTaskManager := optimrun.NewOptimTaskManager(optimStore, binanceConfig)
						web.SetOptimTaskManager(optimTaskManager)
						logger.Info("✅ 參數優化任務管理器已設置")
					}

					// 初始化智能參數推薦服務（使用空的交易所工廠，將使用 Binance 公開 API）
					smartParamsService := backtest.NewSmartParamsService(nil, backtest.SmartParamsConfig{})
					web.SetSmartParamsService(smartParamsService)
					logger.Info("✅ 智能參數推薦服務已設置（簡化模式）")

					// 初始化自動回測調度器（默認禁用，等待配置完成）
					autoSchedulerConfig := backtest.AutoSchedulerConfig{
						Enabled: false,
					}
					autoScheduler := backtest.NewAutoBacktestScheduler(taskManager, smartParamsService, autoSchedulerConfig)
					web.SetAutoBacktestScheduler(autoScheduler)
					logger.Info("✅ 自動回測調度器已設置（待配置）")
				} else {
					smartParamsService := backtest.NewSmartParamsService(nil, backtest.SmartParamsConfig{})
					web.SetSmartParamsService(smartParamsService)
					logger.Info("✅ 智能參數推薦服務已設置（僅推薦，無任務存儲）")
				}
			} else {
				// 配置不完整且無 GetStorage 時仍提供智能參數推薦
				smartParamsService := backtest.NewSmartParamsService(nil, backtest.SmartParamsConfig{})
				web.SetSmartParamsService(smartParamsService)
				logger.Info("✅ 智能參數推薦服務已設置（僅推薦）")
			}
		}

		// 設置系统監控數據提供者
		if watchdog != nil {
			systemMetricsProvider := web.NewSystemMetricsProvider(storageService, watchdog)
			web.SetSystemMetricsProvider(systemMetricsProvider)
			logger.Info("✅ 系统監控數據提供者已設置")
		}

		// 設置事件中心提供者
		if db != nil {
			web.SetEventProvider(db)
			web.SetTaskProvider(db)
			logger.Info("✅ 事件中心提供者已設置")
			logger.Info("✅ 任務提供者已設置")
		}

		logger.Info("ℹ️ Web 服務已啟动，等待配置完成")
	}

	// Polymarket + LLM 綜合信號（不依賴交易對運行時；需 web 已啟動且配置 ai.modules.polymarket_signal.enabled）
	if webServer != nil && cfg.AI.Modules.PolymarketSignal.Enabled {
		up := config.ResolveModuleAI(cfg, cfg.AI.Modules.PolymarketSignal.UpstreamRef)
		client, errClient := ai.NewAIClient(up.Provider, up.Model, up.APIKey, up.BaseURL)
		if errClient != nil {
			logger.Warn("⚠️ Polymarket 信號已啟用但 AI 客戶端創建失敗: %v", errClient)
		} else {
			pma := ai.NewPolymarketSignalAnalyzer(cfg, client)
			if pma != nil {
				web.SetAIPolymarketSignalProvider(&polymarketSignalAdapter{analyzer: pma})
				interval := cfg.AI.Modules.PolymarketSignal.AnalysisInterval
				if interval < 60 {
					interval = 3600
				}
				go func() {
					_ = pma.TriggerAnalysis()
					ticker := time.NewTicker(time.Duration(interval) * time.Second)
					defer ticker.Stop()
					for range ticker.C {
						_ = pma.TriggerAnalysis()
					}
				}()
				logger.Info("✅ Polymarket 信號分析已啟用（輪詢間隔 %d 秒，Gamma + LLM）", interval)
			}
		}
	}

	// 所有初始化完成，程序進入运行状態
	logger.Info("✅ 系统初始化完成，程序正在运行中...")
	logger.Info("⏱️ [啟動] 總耗時: %v", time.Since(startTime))
	logger.Info("💡 按 Ctrl+C 退出程序")

	// 发布系统启动事件
	if eventBus != nil {
		eventBus.Publish(&event.Event{
			Type: event.EventTypeSystemStart,
			Data: map[string]interface{}{
				"startup_time":     startTime,
				"startup_duration": time.Since(startTime).String(),
				"version":          Version,
			},
		})
	}

	// 等待退出信号（SIGINT 或 SIGTERM）
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("🛑 收到退出信号，开始优雅关闭...")

	// 发布系统停止事件
	if eventBus != nil {
		eventBus.Publish(&event.Event{
			Type: event.EventTypeSystemStop,
			Data: map[string]interface{}{
				"reason": "收到退出信号",
			},
		})
	}

	// 🔥 第一优先级：撤销各交易對的订單（僅在配置完整時）
	if configComplete {
		if cfg.System.CancelOnExit {
			for _, rt := range symbolManager.List() {
				logger.Info("🔄 [%s:%s] 正在撤销所有订單...", rt.Config.Exchange, rt.Config.Symbol)
				cancelCtx, cancelTimeout := context.WithTimeout(context.Background(), 10*time.Second)
				if err := rt.Exchange.CancelAllOrders(cancelCtx, rt.Config.Symbol); err != nil {
					logger.Error("❌ [%s:%s] 撤销订單失败: %v", rt.Config.Exchange, rt.Config.Symbol, err)
				} else {
					logger.Info("✅ [%s:%s] 已撤销所有订單", rt.Config.Exchange, rt.Config.Symbol)
				}
				cancelTimeout()
			}
		}

		// 🔥 平倉（可選）
		if cfg.System.ClosePositionsOnExit {
			for _, rt := range symbolManager.List() {
				logger.Info("🔄 [%s:%s] 正在平掉所有持倉...", rt.Config.Exchange, rt.Config.Symbol)
				closeCtx, closeTimeout := context.WithTimeout(context.Background(), 30*time.Second)
				closeAllPositions(closeCtx, rt.Exchange, rt.Config.Symbol, rt.PriceMonitor)
				closeTimeout()
			}
		}

		// 🔥 停止所有交易對组件
		for _, rt := range symbolManager.List() {
			if rt.Stop != nil {
				rt.Stop()
			}
		}
	}

	// 🔥 第三优先级：停止所有协程（取消 context）
	// 这會通知所有使用 ctx 的协程停止工作（包括事件处理协程）
	cancel()

	// 關閉事件總線，釋放訂閱者 channel 與 dedup goroutine，避免內存泄漏
	if eventBus != nil {
		eventBus.Close()
		logger.Info("✅ 事件總線已關閉")
	}

	// 停止記憶體管理器
	if memoryManager != nil {
		memoryManager.Stop()
	}

	// 等待一小段時间，让事件处理协程完成清理（确保事件队列被处理完）
	time.Sleep(500 * time.Millisecond)

	// 停止 OSS 上傳器並关闭审计日志
	if ossUploader != nil {
		ossUploader.Stop()
	}
	if auditLogger != nil {
		_ = auditLogger.Close()
		storage.SetGlobalAuditLogger(nil)
	}

	// 🔥 第四优先级：停止存儲服務（确保所有事件都已处理完毕）
	logger.Info("⏹️ 正在停止存儲服務...")
	if storageService != nil {
		storageService.Stop()
	}

	// 再等待一小段時间，让存儲服務完成最后的写入
	time.Sleep(200 * time.Millisecond)

	// 打印最终状態（僅在配置完整時）
	if configComplete {
		for _, rt := range symbolManager.List() {
			if rt.SuperPositionManager != nil {
				rt.SuperPositionManager.PrintPositions()
			}
		}
	}

	// 关闭文件日志
	logger.Close()

	// 关闭 aipipe 上报客户端，把待发条目落盘
	if err := aipipe.Close(); err != nil {
		// 不能用 logger（已 Close），直接走 std log
		// 但 logger.Error 之类只是写到 stdout，影响有限
	}

	// 关闭日志存儲
	if globalLogStorage != nil {
		if err := globalLogStorage.Close(); err != nil {
			logger.Error("❌ 关闭日志存儲失败: %v", err)
		}
	}

	logger.Info("✅ 系统已安全退出 QuantMesh")
}

// 以下類型已抽取到 main_adapters_position.go：
//   loggerAdapter, positionExchangeAdapter
// （exchangeProviderAdapter 與 exchangeExecutorAdapter 也在該文件）
