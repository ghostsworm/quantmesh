package main

// 本文件抽自 main.go，集中存放 Web 注冊 / 資金費用同步 / 平倉等輔助函數。
// 拆分目的：避免單文件超過 3000 行硬上限。
// 行為與函數簽名保持不變，僅做位置遷移。

import (
	"context"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/mcp"
	"quantmesh/monitor"
	"quantmesh/notify/aipipe"
	"quantmesh/position"
	"quantmesh/storage"
	"quantmesh/utils"
	"quantmesh/web"
)

var aipipeBootstrapped sync.Once

// bootstrapAipipe 读取 system_settings 里的 aipipe_* 配置，启动上报客户端，
// 并把 logger 的 ERROR/FATAL 钩子接到 aipipe.ReportMessage。
//
// 幂等：多次调用只生效一次（multi-tenant 路径可能调两次）。
// 失败不影响主流程；启动期没填 key → 整套静默 no-op。
func bootstrapAipipe(provider web.SystemSettingsProvider) {
	if provider == nil {
		return
	}
	aipipeBootstrapped.Do(func() {
		cfg := loadAipipeConfigFromSettings(provider)
		if err := aipipe.Reload(cfg); err != nil {
			logger.Warn("aipipe 启动失败: %v", err)
		}
		// 装错误外发钩子：ERROR/FATAL 自动上报
		logger.SetErrorHook(func(level, message string) {
			aipipe.ReportMessage(level, message, "log")
		})
		web.RegisterAipipeReloader(func() {
			c := loadAipipeConfigFromSettings(provider)
			if err := aipipe.Reload(c); err != nil {
				logger.Warn("aipipe 重载失败: %v", err)
			}
		})
		if cfg.Enabled && cfg.APIKey != "" {
			logger.Info("✅ aipipe 错误上报已启用，endpoint=%s", cfg.Endpoint)
		}
	})
}

// bootstrapMCP 构建 mcp.Server 并注入到 web 包。
//
// 调用时机：在 web.NewWebServer 之前，因为 SetupRoutes 必须在 ServeHTTP 之前
// 完成所有路由挂载。
//
// SymbolManager 此时通常还没创建（main 流程顺序如此），所以 BotControl 用
// "全局延迟绑定"——由 NewMCPBotController() 返回的 controller 内部读取
// currentSymbolManager atomic 指针，等运行时落定后自动可用。
//
// allow_write 后续被改 → reloader 会重建 server 并替换。
func bootstrapMCP(version string, provider web.SystemSettingsProvider, st storage.Storage, logSt *storage.LogStorage) {
	if provider == nil {
		return
	}
	build := func() *mcp.Server {
		allowWrite, _ := provider.GetSystemSettingBool(context.Background(), "mcp_allow_write", false)
		providers := mcp.Providers{
			Version:        version,
			Storage:        st,
			LogStorage:     logSt,
			SystemSettings: provider,
			BotControl:     NewMCPBotController(),
		}
		if st != nil {
			providers.BacktestTasks = st.GetBacktestTaskStore()
		}
		s := mcp.NewServer(version, web.MCPTokenCheck)
		mcp.RegisterAllTools(s, providers, allowWrite)
		return s
	}
	web.SetMCPServer(build())
	web.RegisterMCPReloader(func() {
		web.SetMCPServer(build())
		logger.Info("MCP server 已重建（allow_write 切换）")
	})
}

// loadAipipeConfigFromSettings 从 system_settings 中读取 aipipe 配置。
func loadAipipeConfigFromSettings(provider web.SystemSettingsProvider) aipipe.Config {
	ctx := context.Background()
	cfg := aipipe.Config{Endpoint: aipipe.DefaultEndpoint}
	if v, err := provider.GetSystemSetting(ctx, aipipe.SettingKeyAPIKey); err == nil && v != nil {
		cfg.APIKey = v.Value
	}
	if v, err := provider.GetSystemSetting(ctx, aipipe.SettingKeyEndpoint); err == nil && v != nil && v.Value != "" {
		cfg.Endpoint = v.Value
	}
	if enabled, err := provider.GetSystemSettingBool(ctx, aipipe.SettingKeyEnabled, false); err == nil {
		cfg.Enabled = enabled
	}
	return cfg
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

// registerWebSymbolProvidersForRuntime 在 Bot 啟動成功後掛接 Web /api/status（熱啟動後與 Bot 詳情「运行中」一致）
func registerWebSymbolProvidersForRuntime(rt *SymbolRuntime, bc *config.BotConfig, storageSvc *storage.StorageService) {
	if rt == nil || bc == nil || storageSvc == nil {
		return
	}
	mt := bc.GetMarketType()
	if mt == "" {
		mt = "futures"
	}
	ex := bc.Exchange
	sym := bc.Symbol
	if web.IsSymbolStatusRegistered(ex, sym, mt) {
		// 早期路徑可能只寫入了 Status，priceProviders 仍為空 → PickPriceProvider 落到默認所；在此補齊
		if rt.PriceMonitor != nil {
			web.UpsertPriceProviderForKey(ex, sym, mt, rt.PriceMonitor)
		}
		if rt.SuperPositionManager != nil {
			web.UpsertPositionProviderForKey(ex, sym, mt, web.NewPositionManagerAdapter(rt.SuperPositionManager))
		}
		return
	}
	status := &web.SystemStatus{
		Running:       true,
		Exchange:      ex,
		Symbol:        sym,
		MarketType:    mt,
		CurrentPrice:  0,
		TotalPnL:      0,
		TotalTrades:   0,
		RiskTriggered: false,
		Uptime:        0,
	}
	web.RegisterSymbolProviders(ex, sym, &web.SymbolScopedProviders{
		Status:   status,
		Price:    rt.PriceMonitor,
		Exchange: &exchangeProviderAdapter{exchange: rt.Exchange},
		Position: web.NewPositionManagerAdapter(rt.SuperPositionManager),
		Risk:     rt.RiskMonitor,
		Storage:  web.NewStorageServiceAdapter(storageSvc),
	}, mt)

	startTime := time.Now()
	planMgr := web.GetPlanManager()
	go runSymbolStatusUpdateLoop(rt, status, startTime, storageSvc, planMgr)
}

// runSymbolStatusUpdateLoop 與啟動時 Web 綁定邏輯一致，在 st.Running=false 或 Unregister 後退出
func runSymbolStatusUpdateLoop(rt *SymbolRuntime, st *web.SystemStatus, started time.Time, storageSvc *storage.StorageService, planMgr *position.PlanManager) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	dbQueryCounter := 0
	for range ticker.C {
		if !st.Running {
			return
		}
		if rt.PriceMonitor != nil {
			st.CurrentPrice = rt.PriceMonitor.GetLastPrice()
		}
		if rt.RiskMonitor != nil {
			st.RiskTriggered = rt.RiskMonitor.IsTriggered()
		}
		if rt.SuperPositionManager != nil {
			dbQueryCounter++
			useEstimation := true
			if storageSvc != nil && storageSvc.GetStorage() != nil {
				if dbQueryCounter >= 5 || st.TotalPnL == 0 {
					dbQueryCounter = 0
					now := utils.NowUTC()
					allHistoryStart := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
					pnlSummary, err := storageSvc.GetStorage().GetPnLBySymbol(rt.Config.Symbol, rt.AccountID, allHistoryStart, now)
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
				totalBuyQty := rt.SuperPositionManager.GetTotalBuyQty()
				totalSellQty := rt.SuperPositionManager.GetTotalSellQty()
				profitSpread := rt.SuperPositionManager.GetProfitSpread()
				st.TotalPnL = totalSellQty * profitSpread
				if st.CurrentPrice > 0 {
					orderQtyInBase := rt.Config.OrderQuantity / st.CurrentPrice
					if orderQtyInBase > 0 {
						st.TotalTrades = int((totalBuyQty + totalSellQty) / (orderQtyInBase * 2))
					}
				}
			}
			if planMgr != nil {
				currentValue := rt.SuperPositionManager.GetTotalPositionValueUSDT()
				_ = planMgr.CheckPlanProgress(context.Background(), rt.Config.Exchange, rt.Config.Symbol, currentValue)
			}
		}
		st.Uptime = int64(time.Since(started).Seconds())
	}
}

func unregisterWebSymbolProvidersForRuntime(bc *config.BotConfig) {
	if bc == nil {
		return
	}
	mt := bc.GetMarketType()
	if mt == "" {
		mt = "futures"
	}
	web.UnregisterSymbolProviders(bc.Exchange, bc.Symbol, mt)
}

// closeAllPositions 平掉所有持倉（退出時使用）
func closeAllPositions(ctx context.Context, ex exchange.IExchange, symbol string, priceMonitor *monitor.PriceMonitor) {
	positions, err := ex.GetPositions(ctx, symbol)
	if err != nil {
		logger.Error("❌ 查詢持倉失败，無法平倉: %v", err)
		return
	}

	if len(positions) == 0 {
		logger.Info("ℹ️ 當前没有持倉，無需平倉")
		return
	}

	currentPrice := 0.0
	if priceMonitor != nil {
		currentPrice = priceMonitor.GetLastPrice()
	}

	if currentPrice <= 0 {
		var priceErr error
		currentPrice, priceErr = ex.GetLatestPrice(ctx, symbol)
		if priceErr != nil || currentPrice <= 0 {
			logger.Warn("⚠️ 無法獲取當前價格，將使用持倉標記價格平倉")
		}
	}

	needCloseCount := 0
	for _, pos := range positions {
		if pos.Size != 0 {
			needCloseCount++
		}
	}

	if needCloseCount == 0 {
		logger.Info("ℹ️ 當前没有有效持倉，無需平倉")
		return
	}

	logger.Info("🔄 发現 %d 個持倉需要平倉", needCloseCount)

	successCount := 0
	failCount := 0

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

		logger.Info("🔄 [平倉] %s %s %.6f @ %.2f (ReduceOnly)", side, pos.Symbol, quantity, closePrice)

		orderReq := &exchange.OrderRequest{
			Symbol:        symbol,
			Side:          side,
			Type:          exchange.OrderTypeLimit,
			TimeInForce:   exchange.TimeInForceGTC,
			Quantity:      quantity,
			Price:         closePrice,
			ReduceOnly:    true,
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

		time.Sleep(100 * time.Millisecond)
	}

	logger.Info("📊 [平倉完成] 成功: %d, 失败: %d", successCount, failCount)

	if successCount > 0 {
		logger.Info("⏳ 等待平倉單成交...")
		time.Sleep(2 * time.Second)
	}
}

// closeAllPositionsWithResult 平掉所有持倉並返回結果（用於 API）
func closeAllPositionsWithResult(ctx context.Context, ex exchange.IExchange, symbol string, priceMonitor *monitor.PriceMonitor) (successCount, failCount int, err error) {
	positions, err := ex.GetPositions(ctx, symbol)
	if err != nil {
		logger.Error("❌ 查詢持倉失败，無法平倉: %v", err)
		return 0, 0, err
	}

	if len(positions) == 0 {
		logger.Info("ℹ️ 當前没有持倉，無需平倉")
		return 0, 0, nil
	}

	currentPrice := 0.0
	if priceMonitor != nil {
		currentPrice = priceMonitor.GetLastPrice()
	}

	if currentPrice <= 0 {
		var priceErr error
		currentPrice, priceErr = ex.GetLatestPrice(ctx, symbol)
		if priceErr != nil || currentPrice <= 0 {
			logger.Warn("⚠️ 無法獲取當前價格，將使用持倉標記價格平倉")
		}
	}

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

	logger.Info("🧹 [平倉] 正在取消 %s 的所有挂單...", symbol)
	if err := ex.CancelAllOrders(ctx, symbol); err != nil {
		logger.Warn("⚠️ [平倉] 取消挂單失败: %v (將继续尝試平倉)", err)
	}

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
			Type:          exchange.OrderTypeMarket,
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
