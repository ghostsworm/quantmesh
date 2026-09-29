package main

// 本文件抽自 main.go，集中存放面向 Web API / 業務系統的適配器類型。
// 拆分目的：避免單文件超過 3000 行硬上限。
// 行為與類型語意保持不變，僅做位置遷移。

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/ai"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
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
	accountScope   string
	marketType     string
	botID          string
	exchange       string
}

func (a *reconciliationStorageAdapter) SaveReconciliationHistory(symbol string, reconcileTime time.Time, localPosition, exchangePosition, positionDiff float64,
	activeBuyOrders, activeSellOrders int, pendingSellQty, totalBuyQty, totalSellQty, estimatedProfit float64) error {
	return a.storageService.SaveReconciliationHistoryDirect(a.exchange, symbol, a.accountID, a.accountScope, a.marketType, a.botID, reconcileTime, localPosition, exchangePosition, positionDiff,
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
	storage      storage.Storage
	accountID    string
	accountScope string
	marketType   string
	botID        string
}

func (a *reconciliationRestoreAdapter) GetLatestReconciliationHistory(exchange, symbol string) (interface{}, error) {
	if a.storage == nil {
		return nil, nil
	}
	scoped, ok := a.storage.(interface {
		GetLatestReconciliationHistoryByScope(exchange, symbol, account, marketType, accountScope, botID string) (*storage.ReconciliationHistory, error)
	})
	if !ok {
		return nil, fmt.Errorf("storage does not support scoped reconciliation restore")
	}
	return scoped.GetLatestReconciliationHistoryByScope(exchange, symbol, a.accountID, a.marketType, a.accountScope, a.botID)
}

func (a *reconciliationRestoreAdapter) GetReconciliationCount(exchange, symbol string) (int64, error) {
	if a.storage == nil {
		return 0, nil
	}
	scoped, ok := a.storage.(interface {
		GetReconciliationCountByScope(exchange, symbol, account, marketType, accountScope, botID string) (int64, error)
	})
	if !ok {
		return 0, fmt.Errorf("storage does not support scoped reconciliation counts")
	}
	return scoped.GetReconciliationCountByScope(exchange, symbol, a.accountID, a.marketType, a.accountScope, a.botID)
}

// tradeStorageAdapter 交易存儲适配器
type tradeStorageAdapter struct {
	storageService *storage.StorageService
	accountID      string // 账戶標识
	accountScope   string // 不可逆凭据作用域摘要
	botID          string // 與運行時 Bot 一致，寫入 trades.bot_id
	marketType     string
	pnlAsset       string
}

type strategyRuntimeStateAdapter struct {
	storageService *storage.StorageService
	botID          string
}

type scopedGridRuntimeStateAdapter struct {
	base        *strategyRuntimeStateAdapter
	strategyKey string
}

func (a *scopedGridRuntimeStateAdapter) LoadRuntimeState(_ string) (int, string, bool, error) {
	return a.base.LoadRuntimeState(a.strategyKey)
}

func (a *scopedGridRuntimeStateAdapter) SaveRuntimeState(_ string, schemaVersion int, payload string) error {
	return a.base.SaveRuntimeState(a.strategyKey, schemaVersion, payload)
}

func (a *strategyRuntimeStateAdapter) LoadRuntimeState(strategyName string) (int, string, bool, error) {
	if a == nil || a.storageService == nil || a.storageService.GetStorage() == nil {
		return 0, "", false, fmt.Errorf("strategy runtime state storage is unavailable")
	}
	reader, ok := a.storageService.GetStorage().(storage.StrategyRuntimeStateStore)
	if !ok {
		return 0, "", false, fmt.Errorf("storage backend does not support strategy runtime state")
	}
	state, err := reader.GetStrategyRuntimeState(a.botID, strategyName)
	if err != nil || state == nil {
		return 0, "", false, err
	}
	return state.SchemaVersion, state.Payload, true, nil
}

func (a *strategyRuntimeStateAdapter) SaveRuntimeState(strategyName string, schemaVersion int, payload string) error {
	if a == nil || a.storageService == nil || a.storageService.GetStorage() == nil {
		return fmt.Errorf("strategy runtime state storage is unavailable")
	}
	writer, ok := a.storageService.GetStorage().(storage.StrategyRuntimeStateStore)
	if !ok {
		return fmt.Errorf("storage backend does not support strategy runtime state")
	}
	return writer.SetStrategyRuntimeState(&storage.StrategyRuntimeState{
		BotID: a.botID, StrategyName: strategyName, SchemaVersion: schemaVersion, Payload: payload,
	})
}

func (a *tradeStorageAdapter) SaveTradeIdempotent(trade *storage.Trade) error {
	if trade == nil || strings.TrimSpace(trade.ExecutionKey) == "" {
		return fmt.Errorf("保存幂等成交失败: 成交记录或执行键为空")
	}
	if a.storageService == nil {
		return fmt.Errorf("保存幂等成交失败: 存储服务未初始化")
	}
	st := a.storageService.GetStorage()
	if st == nil {
		return fmt.Errorf("保存幂等成交失败: 存储不可用")
	}
	writer, ok := st.(interface{ SaveTradeIdempotent(*storage.Trade) error })
	if !ok {
		return fmt.Errorf("保存幂等成交失败: 存储后端不支持执行幂等键")
	}
	canonical := *trade
	if canonical.BotID == "" {
		canonical.BotID = a.botID
	}
	if canonical.Account == "" {
		canonical.Account = a.accountID
	}
	if canonical.AccountScope == "" {
		canonical.AccountScope = a.accountScope
	}
	if canonical.MarketType == "" {
		canonical.MarketType = a.marketType
	}
	canonical.MarketType = strings.ToLower(strings.TrimSpace(canonical.MarketType))
	if canonical.PnLAsset == "" {
		canonical.PnLAsset = a.pnlAsset
	}
	canonical.PnLAsset = strings.ToUpper(strings.TrimSpace(canonical.PnLAsset))
	return writer.SaveTradeIdempotent(&canonical)
}

func (a *tradeStorageAdapter) ReplayPendingGridTrade(ctx context.Context, scope execution.IntentScope, orderID int64, cumulativeQty float64, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a == nil || a.storageService == nil || scope.Account != a.accountScope || scope.Bot != a.botID {
		return fmt.Errorf("pending grid trade runtime owner mismatch")
	}
	var trade storage.Trade
	if err := json.Unmarshal(payload, &trade); err != nil {
		return fmt.Errorf("decode pending grid trade: %w", err)
	}
	finite := func(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
	if orderID <= 0 || !finite(cumulativeQty) || cumulativeQty <= 0 || trade.SellOrderID != orderID ||
		trade.ExecutionKey != position.GridTradeExecutionKey(scope.Bot, scope.Exchange, scope.Market, orderID, scope.Symbol, cumulativeQty) ||
		trade.BotID != scope.Bot || !strings.EqualFold(trade.Exchange, scope.Exchange) ||
		!strings.EqualFold(trade.MarketType, scope.Market) || trade.Symbol != scope.Symbol ||
		trade.BuyPrice <= 0 || trade.SellPrice <= 0 || trade.Quantity <= 0 ||
		!finite(trade.BuyPrice) || !finite(trade.SellPrice) || !finite(trade.Quantity) || !finite(trade.PnL) ||
		!finite(trade.ExchangePnL) || !finite(trade.Fee) || !finite(trade.BuyPriceDeviation) || !finite(trade.SellPriceDeviation) {
		return fmt.Errorf("pending grid trade does not match the persisted intent scope or fill")
	}
	if trade.Account != "" && trade.Account != a.accountID {
		return fmt.Errorf("pending grid trade account does not match the runtime owner")
	}
	if trade.AccountScope != "" && trade.AccountScope != a.accountScope {
		return fmt.Errorf("pending grid trade credential scope does not match the runtime owner")
	}
	return a.SaveTradeIdempotent(&trade)
}

// SaveEvent 寫入通用事件（如手續費補查更正 trade_fee_correction），自動補上 bot_id。
// 存儲不可用時返回錯誤，由調用方記錄告警，避免更正記錄被靜默丟棄。
func (a *tradeStorageAdapter) SaveEvent(eventType string, data map[string]interface{}) error {
	if a.storageService == nil {
		return fmt.Errorf("保存事件 %s 失败: 存儲服務未初始化", eventType)
	}
	st := a.storageService.GetStorage()
	if st == nil {
		return fmt.Errorf("保存事件 %s 失败: 存儲不可用", eventType)
	}
	payload := make(map[string]interface{}, len(data)+1)
	for k, v := range data {
		payload[k] = v
	}
	if _, ok := payload["bot_id"]; !ok && a.botID != "" {
		payload["bot_id"] = a.botID
	}
	if err := st.SaveEvent(eventType, payload); err != nil {
		return fmt.Errorf("保存事件 %s 失败 (bot=%s): %w", eventType, a.botID, err)
	}
	return nil
}

func (a *tradeStorageAdapter) SaveTrade(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, fee float64, feeAsset string, createdAt time.Time, botID string) error {
	return a.SaveTradeWithDeviation(buyOrderID, sellOrderID, exchange, symbol, buyPrice, sellPrice, quantity, pnl, fee, feeAsset, 0, 0, createdAt, botID)
}

func (a *tradeStorageAdapter) SaveTradeWithDeviation(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, fee float64, feeAsset string, buyPriceDeviation, sellPriceDeviation float64, createdAt time.Time, botID string) error {
	bid := botID
	if bid == "" {
		bid = a.botID
	}
	return a.persistTrade(&storage.Trade{
		BuyOrderID:         buyOrderID,
		SellOrderID:        sellOrderID,
		BotID:              bid,
		Exchange:           exchange,
		MarketType:         a.marketType,
		PnLAsset:           a.pnlAsset,
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
	bid := botID
	if bid == "" {
		bid = a.botID
	}
	return a.persistTrade(&storage.Trade{
		BuyOrderID: buyOrderID, SellOrderID: sellOrderID, BotID: bid, Exchange: exchange,
		MarketType: a.marketType, PnLAsset: a.pnlAsset, Account: a.accountID, AccountScope: a.accountScope,
		Symbol: symbol, BuyPrice: buyPrice, SellPrice: sellPrice, Quantity: quantity, PnL: pnl,
		ExchangePnL: exchangePnL, Fee: fee, FeeAsset: feeAsset,
		BuyPriceDeviation: buyPriceDeviation, SellPriceDeviation: sellPriceDeviation, CreatedAt: createdAt,
	})
}

// SaveTradeWithExchangePnLAndMarketType preserves market identity through the runtime adapter.
func (a *tradeStorageAdapter) SaveTradeWithExchangePnLAndMarketType(buyOrderID, sellOrderID int64, exchange, marketType, symbol string, buyPrice, sellPrice, quantity, pnl, exchangePnL, fee float64, feeAsset string, buyPriceDeviation, sellPriceDeviation float64, createdAt time.Time, botID string) error {
	bid := botID
	if bid == "" {
		bid = a.botID
	}
	return a.persistTrade(&storage.Trade{
		BuyOrderID: buyOrderID, SellOrderID: sellOrderID, BotID: bid,
		Exchange: exchange, MarketType: strings.ToLower(strings.TrimSpace(marketType)), PnLAsset: a.pnlAsset,
		Account: a.accountID, Symbol: symbol, BuyPrice: buyPrice, SellPrice: sellPrice,
		Quantity: quantity, PnL: pnl, ExchangePnL: exchangePnL, Fee: fee, FeeAsset: feeAsset,
		BuyPriceDeviation: buyPriceDeviation, SellPriceDeviation: sellPriceDeviation, CreatedAt: createdAt,
	})
}

func (a *tradeStorageAdapter) persistTrade(trade *storage.Trade) error {
	if a.storageService == nil {
		return fmt.Errorf("保存成交失败: 存储服务未初始化")
	}
	st := a.storageService.GetStorage()
	if st == nil {
		return fmt.Errorf("保存成交失败: 存储不可用")
	}
	if trade.Account == "" {
		trade.Account = a.accountID
	}
	if trade.AccountScope == "" {
		trade.AccountScope = a.accountScope
	}
	if trade.MarketType == "" {
		trade.MarketType = a.marketType
	}
	if trade.PnLAsset == "" {
		trade.PnLAsset = a.pnlAsset
	}
	return st.SaveTrade(trade)
}

// snapshotRuntimeAdapter 適配 SymbolRuntime 為 monitor.RuntimeSnapshotSource（用於每日快照）
type snapshotRuntimeAdapter struct {
	rt *SymbolRuntime
}

func (a *snapshotRuntimeAdapter) Exchange() string     { return a.rt.Config.Exchange }
func (a *snapshotRuntimeAdapter) MarketType() string   { return a.rt.Config.GetMarketType() }
func (a *snapshotRuntimeAdapter) Symbol() string       { return a.rt.Config.Symbol }
func (a *snapshotRuntimeAdapter) Account() string      { return a.rt.AccountID }
func (a *snapshotRuntimeAdapter) AccountScope() string { return a.rt.AccountScope }
func (a *snapshotRuntimeAdapter) CurrentSnapshot() (currentPrice, unrealizedPnL, totalPositionValue float64) {
	if a.rt.PriceMonitor == nil || a.rt.SuperPositionManager == nil {
		return 0, 0, 0
	}
	currentPrice = a.rt.PriceMonitor.GetLastPrice()
	unrealizedPnL = a.rt.SuperPositionManager.GetUnrealizedPnL(currentPrice)
	totalPositionValue = a.rt.SuperPositionManager.GetTotalPositionValueAtPrice(currentPrice)
	return currentPrice, unrealizedPnL, totalPositionValue
}

func (a *snapshotRuntimeAdapter) SpotInventoryQty(ctx context.Context) (float64, bool) {
	if a == nil || a.rt == nil || a.rt.Exchange == nil || !strings.EqualFold(a.rt.Config.GetMarketType(), "spot") {
		return 0, false
	}
	sampler, ok := a.rt.Exchange.(interface {
		SpotInventoryQty(context.Context) (float64, error)
	})
	if !ok {
		return 0, false
	}
	qty, err := sampler.SpotInventoryQty(ctx)
	return qty, err == nil && !math.IsNaN(qty) && !math.IsInf(qty, 0) && qty >= 0
}

// AccountEquityUSDT 從交易所 GetAccount 拉取帳戶權益（U 本位總權益），用于統計頁真實淨值曲線
func (a *snapshotRuntimeAdapter) AccountEquityUSDT(ctx context.Context) (float64, bool) {
	if a.rt == nil || a.rt.Exchange == nil {
		return 0, false
	}
	if source, ok := a.rt.Exchange.(interface {
		AccountEquityUSDT(context.Context) (float64, bool)
	}); ok {
		value, available := source.AccountEquityUSDT(ctx)
		return value, available && !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
	}
	if strings.EqualFold(a.rt.Config.GetMarketType(), "spot") {
		// Generic Account totals may add balances denominated in different assets.
		// Until an adapter provides complete quote-currency valuation, omit rather
		// than persist a dimensionally invalid equity sample.
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
	return total, !math.IsNaN(total) && !math.IsInf(total, 0) && total >= 0
}
