package main

// 本文件抽自 main.go，集中存放 Web 注冊 / 資金費用同步 / 平倉等輔助函數。
// 拆分目的：避免單文件超過 3000 行硬上限。
// 行為與函數簽名保持不變，僅做位置遷移。

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/exchange/accounting"
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

type runtimePnLReader interface {
	GetPnLBySymbolAccountScopeAndAsset(symbol, accountScope, exchange, marketType, asset string, startTime, endTime time.Time) (*storage.PnLSummary, error)
}

func getRuntimePnLSummary(reader interface{}, rt *SymbolRuntime, start, end time.Time) (*storage.PnLSummary, error) {
	if rt == nil || reader == nil {
		return nil, fmt.Errorf("runtime PnL source is unavailable")
	}
	if strings.TrimSpace(rt.AccountScope) == "" {
		return nil, fmt.Errorf("runtime PnL requires immutable account scope")
	}
	if rt.Exchange == nil {
		return nil, fmt.Errorf("runtime PnL denomination source is unavailable")
	}
	asset := strings.ToUpper(strings.TrimSpace(rt.Exchange.GetQuoteAsset()))
	if asset == "" {
		return nil, fmt.Errorf("runtime PnL denomination is unavailable")
	}
	scopedReader, ok := reader.(runtimePnLReader)
	if !ok {
		return nil, fmt.Errorf("storage cannot query PnL by immutable scope and denomination")
	}
	return scopedReader.GetPnLBySymbolAccountScopeAndAsset(rt.Config.Symbol, rt.AccountScope, rt.Config.Exchange, rt.Config.GetMarketType(), asset, start, end)
}

func refreshRuntimePnLStatus(status *web.SystemStatus, reader interface{}, rt *SymbolRuntime, start, end time.Time) error {
	if status == nil {
		return fmt.Errorf("runtime PnL status is unavailable")
	}
	summary, err := getRuntimePnLSummary(reader, rt, start, end)
	if err != nil {
		clearRuntimePnLStatus(status)
		return err
	}
	if summary == nil || strings.TrimSpace(summary.PnLAsset) == "" || math.IsNaN(summary.TotalPnL) || math.IsInf(summary.TotalPnL, 0) {
		clearRuntimePnLStatus(status)
		return fmt.Errorf("runtime PnL summary lacks a valid amount or asset")
	}
	status.TotalPnL = summary.TotalPnL
	status.TotalTrades = summary.TotalTrades
	status.TotalPnLAsset = strings.ToUpper(strings.TrimSpace(summary.PnLAsset))
	status.TotalPnLVerified = true
	return nil
}

func clearRuntimePnLStatus(status *web.SystemStatus) {
	status.TotalPnL = 0
	status.TotalTrades = 0
	status.TotalPnLAsset = ""
	status.TotalPnLVerified = false
}

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

// fundingIncomeHistoryPageSize is Binance's configured income-history page limit.
const fundingIncomeHistoryPageSize = 1000

type scopedWithdrawExchange struct {
	exchange.IExchange
	accountScope string
}

func (e scopedWithdrawExchange) WithdrawalAccountScope() string { return e.accountScope }

func (e scopedWithdrawExchange) ReadAccountEvidence(ctx context.Context, since time.Time) (accounting.Snapshot, error) {
	source, ok := e.IExchange.(accounting.Source)
	if !ok {
		return accounting.Snapshot{}, fmt.Errorf("exchange does not provide account income evidence")
	}
	return source.ReadAccountEvidence(ctx, since)
}

func (e scopedWithdrawExchange) GetAccountFresh(ctx context.Context) (*exchange.Account, error) {
	reader, ok := e.IExchange.(interface {
		GetAccountFresh(context.Context) (*exchange.Account, error)
	})
	if !ok {
		return nil, fmt.Errorf("exchange does not provide uncached account balances")
	}
	return reader.GetAccountFresh(ctx)
}

func (e scopedWithdrawExchange) SupportsFundingIncomeHistory() bool {
	supported, ok := e.IExchange.(interface{ SupportsFundingIncomeHistory() bool })
	return ok && supported.SupportsFundingIncomeHistory()
}

func fundingIncomeSyncWindow(coveredFrom, now time.Time) (time.Time, time.Time, error) {
	if now.IsZero() {
		return time.Time{}, time.Time{}, fmt.Errorf("funding income sync requires a valid current time")
	}
	now = now.UTC()
	start := now.AddDate(0, 0, -30)
	if coveredFrom.IsZero() {
		// A new credential scope must not re-credit profits earned before this
		// scope was first observed; prior scopes may contain already-withdrawn PnL.
		start = now.Truncate(time.Millisecond)
	} else if coveredFrom.After(start) {
		start = coveredFrom.UTC()
	}
	if !start.Before(now) {
		return time.Time{}, time.Time{}, fmt.Errorf("funding income sync window is empty or starts in the future")
	}
	return start, now, nil
}

func fundingIncomeMarketType(marketType string) string {
	marketType = strings.ToLower(strings.TrimSpace(marketType))
	if marketType == config.MarketTypeFundingCarry || marketType == config.MarketTypeFundingPerpSpread {
		return "futures"
	}
	return marketType
}

func fundingIncomeManagedBySpecialRuntime(marketType string) bool {
	switch strings.ToLower(strings.TrimSpace(marketType)) {
	case config.MarketTypeFundingCarry, config.MarketTypeFundingPerpSpread:
		return true
	default:
		return false
	}
}

// startFundingIncomeSync 定時從交易所拉取資金費用（FUNDING_FEE）並寫入 funding_payments
func startFundingIncomeSync(ctx context.Context, st storage.Storage, ex exchange.IExchange, exchangeName, symbol, accountID, marketType, accountScope string) {
	if st == nil || ex == nil {
		return
	}
	supported, ok := ex.(interface{ SupportsFundingIncomeHistory() bool })
	if !ok || !supported.SupportsFundingIncomeHistory() {
		logger.Warn("⚠️ 交易所未实现可核验的资金费收入历史，提现账本覆盖将保持未验证 exchange=%s symbol=%s", exchangeName, symbol)
		return
	}
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	// 首次延遲 1 分鐘後立即回補近 30 天；若只等 ticker，首次同步會延後 6 小時。
	initialDelay := time.NewTimer(time.Minute)
	defer initialDelay.Stop()
	select {
	case <-ctx.Done():
		return
	case <-initialDelay.C:
	}
	for {
		if err := syncFundingIncomeOnce(ctx, st, ex, exchangeName, symbol, accountID, marketType, accountScope); err != nil {
			logger.Warn("⚠️ 資金費收入同步失敗 exchange=%s symbol=%s: %v", exchangeName, symbol, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func syncFundingIncomeOnce(ctx context.Context, st storage.Storage, ex exchange.IExchange, exchangeName, symbol, accountID, marketType, accountScope string) error {
	fundingMarketType := fundingIncomeMarketType(marketType)
	coverageReader, ok := st.(interface {
		GetFundingIncomeCoverage(string, string, string, string) (time.Time, time.Time, error)
	})
	if !ok {
		return fmt.Errorf("storage lacks funding coverage read capability")
	}
	coveredFrom, _, err := coverageReader.GetFundingIncomeCoverage(exchangeName, symbol, fundingMarketType, accountScope)
	if err != nil {
		return fmt.Errorf("read prior funding coverage: %w", err)
	}
	startTime, endTime, err := fundingIncomeSyncWindow(coveredFrom, time.Now())
	if err != nil {
		return err
	}
	list, err := ex.GetIncomeHistory(ctx, symbol, "FUNDING_FEE", startTime.UnixMilli(), endTime.UnixMilli())
	if err != nil {
		return fmt.Errorf("fetch funding history: %w", err)
	}
	saved := 0
	fullyPersisted := len(list) < fundingIncomeHistoryPageSize
	if !fullyPersisted {
		logger.Warn("⚠️ 資金費 API 返回已達單頁上限，不能證明歷史完整，保留舊覆蓋水位 exchange=%s symbol=%s count=%d", exchangeName, symbol, len(list))
	}
	for _, inc := range list {
		if inc == nil {
			logger.Warn("⚠️ 跳過空資金費記錄 exchange=%s symbol=%s", exchangeName, symbol)
			fullyPersisted = false
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(inc.Symbol), strings.TrimSpace(symbol)) || !strings.EqualFold(strings.TrimSpace(inc.IncomeType), "FUNDING_FEE") {
			logger.Warn("⚠️ 資金費 API 返回記錄與查詢範圍不一致，不能標記完整 exchange=%s requested_symbol=%s returned_symbol=%s income_type=%s", exchangeName, symbol, inc.Symbol, inc.IncomeType)
			fullyPersisted = false
			continue
		}
		if inc.TradeTime.Before(startTime) || inc.TradeTime.After(endTime) {
			logger.Warn("⚠️ 資金費 API 返回記錄超出查詢時間窗口，不能標記完整 exchange=%s symbol=%s transaction_id=%d", exchangeName, symbol, inc.TransactionID)
			fullyPersisted = false
			continue
		}
		if err := st.SaveFundingPayment(&storage.FundingPayment{
			Exchange: exchangeName, Symbol: inc.Symbol, Account: accountID, MarketType: fundingMarketType,
			AccountScope: accountScope, IncomeType: inc.IncomeType, Income: inc.Income,
			Asset: inc.Asset, Info: inc.Info, TransactionID: inc.TransactionID, TradeTime: inc.TradeTime,
		}); err != nil {
			logger.Warn("⚠️ 保存資金費記錄失敗 exchange=%s symbol=%s transaction_id=%d: %v", exchangeName, symbol, inc.TransactionID, err)
			fullyPersisted = false
			continue
		}
		saved++
	}
	if saved > 0 {
		logger.Info("💰 資金費用同步: %s %s 核對 %d 筆", exchangeName, symbol, saved)
	}
	if !fullyPersisted {
		return nil
	}
	coverageWriter, ok := st.(interface {
		MarkFundingIncomeCoverage(string, string, string, string, time.Time, time.Time) error
	})
	if !ok {
		return fmt.Errorf("storage lacks funding coverage write capability")
	}
	if err := coverageWriter.MarkFundingIncomeCoverage(exchangeName, symbol, fundingMarketType, accountScope, startTime, endTime); err != nil {
		return fmt.Errorf("mark complete funding coverage: %w", err)
	}
	return nil
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
	dbQueryCounter := 4
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
			if dbQueryCounter >= 5 {
				dbQueryCounter = 0
				now := utils.NowUTC()
				allHistoryStart := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
				var err error
				if storageSvc == nil || storageSvc.GetStorage() == nil {
					clearRuntimePnLStatus(st)
					err = fmt.Errorf("PnL storage is unavailable")
				} else {
					err = refreshRuntimePnLStatus(st, storageSvc.GetStorage(), rt, allHistoryStart, now)
				}
				if err != nil {
					logger.Warn("[%s:%s] 盈亏状态未核实: %v", rt.Config.Exchange, rt.Config.Symbol, err)
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
// 流程要求「穿價嘗試 + 明確終態 + 持倉核實」，不把提交當完成：
//  1. 參考價取 新鮮最新價 / 價格監控 / 標記價 中對平倉方最不利的一個（SELL 取最低、BUY 取最高），
//     再按 shutdownCloseSlippageRatio 穿價：SELL ≤ 參考價×(1−滑點)，BUY ≥ 參考價×(1+滑點)，IOC；
//  2. 輪詢本次訂單，超時則逐筆撤單並核實終態，UNKNOWN 不補單；
//  3. 重查交易所持倉，剩餘部分以 MARKET ReduceOnly 補平，並再次核實持倉；
//  4. 不撤銷其他委託；無法核實時返回錯誤並阻止獨立路徑盲目重試。

const (
	// shutdownCloseSlippageRatio 退出平倉限價單相對參考價的穿價比例（0.003 = 0.3%）
	shutdownCloseSlippageRatio = 0.003
	// shutdownCloseFillWait 等待限價平倉單成交 / 市價補單後持倉歸零的最長時間
	shutdownCloseFillWait = 3 * time.Second
	// shutdownClosePollInterval 輪詢訂單狀態 / 持倉的間隔
	shutdownClosePollInterval = 200 * time.Millisecond
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
		if p <= 0 || math.IsNaN(p) || math.IsInf(p, 0) {
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
		if math.IsNaN(px) || math.IsInf(px, 0) {
			return 0
		}
		return px
	}
	px := ref * (1 + shutdownCloseSlippageRatio)
	if priceDecimals >= 0 {
		// 向上取整（-floor(-x)），避免取整後回落到參考價附近
		px = -utils.FloorToDecimals(-px, priceDecimals)
	}
	if math.IsNaN(px) || math.IsInf(px, 0) {
		return 0
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
		if p.Size != 0 {
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
func closeAllPositionsMarketable(ctx context.Context, ex exchange.IExchange, symbol string, monitorPrice float64, opts shutdownCloseOptions) (failCount int, resultErr error) {
	_, failCount, resultErr = closePositionsVerifiedResult(ctx, ex, symbol, monitorPrice, opts)
	return failCount, resultErr
}

// closePositionsVerifiedResult counts confirmed positions, never submission ACKs.
// Partial or uncertain operations report zero confirmed successes conservatively;
// only the final successful order/position verification permits a success count.
func closePositionsVerifiedResult(ctx context.Context, ex exchange.IExchange, symbol string, monitorPrice float64, opts shutdownCloseOptions) (confirmedCount, failCount int, resultErr error) {
	if ex == nil || opts.fillWait <= 0 || opts.pollInterval <= 0 {
		return 0, 0, fmt.Errorf("invalid shutdown close dependencies or timeouts")
	}
	if config.IsSpotMarketType(ex.GetMarketType()) {
		return 0, 0, fmt.Errorf("spot shutdown requires owned inventory reconciliation")
	}
	open, err := queryShutdownPositions(ctx, ex, symbol)
	if err != nil || len(open) == 0 {
		return 0, len(open), err
	}
	// Any failure after a submission must survive into the runtime: a caller may
	// not interpret an error as permission for another independent close attempt.
	submitted := false
	defer func() {
		if resultErr != nil && submitted {
			resultErr = errors.Join(errShutdownCloseUnverified, resultErr)
			failCount = max(1, failCount)
		}
	}()
	freshPrice, priceErr := ex.GetLatestPrice(ctx, symbol)
	if priceErr != nil {
		freshPrice = 0
	}
	for _, pos := range open {
		side, qty := closeSideAndQty(pos.Size)
		px := marketableClosePrice(side, ex.GetPriceDecimals(), freshPrice, monitorPrice, pos.MarkPrice)
		if px <= 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return 0, len(open), err
		}
		submitted = true
		if err := submitAndVerifyShutdownOrder(ctx, ex, exchange.OrderRequest{
			Symbol: symbol, Side: side, Type: exchange.OrderTypeLimit, TimeInForce: exchange.TimeInForceIOC,
			Quantity: qty, Price: px, ReduceOnly: true, PriceDecimals: ex.GetPriceDecimals(),
		}, opts); err != nil {
			return 0, len(open), err
		}
	}
	// All submitted limit orders are confirmed terminal before a replacement is
	// considered. A cancel ACK, a position snapshot or a lost ACK is insufficient.
	left, err := queryShutdownPositions(ctx, ex, symbol)
	if err != nil {
		return 0, len(open), err
	}
	if err := validateShutdownResidual(open, left); err != nil {
		return 0, len(left), err
	}
	for _, pos := range left {
		if err := ctx.Err(); err != nil {
			return 0, len(left), err
		}
		side, qty := closeSideAndQty(pos.Size)
		submitted = true
		if err := submitAndVerifyShutdownOrder(ctx, ex, exchange.OrderRequest{
			Symbol: symbol, Side: side, Type: exchange.OrderTypeMarket, Quantity: qty,
			ReduceOnly: true, PriceDecimals: ex.GetPriceDecimals(),
		}, opts); err != nil {
			return 0, len(left), err
		}
	}
	remaining, err := waitPositionsFlat(ctx, ex, symbol, opts)
	if err != nil {
		return 0, max(len(left), len(remaining)), err
	}
	logger.Info("📊 [平倉核實完成] %s 已確認持倉歸零；僅處理本次平倉訂單，未撤其他委託", symbol)
	return len(open), 0, nil
}

// formatShutdownPrice 按精度格式化價格用於日誌
func formatShutdownPrice(px float64, decimals int) string {
	if decimals < 0 {
		decimals = 8
	}
	return fmt.Sprintf("%.*f", decimals, px)
}

// processCloseOnExitTimeout 進程級退出平倉單個 Bot 的超時
const processCloseOnExitTimeout = 30 * time.Second

// processCloseFunc 以完整賬戶/市場/交易對運行時組執行所有權平倉。
type processCloseFunc func(ctx context.Context, runtimes []*SymbolRuntime) error

// runProcessLevelCloseOnExit 執行 system.close_positions_on_exit，並與 Bot 級 close_on_stop 互斥：
//   - 由 decideShutdownCloseOwner 決定負責方，歸 Bot 級的跳過（Stop 時按 close_on_stop_config 平倉）；
//   - 同一 交易所/賬戶/交易對 只按交易所持倉平一次（多個 Bot 共用一個持倉時不重複下單）；
//   - 僅核實成功後標記已完成；提交後不確定則單獨標記待對賬，禁止另一條路徑補單。
func runProcessLevelCloseOnExit(enabled bool, runtimes []*SymbolRuntime, closeFn processCloseFunc) {
	runProcessLevelCloseOnExitContext(context.Background(), enabled, runtimes, closeFn)
}

func runProcessLevelCloseOnExitContext(parent context.Context, enabled bool, runtimes []*SymbolRuntime, closeFn processCloseFunc) {
	if !enabled || closeFn == nil {
		return
	}
	if parent == nil {
		parent = context.Background()
	}
	groups := make(map[string][]*SymbolRuntime)
	var keys []string
	for _, rt := range runtimes {
		if rt == nil {
			continue
		}
		key := shutdownRuntimeScopeKey(rt)
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], rt)
	}
	for _, key := range keys {
		group := groups[key]
		var processOwners []*SymbolRuntime
		for _, rt := range group {
			if rt.shutdownCloseUnverifiedReason() != "" {
				processOwners = nil
				break
			}
			if decideShutdownCloseOwner(enabled, rt.Config) == shutdownCloseProcess {
				processOwners = append(processOwners, rt)
			}
		}
		if len(processOwners) == 0 {
			continue
		}
		ctx, cancel := context.WithTimeout(parent, processCloseOnExitTimeout)
		err := closeFn(ctx, group)
		if ctx.Err() != nil {
			err = errors.Join(err, errShutdownCloseUnverified, ctx.Err())
		}
		cancel()
		if err != nil {
			logger.Warn("⚠️ [%s:%s] 進程級所有權平倉未完成：%v", processOwners[0].Config.Exchange, processOwners[0].Config.Symbol, err)
			if errors.Is(err, errShutdownCloseUnverified) {
				reason := "進程級平倉結果未核實，整個賬戶作用域禁止其他平倉路徑重試"
				for _, rt := range group {
					rt.markShutdownCloseUnverified(reason)
				}
			}
			continue
		}
		for _, rt := range processOwners {
			rt.markShutdownCloseHandled("已由進程級 Bot 所有權執行器完成平倉")
		}
	}
}

// ===== 風控接線（審查報告 C1 / C4）=====

// riskEquityQueryTimeout 熔斷喂數查詢單個賬戶權益的超時
const riskEquityQueryTimeout = 10 * time.Second

// storageTradeHistorySource 用 storage.trades 實現 risk.TradeHistorySource
type storageTradeHistorySource struct {
	storageService *storage.StorageService
}

type tradeHistoryStorageScanner interface {
	ScanTradesContext(ctx context.Context, start, end time.Time, visit func(*storage.Trade) bool) error
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
			Key:      fmt.Sprintf("trade:%s:%s:%d", t.Exchange, t.Account, t.ID),
			NetPnL:   t.PnL - t.Fee,
			PnLAsset: t.PnLAsset,
			Fee:      t.Fee,
			FeeAsset: t.FeeAsset,
			ClosedAt: t.CreatedAt,
		})
	}
	return out, nil
}

func (s *storageTradeHistorySource) ScanTradesBetween(ctx context.Context, start, end time.Time, visit func(risk.TradeOutcome) bool) error {
	if s.storageService == nil || s.storageService.GetStorage() == nil {
		return fmt.Errorf("trade history storage unavailable")
	}
	scanner, ok := s.storageService.GetStorage().(tradeHistoryStorageScanner)
	if !ok {
		return fmt.Errorf("trade history storage does not support complete streaming scans")
	}
	return scanner.ScanTradesContext(ctx, start, end, func(trade *storage.Trade) bool {
		if trade == nil {
			return true
		}
		return visit(risk.TradeOutcome{
			Key:      fmt.Sprintf("trade:%s:%s:%d", trade.Exchange, trade.Account, trade.ID),
			NetPnL:   trade.PnL - trade.Fee,
			PnLAsset: trade.PnLAsset,
			Fee:      trade.Fee,
			FeeAsset: trade.FeeAsset,
			ClosedAt: trade.CreatedAt,
		})
	})
}

// runtimeEquitySource 匯總所有合約運行時所屬賬戶的保證金餘額（含未實現盈虧），按交易所+賬戶去重
type runtimeEquitySource struct {
	manager *SymbolManager
}

func (s *runtimeEquitySource) TotalEquity(ctx context.Context) (float64, error) {
	observation, err := s.ObserveEquity(ctx, time.Time{})
	return observation.Equity, err
}

func circuitBreakerMetricsOptions(gcb *risk.GlobalCircuitBreaker, storageService *storage.StorageService) risk.MetricsFeederOptions {
	return risk.MetricsFeederOptions{
		Location:                      utils.GlobalLocation,
		EquityStore:                   equityStateStore(storageService),
		RequirePersistence:            true,
		RequireTradeHistory:           gcb.RequiresTradeHistory(),
		RequireCashFlowReconciliation: true,
	}
}

// startCircuitBreakerFeeder 啟動熔斷器內部喂數與連線事件訂閱
func startCircuitBreakerFeeder(ctx context.Context, gcb *risk.GlobalCircuitBreaker, eventBus *event.EventBus, storageService *storage.StorageService, symbolManager *SymbolManager) {
	var trades risk.TradeHistorySource
	if storageService != nil && storageService.GetStorage() != nil {
		trades = &storageTradeHistorySource{storageService: storageService}
	} else {
		logger.Warn("⚠️ [全局熔斷] 存儲未啟用：成交歷史指標不可用；若啟用日虧損/連虧觸發，將保持風險數據門控封鎖")
	}
	feeder := risk.NewMetricsFeeder(
		gcb,
		trades,
		&runtimeEquitySource{manager: symbolManager},
		&botManagerProviderAdapter{manager: symbolManager},
		circuitBreakerMetricsOptions(gcb, storageService),
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
		case binance.ConnectivityReconnected:
			eventType = event.EventTypeWebSocketReconnected
		case binance.ConnectivityStopped:
			eventType = event.EventTypeWebSocketStopped
		case binance.ConnectivityAuthFailed:
			eventType = event.EventTypeAPIAuthFailed
		default:
			return
		}
		eventBus.Publish(&event.Event{
			Type: eventType,
			Data: map[string]interface{}{
				"exchange":      e.Exchange,
				"stream":        e.Stream,
				"symbol":        e.Symbol,
				"testnet":       e.Testnet,
				"connection_id": e.ConnectionID,
				"reason":        e.Reason,
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
