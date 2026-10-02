package web

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"quantmesh/cfgmgr"
	"quantmesh/config"
	"quantmesh/exchange"
	qmi18n "quantmesh/i18n"
	"quantmesh/logger"
	"quantmesh/position"
	"quantmesh/storage"
	ordersync "quantmesh/sync"
	"quantmesh/utils"

	"github.com/gin-gonic/gin"
)

const accountEquityCurrency = "USDT"

// respondError 返回翻譯后的錯误响应
func respondError(c *gin.Context, status int, messageKey string, args ...interface{}) {
	lang := GetLanguage(c)

	var data map[string]interface{}
	var errObj error

	// 解析参數
	for _, arg := range args {
		if err, ok := arg.(error); ok {
			errObj = err
		} else if m, ok := arg.(map[string]interface{}); ok {
			data = m
		}
	}

	// 翻譯錯误消息
	message := qmi18n.TWithLang(lang, messageKey, data)

	// 如果有實際的錯误對象，添加详细信息（僅在开发模式）
	if errObj != nil && status >= 500 {
		// 在生產环境可能需要隐藏详细錯误信息
		message = fmt.Sprintf("%s: %v", message, errObj)
	}

	c.JSON(status, gin.H{"error": message})
}

// SystemStatus 系统状態
type SystemStatus struct {
	Running               bool                                 `json:"running"`
	Exchange              string                               `json:"exchange"`
	Symbol                string                               `json:"symbol"`
	MarketType            string                               `json:"market_type,omitempty"` // 市場類型：spot/futures
	QuoteAsset            string                               `json:"quote_asset,omitempty"`
	BaseAsset             string                               `json:"base_asset,omitempty"`
	AccountScope          string                               `json:"-"`
	CurrentPrice          float64                              `json:"current_price"`
	TotalPnL              float64                              `json:"total_pnl"`
	TotalPnLAsset         string                               `json:"total_pnl_asset,omitempty"`
	TotalPnLVerified      bool                                 `json:"total_pnl_verified"`
	TotalTrades           int                                  `json:"total_trades"`
	RiskTriggered         bool                                 `json:"risk_triggered"`
	Uptime                int64                                `json:"uptime"`         // 运行時间（秒）
	OpeningPaused         bool                                 `json:"opening_paused"` // 是否暫停開倉
	PauseReason           string                               `json:"pause_reason"`   // 暫停原因：manual / schedule / periodic / position_limit
	ProtectiveLiquidation position.ProtectiveLiquidationStatus `json:"protective_liquidation"`
}

var (
	// 全局状態（需要從 main.go 注入）
	currentStatus *SystemStatus
	// 多交易對状態（key: exchange:symbol）
	statusBySymbol   = make(map[string]*SystemStatus)
	defaultSymbolKey string
	// 保护 statusBySymbol 的读写鎖
	statusMu sync.RWMutex
	// 版本号（需要從 main.go 注入）
	appVersion string
)

// SymbolScopedProviders 组合一個交易對的所有依赖
type SymbolScopedProviders struct {
	Status   *SystemStatus
	Price    PriceProvider
	Exchange ExchangeProvider
	Position PositionManagerProvider
	Risk     RiskMonitorProvider
	Storage  StorageServiceProvider
	Funding  FundingMonitorProvider
}

// makeSymbolKey 生成交易對唯一 key（含市場類型，避免同名的現貨/合約衝突）
// marketType 可選，為空時默認 "futures"（向后兼容）
func makeSymbolKey(exchange, symbol string, marketType ...string) string {
	mt := "futures"
	if len(marketType) > 0 && marketType[0] != "" {
		mt = marketType[0]
	}
	return strings.ToLower(fmt.Sprintf("%s:%s:%s", exchange, symbol, mt))
}

// makeSymbolKeyCompat 向后兼容的 key 生成（不含 market_type，用於查找時的 fallback）
func makeSymbolKeyCompat(exchange, symbol string) string {
	return strings.ToLower(fmt.Sprintf("%s:%s", exchange, symbol))
}

// resolveStatusBySymbol 從 statusBySymbol 中查找，先嘗試精確匹配（含 market_type），再 fallback 到模糊匹配
func resolveStatusBySymbol(exchange, symbol string, marketType ...string) (*SystemStatus, bool) {
	// 精確匹配
	key := makeSymbolKey(exchange, symbol, marketType...)
	if st, ok := statusBySymbol[key]; ok {
		return st, true
	}
	// fallback: 不帶 market_type 的舊 key 格式
	compatKey := makeSymbolKeyCompat(exchange, symbol)
	if st, ok := statusBySymbol[compatKey]; ok {
		return st, true
	}
	return nil, false
}

// SetStatusProvider 設置状態提供者
func SetStatusProvider(status *SystemStatus) {
	currentStatus = status
}

// SetVersion 設置版本号
func SetVersion(version string) {
	appVersion = version
}

// RegisterSymbolProviders 注册單個交易對的提供者集合
func RegisterSymbolProviders(exchange, symbol string, providers *SymbolScopedProviders, marketType ...string) {
	if providers == nil {
		return
	}
	key := makeSymbolKey(exchange, symbol, marketType...)

	logger.Info("[DEBUG] RegisterSymbolProviders - registering key=%s, hasPosition=%v, hasPrice=%v",
		key, providers.Position != nil, providers.Price != nil)

	// 使用写鎖保护並发写入
	statusMu.Lock()
	statusBySymbol[key] = providers.Status
	statusMu.Unlock()

	providersMu.Lock()
	if providers.Price != nil {
		priceProviders[key] = providers.Price
		logger.Info("[DEBUG] RegisterSymbolProviders - registered price provider for key=%s", key)
	}
	if providers.Exchange != nil {
		exchangeProviders[key] = providers.Exchange
	}
	if providers.Position != nil {
		positionProviders[key] = providers.Position
		logger.Info("[DEBUG] RegisterSymbolProviders - registered position provider for key=%s", key)
	}
	if providers.Risk != nil {
		riskProviders[key] = providers.Risk
	}
	if providers.Storage != nil {
		storageProviders[key] = providers.Storage
	}
	if providers.Funding != nil {
		fundingProviders[key] = providers.Funding
	}
	providersMu.Unlock()
}

// IsSymbolStatusRegistered 判斷該交易對是否已在 statusBySymbol 中注册（含 market_type，默認 futures）
func IsSymbolStatusRegistered(exchange, symbol string, marketType ...string) bool {
	key := makeSymbolKey(exchange, symbol, marketType...)
	statusMu.RLock()
	defer statusMu.RUnlock()
	_, ok := statusBySymbol[key]
	return ok
}

// GetRegisteredSystemStatus 返回已注册的运行状態指針（用於避免啟動階段重複 Register）
func GetRegisteredSystemStatus(exchange, symbol string, marketType ...string) (*SystemStatus, bool) {
	key := makeSymbolKey(exchange, symbol, marketType...)
	statusMu.RLock()
	defer statusMu.RUnlock()
	st, ok := statusBySymbol[key]
	return st, ok
}

// UnregisterSymbolProviders 移除單個交易對的 Web 状態與 provider（Bot 停止時調用）
func UnregisterSymbolProviders(exchange, symbol string, marketType ...string) {
	mt := "futures"
	if len(marketType) > 0 && marketType[0] != "" {
		mt = marketType[0]
	}
	key := makeSymbolKey(exchange, symbol, mt)
	compatKey := makeSymbolKeyCompat(exchange, symbol)

	statusMu.Lock()
	if st, ok := statusBySymbol[key]; ok && st != nil {
		st.Running = false
	}
	delete(statusBySymbol, key)
	delete(statusBySymbol, compatKey)
	statusMu.Unlock()

	providersMu.Lock()
	delete(priceProviders, key)
	delete(exchangeProviders, key)
	delete(positionProviders, key)
	delete(riskProviders, key)
	delete(storageProviders, key)
	delete(fundingProviders, key)
	delete(fundingProviders, compatKey)
	providersMu.Unlock()
}

// RegisterFundingProvider 單独注册资金费率提供者
func RegisterFundingProvider(exchange, symbol string, provider FundingMonitorProvider) {
	if provider == nil {
		return
	}
	key := makeSymbolKey(exchange, symbol)

	// 使用写鎖保护並发写入
	providersMu.Lock()
	fundingProviders[key] = provider
	providersMu.Unlock()
}

// SetDefaultSymbolKey 設置默认交易對（兼容舊接口）
func SetDefaultSymbolKey(exchange, symbol string) {
	defaultSymbolKey = makeSymbolKey(exchange, symbol)
}

// resolveSymbolKey 根據查詢参數獲取 key（支持 market_type 参數）
func resolveSymbolKey(c *gin.Context) string {
	ex := c.Query("exchange")
	sym := c.Query("symbol")
	mt := c.Query("market_type")
	if ex != "" && sym != "" {
		// 先嘗試精確匹配（含 market_type）
		key := makeSymbolKey(ex, sym, mt)
		statusMu.RLock()
		_, exists := statusBySymbol[key]
		statusMu.RUnlock()
		if exists {
			return key
		}
		// fallback: 不帶 market_type
		compatKey := makeSymbolKeyCompat(ex, sym)
		statusMu.RLock()
		_, existsCompat := statusBySymbol[compatKey]
		statusMu.RUnlock()
		if existsCompat {
			return compatKey
		}
		return key
	}
	return defaultSymbolKey
}

// === Provider 映射 ===
var (
	priceProviders    = make(map[string]PriceProvider)
	exchangeProviders = make(map[string]ExchangeProvider)
	positionProviders = make(map[string]PositionManagerProvider)
	riskProviders     = make(map[string]RiskMonitorProvider)
	storageProviders  = make(map[string]StorageServiceProvider)
	fundingProviders  = make(map[string]FundingMonitorProvider)
	// 保护所有 provider 映射的读写鎖
	providersMu sync.RWMutex
)

func pickStatus(c *gin.Context) *SystemStatus {
	if key := resolveSymbolKey(c); key != "" {
		statusMu.RLock()
		st, ok := statusBySymbol[key]
		statusMu.RUnlock()
		if ok && st != nil {
			return st
		}
	}
	return currentStatus
}

// priceProviderFromSymbolRuntime 從 SymbolRuntime（interface{}）反射取出 PriceMonitor，供映射缺失時回退。
func priceProviderFromSymbolRuntime(rt interface{}) PriceProvider {
	if rt == nil {
		return nil
	}
	rv := reflect.ValueOf(rt)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return nil
	}
	pm := rv.FieldByName("PriceMonitor")
	if !pm.IsValid() || pm.IsNil() {
		return nil
	}
	p, ok := pm.Interface().(PriceProvider)
	if ok {
		return p
	}
	return nil
}

// positionProviderFromSymbolRuntime 從 SymbolRuntime 反射取出持倉適配器。
func positionProviderFromSymbolRuntime(rt interface{}) PositionManagerProvider {
	if rt == nil {
		return nil
	}
	rv := reflect.ValueOf(rt)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return nil
	}
	spm := rv.FieldByName("SuperPositionManager")
	if !spm.IsValid() || spm.IsNil() {
		return nil
	}
	if mgr, ok := spm.Interface().(*position.SuperPositionManager); ok && mgr != nil {
		return NewPositionManagerAdapter(mgr)
	}
	return nil
}

// pickSymbolRuntimeByQuery 依查詢参數從 SymbolManager 取运行時（映射鍵不一致或 UUID bot_id 時仍可能命中）。
func pickSymbolRuntimeByQuery(c *gin.Context) interface{} {
	if symbolManagerProvider == nil {
		return nil
	}
	ex := strings.TrimSpace(c.Query("exchange"))
	sym := strings.TrimSpace(c.Query("symbol"))
	if ex == "" || sym == "" {
		return nil
	}
	mt := strings.TrimSpace(strings.ToLower(c.Query("market_type")))
	if rt, ok := symbolManagerProvider.GetEx(ex, sym, mt); ok && rt != nil {
		return rt
	}
	// 遍歷：自定義 bot_id（UUID）時 GenerateBotID 與運行時鍵不一致
	wantMT := mt
	if wantMT == "" {
		wantMT = "futures"
	}
	for _, rtInterface := range symbolManagerProvider.List() {
		if rtInterface == nil {
			continue
		}
		cfgVal := reflect.ValueOf(rtInterface)
		if cfgVal.Kind() == reflect.Ptr {
			if cfgVal.IsNil() {
				continue
			}
			cfgVal = cfgVal.Elem()
		}
		cf := cfgVal.FieldByName("Config")
		if !cf.IsValid() {
			continue
		}
		if cf.Kind() == reflect.Ptr {
			if cf.IsNil() {
				continue
			}
			cf = cf.Elem()
		}
		exF := cf.FieldByName("Exchange")
		symF := cf.FieldByName("Symbol")
		mtF := cf.FieldByName("MarketType")
		if !exF.IsValid() || !symF.IsValid() {
			continue
		}
		if !strings.EqualFold(exF.String(), ex) || !strings.EqualFold(symF.String(), sym) {
			continue
		}
		rtMT := "futures"
		if mtF.IsValid() && strings.TrimSpace(mtF.String()) != "" {
			rtMT = strings.ToLower(strings.TrimSpace(mtF.String()))
		}
		usm := cf.FieldByName("UseSpotMargin")
		if rtMT == "spot" && usm.IsValid() && usm.Bool() {
			rtMT = "spot_margin"
		}
		if strings.EqualFold(rtMT, wantMT) {
			return rtInterface
		}
	}
	return nil
}

// UpsertPriceProviderForKey 將运行時價格寫入映射（用於已註冊 Status 但曾缺 Price 的補齊）。
func UpsertPriceProviderForKey(exchange, symbol, marketType string, pm PriceProvider) {
	if pm == nil {
		return
	}
	if rv := reflect.ValueOf(pm); rv.Kind() == reflect.Ptr && rv.IsNil() {
		return
	}
	key := makeSymbolKey(exchange, symbol, marketType)
	providersMu.Lock()
	priceProviders[key] = pm
	providersMu.Unlock()
}

// UpsertPositionProviderForKey 將持倉適配器寫入映射。
func UpsertPositionProviderForKey(exchange, symbol, marketType string, pm PositionManagerProvider) {
	if pm == nil {
		return
	}
	if rv := reflect.ValueOf(pm); rv.Kind() == reflect.Ptr && rv.IsNil() {
		return
	}
	key := makeSymbolKey(exchange, symbol, marketType)
	providersMu.Lock()
	positionProviders[key] = pm
	providersMu.Unlock()
}

func PickPriceProvider(c *gin.Context) PriceProvider {
	if key := resolveSymbolKey(c); key != "" {
		providersMu.RLock()
		p, ok := priceProviders[key]
		providersMu.RUnlock()
		if ok && p != nil {
			logger.Info("[DEBUG] PickPriceProvider - found provider for key=%s", key)
			return p
		}
		logger.Warn("⚠️ [PickPriceProvider] no provider found for key=%s, falling back to default", key)
	}
	if rt := pickSymbolRuntimeByQuery(c); rt != nil {
		if pp := priceProviderFromSymbolRuntime(rt); pp != nil {
			logger.Info("[DEBUG] PickPriceProvider - resolved via SymbolManager List/GetEx")
			return pp
		}
	}
	logger.Info("[DEBUG] PickPriceProvider - using default priceProvider")
	return priceProvider
}

func pickExchangeProvider(c *gin.Context) ExchangeProvider {
	if key := resolveSymbolKey(c); key != "" {
		providersMu.RLock()
		p, ok := exchangeProviders[key]
		providersMu.RUnlock()
		if ok && p != nil {
			return p
		}
	}
	return exchangeProvider
}

func PickPositionProvider(c *gin.Context) PositionManagerProvider {
	key := resolveSymbolKey(c)
	logger.Info("[DEBUG] PickPositionProvider - resolvedKey=%s", key)

	if key != "" {
		providersMu.RLock()
		p, ok := positionProviders[key]
		providersMu.RUnlock()

		logger.Info("[DEBUG] PickPositionProvider - found in map: %v, provider!=nil: %v", ok, p != nil)

		if ok && p != nil {
			return p
		}
	}
	if rt := pickSymbolRuntimeByQuery(c); rt != nil {
		if pm := positionProviderFromSymbolRuntime(rt); pm != nil {
			logger.Info("[DEBUG] PickPositionProvider - resolved via SymbolManager List/GetEx")
			return pm
		}
	}

	logger.Info("[DEBUG] PickPositionProvider - returning default provider")
	return positionManagerProvider
}

func PickRiskProvider(c *gin.Context) RiskMonitorProvider {
	if key := resolveSymbolKey(c); key != "" {
		providersMu.RLock()
		p, ok := riskProviders[key]
		providersMu.RUnlock()
		if ok && p != nil {
			return p
		}
	}
	return riskMonitorProvider
}

func PickStorageProvider(c *gin.Context) StorageServiceProvider {
	if key := resolveSymbolKey(c); key != "" {
		providersMu.RLock()
		p, ok := storageProviders[key]
		providersMu.RUnlock()
		if ok && p != nil {
			return p
		}
	}
	return storageServiceProvider
}

func PickFundingProvider(c *gin.Context) FundingMonitorProvider {
	if key := resolveSymbolKey(c); key != "" {
		providersMu.RLock()
		p, ok := fundingProviders[key]
		providersMu.RUnlock()
		if ok && p != nil {
			return p
		}
	}
	return fundingMonitorProvider
}

var (
	// 價格提供者（需要從main.go注入）
	priceProvider PriceProvider
)

// PriceProvider 價格提供者接口
type PriceProvider interface {
	GetLastPrice() float64
}

// SetPriceProvider 設置價格提供者
func SetPriceProvider(provider PriceProvider) {
	priceProvider = provider
}

var (
	// 交易所提供者（需要從main.go注入）
	exchangeProvider ExchangeProvider
	// 按交易所 ID 獲取 IExchange（用於利润提取內部轉帳，由 main 注入）
	exchangeGetterFunc func(exchangeID string) exchange.IExchange
)

// SetExchangeGetter 設置按交易所 ID 獲取交易所實例的函數（供利润提取 API 調用 InternalTransfer）
func SetExchangeGetter(f func(exchangeID string) exchange.IExchange) {
	exchangeGetterFunc = f
}

// ExchangeProvider 交易所提供者接口
type ExchangeProvider interface {
	GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*exchange.Candle, error)
	GetFundingRate(ctx context.Context, symbol string) (float64, error)
	// GetPositions 獲取交易所真實持倉資訊
	GetPositions(ctx context.Context, symbol string) ([]*exchange.Position, error)
}

// SetExchangeProvider 設置交易所提供者
func SetExchangeProvider(provider ExchangeProvider) {
	exchangeProvider = provider
}

// getOrders 獲取訂單列表（历史订單）
// GET /api/orders
func getOrders(c *gin.Context) {
	// 优先使用特定交易對的 storage provider
	storageProv := PickStorageProvider(c)

	// 如果找不到特定的 provider，使用全局的 storageServiceProvider
	if storageProv == nil {
		storageProv = storageServiceProvider
	}

	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"orders": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"orders": []interface{}{}})
		return
	}

	// 解析参數
	limitStr := c.DefaultQuery("limit", "100")
	offsetStr := c.DefaultQuery("offset", "0")
	status := c.Query("status")

	limit := 100
	offset := 0
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}
	if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
		offset = o
	}

	orders, err := storage.QueryOrdersWithTimeRange(limit, offset, status, nil, nil)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.query_orders_failed", err)
		return
	}

	// 轉换時间為UTC+8
	ordersResponse := make([]map[string]interface{}, len(orders))
	for i, order := range orders {
		ordersResponse[i] = map[string]interface{}{
			"order_id":        order.OrderID,
			"client_order_id": order.ClientOrderID,
			"symbol":          order.Symbol,
			"side":            order.Side,
			"price":           order.Price,
			"quantity":        order.Quantity,
			"status":          order.Status,
			"created_at":      utils.ToUTC8(order.CreatedAt),
			"updated_at":      utils.ToUTC8(order.UpdatedAt),
		}
	}

	c.JSON(http.StatusOK, gin.H{"orders": ordersResponse})
}

// syncOrders 手动同步订单（仅币安）
// POST /api/orders/sync
func syncOrders(c *gin.Context) {
	exchangeName := c.Query("exchange")
	symbol := c.Query("symbol")

	if exchangeName == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.missing_exchange_or_symbol", fmt.Errorf("exchange和symbol参数必填"))
		return
	}

	// 检查是否是币安交易所
	if exchangeName != "binance" {
		respondError(c, http.StatusBadRequest, "error.only_binance_supported", fmt.Errorf("当前仅支持币安交易所的订单同步"))
		return
	}

	// 获取exchange provider
	exProvider := pickExchangeProvider(c)
	if exProvider == nil {
		respondError(c, http.StatusInternalServerError, "error.exchange_provider_not_found", fmt.Errorf("未找到交易所provider"))
		return
	}

	// 获取storage provider
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		storageProv = storageServiceProvider
	}
	if storageProv == nil {
		respondError(c, http.StatusInternalServerError, "error.storage_provider_not_found", fmt.Errorf("未找到storage provider"))
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		respondError(c, http.StatusInternalServerError, "error.storage_not_found", fmt.Errorf("未找到storage"))
		return
	}

	// 获取exchange实例（需要转换为IExchange接口）
	// 由于ExchangeProvider接口不包含IExchange的所有方法，我们需要通过symbol manager获取
	// 这里我们创建一个临时的同步服务
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 尝试从symbol manager获取exchange实例
	var ex exchange.IExchange
	if symbolManagerProvider != nil {
		rtInterface, exists := symbolManagerProvider.Get(exchangeName, symbol)
		if exists {
			// 使用反射获取Exchange字段
			rtVal := reflect.ValueOf(rtInterface)
			if rtVal.Kind() == reflect.Ptr {
				rtVal = rtVal.Elem()
			}
			exchangeField := rtVal.FieldByName("Exchange")
			if exchangeField.IsValid() && !exchangeField.IsNil() {
				if exInterface, ok := exchangeField.Interface().(exchange.IExchange); ok {
					ex = exInterface
				}
			}
		}
	}

	if ex == nil {
		respondError(c, http.StatusInternalServerError, "error.exchange_instance_not_found", fmt.Errorf("未找到交易所实例，请确保交易对正在运行"))
		return
	}

	// 创建临时同步服务并执行同步
	orderSync := ordersync.NewOrderSyncService(
		ex,
		storage,
		symbol,
		"", // accountID暂时为空
		exchangeName,
		10*time.Minute, // syncInterval，这里只是用于创建，不会实际使用
	)
	status := pickStatus(c)
	if status == nil || strings.TrimSpace(status.AccountScope) == "" || strings.TrimSpace(status.MarketType) == "" || status.Exchange != exchangeName || status.Symbol != symbol {
		respondError(c, http.StatusServiceUnavailable, "error.account_scope_unavailable", fmt.Errorf("无法确认订单同步的账户凭据和市场归属"))
		return
	}
	orderSync.SetTradeScope(status.MarketType, status.AccountScope)

	// 执行同步
	if err := orderSync.Sync(ctx); err != nil {
		logger.Error("❌ [订单同步] 手动同步失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.sync_failed", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "订单同步成功",
	})
}

// getOrderHistory 獲取訂單历史
// GET /api/orders/history
func getOrderHistory(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	logger.Info("[訂單歷史] 查詢参數: exchange=%s, symbol=%s", exchange, symbol)

	// 优先使用特定交易對的 storage provider
	storageProv := PickStorageProvider(c)

	// 如果找不到特定的 provider，使用全局的 storageServiceProvider
	if storageProv == nil {
		logger.Info("[訂單歷史] 未找到特定交易對的 provider，使用全局 storageServiceProvider")
		storageProv = storageServiceProvider
	}

	if storageProv == nil {
		logger.Warn("[訂單歷史] storageServiceProvider 也為 nil，無法查詢")
		c.JSON(http.StatusOK, gin.H{"orders": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		logger.Warn("[訂單歷史] storage.GetStorage() 回傳 nil")
		c.JSON(http.StatusOK, gin.H{"orders": []interface{}{}})
		return
	}

	logger.Info("[訂單歷史] storage 獲取成功，准备查詢數據库")

	// 解析参數
	limitStr := c.DefaultQuery("limit", "100")
	offsetStr := c.DefaultQuery("offset", "0")
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")

	limit := 100
	offset := 0
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}
	if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
		offset = o
	}

	// 解析时间范围（RFC3339格式）
	var startTime, endTime *time.Time
	now := utils.NowUTC()
	defaultStartTime := now.Add(-72 * time.Hour) // 默认最近72小时（与 Web 订单历史页默认一致）

	if startTimeStr != "" {
		if t, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
			startTime = &t
		} else {
			logger.Warn("[訂單歷史] 解析 start_time 失败: %v，使用默认值", err)
		}
	}
	if endTimeStr != "" {
		if t, err := time.Parse(time.RFC3339, endTimeStr); err == nil {
			endTime = &t
		} else {
			logger.Warn("[訂單歷史] 解析 end_time 失败: %v，使用默认值", err)
		}
	}

	// 如果缺失时间参数，使用默认值（最近24小时）
	if startTime == nil {
		startTime = &defaultStartTime
	}
	if endTime == nil {
		endTime = &now
	}

	// 验证时间范围
	if endTime.Before(*startTime) {
		respondError(c, http.StatusBadRequest, "orders.timeRangeInvalid")
		return
	}

	// 验证时间跨度不超过7天
	diffDays := endTime.Sub(*startTime).Hours() / 24
	if diffDays > 7 {
		respondError(c, http.StatusBadRequest, "orders.timeRangeMaxDays")
		return
	}

	// 只查詢已完成或已取消的订單（带时间范围和交易所/交易对筛选）
	orders, err := storage.QueryOrdersWithFilter(limit, offset, "FILLED", exchange, symbol, startTime, endTime)
	if err != nil {
		// 如果查詢失败，尝試查詢所有状態的订單
		orders, err = storage.QueryOrdersWithFilter(limit, offset, "", exchange, symbol, startTime, endTime)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	// 也查詢已取消的订單
	canceledOrders, err := storage.QueryOrdersWithFilter(limit, offset, "CANCELED", exchange, symbol, startTime, endTime)
	if err == nil {
		orders = append(orders, canceledOrders...)
	}

	// 收集已成交的賣單 ID，查詢對應盈虧
	sellOrderIDs := make([]int64, 0)
	for _, o := range orders {
		if o.Status == "FILLED" && o.Side == "SELL" {
			sellOrderIDs = append(sellOrderIDs, o.OrderID)
		}
	}
	pnlMap := make(map[int64]float64)
	if len(sellOrderIDs) > 0 {
		if m, err := storage.GetTradesBySellOrderIDs(sellOrderIDs); err == nil {
			pnlMap = m
		}
	}

	// 轉换時间為UTC+8並格式化返回數據，附加盈虧字段
	ordersResponse := make([]map[string]interface{}, len(orders))
	for i, order := range orders {
		resp := map[string]interface{}{
			"order_id":        order.OrderID,
			"client_order_id": order.ClientOrderID,
			"symbol":          order.Symbol,
			"side":            order.Side,
			"exchange":        order.Exchange,
			"type":            order.Type,
			"price":           order.Price,
			"quantity":        order.Quantity,
			"filled_qty":      order.FilledQty,
			"status":          order.Status,
			"strategy_name":   order.StrategyName,
			"strategy_type":   order.StrategyType,
			"order_source":    order.OrderSource,
			"created_at":      utils.ToUTC8(order.CreatedAt),
			"updated_at":      utils.ToUTC8(order.UpdatedAt),
		}
		// pnl = 网格策略计算的盈亏（基于买卖配对）
		if pnl, ok := pnlMap[order.OrderID]; ok {
			resp["pnl"] = pnl
		} else {
			resp["pnl"] = nil
		}
		// exchange_pnl = 交易所计算的已实现盈亏（基于加权平均成本法）
		resp["exchange_pnl"] = nil
		resp["exchange_pnl_verified"] = false
		ordersResponse[i] = resp
	}

	// 查詢真實的订單总数（不受 limit 限制，带交易所/交易对筛选）
	totalCount := int64(len(orders))
	todayCount := int64(0)

	// 尝試從數據库获取真实总数（带交易所/交易对筛选）
	type orderCounterWithFilter interface {
		CountOrdersWithFilter(status, exchange, symbol string, startTime, endTime *time.Time) (int64, error)
	}
	if counter, ok := storage.(orderCounterWithFilter); ok {
		filledCount, err1 := counter.CountOrdersWithFilter("FILLED", exchange, symbol, startTime, endTime)
		canceledCount, err2 := counter.CountOrdersWithFilter("CANCELED", exchange, symbol, startTime, endTime)
		if err1 == nil && err2 == nil {
			totalCount = filledCount + canceledCount
		}
	}

	// 计算今日订單数（从已返回的订單中统计，按更新日期与列表筛选一致）
	nowLocal := utils.NowConfiguredTimezone()
	todayStr := nowLocal.Format("2006-01-02")
	for _, order := range orders {
		orderDate := utils.ToConfiguredTimezone(order.UpdatedAt).Format("2006-01-02")
		if orderDate == todayStr {
			todayCount++
		}
	}

	// 獲取槓桿倍數（用於計算資金占用）
	leverage := 1
	if pmProvider := PickPositionProvider(c); pmProvider != nil {
		if l := pmProvider.GetLeverage(); l > 0 {
			leverage = l
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"orders":      ordersResponse,
		"total_count": totalCount,
		"today_count": todayCount,
		"leverage":    leverage,
	})
}

// getFixSessions 获取 FIX 会话状态列表
// GET /api/fix/sessions

var (
	// 存儲服務提供者（需要從main.go注入）
	storageServiceProvider StorageServiceProvider
)

// StorageServiceProvider 存儲服務提供者接口
type StorageServiceProvider interface {
	GetStorage() storage.Storage
}

// SetStorageServiceProvider 設置存儲服務提供者
func SetStorageServiceProvider(provider StorageServiceProvider) {
	storageServiceProvider = provider
}

// storageServiceAdapter 存儲服務适配器
type storageServiceAdapter struct {
	service *storage.StorageService
}

// NewStorageServiceAdapter 創建存儲服務适配器
func NewStorageServiceAdapter(service *storage.StorageService) StorageServiceProvider {
	return &storageServiceAdapter{service: service}
}

// GetStorage 獲取存儲接口
func (a *storageServiceAdapter) GetStorage() storage.Storage {
	if a.service == nil {
		logger.Warn("⚠️ storageServiceAdapter.GetStorage: service 為 nil")
		return nil
	}
	st := a.service.GetStorage()
	if st == nil {
		logger.Warn("⚠️ storageServiceAdapter.GetStorage: service.GetStorage() 回傳 nil，storage.enabled 可能為 false 或初始化失败")
	}
	return st
}

// getStatistics 獲取统计數據
// GET /api/statistics
func getStatistics(c *gin.Context) {
	// 优先使用特定交易對的 storage provider
	storageProv := PickStorageProvider(c)

	// 如果找不到特定的 provider，使用全局的 storageServiceProvider
	if storageProv == nil {
		logger.Info("[统计] 未找到特定交易對的 provider，使用全局 storageServiceProvider")
		storageProv = storageServiceProvider
	}

	if storageProv == nil {
		logger.Warn("[统计] storageServiceProvider 也為 nil，無法查詢")
		c.JSON(http.StatusOK, gin.H{
			"total_trades": 0,
			"total_volume": 0,
			"total_pnl":    nil,
			"pnl_verified": false,
			"win_rate":     0,
		})
		return
	}

	store := storageProv.GetStorage()
	if store == nil {
		logger.Warn("[统计] storage.GetStorage() 回傳 nil")
		c.JSON(http.StatusOK, gin.H{
			"total_trades": 0,
			"total_volume": 0,
			"total_pnl":    nil,
			"pnl_verified": false,
			"win_rate":     0,
		})
		return
	}

	logger.Info("[统计] storage 獲取成功，准备查詢數據库")

	// 獲取 exchange、symbol、bot_id 参數（如果有）
	status := pickStatus(c)
	exchange := strings.TrimSpace(c.Query("exchange"))
	symbol := strings.TrimSpace(c.Query("symbol"))
	marketType := strings.ToLower(strings.TrimSpace(c.Query("market_type")))
	if status != nil {
		if exchange == "" {
			exchange = status.Exchange
		}
		if symbol == "" {
			symbol = status.Symbol
		}
		if marketType == "" {
			marketType = strings.ToLower(strings.TrimSpace(status.MarketType))
		}
	}
	botID := strings.TrimSpace(c.Query("bot_id"))
	pnlAsset := ""
	if status != nil {
		pnlAsset = strings.ToUpper(strings.TrimSpace(status.TotalPnLAsset))
		if pnlAsset == "" {
			pnlAsset = strings.ToUpper(strings.TrimSpace(status.QuoteAsset))
		}
	}
	var summary *storage.Statistics
	pnlVerified := false
	if status != nil && status.AccountScope != "" && strings.EqualFold(status.Exchange, exchange) && strings.EqualFold(status.Symbol, symbol) && marketType == strings.ToLower(strings.TrimSpace(status.MarketType)) && marketType != "" && pnlAsset != "" {
		if reader, ok := store.(interface {
			GetStatisticsSummaryByDimension(exchange, marketType, symbol, accountScope, asset, botID string) (*storage.Statistics, error)
		}); ok {
			var queryErr error
			summary, queryErr = reader.GetStatisticsSummaryByDimension(exchange, marketType, symbol, status.AccountScope, pnlAsset, botID)
			if queryErr != nil {
				logger.Warn("[统计] 精确作用域盈亏未核实: %v", queryErr)
			} else if validateProfitStatisticsSnapshot(summary) == nil {
				pnlVerified = true
			} else {
				logger.Warn("[统计] 精确作用域盈亏包含缺失或无效统计值")
			}
		}
	}
	totalTrades, totalVolume, winRate := 0, 0.0, 0.0
	var totalPnL, grossPnL, totalFee, totalBuyDeviation, totalSellDeviation interface{}
	if pnlVerified && summary != nil {
		totalTrades, totalVolume, winRate = summary.TotalTrades, summary.TotalVolume, summary.WinRate
		totalPnL, grossPnL, totalFee = summary.TotalPnL, summary.GrossPnL, summary.TotalFee
		totalBuyDeviation, totalSellDeviation = summary.TotalBuyDeviation, summary.TotalSellDeviation
	}
	pnlBasis := ""
	if pnlVerified {
		pnlBasis = "paired_grid_trades"
	}

	// Today's grid PnL uses the same exact owner, market, symbol and asset checks.
	todayTrades := 0
	var todayPnL interface{}
	todayPnLVerified := false
	var todayExchangePnL interface{}
	todayStart := utils.NowConfiguredTimezone()
	todayStart = time.Date(todayStart.Year(), todayStart.Month(), todayStart.Day(), 0, 0, 0, 0, todayStart.Location())
	if status != nil && status.AccountScope != "" && strings.EqualFold(status.Exchange, exchange) && strings.EqualFold(status.Symbol, symbol) && marketType == strings.ToLower(strings.TrimSpace(status.MarketType)) && marketType != "" && pnlAsset != "" {
		if reader, ok := store.(interface {
			QueryDailyStatisticsByDimension(exchange, marketType, symbol, accountScope, asset, botID string, startDate, endDate time.Time) ([]*storage.DailyStatisticsWithTradeCount, error)
		}); ok {
			todayStats, queryErr := reader.QueryDailyStatisticsByDimension(exchange, marketType, symbol, status.AccountScope, pnlAsset, botID, todayStart, utils.NowConfiguredTimezone())
			if queryErr == nil {
				todayTotal := 0.0
				validRows := true
				for _, stat := range todayStats {
					if stat == nil || stat.TotalTrades < 0 {
						validRows = false
						break
					}
					maxInt := int(^uint(0) >> 1)
					if stat.TotalTrades > maxInt-todayTrades {
						validRows = false
						break
					}
					todayTrades += stat.TotalTrades
					var totalOK bool
					if todayTotal, totalOK = addFiniteProfitValues(todayTotal, stat.TotalPnL); !totalOK {
						validRows = false
						break
					}
				}
				if validRows {
					todayPnLVerified = true
					todayPnL = todayTotal
				} else {
					todayTrades = 0
					logger.Warn("[统计] 今日精确作用域盈亏包含无效或溢出统计值")
				}
			} else {
				logger.Warn("[统计] 今日精确作用域盈亏未核实: %v", queryErr)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"total_trades":                totalTrades,
		"total_volume":                totalVolume,
		"total_pnl":                   totalPnL,
		"gross_pnl":                   grossPnL,
		"total_fee":                   totalFee,
		"win_rate":                    winRate,
		"total_buy_deviation":         totalBuyDeviation,  // 🔥 買入價格偏差總和
		"total_sell_deviation":        totalSellDeviation, // 🔥 賣出價格偏差總和
		"exchange_pnl":                nil,
		"exchange_pnl_verified":       false,
		"unrealized_pnl":              nil,
		"unrealized_pnl_verified":     false,
		"today_trades":                todayTrades, // 🔥 當日成交筆數
		"today_pnl":                   todayPnL,    // 🔥 當日網格盈虧
		"today_pnl_verified":          todayPnLVerified,
		"today_exchange_pnl":          todayExchangePnL,
		"today_exchange_pnl_verified": false,
		"pnl_verified":                pnlVerified,
		"pnl_asset":                   pnlAsset,
		"pnl_basis":                   pnlBasis,
	})
}

// lastAccountEquityPerDayFromHourly reads the account-scoped equity stream; old symbol samples are fallback only.
func lastAccountEquityPerDayFromHourly(st storage.Storage, exchange, marketType, symbol, account, accountScope string, rangeStart, rangeEnd time.Time) map[string]float64 {
	out := make(map[string]float64)
	lastTs := make(map[string]time.Time)
	if st == nil || exchange == "" || symbol == "" {
		return out
	}
	loc := utils.GlobalLocation
	if loc == nil {
		loc = time.Local
	}
	setDaily := func(timestamp time.Time, value float64, preserve bool) {
		dayKey := timestamp.In(loc).Format("2006-01-02")
		if preserve {
			if _, alreadySet := out[dayKey]; alreadySet {
				return
			}
		}
		if prev, ok := lastTs[dayKey]; !ok || timestamp.After(prev) {
			lastTs[dayKey] = timestamp
			out[dayKey] = value
		}
	}
	if accountScope != "" {
		if accountStorage, ok := st.(interface {
			QueryAccountEquityRecordsByScope(exchange, marketType, accountScope string, startTime, endTime time.Time) ([]*storage.AccountEquityRecord, error)
		}); ok {
			accountRecords, err := accountStorage.QueryAccountEquityRecordsByScope(exchange, marketType, accountScope, rangeStart, rangeEnd)
			if err == nil {
				for _, record := range accountRecords {
					if record != nil {
						setDaily(record.Timestamp, record.AccountEquity, false)
					}
				}
			}
		}
	} else if marketType == "" || marketType == "unknown" {
		if accountStorage, ok := st.(interface {
			QueryAccountEquityRecordsByMarketType(exchange, marketType, account string, startTime, endTime time.Time) ([]*storage.AccountEquityRecord, error)
		}); ok {
			accountRecords, err := accountStorage.QueryAccountEquityRecordsByMarketType(exchange, marketType, account, rangeStart, rangeEnd)
			if err == nil {
				for _, record := range accountRecords {
					if record != nil {
						setDaily(record.Timestamp, record.AccountEquity, false)
					}
				}
			}
		}
		// Legacy account rows have no market dimension. Never mix them into a
		// known spot/futures series where the wallet identity is ambiguous.
		if accountStorage, ok := st.(interface {
			QueryAccountEquityRecords(exchange, account string, startTime, endTime time.Time) ([]*storage.AccountEquityRecord, error)
		}); ok {
			accountRecords, err := accountStorage.QueryAccountEquityRecords(exchange, account, rangeStart, rangeEnd)
			if err == nil {
				for _, record := range accountRecords {
					if record != nil {
						setDaily(record.Timestamp, record.AccountEquity, false)
					}
				}
			}
		}
	}
	var legacyRecords []*storage.HourlyEquityRecord
	var err error
	if accountScope == "" {
		if legacyMarketStorage, ok := st.(interface {
			QueryHourlyEquityRecordsByMarketType(exchange, marketType, symbol, account string, startTime, endTime time.Time) ([]*storage.HourlyEquityRecord, error)
		}); ok {
			legacyRecords, err = legacyMarketStorage.QueryHourlyEquityRecordsByMarketType(exchange, marketType, symbol, account, rangeStart, rangeEnd)
		} else {
			legacyRecords, err = st.QueryHourlyEquityRecords(exchange, symbol, account, rangeStart, rangeEnd)
		}
	}
	if err != nil {
		return out
	}
	for _, rec := range legacyRecords {
		if rec == nil || rec.AccountEquity == nil {
			continue
		}
		setDaily(rec.Timestamp, *rec.AccountEquity, true)
	}
	return out
}

func calculateVerifiedMaxDrawdown(dailyRows []map[string]interface{}) (float64, float64, bool) {
	var peak float64
	var maxDrawdown, maxDrawdownPct float64
	validSamples := 0
	for _, row := range dailyRows {
		equity, ok := row["account_equity"].(float64)
		if !ok || !isFiniteNumber(equity) || equity < 0 {
			continue
		}
		validSamples++
		if equity > peak {
			peak = equity
			continue
		}
		if peak <= 0 {
			continue
		}
		drawdown := peak - equity
		if drawdown > maxDrawdown {
			maxDrawdown = drawdown
		}
		drawdownPct := drawdown / peak * 100
		if drawdownPct > maxDrawdownPct {
			maxDrawdownPct = drawdownPct
		}
	}
	return maxDrawdown, maxDrawdownPct, validSamples >= 2 && peak > 0
}

type accountEquityPoint struct {
	at     time.Time
	equity float64
}

func queryAccountEquityPoints(st storage.Storage, exchange, marketType, account, accountScope string, start, end time.Time) ([]accountEquityPoint, bool, error) {
	if st == nil || exchange == "" {
		return nil, false, nil
	}
	var records []*storage.AccountEquityRecord
	var err error
	if accountScope != "" {
		reader, ok := st.(interface {
			QueryAccountEquityRecordsByScope(exchange, marketType, accountScope string, startTime, endTime time.Time) ([]*storage.AccountEquityRecord, error)
		})
		if !ok {
			return nil, false, nil
		}
		records, err = reader.QueryAccountEquityRecordsByScope(exchange, marketType, accountScope, start, end)
	} else {
		reader, ok := st.(interface {
			QueryAccountEquityRecordsByMarketType(exchange, marketType, account string, startTime, endTime time.Time) ([]*storage.AccountEquityRecord, error)
		})
		if !ok {
			return nil, false, nil
		}
		records, err = reader.QueryAccountEquityRecordsByMarketType(exchange, marketType, account, start, end)
	}
	if err != nil {
		return nil, true, err
	}
	points := make([]accountEquityPoint, 0, len(records))
	for _, record := range records {
		if record == nil || record.Timestamp.IsZero() || record.Timestamp.Before(start) || record.Timestamp.After(end) || !isFiniteNumber(record.AccountEquity) || record.AccountEquity < 0 {
			return nil, true, fmt.Errorf("account equity history contains an invalid sample")
		}
		points = append(points, accountEquityPoint{at: record.Timestamp, equity: record.AccountEquity})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].at.Before(points[j].at) })
	return points, true, nil
}

func mergeDailyEquityFallback(points []accountEquityPoint, dailyRows []map[string]interface{}, location *time.Location) []accountEquityPoint {
	if location == nil {
		location = time.Local
	}
	hourlyDays := make(map[string]struct{}, len(points))
	for _, point := range points {
		hourlyDays[point.at.In(location).Format("2006-01-02")] = struct{}{}
	}
	for _, row := range dailyRows {
		equity, ok := row["account_equity"].(float64)
		if !ok || !isFiniteNumber(equity) || equity < 0 {
			continue
		}
		date, ok := row["date"].(string)
		if !ok {
			continue
		}
		if _, exists := hourlyDays[date]; exists {
			continue
		}
		at, err := time.ParseInLocation("2006-01-02", date, location)
		if err != nil {
			continue
		}
		points = append(points, accountEquityPoint{at: at, equity: equity})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].at.Before(points[j].at) })
	return points
}

func calculateMaxDrawdownFromEquityPoints(points []accountEquityPoint) (float64, float64, bool) {
	rows := make([]map[string]interface{}, len(points))
	for i, point := range points {
		rows[i] = map[string]interface{}{"account_equity": point.equity}
	}
	return calculateVerifiedMaxDrawdown(rows)
}

// getDailyStatistics 獲取每日统计（混合模式：优先使用 statistics 表，缺失的日期從 trades 表补充）
// GET /api/statistics/daily
type dailyFundingReader interface {
	GetFundingIncomeCoverage(exchange, symbol, marketType, accountScope string) (time.Time, time.Time, error)
	GetDailyFundingPaymentsByScope(account, exchange, marketType, symbol, accountScope string, startTime, endTime time.Time) (map[string]float64, error)
}

func queryVerifiedDailyFunding(reader dailyFundingReader, account, exchange, marketType, symbol, accountScope, quoteAsset string, startDate, endDate, now time.Time, location *time.Location) (map[string]float64, map[string]string) {
	amounts := make(map[string]float64)
	assets := make(map[string]string)
	quoteAsset = strings.ToUpper(strings.TrimSpace(quoteAsset))
	if reader == nil || location == nil || strings.TrimSpace(accountScope) == "" || quoteAsset == "" || !startDate.Before(endDate) {
		return amounts, assets
	}
	coveredFrom, coveredThrough, err := reader.GetFundingIncomeCoverage(exchange, symbol, marketType, accountScope)
	if err != nil || coveredFrom.IsZero() || coveredThrough.IsZero() {
		return amounts, assets
	}
	startDay := time.Date(startDate.In(location).Year(), startDate.In(location).Month(), startDate.In(location).Day(), 0, 0, 0, 0, location)
	endLocal := endDate.In(location)
	endDay := time.Date(endLocal.Year(), endLocal.Month(), endLocal.Day(), 0, 0, 0, 0, location)
	for day := startDay; !day.After(endDay); day = day.AddDate(0, 0, 1) {
		dayStartUTC, dayEndUTC := day.UTC(), day.AddDate(0, 0, 1).UTC()
		if coveredFrom.After(dayStartUTC) || coveredThrough.Before(dayEndUTC) || dayEndUTC.After(now.UTC()) {
			continue
		}
		payments, queryErr := reader.GetDailyFundingPaymentsByScope(account, exchange, marketType, symbol, accountScope, dayStartUTC, dayEndUTC)
		if queryErr != nil || len(payments) > 1 {
			continue
		}
		amount, exists := payments[quoteAsset]
		if !exists && len(payments) != 0 {
			continue
		}
		if _, finite := addFiniteProfitValues(amount); !finite {
			continue
		}
		dateKey := day.Format("2006-01-02")
		amounts[dateKey], assets[dateKey] = amount, quoteAsset
	}
	return amounts, assets
}

func validateDailyProfitStatisticsRow(stat *storage.DailyStatisticsWithTradeCount) error {
	if stat == nil || stat.Date.IsZero() || stat.TotalTrades < 0 || stat.WinningTrades < 0 || stat.LosingTrades < 0 ||
		stat.WinningTrades > stat.TotalTrades || stat.LosingTrades > stat.TotalTrades-stat.WinningTrades ||
		stat.TotalVolume < 0 || stat.VolumeProfit < 0 || stat.VolumeStopLoss < 0 || stat.WinRate < 0 || stat.WinRate > 1 {
		return fmt.Errorf("daily PnL statistics are missing or outside valid ranges")
	}
	for _, value := range []float64{stat.TotalVolume, stat.TotalPnL, stat.GrossPnL, stat.TotalFee, stat.WinRate,
		stat.VolumeProfit, stat.VolumeStopLoss, stat.OpenPrice, stat.ClosePrice, stat.PriceChange, stat.PriceChangePct} {
		if !isFiniteNumber(value) {
			return fmt.Errorf("daily PnL statistics contain a non-finite value")
		}
	}
	return nil
}

func getDailyStatistics(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"statistics": []interface{}{}, "max_drawdown": nil, "max_drawdown_pct": nil, "max_drawdown_verified": false})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusOK, gin.H{"statistics": []interface{}{}, "max_drawdown": nil, "max_drawdown_pct": nil, "max_drawdown_verified": false})
		return
	}

	// 解析参數
	daysStr := c.DefaultQuery("days", "30")
	days := 30
	if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
		days = d
	}

	startDate := utils.NowConfiguredTimezone().AddDate(0, 0, -days)
	endDate := utils.NowConfiguredTimezone()

	botID := strings.TrimSpace(c.Query("bot_id"))
	status := pickStatus(c)
	exchQ := strings.TrimSpace(c.Query("exchange"))
	symQ := strings.TrimSpace(c.Query("symbol"))
	marketTypeQ := strings.ToLower(strings.TrimSpace(c.Query("market_type")))
	exchForTrades := exchQ
	symForTrades := symQ
	if status != nil {
		if exchForTrades == "" {
			exchForTrades = status.Exchange
		}
		if symForTrades == "" {
			symForTrades = status.Symbol
		}
	}
	selectedScopeMatchesStatus := status != nil && strings.EqualFold(exchForTrades, status.Exchange) && strings.EqualFold(symForTrades, status.Symbol) && status.MarketType != "" && (marketTypeQ == "" || marketTypeQ == strings.ToLower(strings.TrimSpace(status.MarketType)))

	// Date-only statistics have no exchange/market/credential/asset dimensions.
	statsMap := make(map[string]*storage.Statistics)
	// Read paired-trade statistics only through the full immutable accounting scope.
	tradesStatsMap := make(map[string]*storage.DailyStatisticsWithTradeCount)
	dailyTradesVerified := false
	dailyPnLAsset := ""
	if status != nil {
		dailyPnLAsset = strings.ToUpper(strings.TrimSpace(status.TotalPnLAsset))
		if dailyPnLAsset == "" {
			dailyPnLAsset = strings.ToUpper(strings.TrimSpace(status.QuoteAsset))
		}
	}
	if selectedScopeMatchesStatus && status.AccountScope != "" && dailyPnLAsset != "" {
		if reader, ok := st.(interface {
			QueryDailyStatisticsByDimension(exchange, marketType, symbol, accountScope, asset, botID string, startDate, endDate time.Time) ([]*storage.DailyStatisticsWithTradeCount, error)
		}); ok {
			tradesStats, queryErr := reader.QueryDailyStatisticsByDimension(exchForTrades, status.MarketType, symForTrades, status.AccountScope, dailyPnLAsset, botID, startDate, endDate)
			if queryErr != nil {
				logger.Warn("[每日统计] 精确维度盈亏未核实: %v", queryErr)
			} else {
				validRows := true
				for _, tradeStat := range tradesStats {
					if validateDailyProfitStatisticsRow(tradeStat) != nil {
						validRows = false
						break
					}
					dateKey := tradeStat.Date.Format("2006-01-02")
					if _, duplicate := tradesStatsMap[dateKey]; duplicate {
						validRows = false
						break
					}
					tradesStatsMap[dateKey] = tradeStat
				}
				if validRows {
					dailyTradesVerified = true
				} else {
					clear(tradesStatsMap)
					logger.Warn("[每日统计] 精确维度盈亏包含无效或重复行")
				}
			}
		}
	}
	accountID := GetCurrentAccountID()

	// 3b. 從每日快照表查詢未實現盈虧與日內最大回撤
	snapshotMap := make(map[string]*storage.DailySnapshot)
	if selectedScopeMatchesStatus && status.Exchange != "" && status.Symbol != "" {
		var snapshots []*storage.DailySnapshot
		var errSnap error
		if status.AccountScope != "" {
			if scopeStorage, ok := st.(interface {
				QueryDailySnapshotsByScope(exchange, marketType, symbol, accountScope string, startDate, endDate time.Time) ([]*storage.DailySnapshot, error)
			}); ok {
				snapshots, errSnap = scopeStorage.QueryDailySnapshotsByScope(status.Exchange, status.MarketType, status.Symbol, status.AccountScope, startDate, endDate)
			}
		} else if marketStorage, ok := st.(interface {
			QueryDailySnapshotsByMarketType(exchange, marketType, symbol, account string, startDate, endDate time.Time) ([]*storage.DailySnapshot, error)
		}); ok {
			snapshots, errSnap = marketStorage.QueryDailySnapshotsByMarketType(status.Exchange, status.MarketType, status.Symbol, accountID, startDate, endDate)
		} else {
			snapshots, errSnap = st.QueryDailySnapshots(status.Exchange, status.Symbol, accountID, startDate, endDate)
		}
		if errSnap == nil {
			for _, snap := range snapshots {
				dateKey := snap.Date.Format("2006-01-02")
				snapshotMap[dateKey] = snap
			}
		}
	}

	// 3c. 小時權益：按日最后一條 account_equity（日快照未寫入權益時仍可畫淨值曲線）
	var hourlyAcctByDay map[string]float64
	if selectedScopeMatchesStatus && status.Exchange != "" && status.Symbol != "" {
		loc := utils.GlobalLocation
		if loc == nil {
			loc = time.Local
		}
		startDay, _ := time.ParseInLocation("2006-01-02", startDate.Format("2006-01-02"), loc)
		endDay, _ := time.ParseInLocation("2006-01-02", endDate.Format("2006-01-02"), loc)
		rangeEnd := endDay.Add(24*time.Hour - time.Nanosecond)
		hourlyAcctByDay = lastAccountEquityPerDayFromHourly(st, status.Exchange, status.MarketType, status.Symbol, accountID, status.AccountScope, startDay, rangeEnd)
	}

	// 4. 獲取日K線數據用於计算开盘/收盘價和涨跌幅
	klineMap := make(map[string]*exchange.Candle)
	exchProv := pickExchangeProvider(c)
	if exchProv != nil && selectedScopeMatchesStatus && status.Symbol != "" {
		ctx := c.Request.Context()
		// 獲取日K線數據（1d 周期），限制天數+1以确保覆盖範圍
		candles, err := exchProv.GetHistoricalKlines(ctx, status.Symbol, "1d", days+1)
		if err == nil && len(candles) > 0 {
			candles = exchange.ClipKlineSpikes(candles, 0.03)
			for _, candle := range candles {
				// 將時间戳轉换為日期字符串
				candleTime := time.Unix(candle.Timestamp/1000, 0).UTC()
				dateKey := candleTime.Format("2006-01-02")
				klineMap[dateKey] = candle
			}
		}
	}

	// 4b. Only publish a symbol's funding fee when the full local day is covered
	// and every payment is denominated in the selected quote asset.
	fundingMap := make(map[string]float64)
	fundingAssetMap := make(map[string]string)
	if selectedScopeMatchesStatus && status.AccountScope != "" && status.MarketType != "" && status.QuoteAsset != "" {
		if fundingReader, ok := st.(dailyFundingReader); ok {
			location := utils.GlobalLocation
			if location == nil {
				location = time.Local
			}
			fundingMap, fundingAssetMap = queryVerifiedDailyFunding(fundingReader, accountID, status.Exchange, status.MarketType, status.Symbol, status.AccountScope, status.QuoteAsset, startDate, endDate, time.Now(), location)
		}
	}

	// 4c. orders.realized_pnl lacks denomination and account-scope evidence.
	exchangePnLMap := make(map[string]float64)

	// 5. 合並數據：优先使用 statistics 表的數據，缺失的日期使用 trades 表的數據
	// 構建最终結果
	var result []map[string]interface{}
	startDateStr := startDate.Format("2006-01-02")
	endDateStr := endDate.Format("2006-01-02")

	// 处理所有日期：statistics / trades，以及僅有資金費、交易所已實現或日快照的日期（避免日曆缺日）
	allDates := collectDailyStatDateKeysInRange(startDateStr, endDateStr, statsMap, tradesStatsMap, fundingMap, exchangePnLMap, snapshotMap)

	// 轉换為列表
	var dateList []string
	for dateKey := range allDates {
		dateList = append(dateList, dateKey)
	}

	// 按日期倒序排序
	for i := 0; i < len(dateList)-1; i++ {
		for j := i + 1; j < len(dateList); j++ {
			if dateList[i] < dateList[j] {
				dateList[i], dateList[j] = dateList[j], dateList[i]
			}
		}
	}

	cumulativePnL := 0.0

	// 構建結果（需要按日期正序计算累计盈亏，然后再反轉）
	// 先按日期正序处理
	var tempResult []map[string]interface{}
	for i := len(dateList) - 1; i >= 0; i-- {
		dateKey := dateList[i]
		item := make(map[string]interface{})
		item["date"] = dateKey

		var dailyPnL float64

		// 优先使用 statistics 表的數據
		if stat, exists := statsMap[dateKey]; exists {
			item["total_trades"] = stat.TotalTrades
			item["total_volume"] = stat.TotalVolume
			item["total_pnl"] = stat.TotalPnL
			item["win_rate"] = stat.WinRate
			dailyPnL = stat.TotalPnL
		} else if tradeStat, exists := tradesStatsMap[dateKey]; exists {
			// 使用 trades 表的數據
			item["total_trades"] = tradeStat.TotalTrades
			item["total_volume"] = tradeStat.TotalVolume
			item["total_pnl"] = tradeStat.TotalPnL
			item["gross_pnl"] = tradeStat.GrossPnL
			item["total_fee"] = tradeStat.TotalFee
			item["win_rate"] = tradeStat.WinRate
			item["winning_trades"] = tradeStat.WinningTrades
			item["losing_trades"] = tradeStat.LosingTrades
			item["volume_profit"] = tradeStat.VolumeProfit
			item["volume_stop_loss"] = tradeStat.VolumeStopLoss
			item["pnl_verified"] = true
			item["pnl_asset"] = dailyPnLAsset
			item["pnl_basis"] = "paired_grid_trades"
			dailyPnL = tradeStat.TotalPnL
		} else {
			// 無網格 statistics / trades，但仍有資金費、交易所已實現或快照時仍輸出當日（否則前端日曆顯示「無數據」）
			_, hasFunding := fundingMap[dateKey]
			_, hasExchange := exchangePnLMap[dateKey]
			_, hasSnap := snapshotMap[dateKey]
			if !hasFunding && !hasExchange && !hasSnap {
				continue
			}
			item["total_trades"] = 0
			item["total_volume"] = 0
			item["total_pnl"] = nil
			item["win_rate"] = 0
			item["pnl_verified"] = dailyTradesVerified
			item["pnl_asset"] = dailyPnLAsset
			if dailyTradesVerified {
				item["pnl_basis"] = "paired_grid_trades"
				item["total_pnl"] = 0.0
				dailyPnL = 0
			} else {
				item["gross_pnl"] = nil
				item["total_fee"] = nil
			}
		}

		// 如果 statistics 表的數據存在，但從 trades 表可以獲取盈利/亏损交易數和交易量細分，也添加進去
		if _, exists := statsMap[dateKey]; exists {
			if tradeStat, exists := tradesStatsMap[dateKey]; exists {
				item["winning_trades"] = tradeStat.WinningTrades
				item["losing_trades"] = tradeStat.LosingTrades
				item["volume_profit"] = tradeStat.VolumeProfit
				item["volume_stop_loss"] = tradeStat.VolumeStopLoss
			}
		}

		// 添加K線數據（开盘價、收盘價、涨跌幅）
		if candle, exists := klineMap[dateKey]; exists {
			item["open_price"] = candle.Open
			item["close_price"] = candle.Close
			priceChange := candle.Close - candle.Open
			item["price_change"] = priceChange
			if candle.Open > 0 {
				item["price_change_pct"] = (priceChange / candle.Open) * 100
			} else {
				item["price_change_pct"] = 0
			}
		}

		// 计算累计盈亏
		if dailyTradesVerified {
			var cumulativeOK bool
			if cumulativePnL, cumulativeOK = addFiniteProfitValues(cumulativePnL, dailyPnL); !cumulativeOK {
				c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "每日 PnL 累计溢出，拒绝返回不完整统计"})
				return
			}
			item["cumulative_pnl"] = cumulativePnL
		} else {
			item["cumulative_pnl"] = nil
		}

		// 合併每日快照：未實現盈虧、日內最大回撤、交易所帳戶權益（真實淨值）
		if snap, ok := snapshotMap[dateKey]; ok {
			if strings.EqualFold(strings.TrimSpace(snap.UnrealizedPnLAsset), strings.TrimSpace(dailyPnLAsset)) {
				item["unrealized_pnl"] = snap.UnrealizedPnL
				item["unrealized_pnl_verified"] = true
			} else {
				item["unrealized_pnl"] = nil
				item["unrealized_pnl_verified"] = false
			}
			item["intraday_max_drawdown"] = nil
			item["intraday_max_drawdown_pct"] = nil
			if snap.AccountEquity != nil {
				item["account_equity"] = *snap.AccountEquity
			}
		}
		if _, ok := item["account_equity"]; !ok && hourlyAcctByDay != nil {
			if v, ok2 := hourlyAcctByDay[dateKey]; ok2 {
				item["account_equity"] = v
			}
		}

		// 合併每日資金費用
		if funding, ok := fundingMap[dateKey]; ok {
			item["funding_fee"] = funding
			item["funding_fee_verified"] = true
			item["funding_fee_asset"] = fundingAssetMap[dateKey]
		} else {
			item["funding_fee"] = nil
			item["funding_fee_verified"] = false
		}

		// Exchange PnL stays absent until its denomination and account scope are proven.

		// 賬面盈虧 = 已平倉盈虧 + 未實現盈虧（真正帳面值）
		item["book_value_pnl"] = nil
		item["book_value_pnl_verified"] = false

		tempResult = append(tempResult, item)
	}

	// 反轉結果，使其按日期倒序
	for i := len(tempResult) - 1; i >= 0; i-- {
		result = append(result, tempResult[i])
	}

	// 6. Prefer every account-level hourly observation. Fill only days without
	// hourly observations from daily snapshots, avoiding symbol-local equity.
	equityLocation := utils.GlobalLocation
	if equityLocation == nil {
		equityLocation = time.Local
	}
	equityRangeStart, _ := time.ParseInLocation("2006-01-02", startDate.Format("2006-01-02"), equityLocation)
	equityRangeEndDay, _ := time.ParseInLocation("2006-01-02", endDate.Format("2006-01-02"), equityLocation)
	equityRangeEnd := equityRangeEndDay.Add(24*time.Hour - time.Nanosecond)
	var accountEquityPoints []accountEquityPoint
	accountEquityHistorySupported := false
	var accountEquityHistoryErr error
	if selectedScopeMatchesStatus {
		accountEquityPoints, accountEquityHistorySupported, accountEquityHistoryErr = queryAccountEquityPoints(
			st, status.Exchange, status.MarketType, accountID, status.AccountScope, equityRangeStart, equityRangeEnd,
		)
	}
	if accountEquityHistoryErr == nil {
		accountEquityPoints = mergeDailyEquityFallback(accountEquityPoints, tempResult, equityLocation)
	}
	maxDrawdown, maxDrawdownPct, maxDrawdownVerified := calculateMaxDrawdownFromEquityPoints(accountEquityPoints)
	if !accountEquityHistorySupported {
		maxDrawdown, maxDrawdownPct, maxDrawdownVerified = calculateVerifiedMaxDrawdown(tempResult)
	}
	maxDrawdownVerified = maxDrawdownVerified && accountEquityHistoryErr == nil
	drawdownSampling := "daily_account_equity"
	if accountEquityHistorySupported && len(accountEquityPoints) > 0 {
		drawdownSampling = "hourly_account_equity"
	}
	drawdownAsset := accountEquityCurrency
	maxDrawdownVerified = maxDrawdownVerified && drawdownAsset != ""

	resp := gin.H{
		"statistics":            result,
		"max_drawdown":          nil,
		"max_drawdown_pct":      nil,
		"max_drawdown_verified": maxDrawdownVerified,
		"max_drawdown_sampling": drawdownSampling,
		"pnl_verified":          dailyTradesVerified,
		"funding_pnl_verified":  false,
	}
	if maxDrawdownVerified {
		resp["max_drawdown"] = maxDrawdown
		resp["max_drawdown_pct"] = maxDrawdownPct
		resp["max_drawdown_asset"] = drawdownAsset
	}
	if dailyTradesVerified {
		resp["pnl_asset"] = dailyPnLAsset
		resp["pnl_basis"] = "paired_grid_trades"
	}
	if st := pickStatus(c); st != nil {
		mt := strings.TrimSpace(st.MarketType)
		if mt == "" {
			mt = "futures"
		}
		resp["market_type"] = mt
	}

	c.JSON(http.StatusOK, resp)
}

// getTradeStatistics 獲取交易统计
// GET /api/statistics/trades
func getTradeStatistics(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"trades": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"trades": []interface{}{}})
		return
	}

	// 解析参數
	limitStr := c.DefaultQuery("limit", "100")
	offsetStr := c.DefaultQuery("offset", "0")
	limit := 100
	offset := 0
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}
	if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
		offset = o
	}

	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	} else {
		startTime = utils.NowConfiguredTimezone().AddDate(0, 0, -7) // 默认最近7天
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	} else {
		endTime = utils.NowConfiguredTimezone()
	}

	trades, err := storage.QueryTrades(startTime, endTime, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 轉换時间為UTC+8
	tradesResponse := make([]map[string]interface{}, len(trades))
	for i, trade := range trades {
		tradesResponse[i] = map[string]interface{}{
			"buy_order_id":  trade.BuyOrderID,
			"sell_order_id": trade.SellOrderID,
			"symbol":        trade.Symbol,
			"buy_price":     trade.BuyPrice,
			"sell_price":    trade.SellPrice,
			"quantity":      trade.Quantity,
			"pnl":           trade.PnL,
			"created_at":    utils.ToUTC8(trade.CreatedAt),
		}
	}

	c.JSON(http.StatusOK, gin.H{"trades": tradesResponse})
}

// 这些函數已移动到 web/api_config.go
// 保留这些存根函數以保持向后兼容（如果其他地方有引用）
func getConfig(c *gin.Context) {
	getConfigHandler(c)
}

func updateConfig(c *gin.Context) {
	updateConfigHandler(c)
}

func startTrading(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	marketType := c.Query("market_type")

	if exchange == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.missing_exchange_or_symbol")
		return
	}

	if symbolManagerProvider == nil {
		respondError(c, http.StatusInternalServerError, "error.symbol_manager_unavailable")
		return
	}

	err := symbolManagerProvider.StartSymbol(exchange, symbol, marketType)
	if err != nil {
		logger.Error("❌ [%s:%s:%s] 啟动交易失败: %v", exchange, symbol, marketType, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 更新状態
	key := makeSymbolKey(exchange, symbol, marketType)
	statusMu.Lock()
	if status, ok := statusBySymbol[key]; ok {
		status.Running = true
	} else {
		statusBySymbol[key] = &SystemStatus{
			Running:    true,
			Exchange:   exchange,
			Symbol:     symbol,
			MarketType: marketType,
		}
	}
	statusMu.Unlock()

	logger.Info("✅ [%s:%s:%s] 交易已啟动", exchange, symbol, marketType)
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("交易已啟动: %s:%s", exchange, symbol)})
}

func stopTrading(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	marketType := c.Query("market_type")

	if exchange == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.missing_exchange_or_symbol")
		return
	}

	if symbolManagerProvider == nil {
		respondError(c, http.StatusInternalServerError, "error.symbol_manager_unavailable")
		return
	}

	err := symbolManagerProvider.StopSymbol(exchange, symbol)
	if err != nil {
		logger.Error("❌ [%s:%s:%s] 停止交易失败: %v", exchange, symbol, marketType, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 更新状態
	key := makeSymbolKey(exchange, symbol, marketType)
	statusMu.Lock()
	if status, ok := statusBySymbol[key]; ok {
		status.Running = false
	}
	statusMu.Unlock()

	logger.Info("⏹️ [%s:%s:%s] 交易已停止", exchange, symbol, marketType)
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("交易已停止: %s:%s", exchange, symbol)})
}

// ClosePositionsResponse 平倉响应
type ClosePositionsResponse struct {
	SuccessCount int    `json:"success_count"`
	FailCount    int    `json:"fail_count"`
	Message      string `json:"message"`
}

func closeAllPositions(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")

	if exchange == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.missing_exchange_or_symbol")
		return
	}

	if symbolManagerProvider == nil {
		respondError(c, http.StatusInternalServerError, "error.symbol_manager_unavailable")
		return
	}

	// 通過适配器調用 ClosePositions 方法
	adapter, ok := symbolManagerProvider.(interface {
		ClosePositions(exchange, symbol string) (*ClosePositionsResponse, error)
	})
	if !ok {
		respondError(c, http.StatusInternalServerError, "error.close_positions_not_supported")
		return
	}

	result, err := adapter.ClosePositions(exchange, symbol)
	if err != nil {
		logger.Error("❌ [%s:%s] 平倉失败: %v", exchange, symbol, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.Info("📊 [%s:%s] 平倉完成: 成功=%d, 失败=%d", exchange, symbol, result.SuccessCount, result.FailCount)
	c.JSON(http.StatusOK, result)
}

// ========== 交易控制相关API ==========

var (
	// SymbolManager 提供者（需要從main.go注入）
	symbolManagerProvider SymbolManagerProvider
)

// SymbolManagerProvider SymbolManager 提供者接口
type SymbolManagerProvider interface {
	Get(exchange, symbol string) (interface{}, bool)               // 回傳 SymbolRuntime（使用 interface{} 避免循环依赖）
	GetEx(exchange, symbol, marketType string) (interface{}, bool) // 按 market_type 獲取（spot/futures），空則默認 futures
	GetByBotID(botID string) (interface{}, bool)                   // 按 Bot UUID（或确定性 ID）獲取運行時；不存在則 (nil, false)
	List() []interface{}                                           // 回傳 SymbolRuntime 列表
	StartSymbol(exchange, symbol, marketType string) error         // 啟动指定交易所/币种的交易，marketType 為空時自動選首個未運行的
	StopSymbol(exchange, symbol string) error                      // 停止指定交易所/币种的交易
}

// TradingParamsUpdater 交易参數热更新接口（可选接口，用於配置变更時推送到运行時）
type TradingParamsUpdater interface {
	UpdateTradingParams(latestConfig *config.Config) []string
}

// EquityScopeConfigUpdater synchronizes the configured portfolio scope independently from live trading parameters.
type EquityScopeConfigUpdater interface {
	UpdateEquityScopeConfig(latestConfig *config.Config)
}

// RegisterSymbolManager 注册 SymbolManager
func RegisterSymbolManager(provider SymbolManagerProvider) {
	symbolManagerProvider = provider
}

// ========== 系统監控相关API ==========

var (
	// 系统監控數據提供者（需要從main.go注入）
	systemMetricsProvider SystemMetricsProvider
)

// SystemMetricsProvider 系统監控數據提供者接口
type SystemMetricsProvider interface {
	GetCurrentMetrics() (*SystemMetricsResponse, error)
	GetMetrics(startTime, endTime time.Time, granularity string) ([]*SystemMetricsResponse, error)
	GetDailyMetrics(days int) ([]*DailySystemMetricsResponse, error)
}

// SystemMetricsResponse 系统監控數據响应
type SystemMetricsResponse struct {
	Timestamp     time.Time `json:"timestamp"`
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryMB      float64   `json:"memory_mb"`
	MemoryPercent float64   `json:"memory_percent"`
	ProcessID     int       `json:"process_id"`
}

// DailySystemMetricsResponse 每日彙總數據响应
type DailySystemMetricsResponse struct {
	Date          time.Time `json:"date"`
	AvgCPUPercent float64   `json:"avg_cpu_percent"`
	MaxCPUPercent float64   `json:"max_cpu_percent"`
	MinCPUPercent float64   `json:"min_cpu_percent"`
	AvgMemoryMB   float64   `json:"avg_memory_mb"`
	MaxMemoryMB   float64   `json:"max_memory_mb"`
	MinMemoryMB   float64   `json:"min_memory_mb"`
	SampleCount   int       `json:"sample_count"`
}

// SetSystemMetricsProvider 設置系统監控數據提供者
func SetSystemMetricsProvider(provider SystemMetricsProvider) {
	systemMetricsProvider = provider
}

// getSystemMetrics 獲取系统監控數據
// GET /api/system/metrics
// 参數：
//   - start_time: 开始時间（可選，ISO 8601格式，默认最近7天）
//   - end_time: 結束時间（可選，ISO 8601格式，默认當前時间）
//   - granularity: 粒度（detail/daily，默认detail）
func getSystemMetrics(c *gin.Context) {
	if systemMetricsProvider == nil {
		c.JSON(http.StatusOK, gin.H{"metrics": []interface{}{}})
		return
	}

	// 解析参數
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")
	granularity := c.DefaultQuery("granularity", "detail")

	var startTime, endTime time.Time
	var err error

	if startTimeStr == "" {
		// 默认最近7天
		startTime = utils.NowConfiguredTimezone().Add(-7 * 24 * time.Hour)
	} else {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	}

	if endTimeStr == "" {
		endTime = utils.NowConfiguredTimezone()
	} else {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	}

	if granularity == "daily" {
		// 返回每日彙總數據
		days := int(endTime.Sub(startTime).Hours() / 24)
		if days <= 0 {
			days = 30 // 默认30天
		}
		// 限制查詢天數，防止返回過多數據
		if days > 365 {
			days = 365 // 最多查詢1年
		}
		dailyMetrics, err := systemMetricsProvider.GetDailyMetrics(days)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"metrics": dailyMetrics, "granularity": "daily"})
	} else {
		// 返回细粒度數據
		metrics, err := systemMetricsProvider.GetMetrics(startTime, endTime, "detail")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"metrics": metrics, "granularity": "detail"})
	}
}

// getCurrentSystemMetrics 獲取當前系统状態
// GET /api/system/metrics/current
func getCurrentSystemMetrics(c *gin.Context) {
	if systemMetricsProvider == nil {
		// 返回完整的對象結構，避免前端访问 undefined 字段
		c.JSON(http.StatusOK, &SystemMetricsResponse{
			Timestamp:     utils.ToUTC8(time.Now()),
			CPUPercent:    0,
			MemoryMB:      0,
			MemoryPercent: 0,
			ProcessID:     0,
		})
		return
	}

	metrics, err := systemMetricsProvider.GetCurrentMetrics()
	if err != nil {
		// 即使出錯也返回完整的對象結構
		c.JSON(http.StatusOK, &SystemMetricsResponse{
			Timestamp:     utils.ToUTC8(time.Now()),
			CPUPercent:    0,
			MemoryMB:      0,
			MemoryPercent: 0,
			ProcessID:     0,
		})
		return
	}

	// 确保所有字段都有默认值
	if metrics == nil {
		metrics = &SystemMetricsResponse{
			Timestamp:     utils.ToUTC8(time.Now()),
			CPUPercent:    0,
			MemoryMB:      0,
			MemoryPercent: 0,
			ProcessID:     0,
		}
	}

	c.JSON(http.StatusOK, metrics)
}

// getDailySystemMetrics 獲取每日彙總數據
// GET /api/system/metrics/daily
// 参數：
//   - days: 查詢天數（默认30天）
func getDailySystemMetrics(c *gin.Context) {
	if systemMetricsProvider == nil {
		c.JSON(http.StatusOK, gin.H{"metrics": []interface{}{}})
		return
	}

	daysStr := c.DefaultQuery("days", "30")
	days := 30
	if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
		days = d
	}

	metrics, err := systemMetricsProvider.GetDailyMetrics(days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"metrics": metrics})
}

// ========== 槽位數據相关API ==========

var (
	// 槽位數據提供者（需要從main.go注入）
	positionManagerProvider PositionManagerProvider
	// 订單金額配置（用於计算订單數量）
	orderQuantityConfig float64
)

// SetOrderQuantityConfig 設置订單金額配置
func SetOrderQuantityConfig(quantity float64) {
	orderQuantityConfig = quantity
}

// PositionManagerProvider 槽位數據提供者接口
type PositionManagerProvider interface {
	GetAllSlots() []SlotInfo
	GetSlotCount() int
	GetReconcileCount() int64
	GetLastReconcileTime() time.Time
	GetTotalBuyQty() float64
	GetTotalSellQty() float64
	GetPriceInterval() float64
	GetProfitSpread() float64
	GetLeverage() int // 獲取杠杆倍數
}

// SlotInfo 槽位信息
type SlotInfo struct {
	Exchange              string    `json:"exchange"`
	Symbol                string    `json:"symbol"`
	Price                 float64   `json:"price"`
	PositionStatus        string    `json:"position_status"` // EMPTY/FILLED
	PositionQty           float64   `json:"position_qty"`
	AvgBuyPrice           float64   `json:"avg_buy_price"`
	BuyFee                float64   `json:"buy_fee"`
	CostBasisUnverified   bool      `json:"cost_basis_unverified"`
	FeeValuationUnknown   bool      `json:"fee_valuation_unknown"`
	PendingFeeSupplements int       `json:"pending_fee_supplements"`
	PositionLeg           string    `json:"position_leg"`
	OrderID               int64     `json:"order_id"`
	ClientOID             string    `json:"client_order_id"`
	OrderSide             string    `json:"order_side"`   // BUY/SELL
	OrderStatus           string    `json:"order_status"` // NOT_PLACED/PLACED/CONFIRMED/PARTIALLY_FILLED/FILLED/CANCELED
	OrderPrice            float64   `json:"order_price"`
	OrderFilledQty        float64   `json:"order_filled_qty"`
	OrderCreatedAt        time.Time `json:"order_created_at"`
	SlotStatus            string    `json:"slot_status"`   // FREE/PENDING/LOCKED
	StrategyName          string    `json:"strategy_name"` // 策略名称
	StrategyType          string    `json:"strategy_type"` // 策略類型
}

// SetPositionManagerProvider 設置槽位數據提供者
func SetPositionManagerProvider(provider PositionManagerProvider) {
	positionManagerProvider = provider
}

// positionManagerAdapter 槽位管理器适配器
type positionManagerAdapter struct {
	manager *position.SuperPositionManager
}

// NewPositionManagerAdapter 創建槽位管理器适配器
func NewPositionManagerAdapter(manager *position.SuperPositionManager) PositionManagerProvider {
	return &positionManagerAdapter{manager: manager}
}

// GetAllSlots 獲取所有槽位信息
func (a *positionManagerAdapter) GetAllSlots() []SlotInfo {
	detailedSlots := a.manager.GetAllSlotsDetailed()

	// 🔥 調試：打印管理器的交易對信息
	symbol := a.manager.GetSymbol()
	exchange := a.manager.GetExchange()
	anchorPrice := a.manager.GetAnchorPrice()
	logger.Info("[DEBUG] GetAllSlots called - exchange=%s, symbol=%s, anchorPrice=%.2f, slotsCount=%d",
		exchange, symbol, anchorPrice, len(detailedSlots))

	slots := make([]SlotInfo, len(detailedSlots))
	for i, ds := range detailedSlots {
		slots[i] = SlotInfo{
			Exchange:              exchange,
			Symbol:                symbol,
			Price:                 ds.Price,
			PositionStatus:        ds.PositionStatus,
			PositionQty:           ds.PositionQty,
			AvgBuyPrice:           ds.AvgBuyPrice,
			BuyFee:                ds.BuyFee,
			CostBasisUnverified:   ds.CostBasisUnverified,
			FeeValuationUnknown:   ds.FeeValuationUnknown,
			PendingFeeSupplements: ds.PendingFeeSupplements,
			PositionLeg:           ds.PositionLeg,
			OrderID:               ds.OrderID,
			ClientOID:             ds.ClientOID,
			OrderSide:             ds.OrderSide,
			OrderStatus:           ds.OrderStatus,
			OrderPrice:            ds.OrderPrice,
			OrderFilledQty:        ds.OrderFilledQty,
			OrderCreatedAt:        utils.ToUTC8(ds.OrderCreatedAt),
			SlotStatus:            ds.SlotStatus,
			StrategyName:          ds.StrategyName,
			StrategyType:          ds.StrategyType,
		}
	}
	return slots
}

// GetVerifiedUnrealizedPnL exposes the strategy's fail-closed local valuation.
func (a *positionManagerAdapter) GetVerifiedUnrealizedPnL(currentPrice float64) (float64, bool) {
	if a == nil || a.manager == nil {
		return 0, false
	}
	return a.manager.GetUnrealizedPnLVerified(currentPrice)
}

func (a *positionManagerAdapter) GetVerifiedUnrealizedPnLForAsset(currentPrice float64, asset string) (float64, bool) {
	if a == nil || a.manager == nil || !strings.EqualFold(a.manager.GetPnLAsset(), asset) {
		return 0, false
	}
	return a.manager.GetUnrealizedPnLVerified(currentPrice)
}

func (a *positionManagerAdapter) GetDirection() string { return a.manager.GetDirection() }

// GetSlotCount 獲取槽位總數
func (a *positionManagerAdapter) GetSlotCount() int {
	return a.manager.GetSlotCount()
}

// GetReconcileCount 獲取對账次數
func (a *positionManagerAdapter) GetReconcileCount() int64 {
	return a.manager.GetReconcileCount()
}

// GetLastReconcileTime 獲取最后對账時间
func (a *positionManagerAdapter) GetLastReconcileTime() time.Time {
	return a.manager.GetLastReconcileTime()
}

// GetTotalBuyQty 獲取累计買入數量
func (a *positionManagerAdapter) GetTotalBuyQty() float64 {
	return a.manager.GetTotalBuyQty()
}

// GetTotalSellQty 獲取累计賣出數量
func (a *positionManagerAdapter) GetTotalSellQty() float64 {
	return a.manager.GetTotalSellQty()
}

// GetPriceInterval 獲取價格间隔
func (a *positionManagerAdapter) GetPriceInterval() float64 {
	return a.manager.GetPriceInterval()
}

// GetProfitSpread 獲取利潤間距（平倉價差）
func (a *positionManagerAdapter) GetProfitSpread() float64 {
	return a.manager.GetProfitSpread()
}

// GetLeverage 獲取杠杆倍數
func (a *positionManagerAdapter) GetLeverage() int {
	return a.manager.GetLeverage()
}

// getSlots 獲取所有槽位信息
// GET /api/slots
func getSlots(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")

	pmProvider := PickPositionProvider(c)
	if pmProvider == nil {
		c.JSON(http.StatusOK, gin.H{"slots": []interface{}{}, "count": 0})
		return
	}

	slots := pmProvider.GetAllSlots()
	count := pmProvider.GetSlotCount()

	// 🔥 調試：打印前3個槽位的價格
	if len(slots) > 0 {
		logger.Info("[DEBUG] getSlots - exchange=%s, symbol=%s, total=%d, first 3 prices: %.2f, %.2f, %.2f",
			exchange, symbol, len(slots),
			slots[0].Price,
			slots[min(1, len(slots)-1)].Price,
			slots[min(2, len(slots)-1)].Price)
	}

	c.JSON(http.StatusOK, gin.H{
		"slots": slots,
		"count": count,
	})
}

// ========== 策略资金分配相关API ==========

var (
	// 策略數據提供者（需要從main.go注入）
	strategyProvider StrategyProvider
)

// getExchangeForCancel 獲取交易所實例：優先從運行中的 bot 獲取，否則從配置按需創建
func getExchangeForCancel(exchangeName, symbol, marketType string) (exchange.IExchange, error) {
	if exchangeGetterFunc != nil {
		if ex := exchangeGetterFunc(exchangeName); ex != nil {
			return ex, nil
		}
	}
	// 無運行中的 bot 時，從配置按需創建（支持訂單管理在 bot 未運行時取消訂單）
	if globalConfig == nil {
		return nil, fmt.Errorf("配置未加載")
	}
	if marketType == "" {
		marketType = "futures"
	}
	return exchange.NewExchange(globalConfig, exchangeName, symbol, marketType)
}

// cancelOrder 取消订單
// POST /api/orders/:id/cancel
func cancelOrder(c *gin.Context) {
	orderIDStr := c.Param("id")
	orderID, err := strconv.ParseInt(orderIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "無效的订單ID"})
		return
	}

	// 獲取交易所和交易對
	exchangeName := c.Query("exchange")
	symbol := c.Query("symbol")
	marketType := c.DefaultQuery("market_type", "futures")
	if exchangeName == "" || symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 exchange 或 symbol 参數"})
		return
	}

	ex, err := getExchangeForCancel(exchangeName, symbol, marketType)
	if err != nil {
		logger.Warn("❌ [取消订單] 獲取交易所失敗: exchange=%s, symbol=%s, error=%v", exchangeName, symbol, err)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "交易所不存在: " + exchangeName})
		return
	}

	// 調用交易所取消订單
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	if err := ex.CancelOrder(ctx, symbol, orderID); err != nil {
		logger.Error("❌ [取消订單] 失败: orderID=%d, exchange=%s, symbol=%s, error=%v", orderID, exchangeName, symbol, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "取消订單失败: " + err.Error()})
		return
	}

	logger.Info("✅ [取消订單] 成功: orderID=%d, exchange=%s, symbol=%s", orderID, exchangeName, symbol)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "订單已取消", "order_id": orderID})
}

// batchCancelOrders 批量取消订單
// POST /api/orders/cancel
func batchCancelOrders(c *gin.Context) {
	var req struct {
		OrderIDs   []int64 `json:"order_ids"`
		Exchange   string  `json:"exchange"`
		Symbol     string  `json:"symbol"`
		MarketType string  `json:"market_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "無效的请求數據"})
		return
	}

	if len(req.OrderIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "订單ID列表為空"})
		return
	}

	if req.Exchange == "" || req.Symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 exchange 或 symbol 参數"})
		return
	}

	if req.MarketType == "" {
		req.MarketType = "futures"
	}

	ex, err := getExchangeForCancel(req.Exchange, req.Symbol, req.MarketType)
	if err != nil {
		logger.Warn("❌ [批量取消订單] 獲取交易所失敗: exchange=%s, symbol=%s, error=%v", req.Exchange, req.Symbol, err)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "交易所不存在: " + req.Exchange})
		return
	}

	// 調用交易所批量取消订單
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	if err := ex.BatchCancelOrders(ctx, req.Symbol, req.OrderIDs); err != nil {
		logger.Error("❌ [批量取消订單] 失败: orderIDs=%v, exchange=%s, symbol=%s, error=%v", req.OrderIDs, req.Exchange, req.Symbol, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "批量取消订單失败: " + err.Error()})
		return
	}

	logger.Info("✅ [批量取消订單] 成功: count=%d, exchange=%s, symbol=%s", len(req.OrderIDs), req.Exchange, req.Symbol)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("已取消 %d 個订單", len(req.OrderIDs)), "count": len(req.OrderIDs)})
}

// ExchangeOpenOrderInfo 交易所开放委托信息
type ExchangeOpenOrderInfo struct {
	OrderID       int64     `json:"order_id"`
	ClientOrderID string    `json:"client_order_id"`
	Exchange      string    `json:"exchange"`
	Symbol        string    `json:"symbol"`
	Price         float64   `json:"price"`
	Quantity      float64   `json:"quantity"`
	ExecutedQty   float64   `json:"executed_qty"`
	Side          string    `json:"side"`
	Type          string    `json:"type"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	IsMine        bool      `json:"is_mine"`       // 是否为本机器人管理的委托
	StrategyName  string    `json:"strategy_name"` // 如果是本机器人的委托，关联的策略名
	SlotPrice     float64   `json:"slot_price"`    // 关联槽位价格
}

// getExchangeOpenOrders 直接从交易所查询开放委托，并与内部 slots 对比标记
// GET /api/orders/exchange-open
func getExchangeOpenOrders(c *gin.Context) {
	exchangeName := c.Query("exchange")
	symbol := c.Query("symbol")
	marketType := c.DefaultQuery("market_type", "futures")

	if exchangeName == "" || symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 exchange 或 symbol 参數"})
		return
	}

	ex, err := getExchangeForCancel(exchangeName, symbol, marketType)
	if err != nil {
		logger.Warn("❌ [交易所委托] 获取交易所失败: exchange=%s, symbol=%s, error=%v", exchangeName, symbol, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取交易所失败: " + err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	orders, err := ex.GetOpenOrders(ctx, symbol)
	if err != nil {
		logger.Error("❌ [交易所委托] 查询失败: exchange=%s, symbol=%s, error=%v", exchangeName, symbol, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查询交易所委托失败: " + err.Error()})
		return
	}

	// 构建内部 slots 的 orderID 集合，用于标记哪些是"我们的"委托
	myOrderIDs := make(map[int64]struct {
		StrategyName string
		SlotPrice    float64
	})
	pmProvider := PickPositionProvider(c)
	if pmProvider != nil {
		for _, slot := range pmProvider.GetAllSlots() {
			if slot.OrderID > 0 {
				myOrderIDs[slot.OrderID] = struct {
					StrategyName string
					SlotPrice    float64
				}{slot.StrategyName, slot.Price}
			}
		}
	}

	result := make([]ExchangeOpenOrderInfo, 0, len(orders))
	for _, o := range orders {
		info := ExchangeOpenOrderInfo{
			OrderID:       o.OrderID,
			ClientOrderID: o.ClientOrderID,
			Exchange:      exchangeName,
			Symbol:        o.Symbol,
			Price:         o.Price,
			Quantity:      o.Quantity,
			ExecutedQty:   o.ExecutedQty,
			Side:          string(o.Side),
			Type:          string(o.Type),
			Status:        string(o.Status),
			CreatedAt:     utils.ToUTC8(o.CreatedAt),
		}
		if meta, ok := myOrderIDs[o.OrderID]; ok {
			info.IsMine = true
			info.StrategyName = meta.StrategyName
			info.SlotPrice = meta.SlotPrice
		}
		result = append(result, info)
	}

	logger.Info("✅ [交易所委托] 查询成功: exchange=%s, symbol=%s, count=%d", exchangeName, symbol, len(result))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"orders":  result,
		"count":   len(result),
	})
}

// cancelAllExchangeOrders 取消交易所该交易对的所有开放委托（一键清理）
// POST /api/orders/cancel-all-exchange
func cancelAllExchangeOrders(c *gin.Context) {
	var req struct {
		Exchange   string `json:"exchange"`
		Symbol     string `json:"symbol"`
		MarketType string `json:"market_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的请求数据"})
		return
	}
	if req.Exchange == "" || req.Symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 exchange 或 symbol 参數"})
		return
	}
	if req.MarketType == "" {
		req.MarketType = "futures"
	}

	ex, err := getExchangeForCancel(req.Exchange, req.Symbol, req.MarketType)
	if err != nil {
		logger.Warn("❌ [一键清理] 获取交易所失败: exchange=%s, symbol=%s, error=%v", req.Exchange, req.Symbol, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取交易所失败: " + err.Error()})
		return
	}

	// 先查询所有开放委托
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	orders, err := ex.GetOpenOrders(ctx, req.Symbol)
	if err != nil {
		logger.Error("❌ [一键清理] 查询委托失败: exchange=%s, symbol=%s, error=%v", req.Exchange, req.Symbol, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查询委托失败: " + err.Error()})
		return
	}

	if len(orders) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "没有需要取消的委托", "count": 0})
		return
	}

	orderIDs := make([]int64, 0, len(orders))
	for _, o := range orders {
		orderIDs = append(orderIDs, o.OrderID)
	}

	if err := ex.BatchCancelOrders(ctx, req.Symbol, orderIDs); err != nil {
		logger.Error("❌ [一键清理] 批量取消失败: exchange=%s, symbol=%s, count=%d, error=%v", req.Exchange, req.Symbol, len(orderIDs), err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "批量取消失败: " + err.Error()})
		return
	}

	logger.Info("✅ [一键清理] 成功取消全部委托: exchange=%s, symbol=%s, count=%d", req.Exchange, req.Symbol, len(orderIDs))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("已取消全部 %d 个委托", len(orderIDs)),
		"count":   len(orderIDs),
	})
}

// RiskMonitorProvider 风控監控提供者接口
type RiskMonitorProvider interface {
	IsTriggered() bool
	GetTriggeredTime() time.Time
	GetRecoveredTime() time.Time
	GetMonitorSymbols() []string
	GetSymbolData(symbol string) interface{}
}

var (
	riskMonitorProvider RiskMonitorProvider
)

// SetRiskMonitorProvider 設置风控監控提供者
func SetRiskMonitorProvider(provider RiskMonitorProvider) {
	riskMonitorProvider = provider
}

// RiskStatusResponse 风控状態响应
type RiskStatusResponse struct {
	Triggered      bool      `json:"triggered"`
	TriggeredTime  time.Time `json:"triggered_time"`
	RecoveredTime  time.Time `json:"recovered_time"`
	MonitorSymbols []string  `json:"monitor_symbols"`
}

// SymbolMonitorData 币种監控數據
type SymbolMonitorData struct {
	Symbol         string    `json:"symbol"`
	CurrentPrice   float64   `json:"current_price"`
	AveragePrice   float64   `json:"average_price"`
	PriceDeviation float64   `json:"price_deviation"`
	CurrentVolume  float64   `json:"current_volume"`
	AverageVolume  float64   `json:"average_volume"`
	VolumeRatio    float64   `json:"volume_ratio"`
	IsAbnormal     bool      `json:"is_abnormal"`
	LastUpdate     time.Time `json:"last_update"`
}

// getRiskStatus 獲取风控状態
// GET /api/risk/status
func getRiskStatus(c *gin.Context) {
	riskProv := PickRiskProvider(c)
	if riskProv == nil {
		c.JSON(http.StatusOK, RiskStatusResponse{
			Triggered:      false,
			MonitorSymbols: []string{},
		})
		return
	}

	response := RiskStatusResponse{
		Triggered:      riskProv.IsTriggered(),
		TriggeredTime:  riskProv.GetTriggeredTime(),
		RecoveredTime:  riskProv.GetRecoveredTime(),
		MonitorSymbols: riskProv.GetMonitorSymbols(),
	}

	c.JSON(http.StatusOK, response)
}

// getRiskMonitorData 獲取監控币种數據
// GET /api/risk/monitor
func getRiskMonitorData(c *gin.Context) {
	riskProv := PickRiskProvider(c)
	if riskProv == nil {
		c.JSON(http.StatusOK, gin.H{"symbols": []interface{}{}})
		return
	}

	symbols := riskProv.GetMonitorSymbols()
	var monitorData []SymbolMonitorData

	for _, symbol := range symbols {
		data := riskProv.GetSymbolData(symbol)
		if data == nil {
			continue
		}

		// 使用反射提取數據
		v := reflect.ValueOf(data)
		if v.Kind() == reflect.Ptr {
			v = v.Elem()
		}

		symbolData := SymbolMonitorData{
			Symbol: symbol,
		}

		// 提取字段
		if field := v.FieldByName("CurrentPrice"); field.IsValid() && field.CanFloat() {
			symbolData.CurrentPrice = field.Float()
		}
		if field := v.FieldByName("AveragePrice"); field.IsValid() && field.CanFloat() {
			symbolData.AveragePrice = field.Float()
		}
		if field := v.FieldByName("CurrentVolume"); field.IsValid() && field.CanFloat() {
			symbolData.CurrentVolume = field.Float()
		}
		if field := v.FieldByName("AverageVolume"); field.IsValid() && field.CanFloat() {
			symbolData.AverageVolume = field.Float()
		}
		if field := v.FieldByName("LastUpdate"); field.IsValid() {
			if t, ok := field.Interface().(time.Time); ok {
				symbolData.LastUpdate = t
			}
		}

		// 计算偏离度和比率
		if symbolData.AveragePrice > 0 {
			symbolData.PriceDeviation = (symbolData.CurrentPrice - symbolData.AveragePrice) / symbolData.AveragePrice * 100
		}
		if symbolData.AverageVolume > 0 {
			symbolData.VolumeRatio = symbolData.CurrentVolume / symbolData.AverageVolume
		}

		// 判断是否异常（简單判断）
		symbolData.IsAbnormal = math.Abs(symbolData.PriceDeviation) > 10 || symbolData.VolumeRatio > 3

		monitorData = append(monitorData, symbolData)
	}

	c.JSON(http.StatusOK, gin.H{"symbols": monitorData})
}

// RiskCheckHistoryResponse 风控检查历史响应
type RiskCheckHistoryResponse struct {
	CheckTime    time.Time             `json:"check_time"`
	Symbols      []RiskCheckSymbolInfo `json:"symbols"`
	HealthyCount int                   `json:"healthy_count"`
	TotalCount   int                   `json:"total_count"`
}

// RiskCheckSymbolInfo 风控检查币种信息
type RiskCheckSymbolInfo struct {
	Symbol         string  `json:"symbol"`
	IsHealthy      bool    `json:"is_healthy"`
	PriceDeviation float64 `json:"price_deviation"`
	VolumeRatio    float64 `json:"volume_ratio"`
	Reason         string  `json:"reason"`
}

// getRiskCheckHistory 獲取风控检查历史
// GET /api/risk/history
// 参數：
//   - start_time: 开始時间（可選，ISO 8601格式，默认最近90天）
//   - end_time: 結束時间（可選，ISO 8601格式，默认當前時间）
func getRiskCheckHistory(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"history": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"history": []interface{}{}})
		return
	}

	// 解析参數
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")
	limitStr := c.Query("limit")
	botID := c.Query("bot_id")

	var startTime, endTime time.Time
	var err error
	limit := 500 // 默认限制500条

	if startTimeStr == "" {
		// 默认最近7天（减少默认數據量）
		startTime = time.Now().AddDate(0, 0, -7)
	} else {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	}

	if endTimeStr == "" {
		endTime = time.Now()
	} else {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	}

	if limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
			// 最大限制為2000条
			if limit > 2000 {
				limit = 2000
			}
		}
	}

	// 查詢历史數據
	histories, err := storage.QueryRiskCheckHistory(startTime, endTime, limit, botID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 轉换為 API 响应格式
	result := make([]RiskCheckHistoryResponse, len(histories))
	for i, h := range histories {
		symbols := make([]RiskCheckSymbolInfo, len(h.Symbols))
		for j, s := range h.Symbols {
			symbols[j] = RiskCheckSymbolInfo{
				Symbol:         s.Symbol,
				IsHealthy:      s.IsHealthy,
				PriceDeviation: s.PriceDeviation,
				VolumeRatio:    s.VolumeRatio,
				Reason:         s.Reason,
			}
		}
		result[i] = RiskCheckHistoryResponse{
			CheckTime:    utils.ToUTC8(h.CheckTime),
			Symbols:      symbols,
			HealthyCount: h.HealthyCount,
			TotalCount:   h.TotalCount,
		}
	}

	c.JSON(http.StatusOK, gin.H{"history": result})
}

// KlineData K線數據响应格式
type KlineData struct {
	Time   int64   `json:"time"` // 時间戳（秒）
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

// publicKlineProviderAdapter 將 exchange.IExchange 適配為 ExchangeProvider，僅用於獲取公開 K 線（無需 bot 啟動）
type publicKlineProviderAdapter struct {
	ex exchange.IExchange
}

func (a *publicKlineProviderAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*exchange.Candle, error) {
	return a.ex.GetHistoricalKlines(ctx, symbol, interval, limit)
}

func (a *publicKlineProviderAdapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return 0, fmt.Errorf("not supported for public kline provider")
}

func (a *publicKlineProviderAdapter) GetPositions(ctx context.Context, symbol string) ([]*exchange.Position, error) {
	return nil, fmt.Errorf("not supported for public kline provider")
}

// getKlines 獲取K線數據
// GET /api/klines
// 查詢参數：
//   - exchange: 交易所（如 binance）
//   - symbol: 交易對（如 BTCUSDT）
//   - interval: K線週期（1m/5m/15m/30m/1h/4h/1d等，默认1m）
//   - limit: 返回K線數量（默认500，最大1000）
//
// 當無對應 bot 時，若傳入 exchange+symbol，會嘗試使用公開 API 拉取主流幣 K 線（如 Binance 無需 API 密鑰）
func getKlines(c *gin.Context) {
	// 獲取交易對與交易所（優先查詢參數，其次系統狀態）
	symbol := c.Query("symbol")
	exchangeName := c.Query("exchange")
	if symbol == "" || exchangeName == "" {
		if st := pickStatus(c); st != nil {
			if symbol == "" {
				symbol = st.Symbol
			}
			if exchangeName == "" {
				exchangeName = st.Exchange
			}
		}
	}
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "無法獲取交易币种，請提供 symbol 參數或先啟動對應 bot"})
		return
	}

	prov := pickExchangeProvider(c)
	// 無 bot 時，若提供了 exchange+symbol，嘗試使用公開 API 拉取 K 線（主流幣如 BTC 等無需認證）
	if prov == nil && exchangeName != "" {
		if pubEx, err := exchange.NewExchangeForPublicKlines(exchangeName, symbol); err == nil {
			prov = &publicKlineProviderAdapter{ex: pubEx}
			logger.Info("📊 [Klines] 使用公開 API 拉取 %s %s K 線（無 bot）", exchangeName, symbol)
		}
	}
	if prov == nil {
		c.JSON(http.StatusOK, gin.H{"klines": []interface{}{}, "symbol": symbol, "interval": c.DefaultQuery("interval", "1m")})
		return
	}

	// 解析查詢参數
	interval := c.DefaultQuery("interval", "1m")
	limitStr := c.DefaultQuery("limit", "500")

	limit := 500
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
		if limit > 1000 {
			limit = 1000
		}
	}

	// 呼叫交易所接口獲取K線數據
	ctx := c.Request.Context()
	candles, err := prov.GetHistoricalKlines(ctx, symbol, interval, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 統一裁剪插針（所有交易所）：將 High/Low 限制在鄰近價格 ±3% 內，避免壞 tick 導致圖表異常
	candles = exchange.ClipKlineSpikes(candles, 0.03)

	// 轉换為API响应格式
	klines := make([]KlineData, len(candles))
	for i, candle := range candles {
		// 將毫秒時间戳轉换為秒（lightweight-charts使用秒级時间戳）
		klines[i] = KlineData{
			Time:   candle.Timestamp / 1000,
			Open:   candle.Open,
			High:   candle.High,
			Low:    candle.Low,
			Close:  candle.Close,
			Volume: candle.Volume,
		}
	}

	c.JSON(http.StatusOK, gin.H{"klines": klines, "symbol": symbol, "interval": interval})
}

// SetConfigStorage 設置配置存儲
func SetConfigStorage(cs storage.ConfigStorage) {
	configStorage = cs
}

// SetConfigManager 設置配置管理器
func SetConfigManager(cm *cfgmgr.ConfigManager) {
	configManager = cm
}
