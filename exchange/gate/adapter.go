package gate

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"
)

type OrderUpdateCallback func(update OrderUpdate)

// GateAdapter Gate.io 交易所适配器
type GateAdapter struct {
	client         *Client
	wsManager      *WebSocketManager
	klineWSManager *KlineWebSocketManager
	symbol         string // 交易對（如 BTCUSDT）
	gateSymbol     string // Gate格式（如 BTC_USDT）
	settle         string // 結算币种：usdt 或 btc
	useWebSocket   bool   // 是否使用 WebSocket 下單

	// 订單ID到價格的映射注册回呼
	orderMappingCallback func(orderID int64, price float64)

	posMode          string  // 持倉模式：dual_long_short 或 single
	quantoMultiplier float64 // 合約乘數
	orderPriceRound  int     // 價格精度
	orderSizeMin     float64 // 最小下單數量
	volumePlace      int     // 數量小數位
	pricePlace       int     // 價格小數位

	priceCacheMu   sync.RWMutex
	priceCache     float64
	priceCacheTime time.Time

	testnet bool // 是否使用測試網
}

// NewGateAdapter 創建 Gate.io 适配器
func NewGateAdapter(cfg map[string]string, symbol string) (*GateAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]
	settle := cfg["settle"]                                      // usdt 或 btc，預設 usdt
	testnet := cfg["testnet"] == "true" || cfg["testnet"] == "1" // 是否使用測試網

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("Gate.io API 配置不完整")
	}

	if settle == "" {
		settle = "usdt" // 預設 USDT 永续合約
	}

	// 轉换交易對格式
	gateSymbol := convertToGateSymbol(symbol)

	client := NewClient(apiKey, secretKey, testnet)
	wsManager := NewWebSocketManager(apiKey, secretKey, settle, testnet)

	if testnet {
		logger.Info("🌐 [Gate] 使用測試網模式")
	}

	adapter := &GateAdapter{
		client:       client,
		wsManager:    wsManager,
		symbol:       symbol,
		gateSymbol:   gateSymbol,
		settle:       settle,
		useWebSocket: false, // 默认使用 REST API 下單
	}
	// 保存 testnet 状態，用於后续創建 klineWSManager
	adapter.testnet = testnet

	// 初始化獲取合約信息和持倉模式
	ctxInit, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. 獲取合約信息
	if err := adapter.fetchContractInfo(ctxInit); err != nil {
		logger.Warn("⚠️ [Gate] 獲取合約信息失败: %v", err)
		// 使用默认值
		adapter.volumePlace = 0
		adapter.pricePlace = 2
		adapter.orderSizeMin = 1
	}

	// 2. 獲取帳戶信息（判断持倉模式）
	acc, err := adapter.GetAccount(ctxInit)
	if err != nil {
		logger.Warn("⚠️ [Gate] 初始化獲取帳戶信息失败: %v", err)
		adapter.posMode = "dual_long_short" // 默认双向持倉
	} else {
		if acc.PosMode == "dual_long_short" {
			adapter.posMode = "dual_long_short"
		} else {
			adapter.posMode = "single"
		}

		posModeDesc := "双向持倉"
		if adapter.posMode == "single" {
			posModeDesc = "單向持倉"
		}
		logger.Info("ℹ️ [Gate] 持倉模式: %s (%s)", posModeDesc, adapter.posMode)
	}

	// 3. 如果配置了杠杆，自动設置杠杆
	if leverageStr := cfg["leverage"]; leverageStr != "" {
		leverage, err := strconv.Atoi(leverageStr)
		if err == nil && leverage > 0 {
			logger.Info("ℹ️ [Gate] 检测到杠杆配置: %dx，正在設置...", leverage)
			if err := adapter.SetLeverage(ctxInit, leverage); err != nil {
				logger.Warn("⚠️ [Gate] 設置杠杆失败: %v", err)
				// 不返回錯误，允許继续运行（杠杆可能已在网站設置）
			}
		}
	}

	return adapter, nil
}

// GetName 獲取交易所名称
func (g *GateAdapter) GetName() string {
	return "Gate.io"
}

// GetMarketType 獲取市場類型：futures 合約
func (g *GateAdapter) GetMarketType() string {
	return "futures"
}

// GetPriceDecimals 獲取價格精度
func (g *GateAdapter) GetPriceDecimals() int {
	return g.pricePlace
}

// GetQuantityDecimals 獲取數量精度
func (g *GateAdapter) GetQuantityDecimals() int {
	return g.volumePlace
}

// fetchContractInfo 獲取合約信息
func (g *GateAdapter) fetchContractInfo(ctx context.Context) error {
	contract, err := g.client.GetContract(ctx, g.settle, g.gateSymbol)
	if err != nil {
		return fmt.Errorf("獲取合約信息失败: %w", err)
	}

	// 解析合約乘數
	if contract.QuantoMultiplier != "" {
		g.quantoMultiplier, _ = strconv.ParseFloat(contract.QuantoMultiplier, 64)
	}

	// 解析價格精度（如 "0.1" -> 1位小數）
	if contract.OrderPriceRound != "" {
		priceRound, _ := strconv.ParseFloat(contract.OrderPriceRound, 64)
		g.pricePlace = calculateDecimalPlaces(priceRound)
	}

	// 解析數量精度
	// Gate.io 的 order_size_round 字段可能為空,需要推断精度
	if contract.OrderSizeRound != "" {
		sizeRound, _ := strconv.ParseFloat(contract.OrderSizeRound, 64)
		g.volumePlace = calculateDecimalPlaces(sizeRound)
	} else {
		// 如果 order_size_round 為空,根據 order_size_min 推断
		// 對於 USDT 永续合約,通常支援小數下單
		// ETH_USDT 等主流币种一般支援 0.01 精度(2位小數)
		minSize := contract.OrderSizeMin
		if minSize >= 1 {
			// 最小量 >= 1,通常是整數合約(如 BTC)
			// 但也可能支援小數,使用 0.01 精度较安全
			g.volumePlace = 2 // 默认2位小數
		} else {
			// 最小量 < 1,根據最小量计算精度
			g.volumePlace = calculateDecimalPlaces(minSize)
		}
	}

	// 最小下單數量
	g.orderSizeMin = contract.OrderSizeMin

	// 计算實際最小下單量(张數 × 乘數 = 實際币數量)
	actualMinSize := g.orderSizeMin * g.quantoMultiplier
	if actualMinSize == 0 {
		actualMinSize = g.orderSizeMin // 如果乘數為0,直接用张數
	}

	logger.Info("ℹ️ [Gate 合約信息] %s, 每张合約:%.2f, 價格精度:%d, 數量精度:%d, 最小下單量:%.2f (%.0f张)",
		g.gateSymbol, g.quantoMultiplier, g.pricePlace, g.volumePlace, actualMinSize, g.orderSizeMin)

	return nil
}

// PlaceOrder 下單
func (g *GateAdapter) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	// 使用 REST API 下單（更可靠）
	return g.placeOrderViaREST(ctx, req)
}

// placeOrderViaREST 通過 REST API 下單
func (g *GateAdapter) placeOrderViaREST(ctx context.Context, req *OrderRequest) (*Order, error) {
	// Gate.io 的 size 是张數,需要從實際币數量换算
	// 如果合約乘數為 0,则直接使用數量
	var contractSize int64
	if g.quantoMultiplier > 0 {
		// 计算张數 = 實際數量 / 每张合約數量
		contracts := req.Quantity / g.quantoMultiplier
		contractSize = int64(contracts)
		// 如果小於1张,至少下1张
		if contractSize == 0 && req.Quantity > 0 {
			contractSize = 1
		}
	} else {
		// 直接使用數量(整數)
		contractSize = int64(req.Quantity)
	}

	// 轉换方向和數量: Gate.io 使用正负數表示方向
	// BUY(買入) = 正數, SELL(賣出) = 负數
	// reduce_only参數會告诉交易所这是平倉單,不需要反轉符号
	var size int64
	if req.Side == SideBuy {
		size = contractSize
	} else {
		size = -contractSize
	}

	// 格式化價格
	priceStr := fmt.Sprintf("%.*f", g.pricePlace, req.Price)

	// Gate.io 要求 text 字段必須以 "t-" 开头,且长度不超過30個字符
	// 使用统一的 utils 包添加返佣前缀（會自动处理长度限制）
	clientOrderID := req.ClientOrderID
	if clientOrderID != "" {
		clientOrderID = utils.AddBrokerPrefix("gate", clientOrderID)
	}

	// 構造订單参數
	order := map[string]interface{}{
		"contract": g.gateSymbol,
		"size":     size,
		"price":    priceStr,
		"tif":      "gtc", // Good Till Cancel
		"text":     clientOrderID,
	}

	// 只减倉標記 (Gate.io 使用 reduce_only,不需要 close 標記)
	if req.ReduceOnly {
		order["reduce_only"] = true
	}

	// 只做 Maker
	if req.PostOnly {
		order["tif"] = "poc" // Post Only
	}

	// 发送下單请求
	futuresOrder, err := g.client.PlaceOrder(ctx, g.settle, order)
	if err != nil {
		// 检查是否保证金不足
		if strings.Contains(err.Error(), "insufficient") || strings.Contains(err.Error(), "balance") {
			return nil, fmt.Errorf("保证金不足: %w", err)
		}
		return nil, err
	}

	// 轉换為標准订單格式
	result := &Order{
		OrderID:       futuresOrder.ID,
		ClientOrderID: futuresOrder.Text,
		Symbol:        g.symbol,
		Side:          convertSide(float64(futuresOrder.Size)),
		Type:          OrderTypeLimit,
		Price:         req.Price,
		Quantity:      abs(float64(futuresOrder.Size)),
		ExecutedQty:   abs(float64(futuresOrder.FillSize)),
		Status:        convertStatus(futuresOrder.Status),
		CreatedAt:     time.Unix(int64(futuresOrder.CreateTime), 0),
		UpdateTime:    int64(futuresOrder.FinishTime * 1000),
	}

	// 解析成交均價
	if futuresOrder.FillPrice != "" {
		result.AvgPrice, _ = strconv.ParseFloat(futuresOrder.FillPrice, 64)
	}

	return result, nil
}

// BatchPlaceOrders 批量下單
func (g *GateAdapter) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	placedOrders := make([]*Order, 0, len(orders))
	hasMarginError := false

	for _, orderReq := range orders {
		order, err := g.PlaceOrder(ctx, orderReq)
		if err != nil {
			logger.Warn("⚠️ [Gate] 下單失败 %.2f %s: %v",
				orderReq.Price, orderReq.Side, err)

			if strings.Contains(err.Error(), "保证金不足") {
				hasMarginError = true
			}
			continue
		}

		// 确保包含请求的價格
		order.Price = orderReq.Price

		// 注册订單ID到價格的映射
		if g.orderMappingCallback != nil && order.OrderID > 0 {
			g.orderMappingCallback(order.OrderID, orderReq.Price)
			logger.Debug("🔍 [Gate映射] 注册 订單ID=%d -> 價格=%.2f", order.OrderID, orderReq.Price)
		}

		placedOrders = append(placedOrders, order)
	}

	return placedOrders, hasMarginError
}

// CancelOrder 取消訂單
func (g *GateAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	orderIDStr := strconv.FormatInt(orderID, 10)
	_, err := g.client.CancelOrder(ctx, g.settle, orderIDStr)
	if err != nil {
		// 订單不存在不算錯误
		if strings.Contains(err.Error(), "ORDER_NOT_FOUND") || strings.Contains(err.Error(), "not found") {
			logger.Info("ℹ️ [Gate] 订單 %d 已不存在，跳過取消", orderID)
			return nil
		}
		return fmt.Errorf("取消訂單失败: %w", err)
	}

	logger.Info("✅ [Gate] 取消訂單成功: %d", orderID)
	return nil
}

// BatchCancelOrders 批量取消訂單
func (g *GateAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	if len(orderIDs) == 0 {
		return nil
	}

	// Gate.io 批量撤單API一次最多20個
	for i := 0; i < len(orderIDs); i += 20 {
		end := i + 20
		if end > len(orderIDs) {
			end = len(orderIDs)
		}

		batch := orderIDs[i:end]
		orderIDStrs := make([]string, len(batch))
		for j, id := range batch {
			orderIDStrs[j] = strconv.FormatInt(id, 10)
		}

		results, err := g.client.BatchCancelOrders(ctx, g.settle, orderIDStrs)
		if err != nil {
			logger.Warn("⚠️ [Gate] 批量撤單请求失败: %v", err)
			continue
		}

		// 处理結果並统计
		successCount := 0
		notFoundCount := 0
		failCount := 0

		for _, result := range results {
			orderID, _ := result["id"].(string)
			succeeded, _ := result["succeeded"].(bool)
			message, _ := result["message"].(string)

			if succeeded {
				successCount++
				logger.Info("✅ [Gate] 取消訂單成功: %s", orderID)
			} else if strings.Contains(message, "not found") || strings.Contains(message, "ORDER_NOT_FOUND") {
				notFoundCount++
				logger.Debug("ℹ️ [Gate] 订單 %s 已不存在(可能已成交/已撤销)", orderID)
			} else {
				failCount++
				logger.Warn("⚠️ [Gate] 取消訂單失败 %s: %s", orderID, message)
			}
		}

		// 批次彙總
		if len(batch) > 0 {
			logger.Info("📊 [Gate] 批次撤單: 成功%d個, 已不存在%d個, 失败%d個", successCount, notFoundCount, failCount)
		}

		// 批次间延迟
		if end < len(orderIDs) {
			time.Sleep(100 * time.Millisecond)
		}
	}

	return nil
}

// GetOrder 查詢訂單
func (g *GateAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	orderIDStr := strconv.FormatInt(orderID, 10)
	futuresOrder, err := g.client.GetOrder(ctx, g.settle, orderIDStr)
	if err != nil {
		return nil, err
	}

	// 轉换為標准格式
	order := &Order{
		OrderID:       futuresOrder.ID,
		ClientOrderID: futuresOrder.Text,
		Symbol:        g.symbol,
		Side:          convertSide(float64(futuresOrder.Size)),
		Type:          OrderTypeLimit,
		Quantity:      abs(float64(futuresOrder.Size)),
		ExecutedQty:   abs(float64(futuresOrder.FillSize)),
		Status:        convertStatus(futuresOrder.Status),
		CreatedAt:     time.Unix(int64(futuresOrder.CreateTime), 0),
		UpdateTime:    int64(futuresOrder.FinishTime * 1000),
	}

	// 解析價格
	if futuresOrder.Price != "" {
		order.Price, _ = strconv.ParseFloat(futuresOrder.Price, 64)
	}

	// 解析成交均價
	if futuresOrder.FillPrice != "" {
		order.AvgPrice, _ = strconv.ParseFloat(futuresOrder.FillPrice, 64)
	}

	return order, nil
}

// GetOpenOrders 查詢未完成订單
func (g *GateAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	futuresOrders, err := g.client.GetOpenOrders(ctx, g.settle, g.gateSymbol)
	if err != nil {
		return nil, err
	}

	orders := make([]*Order, 0, len(futuresOrders))
	for _, fo := range futuresOrders {
		order := &Order{
			OrderID:       fo.ID,
			ClientOrderID: fo.Text,
			Symbol:        g.symbol,
			Side:          convertSide(float64(fo.Size)),
			Type:          OrderTypeLimit,
			Quantity:      abs(float64(fo.Size)),
			ExecutedQty:   abs(float64(fo.FillSize)),
			Status:        convertStatus(fo.Status),
			CreatedAt:     time.Unix(int64(fo.CreateTime), 0),
			UpdateTime:    int64(fo.FinishTime * 1000),
		}

		// 解析價格
		if fo.Price != "" {
			order.Price, _ = strconv.ParseFloat(fo.Price, 64)
		}

		// 解析成交均價
		if fo.FillPrice != "" {
			order.AvgPrice, _ = strconv.ParseFloat(fo.FillPrice, 64)
		}

		orders = append(orders, order)
	}

	return orders, nil
}

// GetAccount 獲取帳戶信息
func (g *GateAdapter) GetAccount(ctx context.Context) (*Account, error) {
	futuresAcc, err := g.client.GetAccount(ctx, g.settle)
	if err != nil {
		return nil, err
	}

	// 解析餘額
	total, _ := strconv.ParseFloat(futuresAcc.Total, 64)
	available, _ := strconv.ParseFloat(futuresAcc.Available, 64)
	unrealisedPnl, _ := strconv.ParseFloat(futuresAcc.UnrealisedPnl, 64)

	posMode := "single"
	if futuresAcc.InDualMode {
		posMode = "dual_long_short"
	}

	// 獲取當前合約的杠杆設置
	leverage := 1 // 默认1倍
	if fp, err := g.client.GetPosition(ctx, g.settle, g.gateSymbol); err == nil {
		// 检查是否為逐倉模式
		leverageValue, _ := strconv.Atoi(fp.Leverage)
		if leverageValue != 0 {
			// 逐倉模式
			leverage = leverageValue
			logger.Warn("⚠️ [Gate] 當前為逐倉模式(杠杆倍數=%dx),本系统僅支援全倉模式。请在 Gate.io 网站將持倉模式改為全倉", leverage)
		} else {
			// 全倉模式,從 CrossLeverageLimit 獲取
			crossLeverage, _ := strconv.Atoi(fp.CrossLeverageLimit)
			if crossLeverage > 0 {
				leverage = crossLeverage
			}
		}
	}

	account := &Account{
		TotalWalletBalance: total,
		AvailableBalance:   available,
		TotalMarginBalance: total + unrealisedPnl,
		AccountLeverage:    leverage,
		PosMode:            posMode,
	}

	return account, nil
}

// GetPositions 獲取持倉信息
func (g *GateAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	// 使用單個持倉查詢接口獲取更详细的信息
	fp, err := g.client.GetPosition(ctx, g.settle, g.gateSymbol)
	if err != nil {
		return nil, err
	}

	positions := make([]*Position, 0)

	// 跳過空倉
	if fp.Size == 0 {
		return positions, nil
	}

	// 检查是否為逐倉模式
	leverage, _ := strconv.Atoi(fp.Leverage)
	if leverage != 0 {
		logger.Warn("⚠️ [Gate] 當前為逐倉模式(杠杆倍數=%dx),本系统僅支援全倉模式。请在 Gate.io 网站將持倉模式改為全倉", leverage)
		return nil, fmt.Errorf("不支援逐倉模式,请改為全倉模式")
	}

	// 全倉模式下,從 CrossLeverageLimit 獲取杠杆倍數
	crossLeverage, _ := strconv.Atoi(fp.CrossLeverageLimit)
	if crossLeverage == 0 {
		crossLeverage = 1 // 默认1倍
	}

	entryPrice, _ := strconv.ParseFloat(fp.EntryPrice, 64)
	markPrice, _ := strconv.ParseFloat(fp.MarkPrice, 64)
	unrealisedPnl, _ := strconv.ParseFloat(fp.UnrealisedPnl, 64)

	position := &Position{
		Symbol:        g.symbol,
		Size:          float64(fp.Size),
		EntryPrice:    entryPrice,
		MarkPrice:     markPrice,
		UnrealizedPNL: unrealisedPnl,
		Leverage:      crossLeverage,
		MarginType:    "crossed", // 全倉模式
	}

	positions = append(positions, position)

	return positions, nil
}

// GetBalance 獲取餘額
func (g *GateAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	acc, err := g.GetAccount(ctx)
	if err != nil {
		return 0, err
	}
	return acc.AvailableBalance, nil
}

// SetLeverage 設置全倉杠杆倍數
func (g *GateAdapter) SetLeverage(ctx context.Context, leverage int) error {
	// 驗证杠杆倍數範圍（Gate.io 通常支援 1-125 倍）
	if leverage < 1 || leverage > 125 {
		return fmt.Errorf("杠杆倍數必須在 1-125 之间，當前值: %d", leverage)
	}

	// 检查當前持倉模式
	fp, err := g.client.GetPosition(ctx, g.settle, g.gateSymbol)
	if err != nil {
		// 如果没有持倉，仍然可以設置杠杆（Gate.io 允許）
		logger.Info("ℹ️ [Gate] 當前無持倉，將設置全倉杠杆為 %dx", leverage)
	} else {
		// 检查是否為逐倉模式
		leverageValue, _ := strconv.Atoi(fp.Leverage)
		if leverageValue != 0 {
			return fmt.Errorf("當前為逐倉模式，無法設置全倉杠杆。请先在 Gate.io 网站將持倉模式改為全倉")
		}
		logger.Info("ℹ️ [Gate] 當前全倉杠杆: %s，將設置為 %dx", fp.CrossLeverageLimit, leverage)
	}

	// 調用 API 設置杠杆
	err = g.client.SetLeverage(ctx, g.settle, g.gateSymbol, leverage)
	if err != nil {
		return fmt.Errorf("設置杠杆失败: %w", err)
	}

	logger.Info("✅ [Gate] 全倉杠杆已設置為 %dx", leverage)
	return nil
}

// StartOrderStream 啟動訂單流
func (g *GateAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	// 包装回呼函數,將合約张數轉换為币數量
	wrappedCallback := func(update interface{}) {
		if orderUpdate, ok := update.(OrderUpdate); ok {
			// Gate.io返回的是合約张數,需要乘以quanto_multiplier轉换為币數量
			if g.quantoMultiplier > 0 {
				orderUpdate.Quantity = orderUpdate.Quantity * g.quantoMultiplier
				orderUpdate.ExecutedQty = orderUpdate.ExecutedQty * g.quantoMultiplier
			}
			callback(orderUpdate)
		} else {
			callback(update)
		}
	}

	g.wsManager.SetOrderCallback(wrappedCallback)

	// 如果 WebSocket 未运行，则啟动
	if !g.wsManager.IsRunning() {
		return g.wsManager.Start(ctx, g.symbol)
	}

	return nil
}

// StopOrderStream 停止訂單流
func (g *GateAdapter) StopOrderStream() error {
	return g.wsManager.Stop()
}

// StartPriceStream 啟動價格流
func (g *GateAdapter) StartPriceStream(ctx context.Context, callback func(string, float64)) error {
	g.wsManager.SetPriceCallback(callback)

	// 如果 WebSocket 未运行，则啟动
	if !g.wsManager.IsRunning() {
		return g.wsManager.Start(ctx, g.symbol)
	}

	return nil
}

// GetLatestPrice 獲取最新價格
func (g *GateAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	// 只有當请求的交易對與适配器初始化的交易對匹配時，才使用 WebSocket 缓存
	// 这样可以避免在多交易對场景下返回錯误的價格
	if symbol == g.symbol {
		price := g.wsManager.GetLatestPrice()
		if price > 0 {
			return price, nil
		}
	}

	// 使用 REST API 查詢期货價格（支援任意交易對）
	price, err := g.GetFuturesPrice(ctx, symbol)
	if err == nil && price > 0 {
		// 更新缓存（僅當交易對匹配時）
		if symbol == g.symbol {
			g.priceCacheMu.Lock()
			g.priceCache = price
			g.priceCacheTime = time.Now()
			g.priceCacheMu.Unlock()
		}
		return price, nil
	}

	// 最后尝試返回缓存價格（僅當交易對匹配且缓存未過期時）
	if symbol == g.symbol {
		g.priceCacheMu.RLock()
		defer g.priceCacheMu.RUnlock()

		if time.Since(g.priceCacheTime) < 5*time.Second && g.priceCache > 0 {
			return g.priceCache, nil
		}
	}

	return 0, fmt.Errorf("價格數據不可用")
}

// SetOrderMappingCallback 設置订單映射回呼
func (g *GateAdapter) SetOrderMappingCallback(callback func(orderID int64, price float64)) {
	g.orderMappingCallback = callback
}

// GetHistoricalKlines 獲取歷史K線數據
func (g *GateAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	// 轉换交易對格式
	gateSymbol := convertToGateSymbol(symbol)

	// 轉换K線週期格式
	gateInterval := interval
	if interval == "1m" {
		gateInterval = "1m"
	} else if interval == "5m" {
		gateInterval = "5m"
	} else if interval == "15m" {
		gateInterval = "15m"
	}

	// 調用REST API獲取K線數據
	candlesticks, err := g.client.GetCandlesticks(ctx, g.settle, gateSymbol, gateInterval, limit)
	if err != nil {
		return nil, fmt.Errorf("獲取歷史K線失败: %w", err)
	}

	// 轉换為標准格式
	candles := make([]*Candle, 0, len(candlesticks))
	for _, cs := range candlesticks {
		// 解析價格字符串
		open, _ := parseFloat(cs.Open)
		high, _ := parseFloat(cs.High)
		low, _ := parseFloat(cs.Low)
		close, _ := parseFloat(cs.Close)
		volume := float64(cs.Volume)

		candles = append(candles, &Candle{
			Symbol:    symbol,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
			Timestamp: cs.Timestamp,
			IsClosed:  true, // 历史K線都是已完結的
		})
	}

	return candles, nil
}

// StartKlineStream 啟動K線流
func (g *GateAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(interface{})) error {
	if g.klineWSManager == nil {
		g.klineWSManager = NewKlineWebSocketManager(g.settle, g.testnet)
	}
	return g.klineWSManager.Start(ctx, symbols, interval, callback)
}

// StopKlineStream 停止K線流
func (g *GateAdapter) StopKlineStream() {
	if g.klineWSManager != nil {
		g.klineWSManager.Stop()
	}
}

// GetBaseAsset 獲取基础资產（交易币种）
func (g *GateAdapter) GetBaseAsset() string {
	// 從交易對中提取基础资產（如 BTCUSDT -> BTC）
	parts := strings.Split(g.symbol, "USDT")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// GetQuoteAsset 獲取计價资產（結算币种）
func (g *GateAdapter) GetQuoteAsset() string {
	// Gate.io USDT永续合約使用USDT作為计價资產
	return "USDT"
}

// GetFundingRate 獲取资金费率
func (g *GateAdapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	// Gate.io API: GET /api/v4/futures/{settle}/funding_rate
	// 需要轉换交易對格式
	gateSymbol := convertToGateSymbol(symbol)

	path := fmt.Sprintf("/futures/%s/funding_rate", g.settle)
	queryString := fmt.Sprintf("contract=%s", gateSymbol)

	respBody, err := g.client.DoRequest(ctx, "GET", path, queryString, nil)
	if err != nil {
		return 0, fmt.Errorf("獲取资金费率失败: %w", err)
	}

	// 解析响应（Gate.io返回數组）
	var results []struct {
		Contract    string `json:"contract"`
		FundingRate string `json:"funding_rate"` // Gate.io返回字符串格式
	}

	if err := json.Unmarshal(respBody, &results); err != nil {
		// 尝試解析單個對象格式
		var result struct {
			Contract    string `json:"contract"`
			FundingRate string `json:"funding_rate"`
		}
		if err2 := json.Unmarshal(respBody, &result); err2 == nil {
			fundingRate, err3 := strconv.ParseFloat(result.FundingRate, 64)
			if err3 != nil {
				return 0, fmt.Errorf("解析资金费率失败: %w", err3)
			}
			return fundingRate, nil
		}
		return 0, fmt.Errorf("解析响应失败: %w, 响应: %s", err, string(respBody))
	}

	// 查找匹配的交易對
	for _, result := range results {
		if result.Contract == gateSymbol {
			fundingRate, err := strconv.ParseFloat(result.FundingRate, 64)
			if err != nil {
				return 0, fmt.Errorf("解析资金费率失败: %w", err)
			}
			return fundingRate, nil
		}
	}

	return 0, fmt.Errorf("未找到交易對 %s 的资金费率", symbol)
}

// FundingInfo 資金費率詳情（供 exchange wrapper 轉換）
type FundingInfo struct {
	Symbol          string
	Rate            float64
	NextFundingTime time.Time
	MarkPrice       float64
	IndexPrice      float64
}

func gateEstimateNextFundingUTC8h(now time.Time) time.Time {
	now = now.UTC()
	hour := now.Hour()
	base := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	switch {
	case hour < 8:
		return base.Add(8 * time.Hour)
	case hour < 16:
		return base.Add(16 * time.Hour)
	default:
		return base.Add(24 * time.Hour)
	}
}

// GetFundingInfo 從期貨 tickers 獲取資金費、下次結算時間與標記/指數價
func (g *GateAdapter) GetFundingInfo(ctx context.Context, symbol string) (*FundingInfo, error) {
	gateSymbol := convertToGateSymbol(symbol)
	path := fmt.Sprintf("/futures/%s/tickers", g.settle)
	queryString := fmt.Sprintf("contract=%s", gateSymbol)
	respBody, err := g.client.DoRequest(ctx, "GET", path, queryString, nil)
	if err != nil {
		return nil, fmt.Errorf("獲取期貨 tickers 失败: %w", err)
	}
	var rows []struct {
		Contract           string  `json:"contract"`
		Last               string  `json:"last"`
		FundingRate        string  `json:"funding_rate"`
		FundingNextApply   float64 `json:"funding_next_apply"` // Unix 時間戳（秒，浮點）
		MarkPrice          string  `json:"mark_price"`
		IndexPrice         string  `json:"index_price"`
	}
	if err := json.Unmarshal(respBody, &rows); err != nil {
		return nil, fmt.Errorf("解析 tickers 失败: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("未找到合約 %s", gateSymbol)
	}
	r := rows[0]
	rate, _ := strconv.ParseFloat(r.FundingRate, 64)
	mark, _ := strconv.ParseFloat(r.MarkPrice, 64)
	if mark == 0 {
		mark, _ = strconv.ParseFloat(r.Last, 64)
	}
	idx, _ := strconv.ParseFloat(r.IndexPrice, 64)
	if idx == 0 {
		idx = mark
	}
	var next time.Time
	if r.FundingNextApply > 0 {
		next = time.Unix(int64(r.FundingNextApply), 0).UTC()
	}
	if next.IsZero() {
		next = gateEstimateNextFundingUTC8h(time.Now().UTC())
	}
	return &FundingInfo{
		Symbol:          symbol,
		Rate:            rate,
		NextFundingTime: next,
		MarkPrice:       mark,
		IndexPrice:      idx,
	}, nil
}

// GetFuturesPrice 獲取期货市场價格
func (g *GateAdapter) GetFuturesPrice(ctx context.Context, symbol string) (float64, error) {
	// 轉换為 Gate.io 期货格式: BTCUSDT -> BTC_USDT
	gateSymbol := convertToGateSymbol(symbol)

	// Gate.io 期货 API: GET /api/v4/futures/{settle}/tickers
	path := fmt.Sprintf("/futures/%s/tickers", g.settle)
	queryString := fmt.Sprintf("contract=%s", gateSymbol)

	respBody, err := g.client.DoRequest(ctx, "GET", path, queryString, nil)
	if err != nil {
		return 0, fmt.Errorf("獲取期货價格失败: %w", err)
	}

	// 解析响应
	var results []struct {
		Contract string `json:"contract"`
		Last     string `json:"last"`
	}

	if err := json.Unmarshal(respBody, &results); err != nil {
		return 0, fmt.Errorf("解析响应失败: %w", err)
	}

	if len(results) == 0 {
		return 0, fmt.Errorf("未找到合約 %s 的期货價格", gateSymbol)
	}

	price, err := strconv.ParseFloat(results[0].Last, 64)
	if err != nil {
		return 0, fmt.Errorf("解析價格失败: %w", err)
	}

	return price, nil
}

// GetSpotPrice 獲取現貨市场價格
func (g *GateAdapter) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	// 轉换為 Gate.io 現貨格式: BTCUSDT -> BTC_USDT
	spotSymbol := convertToGateSymbol(symbol)

	// Gate.io 現貨 API: GET /api/v4/spot/tickers
	path := "/spot/tickers"
	queryString := fmt.Sprintf("currency_pair=%s", spotSymbol)

	respBody, err := g.client.DoRequest(ctx, "GET", path, queryString, nil)
	if err != nil {
		return 0, fmt.Errorf("獲取現貨價格失败: %w", err)
	}

	// 解析响应
	var results []struct {
		CurrencyPair string `json:"currency_pair"`
		Last         string `json:"last"`
	}

	if err := json.Unmarshal(respBody, &results); err != nil {
		return 0, fmt.Errorf("解析响应失败: %w", err)
	}

	if len(results) == 0 {
		return 0, fmt.Errorf("未找到交易對 %s 的現貨價格", symbol)
	}

	price, err := strconv.ParseFloat(results[0].Last, 64)
	if err != nil {
		return 0, fmt.Errorf("解析價格失败: %w", err)
	}

	return price, nil
}

// calculateDecimalPlaces 计算小數位數
func calculateDecimalPlaces(value float64) int {
	if value >= 1 {
		return 0
	}

	str := fmt.Sprintf("%.10f", value)
	parts := strings.Split(str, ".")
	if len(parts) != 2 {
		return 0
	}

	// 计算小數点后第一個非零數字的位置
	for i, c := range parts[1] {
		if c != '0' {
			return i + 1
		}
	}

	return 0
}

// GetOrderBook 獲取訂單簿深度
func (g *GateAdapter) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	// 轉换交易對格式
	gateSymbol := convertToGateSymbol(symbol)

	// 調用 Gate.io API 獲取訂單簿
	gateOrderBook, err := g.client.GetOrderBook(ctx, g.settle, gateSymbol, limit)
	if err != nil {
		return nil, fmt.Errorf("獲取訂單簿深度失败: %w", err)
	}

	// 轉换買盘數據（價格從高到低，Gate.io 已經按此顺序返回）
	bids := make([]OrderBookLevel, 0, len(gateOrderBook.Bids))
	for _, bid := range gateOrderBook.Bids {
		price, err := strconv.ParseFloat(bid.P, 64)
		if err != nil {
			continue
		}
		bids = append(bids, OrderBookLevel{
			Price:    price,
			Quantity: float64(bid.S),
		})
	}

	// 轉换賣盘數據（價格從低到高，Gate.io 已經按此顺序返回）
	asks := make([]OrderBookLevel, 0, len(gateOrderBook.Asks))
	for _, ask := range gateOrderBook.Asks {
		price, err := strconv.ParseFloat(ask.P, 64)
		if err != nil {
			continue
		}
		asks = append(asks, OrderBookLevel{
			Price:    price,
			Quantity: float64(ask.S),
		})
	}

	// 使用更新時间戳
	timestamp := int64(gateOrderBook.Update)
	if timestamp == 0 {
		timestamp = int64(gateOrderBook.Current)
	}

	return &OrderBook{
		Symbol:    symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: timestamp,
	}, nil
}

// InternalTransfer 交易所內部轉帳（POST /wallet/transfers：現貨 ↔ 永续合約帳戶）
func (g *GateAdapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	from, to, err := mapGateWalletEndpoints(fromAccount, toAccount)
	if err != nil {
		return "", err
	}
	cur := strings.TrimSpace(asset)
	if cur == "" {
		cur = "USDT"
	}
	amt := strconv.FormatFloat(amount, 'f', 8, 64)
	txID, err := g.client.WalletTransfer(ctx, cur, amt, from, to, g.settle)
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(txID, 10), nil
}

func mapGateWalletEndpoints(fromAccount, toAccount string) (from, to string, err error) {
	f := strings.ToUpper(strings.TrimSpace(fromAccount))
	t := strings.ToUpper(strings.TrimSpace(toAccount))
	switch {
	case (f == "UMFUTURE" || f == "CONTRACT" || f == "FUTURES") && (t == "SPOT" || t == "MAIN"):
		return "futures", "spot", nil
	case (f == "SPOT" || f == "MAIN") && (t == "UMFUTURE" || t == "CONTRACT" || t == "FUTURES"):
		return "spot", "futures", nil
	default:
		return "", "", fmt.Errorf("Gate 不支援的劃轉: %s -> %s（僅支援現貨與合約帳戶互轉）", fromAccount, toAccount)
	}
}
