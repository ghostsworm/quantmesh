package bitget

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
)

// 為了避免循環匯入，在这里定义需要的接口和類型
// 这些類型应該與 exchange/types.go 中的定义保持一致

type Side string
type OrderType string
type OrderStatus string
type TimeInForce string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

const (
	OrderTypeLimit OrderType = "LIMIT"
)

const (
	OrderStatusNew OrderStatus = "NEW"
)

const (
	TimeInForceGTC TimeInForce = "GTC"
)

type OrderRequest struct {
	Symbol        string
	Side          Side
	Type          OrderType
	TimeInForce   TimeInForce
	Quantity      float64
	Price         float64
	ReduceOnly    bool
	PostOnly      bool // 是否只做 Maker（Post Only）
	PriceDecimals int
	ClientOrderID string // 自定义订單ID
}

type Order struct {
	OrderID       int64
	ClientOrderID string
	Symbol        string
	Side          Side
	Type          OrderType
	Price         float64
	Quantity      float64
	ExecutedQty   float64
	AvgPrice      float64
	Status        OrderStatus
	CreatedAt     time.Time
	UpdateTime    int64
}

type Position struct {
	Symbol         string
	Size           float64
	EntryPrice     float64
	MarkPrice      float64
	UnrealizedPNL  float64
	Leverage       int
	MarginType     string
	IsolatedMargin float64
}

type Account struct {
	TotalWalletBalance float64
	TotalMarginBalance float64
	AvailableBalance   float64
	Positions          []*Position
	PosMode            string // "hedge_mode" or "one_way_mode"
	AccountLeverage    int    // 账戶级别的杠杆倍數
}

type OrderUpdate struct {
	OrderID       int64
	ClientOrderID string
	Symbol        string
	Side          Side
	Type          OrderType
	Status        OrderStatus
	Price         float64
	Quantity      float64
	ExecutedQty   float64
	AvgPrice      float64
	UpdateTime    int64
}

type OrderUpdateCallback func(update OrderUpdate)

// OrderBookLevel 订單簿檔位（本地類型，避免循環匯入）
type OrderBookLevel struct {
	Price    float64
	Quantity float64
}

// OrderBook 订單簿（本地類型，避免循環匯入）
type OrderBook struct {
	Symbol    string
	Bids      []OrderBookLevel
	Asks      []OrderBookLevel
	Timestamp int64
}

// BitgetAdapter Bitget 交易所适配器
type BitgetAdapter struct {
	client         *Client
	wsManager      *WebSocketManager
	klineWSManager *KlineWebSocketManager
	symbol         string // 交易對（如 ETHUSDT，V2 API 不带 _UMCBL 后缀）
	useWebSocket   bool   // 是否使用 WebSocket 下單

	// 🔥 新增：订單ID到價格的映射注册回呼
	// 用於在下單成功后立即建立映射，避免 WebSocket 更新先到導致找不到槽位
	orderMappingCallback func(orderID int64, price float64)

	posMode      string // 持倉模式：hedge_mode 或 one_way_mode
	productType  string // 合約類型：usdt-futures（U本位）或 coin-futures（币本位）
	marginCoin   string // 保证金币种：自动從合約信息獲取
	volumePlace  int    // 數量小數位（從合約信息獲取）
	pricePlace   int    // 價格小數位（從合約信息獲取）
	minTradeNum  string // 最小下單數量
	minTradeUSDT string // 最小下單金額（USDT）
	baseAsset    string // 基础资產（交易币种），如 BTC
	quoteAsset   string // 计價资產（結算币种），如 USDT、USD
	testnet      bool   // 是否使用測試網
}

// NewBitgetAdapter 創建 Bitget 适配器
func NewBitgetAdapter(cfg map[string]string, symbol string) (*BitgetAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]
	passphrase := cfg["passphrase"]
	testnet := cfg["testnet"] == "true" || cfg["testnet"] == "1" // 是否使用測試網

	if apiKey == "" || secretKey == "" || passphrase == "" {
		return nil, fmt.Errorf("bitget API 配置不完整")
	}

	// Bitget V2 合約符号格式：直接使用 ETHUSDT（不带 _UMCBL 后缀）
	bitgetSymbol := convertToBitgetSymbol(symbol)

	client := NewClient(apiKey, secretKey, passphrase, testnet)
	wsManager := NewWebSocketManager(apiKey, secretKey, passphrase, testnet)

	if testnet {
		logger.Info("🌐 [Bitget] 使用測試網模式")
	}

	adapter := &BitgetAdapter{
		client:       client,
		wsManager:    wsManager,
		symbol:       bitgetSymbol,
		useWebSocket: false, // 使用 REST API 下單（混合模式）
		testnet:      testnet,
	}

	// 初始化獲取合約信息和持倉模式
	ctxInit, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. 先獲取合約信息（必須先獲取，因為需要設置productType和marginCoin）
	if err := adapter.fetchContractInfo(ctxInit); err != nil {
		logger.Warn("⚠️ [Bitget] 獲取合約信息失败: %v", err)
		// 使用默认值
		adapter.volumePlace = 4
		adapter.pricePlace = 2
		adapter.productType = "usdt-futures"
		adapter.marginCoin = "USDT"
	}

	// 2. 獲取持倉模式和帳戶資訊
	acc, err := adapter.GetAccount(ctxInit)
	if err != nil {
		logger.Warn("⚠️ [Bitget] 初始化獲取帳戶信息失败: %v", err)
		adapter.posMode = "hedge_mode" // 默认双向持倉
	} else {
		adapter.posMode = acc.PosMode
		// 显示持倉模式（双向/單向）
		posModeDesc := "双向持倉"
		if acc.PosMode == "one_way_mode" {
			posModeDesc = "單向持倉"
		}
		logger.Info("ℹ️ [Bitget] 持倉模式: %s (%s)", posModeDesc, acc.PosMode)
	}

	// 移除这里的自动连接，统一由 StartPriceStream 或 StartOrderStream 触发
	// 这样可以避免重複连接和日志重複
	/*
		ctx := context.Background()
		go func() {
			logger.Info("🔗 [Bitget] 正在连接 WebSocket...")
			if err := wsManager.ConnectAndLogin(ctx, bitgetSymbol); err != nil {
				logger.Warn("⚠️ [Bitget] WebSocket 连接失败: %v（不影响交易）", err)
			} else {
				logger.Info("✅ [Bitget] WebSocket 已连接並登錄")
			}
		}()
	*/

	return adapter, nil
}

// GetName 獲取交易所名称
func (b *BitgetAdapter) GetName() string {
	return "Bitget"
}

// GetMarketType 獲取市場類型：futures 合約
func (b *BitgetAdapter) GetMarketType() string {
	return "futures"
}

// fetchContractInfo 獲取合約信息（數量精度、價格精度等）
func (b *BitgetAdapter) fetchContractInfo(ctx context.Context) error {
	// 尝試從多個合約類型中查找（先U本位，再币本位）
	productTypes := []string{"usdt-futures", "coin-futures", "usdc-futures"}
	var lastErr error

	for _, pt := range productTypes {
		path := fmt.Sprintf("/api/v2/mix/market/contracts?productType=%s&symbol=%s", pt, b.symbol)
		resp, err := b.client.DoRequest(ctx, "GET", path, nil)
		if err != nil {
			lastErr = err
			continue
		}

		// 解析合約信息
		var dataList []struct {
			Symbol             string   `json:"symbol"`
			VolumePlace        string   `json:"volumePlace"`        // 數量小數位
			PricePlace         string   `json:"pricePlace"`         // 價格小數位
			MinTradeNum        string   `json:"minTradeNum"`        // 最小下單數量
			MinTradeUSDT       string   `json:"minTradeUSDT"`       // 最小下單金額
			BaseCoin           string   `json:"baseCoin"`           // 基础币种
			QuoteCoin          string   `json:"quoteCoin"`          // 计價币种
			SupportMarginCoins []string `json:"supportMarginCoins"` // 支援的保证金币种
		}

		if err := json.Unmarshal(resp.Data, &dataList); err != nil {
			lastErr = fmt.Errorf("解析合約信息失败: %w", err)
			continue
		}

		if len(dataList) == 0 {
			continue // 尝試下一個productType
		}

		// 找到合約信息
		contract := dataList[0]
		b.productType = pt
		b.volumePlace, _ = strconv.Atoi(contract.VolumePlace)
		b.pricePlace, _ = strconv.Atoi(contract.PricePlace)
		b.minTradeNum = contract.MinTradeNum
		b.minTradeUSDT = contract.MinTradeUSDT
		b.baseAsset = contract.BaseCoin
		b.quoteAsset = contract.QuoteCoin

		// 設置保证金币种（优先使用supportMarginCoins的第一個，否则使用quoteCoin）
		if len(contract.SupportMarginCoins) > 0 {
			b.marginCoin = contract.SupportMarginCoins[0]
		} else {
			b.marginCoin = contract.QuoteCoin
		}

		// 判断合約類型描述
		contractTypeDesc := "U本位合約"
		if pt == "coin-futures" {
			contractTypeDesc = "币本位合約"
		} else if pt == "usdc-futures" {
			contractTypeDesc = "USDC合約"
		}

		logger.Info("ℹ️ [Bitget 合約信息] %s - %s, 數量精度:%d, 價格精度:%d, 基础币种:%s, 计價币种:%s, 保证金:%s",
			b.symbol, contractTypeDesc, b.volumePlace, b.pricePlace, b.baseAsset, b.quoteAsset, b.marginCoin)

		return nil
	}

	if lastErr != nil {
		return fmt.Errorf("未找到合約信息 %s: %w", b.symbol, lastErr)
	}
	return fmt.Errorf("未找到合約信息: %s", b.symbol)
}

// PlaceOrder 下單（使用 REST API）
func (b *BitgetAdapter) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	// 混合模式：使用 REST API 下單，更稳定可靠
	return b.placeOrderViaREST(ctx, req)
}

// placeOrderViaREST 通過 REST API 下單
func (b *BitgetAdapter) placeOrderViaREST(ctx context.Context, req *OrderRequest) (*Order, error) {
	// 确定 side 和 tradeSide
	side := strings.ToLower(string(req.Side))
	var tradeSide string

	// 🔥 Bitget 双向持倉的特殊逻辑：
	// 开多：side=buy, tradeSide=open
	// 平多：side=buy, tradeSide=close （注意！平多也是 buy）
	// 开空：side=sell, tradeSide=open
	// 平空：side=sell, tradeSide=close
	if b.posMode == "hedge_mode" {
		if req.ReduceOnly {
			// 平倉：保持 side 方向不变，只改 tradeSide
			// 如果是 SELL（賣出），實際上是要平多倉，需要改為 buy
			if req.Side == SideSell {
				side = "buy" // 平多倉必須用 buy
			} else {
				side = "sell" // 平空倉必須用 sell
			}
			tradeSide = "close"
		} else {
			tradeSide = "open"
		}
	}

	// 🔥 使用合約信息中的精度格式化數量和價格
	// 特殊处理：如果數量過小，自动調整為最小下單量
	if req.Quantity <= 0 && b.volumePlace >= 0 {
		req.Quantity = math.Pow10(-b.volumePlace)
		logger.Warn("⚠️ [Bitget] 下單數量原始值為 0，已自动調整為最小單位: %.8f", req.Quantity)
	}

	quantityStr := fmt.Sprintf("%.*f", b.volumePlace, req.Quantity)
	priceStr := fmt.Sprintf("%.*f", b.pricePlace, req.Price)

	// 如果截断后數量為 0，也需要兜底
	q, _ := strconv.ParseFloat(quantityStr, 64)
	if q <= 0 && b.volumePlace >= 0 {
		minQty := math.Pow10(-b.volumePlace)
		quantityStr = fmt.Sprintf("%.*f", b.volumePlace, minQty)
		logger.Warn("⚠️ [Bitget] 數量截断后為 0，使用最小精度兜底: %s", quantityStr)
	}

	// 根據 PostOnly 参數选擇 force 類型
	forceType := "gtc" // 默认使用 GTC (Good Till Cancel)
	if req.PostOnly {
		forceType = "post_only" // Post Only - 只做 Maker
	}

	// Bitget V2 下單参數
	body := map[string]interface{}{
		"symbol":      req.Symbol,
		"productType": b.productType,
		"marginMode":  "crossed",
		"marginCoin":  "USDT",
		"side":        side,
		"orderType":   "limit",
		"price":       priceStr,
		"size":        quantityStr,
		"force":       forceType,
	}

	// 設置自定义订單ID
	if req.ClientOrderID != "" {
		body["clientOid"] = req.ClientOrderID
	}

	// 双向持倉模式下添加 tradeSide（必須）
	// 🔥 关键：双向持倉模式下，不能使用 reduceOnly 参數，只能用 tradeSide=close
	if tradeSide != "" {
		body["tradeSide"] = tradeSide
	}

	// 🔥 單向持倉模式下，如果是只减倉，必須使用 reduceOnly 参數
	// 注意：單向持倉時 tradeSide 参數必須省略，否则會报錯
	if b.posMode != "hedge_mode" && req.ReduceOnly {
		body["reduceOnly"] = "YES"
	}

	// 只请求1次，不重試
	resp, err := b.client.DoRequest(ctx, "POST", "/api/v2/mix/order/place-order", body)
	if err != nil {
		// 检查錯误類型
		if strings.Contains(err.Error(), "insufficient balance") || strings.Contains(err.Error(), "40007") {
			return nil, fmt.Errorf("保证金不足: %w", err)
		}
		return nil, err
	}

	// 解析响应
	var data struct {
		OrderID       string `json:"orderId"`
		ClientOrderID string `json:"clientOid"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("解析下單响应失败: %w", err)
	}

	// 🔍 添加調試：打印完整响应
	logger.Debug("🔍 [Bitget REST] 下單响应: %s", string(resp.Data))

	orderID, _ := strconv.ParseInt(data.OrderID, 10, 64)
	if orderID == 0 {
		return nil, fmt.Errorf("下單响应中orderId為空或無效: %s", string(resp.Data))
	}

	order := &Order{
		OrderID:       orderID,
		ClientOrderID: data.ClientOrderID,
		Symbol:        req.Symbol,
		Side:          req.Side,
		Type:          req.Type,
		Price:         req.Price,
		Quantity:      req.Quantity,
		Status:        OrderStatusNew,
		CreatedAt:     time.Now(),
	}

	// 🔥 诊断：獲取當前市场價格，检查订單價格是否合理
	ctxPrice, cancelPrice := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelPrice()
	currentPrice, err := b.GetLatestPrice(ctxPrice, b.symbol)
	if err == nil {
		priceDiff := req.Price - currentPrice
		priceDiffPercent := (priceDiff / currentPrice) * 100
		logger.Debug("🔍 [Bitget下單诊断] 订單價格: %.2f, 當前價格: %.2f, 價差: %.2f (%.3f%%)",
			req.Price, currentPrice, priceDiff, priceDiffPercent)
	}

	// 注意：不在这里打印日志，由executor统一打印避免重複
	return order, nil
}

// BatchPlaceOrders 批量下單
func (b *BitgetAdapter) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	placedOrders := make([]*Order, 0, len(orders))
	hasMarginError := false

	for _, orderReq := range orders {
		order, err := b.PlaceOrder(ctx, orderReq)
		if err != nil {
			logger.Warn("⚠️ [Bitget] 下單失败 %.2f %s: %v",
				orderReq.Price, orderReq.Side, err)

			if strings.Contains(err.Error(), "保证金不足") {
				hasMarginError = true
			}
			continue
		}

		// 🔥 关键：确保 order.Price 包含请求的價格
		// 这样調用者就能正确建立 orderID -> price 的映射
		order.Price = orderReq.Price

		// 🔥 新增：立即注册订單ID到價格的映射
		// 这样可以防止 WebSocket 更新先到導致找不到槽位
		if b.orderMappingCallback != nil && order.OrderID > 0 {
			b.orderMappingCallback(order.OrderID, orderReq.Price)
			logger.Debug("🔍 [Bitget映射] 注册 订單ID=%d -> 價格=%.2f", order.OrderID, orderReq.Price)
		}

		placedOrders = append(placedOrders, order)
	}

	return placedOrders, hasMarginError
}

// CancelOrder 取消訂單
func (b *BitgetAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	body := map[string]interface{}{
		"symbol":      b.symbol,
		"productType": b.productType,
		"marginCoin":  b.marginCoin,
		"orderId":     fmt.Sprintf("%d", orderID),
	}

	_, err := b.client.DoRequest(ctx, "POST", "/api/v2/mix/order/cancel-order", body)
	if err != nil {
		// 订單不存在不算錯误
		if strings.Contains(err.Error(), "order does not exist") || strings.Contains(err.Error(), "40029") {
			logger.Info("ℹ️ [Bitget] 订單 %d 已不存在，跳過取消", orderID)
			return nil
		}
		return fmt.Errorf("取消訂單失败: %w", err)
	}

	logger.Info("✅ [Bitget] 取消訂單成功: %d", orderID)
	return nil
}

// BatchCancelOrders 批量取消訂單
func (b *BitgetAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	if len(orderIDs) == 0 {
		return nil
	}

	// 🔥 Bitget 批量撤單限制：最多20個，必須傳symbol、productType、marginCoin
	batchSize := 20
	for i := 0; i < len(orderIDs); i += batchSize {
		end := i + batchSize
		if end > len(orderIDs) {
			end = len(orderIDs)
		}

		batch := orderIDs[i:end]

		// 🔥 如果只有1個订單，直接用單個撤單接口
		if len(batch) == 1 {
			if err := b.CancelOrder(ctx, symbol, batch[0]); err != nil {
				logger.Warn("⚠️ [Bitget] 取消訂單失败 %d: %v", batch[0], err)
			}
			continue
		}

		// 構造订單ID字符串列表
		orderIDStrs := make([]string, len(batch))
		for j, id := range batch {
			orderIDStrs[j] = fmt.Sprintf("%d", id)
		}

		// 🔥 确保所有必需参數都存在
		body := map[string]interface{}{
			"symbol":      b.symbol,      // 必需
			"productType": b.productType, // 必需：USDT-FUTURES
			"marginCoin":  b.marginCoin,  // 必需：USDT
			"orderIdList": orderIDStrs,   // 必需：订單ID列表
		}

		_, err := b.client.DoRequest(ctx, "POST", "/api/v2/mix/order/batch-cancel-orders", body)
		if err != nil {
			logger.Warn("⚠️ [Bitget] 批量撤單失败 (共%d個): %v", len(batch), err)
			// 失败時尝試單個撤單
			logger.Info("🔄 [Bitget] 改為逐個撤單...")
			for _, orderID := range batch {
				_ = b.CancelOrder(ctx, symbol, orderID)
				time.Sleep(100 * time.Millisecond) // 避免限频
			}
		} else {
			logger.Info("✅ [Bitget] 批量撤單成功: %d 個订單", len(batch))
		}

		// 避免限频
		if i+batchSize < len(orderIDs) {
			time.Sleep(100 * time.Millisecond)
		}
	}

	return nil
}

// CancelAllOrders 一键全撤所有订單（Bitget特有功能）
func (b *BitgetAdapter) CancelAllOrders(ctx context.Context) error {
	body := map[string]interface{}{
		"productType": b.productType, // 必需：USDT-FUTURES
		"marginCoin":  b.marginCoin,  // 必需：USDT
	}

	resp, err := b.client.DoRequest(ctx, "POST", "/api/v2/mix/order/cancel-all-orders", body)
	if err != nil {
		return fmt.Errorf("一键全撤失败: %w", err)
	}

	// 解析响应
	var data struct {
		SuccessList []struct {
			OrderID   string `json:"orderId"`
			ClientOid string `json:"clientOid"`
		} `json:"successList"`
		FailureList []struct {
			OrderID   string `json:"orderId"`
			ClientOid string `json:"clientOid"`
			ErrorMsg  string `json:"errorMsg"`
		} `json:"failureList"`
	}

	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return fmt.Errorf("解析一键全撤响应失败: %w", err)
	}

	logger.Info("✅ [Bitget 一键全撤] 成功: %d 個, 失败: %d 個",
		len(data.SuccessList), len(data.FailureList))

	if len(data.FailureList) > 0 {
		for _, fail := range data.FailureList {
			logger.Warn("⚠️ [Bitget 一键全撤失败] 订單ID: %s, 原因: %s", fail.OrderID, fail.ErrorMsg)
		}
	}

	return nil
}

// GetOrder 查詢訂單
func (b *BitgetAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	path := fmt.Sprintf("/api/v2/mix/order/detail?symbol=%s&productType=%s&orderId=%d", b.symbol, b.productType, orderID)
	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	// 解析订單详情
	var data struct {
		Symbol    string `json:"symbol"`
		Size      string `json:"size"`
		OrderId   string `json:"orderId"`
		ClientOid string `json:"clientOid"`
		FilledQty string `json:"filledQty"`
		Price     string `json:"price"`
		Side      string `json:"side"`
		Status    string `json:"status"`
		PriceAvg  string `json:"priceAvg"`
		CTime     string `json:"cTime"`
		UTime     string `json:"uTime"`
	}

	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("解析订單详情失败: %w", err)
	}

	// 轉换為通用格式
	ordID, _ := strconv.ParseInt(data.OrderId, 10, 64)
	price, _ := strconv.ParseFloat(data.Price, 64)
	quantity, _ := strconv.ParseFloat(data.Size, 64)
	executedQty, _ := strconv.ParseFloat(data.FilledQty, 64)
	avgPrice, _ := strconv.ParseFloat(data.PriceAvg, 64)
	updateTime, _ := strconv.ParseInt(data.UTime, 10, 64)

	side := SideBuy
	if data.Side == "sell" {
		side = SideSell
	}

	var status OrderStatus = "NEW"
	switch data.Status {
	case "new":
		status = "NEW"
	case "partial-fill":
		status = "PARTIALLY_FILLED"
	case "full-fill":
		status = "FILLED"
	case "cancelled":
		status = "CANCELED"
	}

	return &Order{
		OrderID:       ordID,
		ClientOrderID: data.ClientOid,
		Symbol:        data.Symbol,
		Side:          side,
		Type:          OrderTypeLimit,
		Price:         price,
		Quantity:      quantity,
		ExecutedQty:   executedQty,
		AvgPrice:      avgPrice,
		Status:        status,
		UpdateTime:    updateTime,
	}, nil
}

// GetOpenOrders 查詢未完成订單
func (b *BitgetAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	path := fmt.Sprintf("/api/v2/mix/order/orders-pending?symbol=%s&productType=%s", b.symbol, b.productType)
	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	// 解析订單列表（V2 API 返回對象格式）
	var wrapper struct {
		EntrustedList []struct {
			Symbol        string `json:"symbol"`
			Size          string `json:"size"`
			OrderId       string `json:"orderId"`
			ClientOid     string `json:"clientOid"`
			FilledQty     string `json:"filledQty"`
			Fee           string `json:"fee"`
			Price         string `json:"price"`
			Side          string `json:"side"` // "buy" or "sell"
			Status        string `json:"status"`
			PriceAvg      string `json:"priceAvg"`
			BaseVolume    string `json:"baseVolume"`
			QuoteVolume   string `json:"quoteVolume"`
			EntrustVolume string `json:"entrustVolume"`
			TradeAmount   string `json:"tradeAmount"`
			CTime         string `json:"cTime"`
			UTime         string `json:"uTime"`
		} `json:"entrustedList"`
	}

	if err := json.Unmarshal(resp.Data, &wrapper); err != nil {
		return nil, fmt.Errorf("解析订單列表失败: %w", err)
	}

	dataList := wrapper.EntrustedList

	orders := make([]*Order, 0, len(dataList))
	for _, item := range dataList {
		orderID, _ := strconv.ParseInt(item.OrderId, 10, 64)
		price, _ := strconv.ParseFloat(item.Price, 64)
		quantity, _ := strconv.ParseFloat(item.Size, 64)
		executedQty, _ := strconv.ParseFloat(item.FilledQty, 64)
		avgPrice, _ := strconv.ParseFloat(item.PriceAvg, 64)
		updateTime, _ := strconv.ParseInt(item.UTime, 10, 64)

		// 轉换方向
		side := SideBuy
		if item.Side == "sell" {
			side = SideSell
		}

		// 轉换状態
		var status OrderStatus = "NEW"
		switch item.Status {
		case "new":
			status = "NEW"
		case "partial-fill":
			status = "PARTIALLY_FILLED"
		case "full-fill":
			status = "FILLED"
		case "cancelled":
			status = "CANCELED"
		}

		orders = append(orders, &Order{
			OrderID:       orderID,
			ClientOrderID: item.ClientOid,
			Symbol:        item.Symbol,
			Side:          side,
			Type:          OrderTypeLimit,
			Price:         price,
			Quantity:      quantity,
			ExecutedQty:   executedQty,
			AvgPrice:      avgPrice,
			Status:        status,
			UpdateTime:    updateTime,
		})
	}

	return orders, nil
}

// GetAccount 獲取帳戶信息
func (b *BitgetAdapter) GetAccount(ctx context.Context) (*Account, error) {
	path := fmt.Sprintf("/api/v2/mix/account/account?symbol=%s&productType=%s&marginCoin=%s", b.symbol, b.productType, b.marginCoin)
	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	// 解析帳戶資訊
	var data struct {
		MarginCoin            string `json:"marginCoin"`
		Locked                string `json:"locked"`
		Available             string `json:"available"`
		CrossMaxAvailable     string `json:"crossedMaxAvailable"`  // 注意：API文檔是crossedMaxAvailable
		FixedMaxAvailable     string `json:"isolatedMaxAvailable"` // 注意：API文檔是isolatedMaxAvailable
		MaxTransferOut        string `json:"maxTransferOut"`
		Equity                string `json:"accountEquity"` // 注意：API文檔是accountEquity
		USDTEquity            string `json:"usdtEquity"`
		BTCEquity             string `json:"btcEquity"`
		PosMode               string `json:"posMode"`
		MarginMode            string `json:"marginMode"`            // 保证金模式：crossed全倉/isolated逐倉
		CrossedMarginLeverage int    `json:"crossedMarginLeverage"` // 全倉杠杆倍數（數字類型）
		IsolatedLongLever     int    `json:"isolatedLongLever"`     // 逐倉多头杠杆（數字類型）
		IsolatedShortLever    int    `json:"isolatedShortLever"`    // 逐倉空头杠杆（數字類型）
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("解析帳戶資訊失败: %w", err)
	}

	// 轉换為通用格式
	available, _ := strconv.ParseFloat(data.Available, 64)
	equity, _ := strconv.ParseFloat(data.Equity, 64)

	// 🔥 强制检查保证金模式：必須是全倉模式
	if data.MarginMode != "crossed" {
		return nil, fmt.Errorf("⚠️ 當前保证金模式為【%s】，本程序僅支援全倉模式(crossed)。\n"+
			"请登錄 Bitget 交易所，將保证金模式切换為【全倉】后再运行程序。\n"+
			"切换路径：合約交易 -> 持倉設置 -> 保证金模式 -> 选擇全倉模式", data.MarginMode)
	}

	// 解析杠杆倍數（全倉模式）
	accountLeverage := data.CrossedMarginLeverage
	if accountLeverage <= 0 {
		accountLeverage = 1 // 默认1倍
	}

	// 显示持倉模式（双向/單向）
	posModeDesc := "双向持倉"
	if data.PosMode == "one_way_mode" {
		posModeDesc = "單向持倉"
	}

	logger.Info("ℹ️ [Bitget 账戶] 保证金模式: crossed(全倉), 持倉模式: %s, 杠杆倍數: %dx, 可用餘額: %.2f %s",
		posModeDesc, accountLeverage, available, data.MarginCoin)

	return &Account{
		TotalWalletBalance: equity,
		TotalMarginBalance: equity,
		AvailableBalance:   available,
		Positions:          []*Position{}, // 持倉資訊需要單独查詢
		PosMode:            data.PosMode,
		AccountLeverage:    accountLeverage, // 添加账戶级别的杠杆倍數
	}, nil
}

// GetPositions 獲取持倉信息
func (b *BitgetAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	path := fmt.Sprintf("/api/v2/mix/position/single-position?symbol=%s&productType=%s&marginCoin=%s", b.symbol, b.productType, b.marginCoin)
	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	// 解析持倉資訊（Bitget 返回數组）
	var dataList []struct {
		MarginCoin        string `json:"marginCoin"`
		Symbol            string `json:"symbol"`
		HoldSide          string `json:"holdSide"` // "long" or "short"
		OpenDelegateCount string `json:"openDelegateCount"`
		Margin            string `json:"margin"`
		Available         string `json:"available"`
		Locked            string `json:"locked"`
		Total             string `json:"total"`
		Leverage          string `json:"leverage"`
		AchievedProfits   string `json:"achievedProfits"`
		AverageOpenPrice  string `json:"averageOpenPrice"`
		MarginMode        string `json:"marginMode"`
		PositionSide      string `json:"positionSide"`
		UnrealizedPL      string `json:"unrealizedPL"`
		LiquidationPrice  string `json:"liquidationPrice"`
		KeepMarginRate    string `json:"keepMarginRate"`
		MarkPrice         string `json:"markPrice"`
	}

	if err := json.Unmarshal(resp.Data, &dataList); err != nil {
		return nil, fmt.Errorf("解析持倉資訊失败: %w", err)
	}

	// 轉换為通用格式
	positions := make([]*Position, 0, len(dataList))
	for _, item := range dataList {
		total, _ := strconv.ParseFloat(item.Total, 64)
		if total == 0 {
			continue // 跳過空持倉
		}

		entryPrice, _ := strconv.ParseFloat(item.AverageOpenPrice, 64)
		markPrice, _ := strconv.ParseFloat(item.MarkPrice, 64)
		unrealizedPNL, _ := strconv.ParseFloat(item.UnrealizedPL, 64)
		leverage, _ := strconv.Atoi(item.Leverage)
		margin, _ := strconv.ParseFloat(item.Margin, 64)

		// Bitget 使用 holdSide 表示方向，需要轉换為正负數
		size := total
		if item.HoldSide == "short" {
			size = -total
		}

		positions = append(positions, &Position{
			Symbol:         item.Symbol,
			Size:           size,
			EntryPrice:     entryPrice,
			MarkPrice:      markPrice,
			UnrealizedPNL:  unrealizedPNL,
			Leverage:       leverage,
			MarginType:     item.MarginMode,
			IsolatedMargin: margin,
		})
	}

	return positions, nil
}

// GetBalance 獲取餘額
func (b *BitgetAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	account, err := b.GetAccount(ctx)
	if err != nil {
		return 0, err
	}
	return account.AvailableBalance, nil
}

// SetOrderMappingCallback 設置订單映射回呼
// 用於在下單成功后立即建立 orderID -> price 的映射
func (b *BitgetAdapter) SetOrderMappingCallback(callback func(orderID int64, price float64)) {
	b.orderMappingCallback = callback
}

// StartOrderStream 啟動訂單流（WebSocket）
// 架構說明：
// - 訂單流通過 main.go 中的 ex.StartOrderStream() 啟动
// - 如果價格流已經啟动，这里會複用同一個 WebSocket 连接
// - 訂單流需要订阅私有频道（orders），需要登錄认证
func (b *BitgetAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	logger.Debug("🔗 [Bitget] 啟動訂單流 WebSocket（私有频道）")

	// 轉换回呼函數
	wrappedCallback := func(update interface{}) {
		// 如果是 *OrderUpdate 指針類型，轉换為通用結構体
		if localUpdate, ok := update.(*OrderUpdate); ok {
			logger.Debug("🔍 [Bitget Adapter] 订單更新回呼触发: ID=%d, ClientOID=%s, Status=%s",
				localUpdate.OrderID, localUpdate.ClientOrderID, string(localUpdate.Status))
			genericUpdate := struct {
				OrderID       int64
				ClientOrderID string
				Symbol        string
				Side          string
				Type          string
				Status        string
				Price         float64
				Quantity      float64
				ExecutedQty   float64
				AvgPrice      float64
				UpdateTime    int64
			}{
				OrderID:       localUpdate.OrderID,
				ClientOrderID: localUpdate.ClientOrderID, // 🔥 关键：傳遞 ClientOrderID
				Symbol:        localUpdate.Symbol,
				Side:          string(localUpdate.Side),
				Type:          string(localUpdate.Type),
				Status:        string(localUpdate.Status),
				Price:         localUpdate.Price,
				Quantity:      localUpdate.Quantity,
				ExecutedQty:   localUpdate.ExecutedQty,
				AvgPrice:      localUpdate.AvgPrice,
				UpdateTime:    localUpdate.UpdateTime,
			}
			callback(genericUpdate)
		} else {
			logger.Warn("⚠️ [Bitget Adapter] 订單更新類型断言失败: %T", update)
		}
	}

	return b.wsManager.Start(ctx, b.symbol, wrappedCallback)
}

// StopOrderStream 停止訂單流
func (b *BitgetAdapter) StopOrderStream() error {
	b.wsManager.Stop()
	return nil
}

// GetLatestPrice 獲取最新價格（僅從 WebSocket 缓存读取）
// 架構說明：
// - 各组件不应直接調用此方法獲取實時價格
// - 實時價格应該通過 PriceMonitor.GetLastPrice() 獲取（订阅模式）
// - 此方法僅用於下單時的價格诊断（检查订單價格與市场價格的偏离）
// - WebSocket 是唯一的價格来源，不使用 REST API
// - 如果 WebSocket 未啟动或断开，返回錯误
func (b *BitgetAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	// 從 WebSocket 缓存读取價格
	if b.wsManager != nil {
		price := b.wsManager.GetLatestPrice()
		if price > 0 {
			return price, nil
		}
	}

	// WebSocket 未啟动或無價格數據
	return 0, fmt.Errorf("WebSocket 價格流未就绪或無價格數據")
}

// StartPriceStream 啟動價格流（WebSocket）
// 架構說明：
// - 價格流通過 PriceMonitor 在 main.go 中啟动（唯一入口）
// - 價格流和訂單流共用同一個 WebSocketManager
// - 如果只需要價格流，傳入 callback=nil 给 wsManager.Start()
func (b *BitgetAdapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	// 注册價格回呼
	b.wsManager.SetPriceCallback(func(s string, p float64) {
		// 過滤交易對
		if s == b.symbol {
			callback(p)
		}
	})

	// 如果 WebSocket 还没啟动，啟动公共频道（ticker）
	// 注意：傳入 nil 作為订單回呼，表示只订阅價格，不订阅订單
	if !b.wsManager.IsRunning() {
		logger.Debug("🔗 [Bitget] 啟動價格流 WebSocket（公共频道）")
		return b.wsManager.Start(ctx, b.symbol, nil)
	}

	logger.Debug("✅ [Bitget] 價格流回呼已注册（WebSocket已在运行）")
	return nil
}

// StartKlineStream 啟動K線流（WebSocket）
func (b *BitgetAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(candle interface{})) error {
	if b.klineWSManager == nil {
		b.klineWSManager = NewKlineWebSocketManager(b.testnet)
	}
	return b.klineWSManager.Start(ctx, symbols, interval, callback)
}

// StopKlineStream 停止K線流
func (b *BitgetAdapter) StopKlineStream() error {
	if b.klineWSManager != nil {
		b.klineWSManager.Stop()
	}
	return nil
}

// GetHistoricalKlines 獲取歷史K線數據
func (b *BitgetAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	// Bitget 支援的K線週期映射
	// 1m, 3m, 5m, 15m, 30m, 1H, 4H, 6H, 12H, 1D, 3D, 1W, 1M
	bitgetInterval := convertToBitgetInterval(interval)

	// 構建请求路径
	// limit: Bitget 最多支援 1000 根K線
	if limit > 1000 {
		limit = 1000
	}

	// 计算結束時间（當前時间）和开始時间
	endTime := time.Now().UnixMilli()

	path := fmt.Sprintf("/api/v2/mix/market/candles?symbol=%s&productType=%s&granularity=%s&limit=%d&endTime=%d",
		b.symbol, b.productType, bitgetInterval, limit, endTime)

	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("獲取歷史K線失败: %w", err)
	}

	// 解析K線數據
	// Bitget 返回格式: [[timestamp, open, high, low, close, volume, ...], ...]
	var dataList [][]string
	if err := json.Unmarshal(resp.Data, &dataList); err != nil {
		return nil, fmt.Errorf("解析K線數據失败: %w", err)
	}

	candles := make([]*Candle, 0, len(dataList))
	for _, item := range dataList {
		if len(item) < 6 {
			continue // 跳過無效數據
		}

		timestamp, _ := strconv.ParseInt(item[0], 10, 64)
		open, _ := strconv.ParseFloat(item[1], 64)
		high, _ := strconv.ParseFloat(item[2], 64)
		low, _ := strconv.ParseFloat(item[3], 64)
		close, _ := strconv.ParseFloat(item[4], 64)
		volume, _ := strconv.ParseFloat(item[5], 64)

		candles = append(candles, &Candle{
			Symbol:    symbol,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
			Timestamp: timestamp,
			IsClosed:  true, // 历史K線都是已完結的
		})
	}

	// Bitget 返回的K線是倒序的（最新的在前），需要反轉
	for i, j := 0, len(candles)-1; i < j; i, j = i+1, j-1 {
		candles[i], candles[j] = candles[j], candles[i]
	}

	return candles, nil
}

// convertToBitgetInterval 將標准K線週期轉换為 Bitget 格式
// 输入: 1m, 3m, 5m, 15m, 30m, 1h, 4h, 6h, 12h, 1d, 3d, 1w, 1M
// 输出: 1m, 3m, 5m, 15m, 30m, 1H, 4H, 6H, 12H, 1D, 3D, 1W, 1M
func convertToBitgetInterval(interval string) string {
	switch interval {
	case "1m":
		return "1m"
	case "3m":
		return "3m"
	case "5m":
		return "5m"
	case "15m":
		return "15m"
	case "30m":
		return "30m"
	case "1h":
		return "1H"
	case "4h":
		return "4H"
	case "6h":
		return "6H"
	case "12h":
		return "12H"
	case "1d":
		return "1D"
	case "3d":
		return "3D"
	case "1w":
		return "1W"
	case "1M":
		return "1M"
	default:
		return interval // 如果已經是 Bitget 格式，直接返回
	}
}

// convertToBitgetSymbol 將標准符号轉换為 Bitget 合約符号
// Bitget V2 API 使用不带后缀的符号格式（如 ETHUSDT）
func convertToBitgetSymbol(symbol string) string {
	// 去掉可能存在的 _UMCBL 后缀（相容舊配置）
	if strings.Contains(symbol, "_UMCBL") {
		return strings.TrimSuffix(symbol, "_UMCBL")
	}
	// V2 API 直接使用原始符号
	return symbol
}

// getHoldSide 根據持倉數量判断持倉方向
func getHoldSide(size float64) string {
	if size > 0 {
		return "long"
	} else if size < 0 {
		return "short"
	}
	return "none"
}

// GetPriceDecimals 獲取價格精度（小數位數）
func (b *BitgetAdapter) GetPriceDecimals() int {
	return b.pricePlace
}

// GetQuantityDecimals 獲取數量精度（小數位數）
func (b *BitgetAdapter) GetQuantityDecimals() int {
	return b.volumePlace
}

// GetBaseAsset 獲取基础资產（交易币种）
func (b *BitgetAdapter) GetBaseAsset() string {
	return b.baseAsset
}

// GetQuoteAsset 獲取计價资產（結算币种）
func (b *BitgetAdapter) GetQuoteAsset() string {
	return b.quoteAsset
}

// GetFundingRate 獲取资金费率
func (b *BitgetAdapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	// Bitget API: GET /api/v2/mix/market/current-fundRate
	// 需要轉换交易對格式
	bitgetSymbol := convertToBitgetSymbol(symbol)

	path := fmt.Sprintf("/api/v2/mix/market/current-fundRate?symbol=%s&productType=USDT-FUTURES", bitgetSymbol)

	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return 0, fmt.Errorf("獲取资金费率失败: %w", err)
	}

	// 解析响应
	var result struct {
		FundingRate string `json:"fundingRate"`
	}

	if err := json.Unmarshal(resp.Data, &result); err != nil {
		// 尝試解析數组格式（Bitget可能返回數组）
		var results []struct {
			Symbol      string `json:"symbol"`
			FundingRate string `json:"fundingRate"`
		}
		if err2 := json.Unmarshal(resp.Data, &results); err2 == nil && len(results) > 0 {
			fundingRate, err3 := strconv.ParseFloat(results[0].FundingRate, 64)
			if err3 != nil {
				return 0, fmt.Errorf("解析资金费率失败: %w", err3)
			}
			return fundingRate, nil
		}
		return 0, fmt.Errorf("解析响应失败: %w", err)
	}

	fundingRate, err := strconv.ParseFloat(result.FundingRate, 64)
	if err != nil {
		return 0, fmt.Errorf("解析资金费率失败: %w", err)
	}

	return fundingRate, nil
}

// GetSpotPrice 獲取現貨市场價格
func (b *BitgetAdapter) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	// 轉换為 Bitget 現貨格式: BTCUSDT -> BTCUSDT
	bitgetSymbol := convertToBitgetSymbol(symbol)

	// Bitget 現貨 API: GET /api/v2/spot/market/tickers
	path := fmt.Sprintf("/api/v2/spot/market/tickers?symbol=%s", bitgetSymbol)

	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return 0, fmt.Errorf("獲取現貨價格失败: %w", err)
	}

	// 解析响应
	var results []struct {
		Symbol string `json:"symbol"`
		LastPr string `json:"lastPr"`
	}

	if err := json.Unmarshal(resp.Data, &results); err != nil {
		return 0, fmt.Errorf("解析响应失败: %w", err)
	}

	if len(results) == 0 {
		return 0, fmt.Errorf("未找到交易對 %s 的現貨價格", symbol)
	}

	price, err := strconv.ParseFloat(results[0].LastPr, 64)
	if err != nil {
		return 0, fmt.Errorf("解析價格失败: %w", err)
	}

	return price, nil
}

// GetOrderBook 獲取訂單簿深度
func (b *BitgetAdapter) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	// Bitget API: GET /api/mix/v1/market/depth
	path := fmt.Sprintf("/api/mix/v1/market/depth?symbol=%s&productType=%s&limit=%d", symbol, b.productType, limit)

	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("獲取訂單簿深度失败: %w", err)
	}

	// 解析响应
	var depthData struct {
		Asks [][]string `json:"asks"` // [[價格, 數量], ...]
		Bids [][]string `json:"bids"` // [[價格, 數量], ...]
		TS   int64      `json:"ts"`   // 時间戳（毫秒）
	}

	if err := json.Unmarshal(resp.Data, &depthData); err != nil {
		return nil, fmt.Errorf("解析订單簿數據失败: %w", err)
	}

	// 轉换買盘數據（價格從高到低）
	bids := make([]OrderBookLevel, 0, len(depthData.Bids))
	for _, bid := range depthData.Bids {
		if len(bid) < 2 {
			continue
		}
		price, err := strconv.ParseFloat(bid[0], 64)
		if err != nil {
			logger.Warn("⚠️ [Bitget] 订單簿買盘價格解析失败: %v", err)
			continue
		}
		quantity, err := strconv.ParseFloat(bid[1], 64)
		if err != nil {
			logger.Warn("⚠️ [Bitget] 订單簿買盘數量解析失败: %v", err)
			continue
		}
		bids = append(bids, OrderBookLevel{
			Price:    price,
			Quantity: quantity,
		})
	}

	// 轉换賣盘數據（價格從低到高）
	asks := make([]OrderBookLevel, 0, len(depthData.Asks))
	for _, ask := range depthData.Asks {
		if len(ask) < 2 {
			continue
		}
		price, err := strconv.ParseFloat(ask[0], 64)
		if err != nil {
			logger.Warn("⚠️ [Bitget] 订單簿賣盘價格解析失败: %v", err)
			continue
		}
		quantity, err := strconv.ParseFloat(ask[1], 64)
		if err != nil {
			logger.Warn("⚠️ [Bitget] 订單簿賣盘數量解析失败: %v", err)
			continue
		}
		asks = append(asks, OrderBookLevel{
			Price:    price,
			Quantity: quantity,
		})
	}

	return &OrderBook{
		Symbol:    symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: depthData.TS,
	}, nil
}

// InternalTransfer 交易所內部轉帳（Bitget 暂未實現）
func (b *BitgetAdapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return "", fmt.Errorf("internal transfer not implemented for Bitget")
}
