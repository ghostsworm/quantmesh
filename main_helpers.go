package main

// 本文件抽自 main.go，集中存放 Web 注冊 / 資金費用同步 / 平倉等輔助函數。
// 拆分目的：避免單文件超過 3000 行硬上限。
// 行為與函數簽名保持不變，僅做位置遷移。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/exchange/binance"
	"quantmesh/logger"
	"quantmesh/mcp"
	"quantmesh/monitor"
	"quantmesh/notify/observability"
	"quantmesh/position"
	"quantmesh/risk"
	"quantmesh/safety"
	"quantmesh/storage"
	"quantmesh/utils"
	"quantmesh/web"
)

var observabilityBootstrapped sync.Once

// ordersSchemaRepairer 由 *storage.SQLStorage 實現；接口定義在使用方。
type ordersSchemaRepairer interface {
	EnsureOrdersSchema() error
}

// repairOrdersSchemaAfterGORM 在 database 包 GORM AutoMigrate 之後重新校驗 orders 複合唯一索引。
func repairOrdersSchemaAfterGORM(storageService *storage.StorageService) {
	if storageService == nil {
		return
	}
	repairer, ok := storageService.GetStorage().(ordersSchemaRepairer)
	if !ok {
		return
	}
	if err := repairer.EnsureOrdersSchema(); err != nil {
		logger.Warn("⚠️ GORM 初始化後修復 orders 唯一索引失败（SaveOrder 會在首次寫入時再嘗試自愈）: %v", err)
	}
}

// bootstrapObservability 读取 system_settings 中的 PostHog / Sentry 配置并启动上报，
// 并把 logger 的 ERROR/FATAL 钩子接到 observability.ReportMessage。
//
// 幂等：多次调用只生效一次（multi-tenant 路径可能调两次）。
// 未启用或未填密钥时保持 no-op，失败不影响主流程。
//
// 注：该 hook 先前由已移除的 bootstrapAipipe 统一安装（一个 hook 里同时转发给
// aipipe 和 observability），因为 SetErrorHook 是后注册覆盖前注册的单槽机制。
// 移除 aipipe 后这里是唯一的 hook 安装点；若将来再加上报通道，仍需在同一个
// hook 内扇出，不要各自 SetErrorHook。
func bootstrapObservability(version string, provider web.SystemSettingsProvider) {
	if provider == nil {
		return
	}
	observabilityBootstrapped.Do(func() {
		cfg := loadObservabilityConfigFromSettings(version, provider)
		observability.Reload(cfg)
		// 装错误外发钩子：ERROR/FATAL 自动上报
		logger.SetErrorHook(func(level, message string) {
			observability.ReportMessage(level, message, "log")
		})
		web.RegisterObservabilityReloader(func() {
			observability.Reload(loadObservabilityConfigFromSettings(version, provider))
		})
		if cfg.PostHogEnabled || cfg.SentryEnabled {
			logger.Info("✅ PostHog/Sentry 可观测性上报配置已加载")
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

// loadObservabilityConfigFromSettings 从 system_settings 中读取 PostHog / Sentry 配置。
func loadObservabilityConfigFromSettings(version string, provider web.SystemSettingsProvider) observability.Config {
	ctx := context.Background()
	cfg := observability.Config{
		PostHogHost: observability.DefaultPostHogHost,
		Environment: observability.DefaultEnvironment,
		Release:     version,
		DistinctID:  "quantmesh-server",
	}
	if v, err := provider.GetSystemSetting(ctx, observability.SettingKeyPostHogProjectKey); err == nil && v != nil {
		cfg.PostHogProjectKey = v.Value
	}
	if v, err := provider.GetSystemSetting(ctx, observability.SettingKeyPostHogHost); err == nil && v != nil && v.Value != "" {
		cfg.PostHogHost = v.Value
	}
	if enabled, err := provider.GetSystemSettingBool(ctx, observability.SettingKeyPostHogEnabled, false); err == nil {
		cfg.PostHogEnabled = enabled
	}
	if v, err := provider.GetSystemSetting(ctx, observability.SettingKeySentryDSN); err == nil && v != nil {
		cfg.SentryDSN = v.Value
	}
	if enabled, err := provider.GetSystemSettingBool(ctx, observability.SettingKeySentryEnabled, false); err == nil {
		cfg.SentryEnabled = enabled
	}
	if v, err := provider.GetSystemSetting(ctx, observability.SettingKeyEnvironment); err == nil && v != nil && v.Value != "" {
		cfg.Environment = v.Value
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

// ===== 進程級退出平倉（close_positions_on_exit）=====
//
// 測試網試跑發現：舊實現用 priceMonitor 的（可能過期的）最新價掛 GTC 限價 ReduceOnly 單，
// SELL 價 76363.90 高於當時市價 ~76348，平倉單掛在盤口上不成交，只 sleep 2s 就退出。
// 新流程保證「可成交 + 核實 + 不留掛單」：
//  1. 參考價取 新鮮最新價 / 價格監控 / 標記價 中對平倉方最不利的一個（SELL 取最低、BUY 取最高），
//     再按 shutdownCloseSlippageRatio 穿價：SELL ≤ 參考價×(1−滑點)，BUY ≥ 參考價×(1+滑點)，IOC；
//  2. 輪詢訂單狀態等待成交，超時未終結的撤單（撤單失敗記 ERROR 帶訂單號）；
//  3. 重查交易所持倉，剩餘部分以 MARKET ReduceOnly 補平，並再次核實持倉；
//  4. 最後撤銷該交易對全部掛單（覆蓋撤單後被網格重掛的止盈單），仍有殘留記 ERROR 帶訂單號。

const (
	// shutdownCloseSlippageRatio 退出平倉限價單相對參考價的穿價比例（0.003 = 0.3%）
	shutdownCloseSlippageRatio = 0.003
	// shutdownCloseFillWait 等待限價平倉單成交 / 市價補單後持倉歸零的最長時間
	shutdownCloseFillWait = 3 * time.Second
	// shutdownClosePollInterval 輪詢訂單狀態 / 持倉的間隔
	shutdownClosePollInterval = 200 * time.Millisecond
	// shutdownCloseCancelTimeout 最終清理掛單的超時（不依賴可能已耗盡的平倉 ctx）
	shutdownCloseCancelTimeout = 5 * time.Second
)

// shutdownCloseOptions 退出平倉的等待參數（測試中縮短）
type shutdownCloseOptions struct {
	fillWait     time.Duration
	pollInterval time.Duration
}

func defaultShutdownCloseOptions() shutdownCloseOptions {
	return shutdownCloseOptions{fillWait: shutdownCloseFillWait, pollInterval: shutdownClosePollInterval}
}

// closeAllPositions 平掉所有持倉（退出時使用）。返回未能平掉的持倉數；查詢持倉失敗時返回 error。
func closeAllPositions(ctx context.Context, ex exchange.IExchange, symbol string, priceMonitor *monitor.PriceMonitor) (int, error) {
	monitorPrice := 0.0
	if priceMonitor != nil {
		monitorPrice = priceMonitor.GetLastPrice()
	}
	return closeAllPositionsMarketable(ctx, ex, symbol, monitorPrice, defaultShutdownCloseOptions())
}

// marketableClosePrice 計算穿價的平倉限價；參考價均無效時返回 0（調用方直接走市價）。
// SELL 取候選最低價再向下穿價，BUY 取候選最高價再向上穿價，保證不會掛在盤口外側。
func marketableClosePrice(side exchange.Side, priceDecimals int, candidates ...float64) float64 {
	ref := 0.0
	for _, p := range candidates {
		if p <= 0 {
			continue
		}
		if ref == 0 || (side == exchange.SideSell && p < ref) || (side == exchange.SideBuy && p > ref) {
			ref = p
		}
	}
	if ref <= 0 {
		return 0
	}
	if side == exchange.SideSell {
		px := ref * (1 - shutdownCloseSlippageRatio)
		if priceDecimals >= 0 {
			px = utils.FloorToDecimals(px, priceDecimals)
		}
		return px
	}
	px := ref * (1 + shutdownCloseSlippageRatio)
	if priceDecimals >= 0 {
		// 向上取整（-floor(-x)），避免取整後回落到參考價附近
		px = -utils.FloorToDecimals(-px, priceDecimals)
	}
	return px
}

// closeSideAndQty 由持倉數量得到平倉方向與數量
func closeSideAndQty(size float64) (exchange.Side, float64) {
	if size > 0 {
		return exchange.SideSell, size
	}
	return exchange.SideBuy, -size
}

// nonZeroPositions 過濾本交易對非零持倉
func nonZeroPositions(positions []*exchange.Position, symbol string) []*exchange.Position {
	out := make([]*exchange.Position, 0, len(positions))
	for _, p := range positions {
		if p == nil || (p.Symbol != "" && !strings.EqualFold(p.Symbol, symbol)) {
			continue
		}
		if p.Size > exchangePositionFlatEpsilon || p.Size < -exchangePositionFlatEpsilon {
			out = append(out, p)
		}
	}
	return out
}

func isTerminalOrderStatus(s exchange.OrderStatus) bool {
	switch s {
	case exchange.OrderStatusFilled, exchange.OrderStatusCanceled, exchange.OrderStatusRejected, exchange.OrderStatusExpired:
		return true
	}
	return false
}

// closeAllPositionsMarketable 見本段頭部註釋
func closeAllPositionsMarketable(ctx context.Context, ex exchange.IExchange, symbol string, monitorPrice float64, opts shutdownCloseOptions) (int, error) {
	positions, err := ex.GetPositions(ctx, symbol)
	if err != nil {
		logger.Error("❌ 查詢持倉失败，無法平倉: %v", err)
		return 0, fmt.Errorf("查詢 %s 持倉失敗: %w", symbol, err)
	}
	open := nonZeroPositions(positions, symbol)
	if len(open) == 0 {
		logger.Info("ℹ️ 當前没有有效持倉，無需平倉")
		return 0, nil
	}
	logger.Info("🔄 发現 %d 個持倉需要平倉", len(open))

	freshPrice, priceErr := ex.GetLatestPrice(ctx, symbol)
	if priceErr != nil {
		logger.Warn("⚠️ [平倉] 獲取 %s 最新價失敗（將只用監控價/標記價，均無效時直接市價）: %v", symbol, priceErr)
		freshPrice = 0
	}

	// 1. 穿價 IOC 限價單
	placedIDs := make([]int64, 0, len(open))
	for _, pos := range open {
		side, qty := closeSideAndQty(pos.Size)
		px := marketableClosePrice(side, ex.GetPriceDecimals(), freshPrice, monitorPrice, pos.MarkPrice)
		if px <= 0 {
			logger.Warn("⚠️ [平倉] %s 無有效參考價，跳過限價直接走市價", symbol)
			continue
		}
		logger.Info("🔄 [平倉] %s %s %.6f @ %s (穿價限價 IOC ReduceOnly，參考價 最新=%.4f 監控=%.4f 標記=%.4f)",
			side, symbol, qty, formatShutdownPrice(px, ex.GetPriceDecimals()), freshPrice, monitorPrice, pos.MarkPrice)
		ord, placeErr := ex.PlaceOrder(ctx, &exchange.OrderRequest{
			Symbol:        symbol,
			Side:          side,
			Type:          exchange.OrderTypeLimit,
			TimeInForce:   exchange.TimeInForceIOC,
			Quantity:      qty,
			Price:         px,
			ReduceOnly:    true,
			PriceDecimals: ex.GetPriceDecimals(),
		})
		if placeErr != nil {
			logger.Warn("⚠️ [平倉] 限價平倉下單失敗 %s %.6f @ %.4f（將以市價補平）: %v", side, qty, px, placeErr)
			continue
		}
		if ord != nil && ord.OrderID != 0 {
			placedIDs = append(placedIDs, ord.OrderID)
		}
	}

	// 2. 等待成交；未終結的撤單
	if len(placedIDs) > 0 {
		logger.Info("⏳ 等待平倉單成交（最多 %s）...", opts.fillWait)
		pending := waitOrdersTerminal(ctx, ex, symbol, placedIDs, opts)
		for _, id := range pending {
			if cancelErr := ex.CancelOrder(ctx, symbol, id); cancelErr != nil {
				logger.Error("❌ [平倉] 撤銷未成交平倉單失敗 %s orderID=%d: %v", symbol, id, cancelErr)
			} else {
				logger.Info("🧹 [平倉] 已撤銷未完全成交的平倉單 orderID=%d", id)
			}
		}
	}

	// 3. 重查持倉，剩餘以市價補平
	failCount := 0
	remaining, err := ex.GetPositions(ctx, symbol)
	if err != nil {
		logger.Error("❌ [平倉] 限價階段後重查 %s 持倉失敗: %v", symbol, err)
		return len(open), fmt.Errorf("重查 %s 持倉失敗: %w", symbol, err)
	}
	left := nonZeroPositions(remaining, symbol)
	if len(left) > 0 {
		for _, pos := range left {
			side, qty := closeSideAndQty(pos.Size)
			logger.Warn("⚠️ [平倉] %s 仍有 %.6f 未平，市價 ReduceOnly 補平 (%s)", symbol, pos.Size, side)
			if _, placeErr := ex.PlaceOrder(ctx, &exchange.OrderRequest{
				Symbol:        symbol,
				Side:          side,
				Type:          exchange.OrderTypeMarket,
				Quantity:      qty,
				ReduceOnly:    true,
				PriceDecimals: ex.GetPriceDecimals(),
			}); placeErr != nil {
				logger.Error("❌ [平倉] 市價補平失敗 %s %s %.6f: %v", symbol, side, qty, placeErr)
			}
		}
		left = waitPositionsFlat(ctx, ex, symbol, opts)
		failCount = len(left)
		for _, pos := range left {
			logger.Error("❌ [平倉] %s 退出時持倉未能平掉: %.6f，請手動處理", symbol, pos.Size)
		}
	}

	// 4. 最終清理：不留任何掛單
	cleanupShutdownOpenOrders(ctx, ex, symbol)

	logger.Info("📊 [平倉完成] 需平 %d，未平 %d", len(open), failCount)
	return failCount, nil
}

// formatShutdownPrice 按精度格式化價格用於日誌
func formatShutdownPrice(px float64, decimals int) string {
	if decimals < 0 {
		decimals = 8
	}
	return fmt.Sprintf("%.*f", decimals, px)
}

// waitOrdersTerminal 輪詢訂單直到全部終結或超時，返回仍未終結（或查詢失敗）的訂單號
func waitOrdersTerminal(ctx context.Context, ex exchange.IExchange, symbol string, ids []int64, opts shutdownCloseOptions) []int64 {
	deadline := time.Now().Add(opts.fillWait)
	pending := append([]int64(nil), ids...)
	for {
		next := pending[:0]
		for _, id := range pending {
			ord, err := ex.GetOrder(ctx, symbol, id)
			if err != nil || ord == nil || !isTerminalOrderStatus(ord.Status) {
				next = append(next, id)
			}
		}
		pending = next
		if len(pending) == 0 || !time.Now().Before(deadline) || ctx.Err() != nil {
			return pending
		}
		time.Sleep(opts.pollInterval)
	}
}

// waitPositionsFlat 輪詢持倉直到歸零或超時，返回仍非零的持倉（查詢失敗時返回 nil 以外的最後一次結果）
func waitPositionsFlat(ctx context.Context, ex exchange.IExchange, symbol string, opts shutdownCloseOptions) []*exchange.Position {
	deadline := time.Now().Add(opts.fillWait)
	var last []*exchange.Position
	for {
		positions, err := ex.GetPositions(ctx, symbol)
		if err != nil {
			logger.Warn("⚠️ [平倉] 核實 %s 持倉失敗: %v", symbol, err)
		} else {
			last = nonZeroPositions(positions, symbol)
			if len(last) == 0 {
				return nil
			}
		}
		if !time.Now().Before(deadline) || ctx.Err() != nil {
			return last
		}
		time.Sleep(opts.pollInterval)
	}
}

// cleanupShutdownOpenOrders 撤銷本交易對全部掛單並核實；仍有殘留時記 ERROR 帶訂單號
func cleanupShutdownOpenOrders(ctx context.Context, ex exchange.IExchange, symbol string) {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownCloseCancelTimeout)
	defer cancel()
	orders, err := ex.GetOpenOrders(cctx, symbol)
	if err != nil {
		logger.Warn("⚠️ [平倉] 查詢 %s 殘留掛單失敗，仍嘗試全部撤銷: %v", symbol, err)
	} else if len(orders) == 0 {
		return
	}
	if cancelErr := ex.CancelAllOrders(cctx, symbol); cancelErr != nil {
		logger.Error("❌ [平倉] 退出前撤銷 %s 全部掛單失敗: %v", symbol, cancelErr)
	}
	left, err := ex.GetOpenOrders(cctx, symbol)
	if err != nil {
		logger.Error("❌ [平倉] 核實 %s 殘留掛單失敗: %v", symbol, err)
		return
	}
	if len(left) > 0 {
		ids := make([]string, 0, len(left))
		for _, o := range left {
			if o != nil {
				ids = append(ids, fmt.Sprintf("%d", o.OrderID))
			}
		}
		logger.Error("❌ [平倉] 退出時 %s 仍有 %d 個掛單未撤銷，訂單號: %s，請手動處理",
			symbol, len(left), strings.Join(ids, ","))
	}
}

// processCloseOnExitTimeout 進程級退出平倉單個 Bot 的超時
const processCloseOnExitTimeout = 30 * time.Second

// processCloseFunc 進程級平倉單個 Bot 的實現，返回失敗數與查詢錯誤（抽出以便測試）
type processCloseFunc func(ctx context.Context, rt *SymbolRuntime) (failCount int, err error)

// runProcessLevelCloseOnExit 執行 system.close_positions_on_exit，並與 Bot 級 close_on_stop 互斥：
//   - 由 decideShutdownCloseOwner 決定負責方，歸 Bot 級的跳過（Stop 時按 close_on_stop_config 平倉）；
//   - 同一 交易所/賬戶/交易對 只按交易所持倉平一次（多個 Bot 共用一個持倉時不重複下單）；
//   - 平倉成功後標記 rt，Stop 中的 close_on_stop 跳過；失敗時不標記，保留 close_on_stop 兜底（其前會重查交易所持倉）。
func runProcessLevelCloseOnExit(enabled bool, runtimes []*SymbolRuntime, closeFn processCloseFunc) {
	if !enabled || closeFn == nil {
		return
	}
	closedKeys := make(map[string]bool)
	for _, rt := range runtimes {
		if rt == nil {
			continue
		}
		sc := rt.Config
		owner := decideShutdownCloseOwner(enabled, sc)
		if owner == shutdownCloseBot {
			logger.Info("ℹ️ [%s:%s] 退出平倉交給 Bot 級 close_on_stop（已配置 close_on_stop_config 或現貨），進程級跳過",
				sc.Exchange, sc.Symbol)
			continue
		}
		if owner != shutdownCloseProcess {
			continue
		}
		key := strings.ToLower(sc.Exchange) + "|" + rt.AccountID + "|" + strings.ToUpper(sc.Symbol)
		if closedKeys[key] {
			rt.markShutdownCloseHandled("同交易所賬戶交易對已由進程級 close_positions_on_exit 平倉")
			continue
		}
		logger.Info("🔄 [%s:%s] 正在平掉所有持倉...", sc.Exchange, sc.Symbol)
		ctx, cancel := context.WithTimeout(context.Background(), processCloseOnExitTimeout)
		failCount, err := closeFn(ctx, rt)
		cancel()
		if err != nil || failCount > 0 {
			logger.Warn("⚠️ [%s:%s] 進程級退出平倉未完全成功（失敗 %d，err=%v），保留 close_on_stop 兜底",
				sc.Exchange, sc.Symbol, failCount, err)
			continue
		}
		closedKeys[key] = true
		rt.markShutdownCloseHandled("已由進程級 close_positions_on_exit 平倉")
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

// ===== 風控接線（審查報告 C1 / C4）=====

// riskEquityQueryTimeout 熔斷喂數查詢單個賬戶權益的超時
const riskEquityQueryTimeout = 10 * time.Second

// storageTradeHistorySource 用 storage.trades 實現 risk.TradeHistorySource
type storageTradeHistorySource struct {
	storageService *storage.StorageService
}

func (s *storageTradeHistorySource) TradesBetween(_ context.Context, start, end time.Time, limit int) ([]risk.TradeOutcome, error) {
	if s.storageService == nil || s.storageService.GetStorage() == nil {
		return nil, nil
	}
	trades, err := s.storageService.GetStorage().QueryTrades(start, end, limit, 0)
	if err != nil {
		return nil, fmt.Errorf("QueryTrades(%s ~ %s, limit=%d): %w", start.Format(time.RFC3339), end.Format(time.RFC3339), limit, err)
	}
	out := make([]risk.TradeOutcome, 0, len(trades))
	for _, t := range trades {
		if t == nil {
			continue
		}
		out = append(out, risk.TradeOutcome{
			Key:      fmt.Sprintf("%s:%s:%d:%d", t.Exchange, t.Symbol, t.BuyOrderID, t.SellOrderID),
			NetPnL:   t.PnL - t.Fee,
			ClosedAt: t.CreatedAt,
		})
	}
	return out, nil
}

// runtimeEquitySource 匯總所有合約運行時所屬賬戶的保證金餘額（含未實現盈虧），按交易所+賬戶去重
type runtimeEquitySource struct {
	manager *SymbolManager
}

func (s *runtimeEquitySource) TotalEquity(ctx context.Context) (float64, error) {
	seen := make(map[string]bool)
	total := 0.0
	var errs []error
	for _, rt := range s.manager.List() {
		if rt == nil || rt.Exchange == nil || rt.Config.GetMarketType() != "futures" {
			continue
		}
		key := rt.Config.Exchange + "|" + rt.AccountID
		if seen[key] {
			continue
		}
		seen[key] = true

		qctx, cancel := context.WithTimeout(ctx, riskEquityQueryTimeout)
		acct, err := rt.Exchange.GetAccount(qctx)
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("GetAccount(%s): %w", rt.Config.Exchange, err))
			continue
		}
		if acct == nil {
			errs = append(errs, fmt.Errorf("GetAccount(%s): 返回空賬戶", rt.Config.Exchange))
			continue
		}
		total += acct.TotalMarginBalance
	}
	if len(errs) > 0 {
		// 任一賬戶失敗都返回錯誤：部分賬戶的權益作為回撤基準會嚴重失真
		return 0, errors.Join(errs...)
	}
	if len(seen) == 0 {
		return 0, fmt.Errorf("沒有運行中的合約 Bot: %w", risk.ErrEquityUnavailable)
	}
	return total, nil
}

// startCircuitBreakerFeeder 啟動熔斷器內部喂數與連線事件訂閱
func startCircuitBreakerFeeder(ctx context.Context, gcb *risk.GlobalCircuitBreaker, eventBus *event.EventBus, storageService *storage.StorageService, symbolManager *SymbolManager) {
	var trades risk.TradeHistorySource
	if storageService != nil && storageService.GetStorage() != nil {
		trades = &storageTradeHistorySource{storageService: storageService}
	} else {
		logger.Warn("⚠️ [全局熔斷] 存儲未啟用：單日已實現盈虧與連續虧損無數據，僅按未實現盈虧計算")
	}
	feeder := risk.NewMetricsFeeder(
		gcb,
		trades,
		&runtimeEquitySource{manager: symbolManager},
		&botManagerProviderAdapter{manager: symbolManager},
		risk.MetricsFeederOptions{Location: utils.GlobalLocation},
	)
	feeder.Start(ctx)
	gcb.SubscribeConnectivityEvents(ctx, eventBus)
	wireBinanceConnectivityEvents(eventBus)
}

// wireBinanceConnectivityEvents 把 Binance 合約用戶數據流的斷線/重連/認證失敗轉發到事件總線（熔斷器訂閱）
func wireBinanceConnectivityEvents(eventBus *event.EventBus) {
	if eventBus == nil {
		return
	}
	binance.SetConnectivityEventHandler(func(e binance.ConnectivityEvent) {
		var eventType event.EventType
		switch e.Type {
		case binance.ConnectivityDisconnected:
			eventType = event.EventTypeWebSocketDisconnected
		case binance.ConnectivityReconnected, binance.ConnectivityStopped:
			// 斷線期間主動停止也清除斷線計時，避免停掉的 Bot 觸發「長時間斷線」熔斷
			eventType = event.EventTypeWebSocketReconnected
		case binance.ConnectivityAuthFailed:
			eventType = event.EventTypeAPIAuthFailed
		default:
			return
		}
		eventBus.Publish(&event.Event{
			Type: eventType,
			Data: map[string]interface{}{
				"exchange": e.Exchange,
				"stream":   e.Stream,
				"symbol":   e.Symbol,
				"testnet":  e.Testnet,
				"reason":   e.Reason,
			},
		})
	})
}

// startCompositeRiskGuard 接線複合風控：註冊全局可用因子，stop_trading 時走與熔斷器相同的暫停開倉路徑。
// 趨勢/深度/資金費率/K線因子是按交易對、且按做多語義評分的，全局暫停所有 Bot（含做空）會誤傷，暫不註冊。
func startCompositeRiskGuard(ctx context.Context, cfg *config.Config, symbolManager *SymbolManager, pauser *risk.OpeningPauseCoordinator,
	notifier risk.AlertNotifier, newsMonitor *monitor.NewsMonitor, macroProvider safety.MacroEventProvider) *safety.CompositeRiskController {
	if cfg == nil || !cfg.CompositeRisk.Enabled {
		return nil
	}
	controller := safety.NewCompositeRiskController(cfg)
	registered := 0
	if cfg.CompositeRisk.Factors.News.Enabled && newsMonitor != nil {
		controller.RegisterFactor(safety.NewNewsRiskFactor(cfg, "", newsMonitor))
		registered++
	}
	if cfg.CompositeRisk.Factors.Macro.Enabled && macroProvider != nil {
		controller.RegisterFactor(safety.NewMacroEventRiskFactor(cfg, macroProvider))
		registered++
	}
	f := cfg.CompositeRisk.Factors
	if f.Trend.Enabled || f.Depth.Enabled || f.FundingRate.Enabled || f.Kline.Enabled {
		logger.Warn("⚠️ [複合風控] trend/depth/funding_rate/kline 因子為按交易對的做多語義評分，暫不支持全局接線，已忽略")
	}
	if registered == 0 {
		logger.Warn("⚠️ [複合風控] 已啟用但沒有可用因子（需啟用 news 並開啟新聞監控，或啟用 macro 並開啟宏觀事件），不生效")
		return nil
	}

	guard := risk.NewCompositeRiskGuard(&botManagerProviderAdapter{manager: symbolManager}, pauser, notifier)
	controller.SetResultHandler(func(r safety.CompositeRiskResult) {
		signal := risk.CompositeRiskRelease
		switch r.Level {
		case safety.RiskStopTrading:
			signal = risk.CompositeRiskStop
		case safety.RiskPauseBuying:
			signal = risk.CompositeRiskHold // 滯回：未回落到 reduce_position 以下前不解除
		}
		guard.Apply(signal, string(r.Level), r.CompositeScore, r.Reasons)
	})
	controller.Start(ctx)
	logger.Info("✅ [複合風控] 已接線 %d 個全局因子，stop_trading 時暫停所有 Bot 開倉", registered)
	return controller
}

// warnDynamicStopLossNotImplemented 動態止損各檢查器仍為 stub：不啟動，明確告警，避免誤以為已生效
func warnDynamicStopLossNotImplemented(cfg *config.Config) {
	if cfg == nil || !cfg.DynamicStopLoss.Enabled {
		return
	}
	logger.Warn("⚠️ [動態止損] 配置 dynamic_stop_loss.enabled=true，但波動率/時間分段/盈利追蹤/趨勢反轉調整均未實現，" +
		"本版本不會調整任何止損，管理器不啟動。請使用 grid_risk_control 的 stop_loss_ratio / trailing_take_profit_ratio")
}
