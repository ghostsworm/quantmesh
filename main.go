package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	// "quantmesh/ai" // AI 功能已迁移到商业插件
	"quantmesh/ai"
	"quantmesh/ai/processor"
	"quantmesh/ai/service"
	"quantmesh/backtest"
	"quantmesh/backtest/optimrun"
	"quantmesh/config"
	"quantmesh/database"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/i18n"
	"quantmesh/inspector"
	"quantmesh/lock"
	"quantmesh/logger"
	"quantmesh/metrics"
	"quantmesh/monitor"
	"quantmesh/notify"
	"quantmesh/order"
	"quantmesh/plugin"
	"quantmesh/position"
	"quantmesh/profit"
	"quantmesh/storage"
	"quantmesh/utils"
	"quantmesh/web"
)

// Version 应用版本号
var Version = "3.54.0-rc4"

// capitalDataSourceAdapter 资金數據源适配器
type capitalDataSourceAdapter struct {
	manager *SymbolManager
	cfg     *config.Config
}

func (a *capitalDataSourceAdapter) GetExchanges() []exchange.IExchange {
	runtimes := a.manager.List()
	exchanges := make([]exchange.IExchange, 0)
	seen := make(map[string]bool)
	for _, rt := range runtimes {
		if rt.Exchange == nil {
			continue
		}
		name := rt.Exchange.GetName()
		if !seen[name] {
			exchanges = append(exchanges, rt.Exchange)
			seen[name] = true
		}
	}
	return exchanges
}

func (a *capitalDataSourceAdapter) GetStrategyConfigs() map[string]config.StrategyConfig {
	return a.cfg.Strategies.Configs
}

func (a *capitalDataSourceAdapter) GetPositionManagers() []web.PositionManagerInfo {
	runtimes := a.manager.List()
	infos := make([]web.PositionManagerInfo, len(runtimes))
	for i, rt := range runtimes {
		infos[i] = web.PositionManagerInfo{
			Exchange: rt.Config.Exchange,
			Symbol:   rt.Config.Symbol,
			Manager:  rt.SuperPositionManager,
		}
	}
	return infos
}

func (a *capitalDataSourceAdapter) GetConfig() *config.Config {
	return a.cfg
}

// buildBinanceConfigForBacktest 從配置中提取 Binance API 配置供回測獲取歷史 K 線使用
func buildBinanceConfigForBacktest(cfg *config.Config) map[string]string {
	out := map[string]string{"api_key": "", "secret_key": "", "testnet": "false"}
	if cfg == nil || cfg.Exchanges == nil {
		return out
	}
	if exCfg, ok := cfg.Exchanges["binance"]; ok {
		out["api_key"] = exCfg.APIKey
		out["secret_key"] = exCfg.SecretKey
		out["testnet"] = fmt.Sprintf("%v", exCfg.Testnet)
	}
	return out
}

// 全局日志存儲實例（用於清理任務和 WebSocket 推送）
var globalLogStorage *storage.LogStorage

// webAuthnLoggerAdapter WebAuthn 日志适配器
type webAuthnLoggerAdapter struct{}

func (w *webAuthnLoggerAdapter) Infof(format string, args ...interface{}) {
	logger.Info(format, args...)
}

func (w *webAuthnLoggerAdapter) Warnf(format string, args ...interface{}) {
	logger.Warn(format, args...)
}

func (w *webAuthnLoggerAdapter) Errorf(format string, args ...interface{}) {
	logger.Error(format, args...)
}

func (w *webAuthnLoggerAdapter) Debugf(format string, args ...interface{}) {
	logger.Debug(format, args...)
}

// reconciliationStorageAdapter 對账存儲适配器
type reconciliationStorageAdapter struct {
	storageService *storage.StorageService
	accountID      string
	exchange       string
}

func (a *reconciliationStorageAdapter) SaveReconciliationHistory(symbol string, reconcileTime time.Time, localPosition, exchangePosition, positionDiff float64,
	activeBuyOrders, activeSellOrders int, pendingSellQty, totalBuyQty, totalSellQty, estimatedProfit float64) error {
	return a.storageService.SaveReconciliationHistoryDirect(a.exchange, symbol, a.accountID, reconcileTime, localPosition, exchangePosition, positionDiff,
		activeBuyOrders, activeSellOrders, pendingSellQty, totalBuyQty, totalSellQty, estimatedProfit)
}

// allocationManagerProviderAdapter 按交易對獲取 AllocationManager（倉位计划需要）
type allocationManagerProviderAdapter struct {
	symbolManager *SymbolManager
}

func (a *allocationManagerProviderAdapter) GetAllocationManager(exchange, symbol string) *position.AllocationManager {
	runtimes := a.symbolManager.List()
	for _, rt := range runtimes {
		if rt.Config.Exchange == exchange && rt.Config.Symbol == symbol && rt.SuperPositionManager != nil {
			return rt.SuperPositionManager.GetAllocationManager()
		}
	}
	return nil
}

// planManagerProviderAdapter 用於 Web API 的 PlanManager 提供者
type planManagerProviderAdapter struct {
	planManager *position.PlanManager
}

func (a *planManagerProviderAdapter) GetPlanManager() *position.PlanManager {
	return a.planManager
}

// AI适配器（用於Web API）
// 注意：AI 功能已迁移到商业插件，开源版不再包含
// 如需使用 AI 功能，请购買商业插件：https://quantmesh.io/plugins

/*
type aiMarketAdapter struct {
	analyzer *ai.MarketAnalyzer
}

func (a *aiMarketAdapter) GetLastAnalysis() interface{} {
	return a.analyzer.GetLastAnalysis()
}

func (a *aiMarketAdapter) GetLastAnalysisTime() time.Time {
	return a.analyzer.GetLastAnalysisTime()
}

func (a *aiMarketAdapter) PerformAnalysis() error {
	return a.analyzer.TriggerAnalysis()
}

type aiParamAdapter struct {
	optimizer *ai.ParameterOptimizer
}

func (a *aiParamAdapter) GetLastOptimization() interface{} {
	return a.optimizer.GetLastOptimization()
}

func (a *aiParamAdapter) GetLastOptimizationTime() time.Time {
	return a.optimizer.GetLastOptimizationTime()
}

func (a *aiParamAdapter) PerformOptimization() error {
	return a.optimizer.TriggerOptimization()
}

type aiRiskAdapter struct {
	analyzer *ai.RiskAnalyzer
}

func (a *aiRiskAdapter) GetLastAnalysis() interface{} {
	return a.analyzer.GetLastAnalysis()
}

func (a *aiRiskAdapter) GetLastAnalysisTime() time.Time {
	return a.analyzer.GetLastAnalysisTime()
}

func (a *aiRiskAdapter) PerformAnalysis() error {
	return a.analyzer.TriggerAnalysis()
}

type aiSentimentAdapter struct {
	analyzer *ai.SentimentAnalyzer
}

func (a *aiSentimentAdapter) GetLastAnalysis() interface{} {
	return a.analyzer.GetLastAnalysis()
}

func (a *aiSentimentAdapter) GetLastAnalysisTime() time.Time {
	return a.analyzer.GetLastAnalysisTime()
}

func (a *aiSentimentAdapter) PerformAnalysis() error {
	return a.analyzer.TriggerAnalysis()
}

type aiPolymarketAdapter struct {
	analyzer *ai.PolymarketSignalAnalyzer
}

func (a *aiPolymarketAdapter) GetLastAnalysis() interface{} {
	return a.analyzer.GetLastAnalysis()
}

func (a *aiPolymarketAdapter) GetLastAnalysisTime() time.Time {
	return a.analyzer.GetLastAnalysisTime()
}

func (a *aiPolymarketAdapter) PerformAnalysis() error {
	return a.analyzer.TriggerAnalysis()
}

type aiPromptAdapter struct {
	manager *ai.PromptManager
}

func (a *aiPromptAdapter) GetAllPrompts() (map[string]interface{}, error) {
	prompts, err := a.manager.GetAllPrompts()
	if err != nil {
		return nil, err
	}
	result := make(map[string]interface{})
	for k, v := range prompts {
		result[k] = map[string]interface{}{
			"module":        v.Module,
			"template":      v.Template,
			"system_prompt": v.SystemPrompt,
		}
	}
	return result, nil
}

func (a *aiPromptAdapter) UpdatePrompt(module, template, systemPrompt string) error {
	return a.manager.UpdatePrompt(module, template, systemPrompt)
}
*/

// reconciliationRestoreAdapter 對账恢複适配器（用於從數據库恢複對账统计）
type reconciliationRestoreAdapter struct {
	storage storage.Storage
}

func (a *reconciliationRestoreAdapter) GetLatestReconciliationHistory(exchange, symbol string) (interface{}, error) {
	if a.storage == nil {
		return nil, nil
	}
	accountID := web.GetCurrentAccountID()
	return a.storage.GetLatestReconciliationHistory(exchange, symbol, accountID)
}

func (a *reconciliationRestoreAdapter) GetReconciliationCount(exchange, symbol string) (int64, error) {
	if a.storage == nil {
		return 0, nil
	}
	accountID := web.GetCurrentAccountID()
	return a.storage.GetReconciliationCount(exchange, symbol, accountID)
}

// tradeStorageAdapter 交易存儲适配器
type tradeStorageAdapter struct {
	storageService *storage.StorageService
	accountID      string // 账戶標识
}

func (a *tradeStorageAdapter) SaveTrade(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, fee float64, feeAsset string, createdAt time.Time) error {
	// 兼容旧接口：价格偏差设为0
	return a.SaveTradeWithDeviation(buyOrderID, sellOrderID, exchange, symbol, buyPrice, sellPrice, quantity, pnl, fee, feeAsset, 0, 0, createdAt)
}

func (a *tradeStorageAdapter) SaveTradeWithDeviation(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, fee float64, feeAsset string, buyPriceDeviation, sellPriceDeviation float64, createdAt time.Time) error {
	if a.storageService == nil {
		return nil
	}
	st := a.storageService.GetStorage()
	if st == nil {
		return nil
	}
	// 使用SQLiteStorage的SaveTradeWithDeviation方法
	if sqliteSt, ok := st.(interface {
		SaveTradeWithDeviation(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, fee float64, feeAsset string, buyPriceDeviation, sellPriceDeviation float64, createdAt time.Time) error
	}); ok {
		return sqliteSt.SaveTradeWithDeviation(buyOrderID, sellOrderID, exchange, symbol, buyPrice, sellPrice, quantity, pnl, fee, feeAsset, buyPriceDeviation, sellPriceDeviation, createdAt)
	}
	// 降级：使用旧接口
	return st.SaveTrade(&storage.Trade{
		BuyOrderID:         buyOrderID,
		SellOrderID:        sellOrderID,
		Exchange:           exchange,
		Account:            a.accountID,
		Symbol:             symbol,
		BuyPrice:           buyPrice,
		SellPrice:          sellPrice,
		Quantity:           quantity,
		PnL:                pnl,
		Fee:                fee,
		FeeAsset:           feeAsset,
		BuyPriceDeviation:  buyPriceDeviation,
		SellPriceDeviation: sellPriceDeviation,
		CreatedAt:          createdAt,
	})
}

// symbolManagerWebAdapter SymbolManager Web API 适配器
// snapshotRuntimeAdapter 適配 SymbolRuntime 為 monitor.RuntimeSnapshotSource（用於每日快照）
type snapshotRuntimeAdapter struct {
	rt *SymbolRuntime
}

func (a *snapshotRuntimeAdapter) Exchange() string { return a.rt.Config.Exchange }
func (a *snapshotRuntimeAdapter) Symbol() string   { return a.rt.Config.Symbol }
func (a *snapshotRuntimeAdapter) Account() string  { return a.rt.AccountID }
func (a *snapshotRuntimeAdapter) CurrentSnapshot() (currentPrice, unrealizedPnL, totalPositionValue float64) {
	if a.rt.PriceMonitor == nil || a.rt.SuperPositionManager == nil {
		return 0, 0, 0
	}
	currentPrice = a.rt.PriceMonitor.GetLastPrice()
	unrealizedPnL = a.rt.SuperPositionManager.GetUnrealizedPnL(currentPrice)
	totalPositionValue = a.rt.SuperPositionManager.GetTotalPositionValueAtPrice(currentPrice)
	return currentPrice, unrealizedPnL, totalPositionValue
}

type symbolManagerWebAdapter struct {
	manager         *SymbolManager
	ctx             context.Context
	cfg             *config.Config
	eventBus        *event.EventBus
	storageService  *storage.StorageService
	distributedLock lock.DistributedLock
}

func (a *symbolManagerWebAdapter) Get(exchange, symbol string) (interface{}, bool) {
	rt, ok := a.manager.Get(exchange, symbol)
	if !ok {
		return nil, false
	}
	return rt, true
}

func (a *symbolManagerWebAdapter) List() []interface{} {
	runtimes := a.manager.List()
	result := make([]interface{}, len(runtimes))
	for i, rt := range runtimes {
		result[i] = rt
	}
	return result
}

func (a *symbolManagerWebAdapter) StartSymbol(exchange, symbol string) error {
	// 检查是否已經运行
	if _, ok := a.manager.Get(exchange, symbol); ok {
		err := fmt.Errorf("交易對 %s:%s 已經在运行", exchange, symbol)
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

	// 從配置管理器獲取最新配置（而不是使用啟动時的配置）
	cfg, err := web.GetLatestConfig()
	if err != nil {
		// 如果獲取最新配置失败，回退到使用啟动時的配置
		logger.Warn("⚠️ 獲取最新配置失败，使用啟动時的配置: %v", err)
		cfg = a.cfg
	}

	// 從配置中查找對应的 SymbolConfig
	var symCfg *config.SymbolConfig
	for i := range cfg.Trading.Symbols {
		if strings.EqualFold(cfg.Trading.Symbols[i].Exchange, exchange) &&
			strings.EqualFold(cfg.Trading.Symbols[i].Symbol, symbol) {
			symCfg = &cfg.Trading.Symbols[i]
			break
		}
	}

	if symCfg == nil {
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

	// 持久化啟用状態：确保重啟后仍保持啟动
	if err := web.SetSymbolEnabled(exchange, symbol, true); err != nil {
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
			strings.EqualFold(cfg.Trading.Symbols[i].Symbol, symbol) {
			symCfg = &cfg.Trading.Symbols[i]
			break
		}
	}
	if symCfg == nil {
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

	// 啟动 SymbolRuntime（使用最新配置）
	rt, err := startSymbolRuntime(a.ctx, cfg, *symCfg, a.eventBus, a.storageService, a.distributedLock)
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

	// 添加到管理器
	a.manager.Add(rt)

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
	rt, ok := a.manager.Get(exchange, symbol)
	if !ok {
		err := fmt.Errorf("交易對 %s:%s 未运行", exchange, symbol)
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

	// 持久化停用状態：确保重啟后不會自动再啟动
	if err := web.SetSymbolEnabled(exchange, symbol, false); err != nil {
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
	a.manager.Remove(exchange, symbol)

	logger.Info("⏹️ [%s:%s] 交易已停止", exchange, symbol)
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

// UpdateTradingParams 实现 TradingParamsUpdater 接口
// 将最新配置推送到所有运行中的 SymbolRuntime，解决配置修改后内存不同步问题
func (a *symbolManagerWebAdapter) UpdateTradingParams(latestConfig *config.Config) []string {
	return a.manager.UpdateRuntimeTradingParams(latestConfig)
}

func (a *symbolManagerWebAdapter) ClosePositions(exchange, symbol string) (*web.ClosePositionsResponse, error) {
	rt, ok := a.manager.Get(exchange, symbol)
	if !ok {
		return nil, fmt.Errorf("交易對 %s:%s 未找到", exchange, symbol)
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

	// 解析調試参數（-debug / --debug）
	debugMode := false
	filteredArgs := []string{os.Args[0]}
	for _, arg := range os.Args[1:] {
		switch arg {
		case "-debug", "--debug":
			debugMode = true
		default:
			filteredArgs = append(filteredArgs, arg)
		}
	}
	if debugMode {
		log.Printf("[INFO] Debug 模式已啟用：Gin 將输出全量请求日志")
	}
	os.Args = filteredArgs

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
		logger.InitLogStorage(func(level, message string) {
			if logStorage != nil {
				logStorage.WriteLog(level, message)
			} else {
				log.Printf("[ERROR] logStorage 為 nil，無法写入日志")
			}
		})
		log.Printf("[INFO] 日志存儲已初始化: %s", logStoragePath)
	}

	logger.Info("🚀 QuantMesh 做市商系統啟动...")
	logger.Info("📦 版本号: %s", Version)

	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	// 检查配置文件是否存在
	var cfg *config.Config
	var configComplete bool
	_, err = os.Stat(configPath)
	if os.IsNotExist(err) {
		// 配置文件不存在，創建最小化配置
		logger.Info("ℹ️ 配置文件不存在，創建最小化配置（僅啟用 Web 服務）")
		cfg = config.CreateMinimalConfig()
		configComplete = false

		// 保存最小化配置到文件（不驗证，因為配置不完整）
		if err := config.SaveConfigWithoutValidation(cfg, configPath); err != nil {
			logger.Warn("⚠️ 保存最小化配置失败: %v，將继续运行", err)
		} else {
			logger.Info("✅ 已創建最小化配置文件: %s", configPath)
		}
	} else {
		// 配置文件存在，加載配置
		cfg, err = config.LoadConfig(configPath)
		if err != nil {
			logger.Fatalf("❌ 加載配置失败: %v", err)
		}

		// 检查配置是否完整（是否有交易所配置和交易對配置）
		configComplete = cfg.App.CurrentExchange != "" &&
			len(cfg.Exchanges) > 0 &&
			cfg.Exchanges[cfg.App.CurrentExchange].APIKey != "" &&
			cfg.Exchanges[cfg.App.CurrentExchange].SecretKey != "" &&
			len(cfg.Trading.Symbols) > 0 &&
			cfg.Trading.Symbols[0].Symbol != ""

		if !configComplete {
			logger.Info("ℹ️ 配置不完整，僅啟动 Web 服務，请通過引導页面完成配置")
		}
	}

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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 啟動定期日誌清理任務（在 ctx 定義之後）
	if globalLogStorage != nil {
		go func() {
			// 每天凌晨 2 點執行清理
			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()

			// 計算到下一個凌晨 2 點的時間
			now := time.Now()
			nextCleanup := time.Date(now.Year(), now.Month(), now.Day(), 2, 0, 0, 0, now.Location())
			if nextCleanup.Before(now) {
				nextCleanup = nextCleanup.Add(24 * time.Hour)
			}
			initialDelay := nextCleanup.Sub(now)

			// 使用 timer 等待到第一個清理時间，同時監听 context
			initialTimer := time.NewTimer(initialDelay)
			defer initialTimer.Stop()

			select {
			case <-ctx.Done():
				return
			case <-initialTimer.C:
				// 立即執行一次清理
				logger.Info("🧹 開始定期清理日誌...")
				rowsAffected, err := globalLogStorage.CleanOldLogsByLevel(7, []string{"INFO", "WARN"})
				if err != nil {
					logger.Warn("⚠️ 清理日志失败: %v", err)
				} else {
					logger.Info("✅ 已清理 %d 条 INFO/WARN 级别日志（7天前）", rowsAffected)
				}

				// 執行 VACUUM 优化
				if err := globalLogStorage.Vacuum(); err != nil {
					logger.Warn("⚠️ 數據库优化失败: %v", err)
				} else {
					logger.Info("✅ 日志數據库优化完成")
				}
			}

			// 定期執行
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					logger.Info("🧹 开始定期清理日志...")
					rowsAffected, err := globalLogStorage.CleanOldLogsByLevel(7, []string{"INFO", "WARN"})
					if err != nil {
						logger.Warn("⚠️ 清理日志失败: %v", err)
					} else {
						logger.Info("✅ 已清理 %d 条 INFO/WARN 级别日志（7天前）", rowsAffected)
					}

					// 執行 VACUUM 优化
					if err := globalLogStorage.Vacuum(); err != nil {
						logger.Warn("⚠️ 數據库优化失败: %v", err)
					} else {
						logger.Info("✅ 日志數據库优化完成")
					}
				}
			}
		}()
	}

	// 事件總線 & 通知 & 存儲
	logger.Info("🔧 正在初始化事件總線...")
	// 增加缓冲区大小到5000，避免事件队列满
	eventBus := event.NewEventBus(5000)
	logger.Info("🔧 正在初始化通知服務...")
	notifier := notify.NewNotificationService(cfg)

	logger.Info("🔧 正在初始化存儲服務...")
	// 若已配置路徑與類型則強制視為啟用（與 config.Validate 一致，避免設置頁開關已開但進程未重啟或配置未寫入導致回測不可用）
	if cfg.Storage.Path != "" && cfg.Storage.Type != "" {
		cfg.Storage.Enabled = true
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

	// 运行 K 线文件迁移（一次性迁移现有文件到统一管理系统）
	if storageService != nil {
		if st := storageService.GetStorage(); st != nil {
			if sqliteStorage, ok := st.(*storage.SQLiteStorage); ok {
				go func() {
					if err := storage.RunKlineFilesMigration(sqliteStorage); err != nil {
						logger.Warn("⚠️ K线文件迁移失败: %v", err)
					} else {
						logger.Info("✅ K线文件迁移完成")
					}
				}()
			}
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

	// 舊的事件处理器（保留用於存儲服務）
	// 使用 worker pool 模式，限制並发數量，避免 goroutine 泄漏
	eventWorkerPool := make(chan struct{}, 10) // 最多10個並发 worker
	go func() {
		defer close(eventWorkerPool)
		for {
			select {
			case <-ctx.Done():
				return
			case evt := <-eventBus.Subscribe():
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

	// 初始化K线数据收集器
	logger.Info("🔧 正在初始化K线数据收集器...")
	var klineCollector *monitor.KlineCollector
	if storageService != nil {
		// 创建交易所实例映射（用于K线收集器）
		// 只创建主要交易所的实例（前5大）
		majorExchanges := []string{"binance", "okx", "bybit", "bitget", "gate"}
		exchanges := make(map[string]exchange.IExchange)
		for _, exchangeName := range majorExchanges {
			if _, exists := cfg.Exchanges[exchangeName]; !exists {
				continue
			}
			// 使用NewExchange创建交易所实例
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
				logger.Info("✅ K线数据收集器已啟动")
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

	// Web 服務器
	var webServer *web.WebServer
	if cfg.Web.Enabled {
		logger.Info("🌐 开始初始化 Web 服務器...")
		// 初始化密碼管理器
		passwordManager, err := web.NewPasswordManager("./data")
		if err != nil {
			logger.Error("❌ 初始化密碼管理器失败: %v", err)
		} else {
			web.SetPasswordManager(passwordManager)
			logger.Info("✅ 密碼管理器已初始化")
		}

		// 初始化 WebAuthn 管理器
		// 根據實際配置決定 RPID 和 RPOrigin
		var rpID string
		var rpOrigin string

		// 優先使用環境變數或配置檔案中的域名
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
			// 後備方案：使用配置的 host
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

		// 初始化配置管理器
		configManager := web.NewConfigManager(configPath)
		web.SetConfigManager(configManager)
		logger.Info("✅ 配置管理器已初始化")

		// 設置版本号
		web.SetVersion(Version)
		logger.Info("✅ 版本号已設置: %s", Version)

		// 初始化配置备份管理器（备份目錄為 config.yaml 同級 backups/）
		backupManager := config.NewBackupManager(configPath)
		web.SetConfigBackupManager(backupManager)
		logger.Info("✅ 配置备份管理器已初始化")

		// 初始化配置历史管理器
		historyManager, err := config.NewHistoryManager("./data", configPath)
		if err != nil {
			logger.Warn("⚠️ 初始化配置历史管理器失败: %v", err)
		} else {
			web.SetConfigHistoryManager(historyManager)
			logger.Info("✅ 配置历史管理器已初始化")
		}

		// 初始化配置热更新器
		hotReloader := config.NewHotReloader(cfg)
		web.SetConfigHotReloader(hotReloader)
		logger.Info("✅ 配置热更新器已初始化")

		// 設置日志存儲提供者（用於Web API日志查詢）
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
				// 等待一下，确保 goroutine 中的日志也能输出
				time.Sleep(200 * time.Millisecond)
			}
		}
	} else {
		logger.Info("ℹ️ Web 服務未啟用（配置中 web.enabled=false）")
	}

	symbolManager := NewSymbolManager(cfg)

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

	// 只有在配置完整時才啟动交易系统
	var firstRuntime *SymbolRuntime
	if configComplete {
		// 啟动所有交易對
		for _, symCfg := range cfg.Trading.Symbols {
			if !symCfg.IsEnabled() {
				logger.Info("⏭️ [%s:%s] 已禁用，跳過自动啟动", symCfg.Exchange, symCfg.Symbol)
				continue
			}
			rt, err := startSymbolRuntime(ctx, cfg, symCfg, eventBus, storageService, distributedLock)
			if err != nil {
				logger.Error("❌ [%s:%s] 啟动失败: %v", symCfg.Exchange, symCfg.Symbol, err)
				continue
			}
			symbolManager.Add(rt)
			if firstRuntime == nil {
				firstRuntime = rt
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
	if webServer != nil && configComplete && firstRuntime != nil {
		statusMap := make(map[string]*web.SystemStatus)
		for _, rt := range symbolManager.List() {
			if rt == nil {
				continue
			}
			marketType := rt.Config.GetMarketType()
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
			statusMap[fmt.Sprintf("%s:%s:%s", rt.Config.Exchange, rt.Config.Symbol, marketType)] = status

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
			web.SetStatusProvider(statusMap[fmt.Sprintf("%s:%s", firstRuntime.Config.Exchange, firstRuntime.Config.Symbol)])
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

			// 初始化價差監控
			if cfg.BasisMonitor.Enabled {
				logger.Info("🔍 初始化價差監控...")
				basisMonitor := monitor.NewBasisMonitor(
					storageService.GetStorage(),
					firstRuntime.Exchange,
					cfg.BasisMonitor.Symbols,
					cfg.BasisMonitor.IntervalMinutes,
				)
				basisMonitor.Start()
				web.SetBasisMonitorProvider(basisMonitor)
				logger.Info("✅ 價差監控已啟动")
			}

			// 初始化新聞監控（Gemini 分析 + NewsAPI 收集）
			if cfg.NewsMonitor.Enabled {
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
				if err := newsMonitor.Start(); err != nil {
					logger.Warn("⚠️ 新聞監控啟动失败: %v", err)
				} else {
					web.SetNewsMonitorProvider(newsMonitor)
					// 將新聞監控注入各运行時的风控監視器
					for _, rt := range symbolManager.List() {
						if rt.RiskMonitor != nil {
							rt.RiskMonitor.SetNewsMonitor(newsMonitor)
						}
					}
					logger.Info("✅ 新聞監控已啟动")
					// 啟動價格历史記錄器（用於預测驗证）
					priceRecorder := monitor.NewPriceHistoryRecorder(cfg, storageService.GetStorage(), getPrice)
					priceRecorder.Start()
					// 啟动預测驗证器
					predVerifier := monitor.NewPredictionVerifier(cfg, storageService.GetStorage())
					predVerifier.Start()
				}
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
			if cfg.AI.Enabled && (cfg.AI.GeminiAPIKey != "" || cfg.AI.APIKey != "") {
				apiKey := cfg.AI.GeminiAPIKey
				if apiKey == "" {
					apiKey = cfg.AI.APIKey
				}
				geminiClient = ai.NewGeminiClient(apiKey)
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

		// 設置全局存儲服務提供者（用於不带 symbol 参數的 API，如提現规则管理）
		if storageService != nil {
			storageAdapter := web.NewStorageServiceAdapter(storageService)
			web.SetStorageServiceProvider(storageAdapter)
			logger.Info("✅ 全局存儲服務提供者已設置")
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

	// 所有初始化完成，程序進入运行状態
	logger.Info("✅ 系统初始化完成，程序正在运行中...")
	logger.Info("💡 按 Ctrl+C 退出程序")

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

	// 关闭日志存儲
	if globalLogStorage != nil {
		if err := globalLogStorage.Close(); err != nil {
			logger.Error("❌ 关闭日志存儲失败: %v", err)
		}
	}

	logger.Info("✅ 系统已安全退出 QuantMesh")
}

// loggerAdapter 适配 logger 到 WebAuthnLogger 接口
type loggerAdapter struct{}

func (l *loggerAdapter) Infof(format string, args ...interface{}) {
	logger.Info(format, args...)
}

func (l *loggerAdapter) Warnf(format string, args ...interface{}) {
	logger.Warn(format, args...)
}

func (l *loggerAdapter) Errorf(format string, args ...interface{}) {
	logger.Error(format, args...)
}

func (l *loggerAdapter) Debugf(format string, args ...interface{}) {
	logger.Debug(format, args...)
}

// positionExchangeAdapter 适配器，將 exchange.IExchange 轉换為 position.IExchange
type positionExchangeAdapter struct {
	exchange exchange.IExchange
}

func (a *positionExchangeAdapter) GetPositions(ctx context.Context, symbol string) (interface{}, error) {
	positions, err := a.exchange.GetPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}

	// 轉换為 position.PositionInfo 切片
	result := make([]*position.PositionInfo, len(positions))
	for i, pos := range positions {
		result[i] = &position.PositionInfo{
			Symbol: pos.Symbol,
			Size:   pos.Size,
		}
	}

	return result, nil
}

func (a *positionExchangeAdapter) GetOpenOrders(ctx context.Context, symbol string) (interface{}, error) {
	return a.exchange.GetOpenOrders(ctx, symbol)
}

func (a *positionExchangeAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (interface{}, error) {
	return a.exchange.GetOrder(ctx, symbol, orderID)
}

// GetOrderForReconciler 為對賬服務提供 GetOrder 方法（返回 *exchange.Order）
func (a *positionExchangeAdapter) GetOrderForReconciler(ctx context.Context, symbol string, orderID int64) (*exchange.Order, error) {
	// exchange.IExchange.GetOrder 已经返回 *exchange.Order，直接返回即可
	return a.exchange.GetOrder(ctx, symbol, orderID)
}

func (a *positionExchangeAdapter) GetBaseAsset() string {
	return a.exchange.GetBaseAsset()
}

func (a *positionExchangeAdapter) GetName() string {
	return a.exchange.GetName()
}

func (a *positionExchangeAdapter) CancelAllOrders(ctx context.Context, symbol string) error {
	return a.exchange.CancelAllOrders(ctx, symbol)
}

func (a *positionExchangeAdapter) GetAccount(ctx context.Context) (interface{}, error) {
	return a.exchange.GetAccount(ctx)
}

func (a *positionExchangeAdapter) GetPriceDecimals() int {
	return a.exchange.GetPriceDecimals()
}

func (a *positionExchangeAdapter) GetQuantityDecimals() int {
	return a.exchange.GetQuantityDecimals()
}

// GetOrderFills 查詢訂單成交記錄（透傳至 exchange）
func (a *positionExchangeAdapter) GetOrderFills(ctx context.Context, symbol string, orderID int64) (interface{}, error) {
	return a.exchange.GetOrderFills(ctx, symbol, orderID)
}

// GetOrderBook 獲取訂單簿深度，轉換為 position.OrderBook
func (a *positionExchangeAdapter) GetOrderBook(ctx context.Context, symbol string, limit int) (*position.OrderBook, error) {
	ob, err := a.exchange.GetOrderBook(ctx, symbol, limit)
	if err != nil {
		return nil, err
	}
	if ob == nil {
		return nil, nil
	}
	bids := make([]position.OrderBookLevel, len(ob.Bids))
	for i, b := range ob.Bids {
		bids[i] = position.OrderBookLevel{Price: b.Price, Quantity: b.Quantity}
	}
	asks := make([]position.OrderBookLevel, len(ob.Asks))
	for i, ask := range ob.Asks {
		asks[i] = position.OrderBookLevel{Price: ask.Price, Quantity: ask.Quantity}
	}
	return &position.OrderBook{
		Symbol:    ob.Symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: ob.Timestamp,
	}, nil
}

// startFundingIncomeSync 定時從交易所拉取資金費用（FUNDING_FEE）並寫入 funding_payments
func startFundingIncomeSync(ctx context.Context, st storage.Storage, ex exchange.IExchange, exchangeName, symbol, accountID string) {
	if st == nil || ex == nil {
		return
	}
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	// 首次延遲 1 分鐘後執行，避免啟動時阻塞
	time.Sleep(1 * time.Minute)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			endTime := time.Now()
			startTime := endTime.AddDate(0, 0, -7)
			startMs := startTime.UnixMilli()
			endMs := endTime.UnixMilli()
			list, err := ex.GetIncomeHistory(ctx, symbol, "FUNDING_FEE", startMs, endMs)
			if err != nil {
				logger.Warn("⚠️ 拉取資金費用失敗: %v", err)
				continue
			}
			for _, inc := range list {
				_ = st.SaveFundingPayment(&storage.FundingPayment{
					Exchange:      exchangeName,
					Symbol:        inc.Symbol,
					Account:       accountID,
					IncomeType:    inc.IncomeType,
					Income:        inc.Income,
					Asset:         inc.Asset,
					Info:          inc.Info,
					TransactionID: inc.TransactionID,
					TradeTime:     inc.TradeTime,
				})
			}
			if len(list) > 0 {
				logger.Info("💰 資金費用同步: %s %s 寫入 %d 筆", exchangeName, symbol, len(list))
			}
		}
	}
}

// exchangeProviderAdapter 适配器，將 exchange.IExchange 轉换為 web.ExchangeProvider
type exchangeProviderAdapter struct {
	exchange exchange.IExchange
}

func (a *exchangeProviderAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*exchange.Candle, error) {
	return a.exchange.GetHistoricalKlines(ctx, symbol, interval, limit)
}

func (a *exchangeProviderAdapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return a.exchange.GetFundingRate(ctx, symbol)
}

func (a *exchangeProviderAdapter) GetPositions(ctx context.Context, symbol string) ([]*exchange.Position, error) {
	return a.exchange.GetPositions(ctx, symbol)
}

// exchangeExecutorAdapter 适配器，將 order.ExchangeOrderExecutor 轉换為 position.OrderExecutorInterface
type exchangeExecutorAdapter struct {
	executor *order.ExchangeOrderExecutor
	eventBus *event.EventBus
	symbol   string
}

func (a *exchangeExecutorAdapter) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	orderReq := &order.OrderRequest{
		Symbol:        req.Symbol,
		Side:          req.Side,
		Price:         req.Price,
		Quantity:      req.Quantity,
		PriceDecimals: req.PriceDecimals,
		ReduceOnly:    req.ReduceOnly,
		PostOnly:      req.PostOnly,      // 傳遞 PostOnly 参數
		ClientOrderID: req.ClientOrderID, // 傳遞 ClientOrderID
		StrategyName:  req.StrategyName,  // 傳遞策略名称
		StrategyType:  req.StrategyType,  // 傳遞策略類型
	}
	ord, err := a.executor.PlaceOrder(orderReq)
	if err != nil {
		return nil, err
	}

	// 发布订單下單事件
	if a.eventBus != nil {
		a.eventBus.Publish(&event.Event{
			Type: event.EventTypeOrderPlaced,
			Data: map[string]interface{}{
				"order_id":        ord.OrderID,
				"client_order_id": ord.ClientOrderID,
				"symbol":          ord.Symbol,
				"side":            ord.Side,
				"price":           ord.Price,
				"quantity":        ord.Quantity,
				"status":          ord.Status,
				"strategy_name":   req.StrategyName,
				"strategy_type":   req.StrategyType,
				"order_source":    req.OrderSource,
				"created_at":      ord.CreatedAt,
			},
		})
	}

	return &position.Order{
		OrderID:       ord.OrderID,
		ClientOrderID: ord.ClientOrderID, // 回傳 ClientOrderID
		Symbol:        ord.Symbol,
		Side:          ord.Side,
		Price:         ord.Price,
		Quantity:      ord.Quantity,
		Status:        ord.Status,
		CreatedAt:     ord.CreatedAt,
	}, nil
}

func (a *exchangeExecutorAdapter) BatchPlaceOrders(orders []*position.OrderRequest) ([]*position.Order, bool) {
	result := a.BatchPlaceOrdersWithDetails(orders)
	return result.PlacedOrders, result.HasMarginError
}

func (a *exchangeExecutorAdapter) BatchPlaceOrdersWithDetails(orders []*position.OrderRequest) *position.BatchPlaceOrdersResult {
	// 建立 ClientOrderID -> 策略信息 的映射，用於事件发布時回填
	strategyMap := make(map[string][3]string) // ClientOrderID -> [StrategyName, StrategyType, OrderSource]
	orderReqs := make([]*order.OrderRequest, len(orders))
	for i, req := range orders {
		orderReqs[i] = &order.OrderRequest{
			Symbol:        req.Symbol,
			Side:          req.Side,
			Price:         req.Price,
			Quantity:      req.Quantity,
			PriceDecimals: req.PriceDecimals,
			ReduceOnly:    req.ReduceOnly,
			PostOnly:      req.PostOnly,      // 傳遞 PostOnly 参數
			ClientOrderID: req.ClientOrderID, // 傳遞 ClientOrderID
			StrategyName:  req.StrategyName,  // 傳遞策略名称
			StrategyType:  req.StrategyType,  // 傳遞策略類型
			OrderSource:   req.OrderSource,   // 傳遞订單來源
		}
		if req.ClientOrderID != "" {
			strategyMap[req.ClientOrderID] = [3]string{req.StrategyName, req.StrategyType, req.OrderSource}
		}
	}
	batchResult := a.executor.BatchPlaceOrdersWithDetails(orderReqs)

	result := &position.BatchPlaceOrdersResult{
		PlacedOrders:     make([]*position.Order, len(batchResult.PlacedOrders)),
		HasMarginError:   batchResult.HasMarginError,
		ReduceOnlyErrors: batchResult.ReduceOnlyErrors,
	}

	for i, ord := range batchResult.PlacedOrders {
		result.PlacedOrders[i] = &position.Order{
			OrderID:       ord.OrderID,
			ClientOrderID: ord.ClientOrderID, // 回傳 ClientOrderID
			Symbol:        ord.Symbol,
			Side:          ord.Side,
			Price:         ord.Price,
			Quantity:      ord.Quantity,
			Status:        ord.Status,
			CreatedAt:     ord.CreatedAt,
		}

		// 发布订單下單事件（回填策略信息）
		sName, sType, oSource := "", "", ""
		if info, ok := strategyMap[ord.ClientOrderID]; ok {
			sName, sType, oSource = info[0], info[1], info[2]
		}
		if a.eventBus != nil {
			a.eventBus.Publish(&event.Event{
				Type: event.EventTypeOrderPlaced,
				Data: map[string]interface{}{
					"order_id":        ord.OrderID,
					"client_order_id": ord.ClientOrderID,
					"symbol":          ord.Symbol,
					"side":            ord.Side,
					"price":           ord.Price,
					"quantity":        ord.Quantity,
					"status":          ord.Status,
					"strategy_name":   sName,
					"strategy_type":   sType,
					"order_source":    oSource,
					"created_at":      ord.CreatedAt,
				},
			})
		}
	}
	return result
}

func (a *exchangeExecutorAdapter) BatchCancelOrders(orderIDs []int64) error {
	return a.executor.BatchCancelOrders(orderIDs)
}

// closeAllPositions 平掉所有持倉（退出時使用）
func closeAllPositions(ctx context.Context, ex exchange.IExchange, symbol string, priceMonitor *monitor.PriceMonitor) {
	// 1. 查詢所有持倉
	positions, err := ex.GetPositions(ctx, symbol)
	if err != nil {
		logger.Error("❌ 查詢持倉失败，無法平倉: %v", err)
		return
	}

	if len(positions) == 0 {
		logger.Info("ℹ️ 當前没有持倉，無需平倉")
		return
	}

	// 2. 獲取當前價格（用於平倉單）
	currentPrice := 0.0
	if priceMonitor != nil {
		currentPrice = priceMonitor.GetLastPrice()
	}

	// 如果價格監控器没有價格，尝試從交易所獲取
	if currentPrice <= 0 {
		var priceErr error
		currentPrice, priceErr = ex.GetLatestPrice(ctx, symbol)
		if priceErr != nil || currentPrice <= 0 {
			logger.Warn("⚠️ 無法獲取當前價格，將使用持倉標記價格平倉")
		}
	}

	// 3. 统计需要平倉的持倉
	needCloseCount := 0
	for _, pos := range positions {
		// Size 正數表示多倉，负數表示空倉，為0表示無持倉
		if pos.Size != 0 {
			needCloseCount++
		}
	}

	if needCloseCount == 0 {
		logger.Info("ℹ️ 當前没有有效持倉，無需平倉")
		return
	}

	logger.Info("🔄 发現 %d 個持倉需要平倉", needCloseCount)

	// 4. 對每個持倉下平倉單
	successCount := 0
	failCount := 0

	for _, pos := range positions {
		// 跳過無持倉
		if pos.Size == 0 {
			continue
		}

		// 确定平倉方向和數量
		var side exchange.Side
		quantity := pos.Size
		if quantity > 0 {
			// 多倉，需要下 SELL 單平倉
			side = exchange.SideSell
		} else {
			// 空倉，需要下 BUY 單平倉（注意 Size 是负數）
			side = exchange.SideBuy
			quantity = -quantity // 轉為正數
		}

		// 确定平倉價格：优先使用當前價格，否则使用標記價格，最后使用开倉價格
		closePrice := currentPrice
		if closePrice <= 0 && pos.MarkPrice > 0 {
			closePrice = pos.MarkPrice
		}
		if closePrice <= 0 && pos.EntryPrice > 0 {
			closePrice = pos.EntryPrice
		}

		if closePrice <= 0 {
			logger.Error("❌ [平倉] 無法确定價格，跳過持倉 %s (Size: %.6f)", pos.Symbol, pos.Size)
			failCount++
			continue
		}

		// 下單平倉
		logger.Info("🔄 [平倉] %s %s %.6f @ %.2f (ReduceOnly)", side, pos.Symbol, quantity, closePrice)

		orderReq := &exchange.OrderRequest{
			Symbol:        symbol,
			Side:          side,
			Type:          exchange.OrderTypeLimit,
			TimeInForce:   exchange.TimeInForceGTC,
			Quantity:      quantity,
			Price:         closePrice,
			ReduceOnly:    true, // 只减倉
			PostOnly:      false,
			PriceDecimals: ex.GetPriceDecimals(),
		}

		_, err := ex.PlaceOrder(ctx, orderReq)
		if err != nil {
			logger.Error("❌ [平倉] 下單失败 %s %.6f @ %.2f: %v", side, quantity, closePrice, err)
			failCount++
		} else {
			logger.Info("✅ [平倉] 已下單 %s %.6f @ %.2f", side, quantity, closePrice)
			successCount++
		}

		// 避免请求過快，稍微延迟
		time.Sleep(100 * time.Millisecond)
	}

	logger.Info("📊 [平倉完成] 成功: %d, 失败: %d", successCount, failCount)

	// 5. 等待一段時间，让平倉單成交（可選）
	if successCount > 0 {
		logger.Info("⏳ 等待平倉單成交...")
		time.Sleep(2 * time.Second)
	}
}

// closeAllPositionsWithResult 平掉所有持倉並返回結果（用於 API）
func closeAllPositionsWithResult(ctx context.Context, ex exchange.IExchange, symbol string, priceMonitor *monitor.PriceMonitor) (successCount, failCount int, err error) {
	// 1. 查詢所有持倉
	positions, err := ex.GetPositions(ctx, symbol)
	if err != nil {
		logger.Error("❌ 查詢持倉失败，無法平倉: %v", err)
		return 0, 0, err
	}

	if len(positions) == 0 {
		logger.Info("ℹ️ 當前没有持倉，無需平倉")
		return 0, 0, nil
	}

	// 2. 獲取當前價格（用於平倉單）
	currentPrice := 0.0
	if priceMonitor != nil {
		currentPrice = priceMonitor.GetLastPrice()
	}

	// 如果價格監控器没有價格，尝試從交易所獲取
	if currentPrice <= 0 {
		var priceErr error
		currentPrice, priceErr = ex.GetLatestPrice(ctx, symbol)
		if priceErr != nil || currentPrice <= 0 {
			logger.Warn("⚠️ 無法獲取當前價格，將使用持倉標記價格平倉")
		}
	}

	// 3. 统计需要平倉的持倉
	needCloseCount := 0
	for _, pos := range positions {
		if pos.Size != 0 {
			needCloseCount++
		}
	}

	if needCloseCount == 0 {
		logger.Info("ℹ️ 當前没有有效持倉，無需平倉")
		return 0, 0, nil
	}

	logger.Info("🔄 发現 %d 個持倉需要平倉", needCloseCount)

	// 0. 先取消所有挂單，确保平倉單能顺利下單
	logger.Info("🧹 [平倉] 正在取消 %s 的所有挂單...", symbol)
	if err := ex.CancelAllOrders(ctx, symbol); err != nil {
		logger.Warn("⚠️ [平倉] 取消挂單失败: %v (將继续尝試平倉)", err)
	}

	// 4. 對每個持倉下平倉單
	successCount = 0
	failCount = 0

	for _, pos := range positions {
		if pos.Size == 0 {
			continue
		}

		var side exchange.Side
		quantity := pos.Size
		if quantity > 0 {
			side = exchange.SideSell
		} else {
			side = exchange.SideBuy
			quantity = -quantity
		}

		logger.Info("🔄 [平倉] %s %s %.6f (市價 ReduceOnly)", side, symbol, quantity)

		orderReq := &exchange.OrderRequest{
			Symbol:        symbol,
			Side:          side,
			Type:          exchange.OrderTypeMarket, // 使用市價單确保立即平倉
			Quantity:      quantity,
			ReduceOnly:    true,
			PriceDecimals: ex.GetPriceDecimals(),
		}

		_, err := ex.PlaceOrder(ctx, orderReq)
		if err != nil {
			logger.Error("❌ [平倉] 下單失败 %s %.6f: %v", side, quantity, err)
			failCount++
		} else {
			logger.Info("✅ [平倉] 已下單 %s %.6f", side, quantity)
			successCount++
		}

		time.Sleep(100 * time.Millisecond)
	}

	logger.Info("📊 [平倉完成] 成功: %d, 失败: %d", successCount, failCount)
	return successCount, failCount, nil
}
