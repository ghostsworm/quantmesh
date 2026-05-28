package main

// 本文件抽自 main.go，集中存放面向 Web API / 業務系統的適配器類型。
// 拆分目的：避免單文件超過 3000 行硬上限。
// 行為與類型語意保持不變，僅做位置遷移。

import (
	"context"
	"fmt"
	"time"

	"quantmesh/ai"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/position"
	"quantmesh/storage"
	"quantmesh/web"
)

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

// polymarketSignalAdapter 將 PolymarketSignalAnalyzer 接到 Web API（開源版內建）。
type polymarketSignalAdapter struct {
	analyzer *ai.PolymarketSignalAnalyzer
}

func (a *polymarketSignalAdapter) GetLastAnalysis() interface{} {
	if a == nil || a.analyzer == nil {
		return nil
	}
	return a.analyzer.GetLastAnalysis()
}

func (a *polymarketSignalAdapter) GetLastAnalysisTime() time.Time {
	if a == nil || a.analyzer == nil {
		return time.Time{}
	}
	return a.analyzer.GetLastAnalysisTime()
}

func (a *polymarketSignalAdapter) PerformAnalysis() error {
	if a == nil || a.analyzer == nil {
		return fmt.Errorf("polymarket analyzer 未初始化")
	}
	return a.analyzer.TriggerAnalysis()
}

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
	botID          string // 與運行時 Bot 一致，寫入 trades.bot_id
}

func (a *tradeStorageAdapter) SaveTrade(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, fee float64, feeAsset string, createdAt time.Time, botID string) error {
	return a.SaveTradeWithDeviation(buyOrderID, sellOrderID, exchange, symbol, buyPrice, sellPrice, quantity, pnl, fee, feeAsset, 0, 0, createdAt, botID)
}

func (a *tradeStorageAdapter) SaveTradeWithDeviation(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, fee float64, feeAsset string, buyPriceDeviation, sellPriceDeviation float64, createdAt time.Time, botID string) error {
	if a.storageService == nil {
		return nil
	}
	st := a.storageService.GetStorage()
	if st == nil {
		return nil
	}
	bid := botID
	if bid == "" {
		bid = a.botID
	}
	if sqliteSt, ok := st.(interface {
		SaveTradeWithExchangePnL(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, exchangePnL, fee float64, feeAsset string, buyPriceDeviation, sellPriceDeviation float64, createdAt time.Time, botID string) error
	}); ok {
		return sqliteSt.SaveTradeWithExchangePnL(buyOrderID, sellOrderID, exchange, symbol, buyPrice, sellPrice, quantity, pnl, 0, fee, feeAsset, buyPriceDeviation, sellPriceDeviation, createdAt, bid)
	}
	if sqliteSt, ok := st.(interface {
		SaveTradeWithDeviation(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, fee float64, feeAsset string, buyPriceDeviation, sellPriceDeviation float64, createdAt time.Time, botID string) error
	}); ok {
		return sqliteSt.SaveTradeWithDeviation(buyOrderID, sellOrderID, exchange, symbol, buyPrice, sellPrice, quantity, pnl, fee, feeAsset, buyPriceDeviation, sellPriceDeviation, createdAt, bid)
	}
	return st.SaveTrade(&storage.Trade{
		BuyOrderID:         buyOrderID,
		SellOrderID:        sellOrderID,
		BotID:              bid,
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

// SaveTradeWithExchangePnL 保存交易記錄（包含交易所盈亏和價格偏差）
func (a *tradeStorageAdapter) SaveTradeWithExchangePnL(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, exchangePnL, fee float64, feeAsset string, buyPriceDeviation, sellPriceDeviation float64, createdAt time.Time, botID string) error {
	if a.storageService == nil {
		return nil
	}
	st := a.storageService.GetStorage()
	if st == nil {
		return nil
	}
	bid := botID
	if bid == "" {
		bid = a.botID
	}
	if sqliteSt, ok := st.(interface {
		SaveTradeWithExchangePnL(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, exchangePnL, fee float64, feeAsset string, buyPriceDeviation, sellPriceDeviation float64, createdAt time.Time, botID string) error
	}); ok {
		return sqliteSt.SaveTradeWithExchangePnL(buyOrderID, sellOrderID, exchange, symbol, buyPrice, sellPrice, quantity, pnl, exchangePnL, fee, feeAsset, buyPriceDeviation, sellPriceDeviation, createdAt, bid)
	}
	return a.SaveTradeWithDeviation(buyOrderID, sellOrderID, exchange, symbol, buyPrice, sellPrice, quantity, pnl, fee, feeAsset, buyPriceDeviation, sellPriceDeviation, createdAt, bid)
}

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

// AccountEquityUSDT 從交易所 GetAccount 拉取帳戶權益（U 本位總權益），用于統計頁真實淨值曲線
func (a *snapshotRuntimeAdapter) AccountEquityUSDT(ctx context.Context) (float64, bool) {
	if a.rt == nil || a.rt.Exchange == nil {
		return 0, false
	}
	acc, err := a.rt.Exchange.GetAccount(ctx)
	if err != nil || acc == nil {
		return 0, false
	}
	total := acc.TotalMarginBalance
	if total <= 0 {
		total = acc.TotalWalletBalance
	}
	return total, true
}
