package web

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"reflect"
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
	Running       bool    `json:"running"`
	Exchange      string  `json:"exchange"`
	Symbol        string  `json:"symbol"`
	MarketType    string  `json:"market_type,omitempty"` // 市場類型：spot/futures
	CurrentPrice  float64 `json:"current_price"`
	TotalPnL      float64 `json:"total_pnl"`
	TotalTrades   int     `json:"total_trades"`
	RiskTriggered bool    `json:"risk_triggered"`
	Uptime        int64   `json:"uptime"`         // 运行時间（秒）
	OpeningPaused bool    `json:"opening_paused"` // 是否暫停開倉
	PauseReason   string  `json:"pause_reason"`   // 暫停原因：manual / schedule / periodic / position_limit
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
		if order.RealizedPnL != nil {
			resp["exchange_pnl"] = *order.RealizedPnL
		} else {
			resp["exchange_pnl"] = nil
		}
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
			"total_pnl":    0,
			"win_rate":     0,
		})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		logger.Warn("[统计] storage.GetStorage() 回傳 nil")
		c.JSON(http.StatusOK, gin.H{
			"total_trades": 0,
			"total_volume": 0,
			"total_pnl":    0,
			"win_rate":     0,
		})
		return
	}

	logger.Info("[统计] storage 獲取成功，准备查詢數據库")

	// 獲取當前账戶標识
	accountID := GetCurrentAccountID()
	logger.Info("[统计] accountID: %s", accountID)

	// 獲取 exchange、symbol、bot_id 参數（如果有）
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	botID := strings.TrimSpace(c.Query("bot_id"))

	// 從數據库獲取统计彙總
	var summary interface{}
	var err error
	if exchange != "" && symbol != "" {
		// 指定了交易所和交易對時，查詢該交易對的统计（概覽頁顯示當前交易對的總盈虧）
		summary, err = storage.GetStatisticsSummaryByExchangeAndSymbol(exchange, symbol, accountID, botID)
		logger.Info("[统计] 查詢交易所 %s 交易對 %s 的统计，accountID: %s bot_id: %q", exchange, symbol, accountID, botID)
	} else if exchange != "" {
		// 只指定了交易所，查詢該交易所的统计
		summary, err = storage.GetStatisticsSummaryByExchange(exchange, accountID)
		logger.Info("[统计] 查詢交易所 %s 的统计，accountID: %s", exchange, accountID)
	} else {
		// 否则查詢所有交易所的统计
		summary, err = storage.GetStatisticsSummary(accountID)
		logger.Info("[统计] 查詢所有交易所的统计，accountID: %s", accountID)
	}

	if err != nil {
		logger.Error("[统计] 查詢失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 使用反射獲取字段值（避免類型断言问题）
	statValue := reflect.ValueOf(summary)
	if statValue.Kind() != reflect.Ptr || statValue.Elem().Kind() != reflect.Struct {
		logger.Error("[统计] 统计數據格式錯误")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "统计數據格式錯误"})
		return
	}

	elem := statValue.Elem()
	totalTrades := int(elem.FieldByName("TotalTrades").Int())
	totalPnL := elem.FieldByName("TotalPnL").Float()
	totalVolume := elem.FieldByName("TotalVolume").Float()
	winRate := elem.FieldByName("WinRate").Float()
	grossPnL := elem.FieldByName("GrossPnL").Float()
	totalFee := elem.FieldByName("TotalFee").Float()
	totalBuyDeviation := elem.FieldByName("TotalBuyDeviation").Float()
	totalSellDeviation := elem.FieldByName("TotalSellDeviation").Float()

	logger.Info("[统计] 查詢結果: TotalTrades=%d, TotalPnL=%.2f, GrossPnL=%.2f, TotalFee=%.2f, TotalVolume=%.2f, BuyDeviation=%.2f, SellDeviation=%.2f", totalTrades, totalPnL, grossPnL, totalFee, totalVolume, totalBuyDeviation, totalSellDeviation)

	// 如果數據库没有數據，尝試從 SuperPositionManager 计算
	pmProvider := PickPositionProvider(c)
	if totalTrades == 0 && pmProvider != nil {
		slots := pmProvider.GetAllSlots()
		totalBuyQty := 0.0
		totalSellQty := 0.0

		for _, slot := range slots {
			if slot.OrderSide == "BUY" && slot.OrderStatus == "FILLED" {
				totalBuyQty += slot.OrderFilledQty
			} else if slot.OrderSide == "SELL" && slot.OrderStatus == "FILLED" {
				totalSellQty += slot.OrderFilledQty
			}
		}

		// 估算交易數（買賣配對）
		estimatedTrades := int((totalBuyQty + totalSellQty) / 2)
		if estimatedTrades > 0 {
			totalTrades = estimatedTrades
			totalVolume = totalBuyQty + totalSellQty
		}
	}

	// 🔥 查詢交易所已實現盈虧（從 orders 表的 realized_pnl 聚合）
	exchangePnL := 0.0
	type exchangePnLGetter interface {
		GetExchangePnLTotal(exchange, symbol, botID string) (float64, error)
	}
	if epGetter, ok := storage.(exchangePnLGetter); ok {
		if ep, err := epGetter.GetExchangePnLTotal(exchange, symbol, botID); err == nil {
			exchangePnL = ep
		}
	}

	// 🔥 待實現盈虧：根據當前持倉和當前價格計算（僅當有持倉且能獲取價格時）
	unrealizedPnL := 0.0
	if exchange != "" && symbol != "" {
		pmProvider := PickPositionProvider(c)
		priceProv := PickPriceProvider(c)
		if pmProvider != nil && priceProv != nil {
			slots := pmProvider.GetAllSlots()
			wsPrice := priceProv.GetLastPrice()
			slotTotalQty := 0.0
			slotTotalCost := 0.0
			for _, slot := range slots {
				if slot.PositionStatus == "FILLED" && slot.PositionQty > 0.000001 && slot.Price > 0.000001 {
					slotTotalQty += slot.PositionQty
					slotTotalCost += slot.Price * slot.PositionQty
				}
			}
			if wsPrice > 0 && slotTotalQty > 0 && slotTotalCost > 0 {
				slotAvgPrice := slotTotalCost / slotTotalQty
				unrealizedPnL = (wsPrice - slotAvgPrice) * slotTotalQty
			}
		}
	}

	// 🔥 當日統計數據
	todayTrades := 0
	todayPnL := 0.0
	todayExchangePnL := 0.0
	if exchange != "" && symbol != "" && storage != nil {
		// 使用反射調用 GetTodayStatisticsByExchangeAndSymbol 方法
		// 這樣可以避免直接引用 storage.TodayStatistics 類型
		method := reflect.ValueOf(storage).MethodByName("GetTodayStatisticsByExchangeAndSymbol")
		if method.IsValid() {
			results := method.Call([]reflect.Value{
				reflect.ValueOf(exchange),
				reflect.ValueOf(symbol),
				reflect.ValueOf(accountID),
				reflect.ValueOf(botID),
			})
			if len(results) == 2 {
				if !results[0].IsNil() {
					todayStats := results[0].Interface()
					// 通過反射獲取字段值
					statsValue := reflect.ValueOf(todayStats).Elem()
					todayTrades = int(statsValue.FieldByName("TotalTrades").Int())
					todayPnL = statsValue.FieldByName("GridPnL").Float()
					todayExchangePnL = statsValue.FieldByName("ExchangePnL").Float()
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"total_trades":         totalTrades,
		"total_volume":         totalVolume,
		"total_pnl":            totalPnL,
		"gross_pnl":            grossPnL,
		"total_fee":            totalFee,
		"win_rate":             winRate,
		"total_buy_deviation":  totalBuyDeviation,  // 🔥 買入價格偏差總和
		"total_sell_deviation": totalSellDeviation, // 🔥 賣出價格偏差總和
		"exchange_pnl":         exchangePnL,        // 🔥 交易所已實現盈虧合計
		"unrealized_pnl":       unrealizedPnL,      // 🔥 待實現盈虧（當前持倉×當前價格）
		"today_trades":         todayTrades,        // 🔥 當日成交筆數
		"today_pnl":            todayPnL,           // 🔥 當日網格盈虧
		"today_exchange_pnl":   todayExchangePnL,   // 🔥 當日交易所盈虧
	})
}

// lastAccountEquityPerDayFromHourly 從 hourly_equity_records 聚合每個日曆日「時间戳最晚的一條非空 account_equity」，供日統計在 daily_snapshots 未帶權益時回填淨值曲線。
func lastAccountEquityPerDayFromHourly(st storage.Storage, exchange, symbol, account string, rangeStart, rangeEnd time.Time) map[string]float64 {
	out := make(map[string]float64)
	lastTs := make(map[string]time.Time)
	if st == nil || exchange == "" || symbol == "" {
		return out
	}
	loc := utils.GlobalLocation
	if loc == nil {
		loc = time.Local
	}
	recs, err := st.QueryHourlyEquityRecords(exchange, symbol, account, rangeStart, rangeEnd)
	if err != nil {
		return out
	}
	for _, rec := range recs {
		if rec == nil || rec.AccountEquity == nil {
			continue
		}
		dayKey := rec.Timestamp.In(loc).Format("2006-01-02")
		if prev, ok := lastTs[dayKey]; !ok || rec.Timestamp.After(prev) {
			lastTs[dayKey] = rec.Timestamp
			out[dayKey] = *rec.AccountEquity
		}
	}
	return out
}

// getDailyStatistics 獲取每日统计（混合模式：优先使用 statistics 表，缺失的日期從 trades 表补充）
// GET /api/statistics/daily
func getDailyStatistics(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"statistics": []interface{}{}, "max_drawdown": 0, "max_drawdown_pct": 0})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusOK, gin.H{"statistics": []interface{}{}, "max_drawdown": 0, "max_drawdown_pct": 0})
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

	// 1. 先從 statistics 表查詢（按 bot 篩選時不使用全局 statistics 表，避免與單 Bot 混賬）
	var statsFromTable []*storage.Statistics
	var err error
	if botID == "" {
		statsFromTable, err = st.QueryStatistics(startDate, endDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	// 2. 構建日期映射（statistics 表中已有的日期）
	statsMap := make(map[string]*storage.Statistics)
	for _, stat := range statsFromTable {
		dateKey := stat.Date.Format("2006-01-02")
		statsMap[dateKey] = stat
	}

	// 3. 從 trades 表查詢所有日期（包含缺失的日期和盈利/亏损交易數）
	tradesStatsMap := make(map[string]*storage.DailyStatisticsWithTradeCount)
	accountID := GetCurrentAccountID()
	tradesStats, err2 := st.QueryDailyStatisticsByExchange(exchForTrades, symForTrades, accountID, startDate, endDate, botID)
	if err2 == nil {
		for _, tradeStat := range tradesStats {
			dateKey := tradeStat.Date.Format("2006-01-02")
			tradesStatsMap[dateKey] = tradeStat
		}
	}

	// 3b. 從每日快照表查詢未實現盈虧與日內最大回撤
	snapshotMap := make(map[string]*storage.DailySnapshot)
	if status != nil && status.Exchange != "" && status.Symbol != "" {
		snapshots, errSnap := st.QueryDailySnapshots(status.Exchange, status.Symbol, accountID, startDate, endDate)
		if errSnap == nil {
			for _, snap := range snapshots {
				dateKey := snap.Date.Format("2006-01-02")
				snapshotMap[dateKey] = snap
			}
		}
	}

	// 3c. 小時權益：按日最后一條 account_equity（日快照未寫入權益時仍可畫淨值曲線）
	var hourlyAcctByDay map[string]float64
	if status != nil && status.Exchange != "" && status.Symbol != "" {
		loc := utils.GlobalLocation
		if loc == nil {
			loc = time.Local
		}
		startDay, _ := time.ParseInLocation("2006-01-02", startDate.Format("2006-01-02"), loc)
		endDay, _ := time.ParseInLocation("2006-01-02", endDate.Format("2006-01-02"), loc)
		rangeEnd := endDay.Add(24*time.Hour - time.Nanosecond)
		hourlyAcctByDay = lastAccountEquityPerDayFromHourly(st, status.Exchange, status.Symbol, accountID, startDay, rangeEnd)
	}

	// 4. 獲取日K線數據用於计算开盘/收盘價和涨跌幅
	klineMap := make(map[string]*exchange.Candle)
	exchProv := pickExchangeProvider(c)
	if exchProv != nil && status != nil && status.Symbol != "" {
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

	// 4b. 獲取每日資金費用（賬戶級，無法按 bot 拆分；帶 bot_id 時不展示以免誤導）
	fundingMap := make(map[string]float64)
	type dailyFundingGetter interface {
		GetDailyFundingPayments(account, exchange string, startTime, endTime time.Time) (map[string]float64, error)
	}
	if botID == "" {
		if stWithFunding, ok := st.(dailyFundingGetter); ok {
			exchangeID := ""
			if status != nil {
				exchangeID = status.Exchange
			}
			dailyFunding, err := stWithFunding.GetDailyFundingPayments(accountID, exchangeID, startDate, endDate)
			if err == nil {
				fundingMap = dailyFunding
			}
		}
	}

	// 4c. 獲取每日交易所已實現盈虧（從 orders 表聚合 realized_pnl）
	exchangePnLMap := make(map[string]float64)
	type dailyExchangePnLGetter interface {
		GetDailyExchangePnL(exchange, symbol string, startDate, endDate time.Time, botID string) (map[string]float64, error)
	}
	if epGetter, ok := st.(dailyExchangePnLGetter); ok {
		exchangeID := exchForTrades
		symbolID := symForTrades
		if dailyEP, err := epGetter.GetDailyExchangePnL(exchangeID, symbolID, startDate, endDate, botID); err == nil {
			exchangePnLMap = dailyEP
		}
	}

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

	// 用於计算最大回撤的累计盈亏數據
	var cumulativePnLList []float64
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
			item["total_pnl"] = 0
			item["win_rate"] = 0
			dailyPnL = 0
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
		cumulativePnL += dailyPnL
		cumulativePnLList = append(cumulativePnLList, cumulativePnL)
		item["cumulative_pnl"] = cumulativePnL

		// 合併每日快照：未實現盈虧、日內最大回撤、交易所帳戶權益（真實淨值）
		if snap, ok := snapshotMap[dateKey]; ok {
			item["unrealized_pnl"] = snap.UnrealizedPnL
			item["intraday_max_drawdown"] = snap.IntradayMaxDrawdown
			item["intraday_max_drawdown_pct"] = snap.IntradayMaxDrawdownPct
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
		} else {
			item["funding_fee"] = 0.0
		}

		// 合併每日交易所已實現盈虧
		if epnl, ok := exchangePnLMap[dateKey]; ok {
			item["exchange_pnl"] = epnl
		} else {
			item["exchange_pnl"] = 0.0
		}

		// 賬面盈虧 = 已平倉盈虧 + 未實現盈虧（真正帳面值）
		unrealized := 0.0
		if snap, ok := snapshotMap[dateKey]; ok {
			unrealized = snap.UnrealizedPnL
		}
		item["book_value_pnl"] = dailyPnL + unrealized

		tempResult = append(tempResult, item)
	}

	// 反轉結果，使其按日期倒序
	for i := len(tempResult) - 1; i >= 0; i-- {
		result = append(result, tempResult[i])
	}

	// 6. 计算最大回撤
	// 注意：这里使用净值（equity）= 虚拟初始本金 + 累计盈亏 来计算回撤
	// 这样可以保证回撤百分比不會超過100%
	// 我们使用累计盈亏的最小值来估算需要的初始本金
	maxDrawdown := 0.0
	maxDrawdownPct := 0.0
	if len(cumulativePnLList) > 0 {
		// 找出累计盈亏的最小值，用於确定虚拟初始本金
		minPnL := cumulativePnLList[0]
		for _, pnl := range cumulativePnLList {
			if pnl < minPnL {
				minPnL = pnl
			}
		}

		// 虚拟初始本金：确保净值始终為正
		// 如果最小累计盈亏是负數，初始本金需要大於其绝對值
		// 使用 |minPnL| * 2 作為初始本金，确保即使在最低点也有正的净值
		initialCapital := 1000.0 // 默认初始本金
		if minPnL < 0 {
			initialCapital = -minPnL * 2 // 确保最低点時净值仍為正
			if initialCapital < 1000 {
				initialCapital = 1000
			}
		}

		// 使用净值计算最大回撤
		peak := initialCapital + cumulativePnLList[0]
		for _, pnl := range cumulativePnLList {
			equity := initialCapital + pnl
			if equity > peak {
				peak = equity
			}
			if peak > 0 {
				drawdown := peak - equity
				drawdownPct := (drawdown / peak) * 100
				if drawdown > maxDrawdown {
					maxDrawdown = drawdown
				}
				if drawdownPct > maxDrawdownPct {
					maxDrawdownPct = drawdownPct
				}
			}
		}

		// 安全检查：回撤百分比不应超過100%
		if maxDrawdownPct > 100 {
			maxDrawdownPct = 100
		}
	}

	resp := gin.H{
		"statistics":       result,
		"max_drawdown":     maxDrawdown,
		"max_drawdown_pct": maxDrawdownPct,
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
	Exchange       string    `json:"exchange"`
	Symbol         string    `json:"symbol"`
	Price          float64   `json:"price"`
	PositionStatus string    `json:"position_status"` // EMPTY/FILLED
	PositionQty    float64   `json:"position_qty"`
	OrderID        int64     `json:"order_id"`
	ClientOID      string    `json:"client_order_id"`
	OrderSide      string    `json:"order_side"`   // BUY/SELL
	OrderStatus    string    `json:"order_status"` // NOT_PLACED/PLACED/CONFIRMED/PARTIALLY_FILLED/FILLED/CANCELED
	OrderPrice     float64   `json:"order_price"`
	OrderFilledQty float64   `json:"order_filled_qty"`
	OrderCreatedAt time.Time `json:"order_created_at"`
	SlotStatus     string    `json:"slot_status"`   // FREE/PENDING/LOCKED
	StrategyName   string    `json:"strategy_name"` // 策略名称
	StrategyType   string    `json:"strategy_type"` // 策略類型
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
			Exchange:       exchange,
			Symbol:         symbol,
			Price:          ds.Price,
			PositionStatus: ds.PositionStatus,
			PositionQty:    ds.PositionQty,
			OrderID:        ds.OrderID,
			ClientOID:      ds.ClientOID,
			OrderSide:      ds.OrderSide,
			OrderStatus:    ds.OrderStatus,
			OrderPrice:     ds.OrderPrice,
			OrderFilledQty: ds.OrderFilledQty,
			OrderCreatedAt: utils.ToUTC8(ds.OrderCreatedAt),
			SlotStatus:     ds.SlotStatus,
			StrategyName:   ds.StrategyName,
			StrategyType:   ds.StrategyType,
		}
	}
	return slots
}

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

// StrategyProvider 策略资金分配提供者接口
type StrategyProvider interface {
	GetCapitalAllocation() map[string]StrategyCapitalInfo
	ReleaseLockedCapital(strategyName string) float64
	ReleaseAllLockedCapital() map[string]float64
}

// StrategyCapitalInfo 策略资金信息
type StrategyCapitalInfo struct {
	Allocated float64 `json:"allocated"`  // 分配的资金
	Used      float64 `json:"used"`       // 已使用的资金（保证金）
	Available float64 `json:"available"`  // 可用资金
	Weight    float64 `json:"weight"`     // 权重
	FixedPool float64 `json:"fixed_pool"` // 固定资金池（如果指定）
}

// SetStrategyProvider 設置策略數據提供者
func SetStrategyProvider(provider StrategyProvider) {
	strategyProvider = provider
}

// strategyProviderAdapter 策略提供者适配器
type strategyProviderAdapter struct {
	getAllocationFunc     func() map[string]StrategyCapitalInfo
	releaseCapitalFunc    func(strategyName string) float64
	releaseAllCapitalFunc func() map[string]float64
}

// NewStrategyProviderAdapter 創建策略提供者适配器
func NewStrategyProviderAdapter(
	getAllocationFunc func() map[string]StrategyCapitalInfo,
	releaseCapitalFunc func(strategyName string) float64,
	releaseAllCapitalFunc func() map[string]float64,
) StrategyProvider {
	return &strategyProviderAdapter{
		getAllocationFunc:     getAllocationFunc,
		releaseCapitalFunc:    releaseCapitalFunc,
		releaseAllCapitalFunc: releaseAllCapitalFunc,
	}
}

// GetCapitalAllocation 獲取策略资金分配信息
func (a *strategyProviderAdapter) GetCapitalAllocation() map[string]StrategyCapitalInfo {
	return a.getAllocationFunc()
}

// ReleaseLockedCapital 释放指定策略的锁定资金
func (a *strategyProviderAdapter) ReleaseLockedCapital(strategyName string) float64 {
	if a.releaseCapitalFunc != nil {
		return a.releaseCapitalFunc(strategyName)
	}
	return 0
}

// ReleaseAllLockedCapital 释放所有策略的锁定资金
func (a *strategyProviderAdapter) ReleaseAllLockedCapital() map[string]float64 {
	if a.releaseAllCapitalFunc != nil {
		return a.releaseAllCapitalFunc()
	}
	return map[string]float64{}
}

// getStrategyAllocation 獲取策略资金分配信息
// GET /api/strategies/allocation
func getStrategyAllocation(c *gin.Context) {
	if strategyProvider == nil {
		c.JSON(http.StatusOK, gin.H{"allocation": map[string]interface{}{}})
		return
	}

	allocation := strategyProvider.GetCapitalAllocation()
	c.JSON(http.StatusOK, gin.H{"allocation": allocation})
}

// releaseStrategyCapital 释放策略的锁定资金
// POST /api/strategies/:id/release-capital
func releaseStrategyCapital(c *gin.Context) {
	strategyName := c.Param("id")
	if strategyName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少策略名称"})
		return
	}

	if strategyProvider == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "策略服務未初始化"})
		return
	}

	released := strategyProvider.ReleaseLockedCapital(strategyName)
	logger.Info("💰 [手动释放资金] 策略 %s 已释放锁定资金: %.2f USDT", strategyName, released)

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"message":  fmt.Sprintf("已释放 %.2f USDT 锁定资金", released),
		"released": released,
		"strategy": strategyName,
	})
}

// releaseAllStrategiesCapital 释放所有策略的锁定资金
// POST /api/strategies/release-all-capital
func releaseAllStrategiesCapital(c *gin.Context) {
	if strategyProvider == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "策略服務未初始化"})
		return
	}

	released := strategyProvider.ReleaseAllLockedCapital()

	totalReleased := 0.0
	for name, amount := range released {
		totalReleased += amount
		logger.Info("💰 [手动释放资金] 策略 %s 已释放锁定资金: %.2f USDT", name, amount)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":        true,
		"message":        fmt.Sprintf("已释放所有策略的锁定资金，總計 %.2f USDT", totalReleased),
		"released":       released,
		"total_released": totalReleased,
	})
}

// ========== 待成交訂單相關API ==========

// getPendingOrders 獲取待成交订單列表
// GET /api/orders/pending
func getPendingOrders(c *gin.Context) {
	pmProvider := PickPositionProvider(c)
	if pmProvider == nil {
		c.JSON(http.StatusOK, gin.H{"orders": []interface{}{}, "leverage": 1})
		return
	}

	slots := pmProvider.GetAllSlots()
	var pendingOrders []PendingOrderInfo

	for _, slot := range slots {
		// 筛选状態為 PLACED/CONFIRMED/PARTIALLY_FILLED 的订單
		if slot.OrderStatus == "PLACED" || slot.OrderStatus == "CONFIRMED" || slot.OrderStatus == "PARTIALLY_FILLED" {
			// 计算订單原始數量：使用配置的订單金額 / 订單價格
			var quantity float64
			if slot.OrderPrice > 0 && orderQuantityConfig > 0 {
				quantity = orderQuantityConfig / slot.OrderPrice
			} else if slot.OrderFilledQty > 0 {
				// 如果無法计算，使用已成交數量作為估算
				quantity = slot.OrderFilledQty
			}

			pendingOrders = append(pendingOrders, PendingOrderInfo{
				OrderID:        slot.OrderID,
				ClientOrderID:  slot.ClientOID,
				Exchange:       slot.Exchange,
				Symbol:         slot.Symbol,
				Price:          slot.OrderPrice,
				Quantity:       quantity,
				Side:           slot.OrderSide,
				Status:         slot.OrderStatus,
				FilledQuantity: slot.OrderFilledQty,
				CreatedAt:      utils.ToUTC8(slot.OrderCreatedAt),
				SlotPrice:      slot.Price,
				StrategyName:   slot.StrategyName,
				StrategyType:   slot.StrategyType,
			})
		}
	}

	// 獲取槓桿倍數（用於計算資金占用）
	leverage := 1
	if pmProvider != nil {
		if l := pmProvider.GetLeverage(); l > 0 {
			leverage = l
		}
	}

	c.JSON(http.StatusOK, gin.H{"orders": pendingOrders, "count": len(pendingOrders), "leverage": leverage})
}

// PendingOrderInfo 待成交订單信息
type PendingOrderInfo struct {
	OrderID        int64     `json:"order_id"`
	ClientOrderID  string    `json:"client_order_id"`
	Exchange       string    `json:"exchange"` // 交易所
	Symbol         string    `json:"symbol"`   // 交易对
	Price          float64   `json:"price"`
	Quantity       float64   `json:"quantity"`
	Side           string    `json:"side"` // BUY/SELL
	Status         string    `json:"status"`
	FilledQuantity float64   `json:"filled_quantity"`
	CreatedAt      time.Time `json:"created_at"`
	SlotPrice      float64   `json:"slot_price"`    // 槽位價格
	StrategyName   string    `json:"strategy_name"` // 策略名称
	StrategyType   string    `json:"strategy_type"` // 策略類型
}

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
