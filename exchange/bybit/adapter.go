package bybit

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"
)

// 為了避免循環匯入，在这里定义需要的類型
type Side string
type OrderType string
type OrderStatus string
type TimeInForce string

const (
	SideBuy  Side = "Buy"
	SideSell Side = "Sell"
)

const (
	OrderTypeLimit  OrderType = "Limit"
	OrderTypeMarket OrderType = "Market"
)

const (
	OrderStatusNew             OrderStatus = "New"
	OrderStatusPartiallyFilled OrderStatus = "PartiallyFilled"
	OrderStatusFilled          OrderStatus = "Filled"
	OrderStatusCanceled        OrderStatus = "Cancelled"
	OrderStatusRejected        OrderStatus = "Rejected"
	OrderStatusExpired         OrderStatus = "Expired"
)

const (
	TimeInForceGTC TimeInForce = "GTC" // Good Till Cancel
	TimeInForcePO  TimeInForce = "PostOnly"
)

type OrderRequest struct {
	Symbol        string
	Side          Side
	Type          OrderType
	TimeInForce   TimeInForce
	Quantity      float64
	Price         float64
	ReduceOnly    bool
	PostOnly      bool
	PriceDecimals int
	ClientOrderID string
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

type Position = PositionInfo

type PositionInfo struct {
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

type Candle struct {
	Symbol    string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	Timestamp int64
	IsClosed  bool
}

type CandleUpdateCallback = func(candle interface{})

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

// BybitAdapter Bybit 交易所适配器
type BybitAdapter struct {
	client           *BybitClient
	symbol           string
	wsManager        *WebSocketManager
	klineWSManager   *KlineWebSocketManager
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
	useTestnet       bool
}

// NewBybitAdapter 創建 Bybit 适配器
func NewBybitAdapter(cfg map[string]string, symbol string) (*BybitAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]
	testnetStr := cfg["testnet"]

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("Bybit API 配置不完整")
	}

	useTestnet := false
	if testnetStr == "true" {
		useTestnet = true
		logger.Info("🌐 [Bybit] 使用測試網模式")
	}

	client := NewBybitClient(apiKey, secretKey, useTestnet)

	adapter := &BybitAdapter{
		client:     client,
		symbol:     symbol,
		useTestnet: useTestnet,
	}

	// 獲取合約信息
	ctxInit, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := adapter.fetchInstrumentInfo(ctxInit); err != nil {
		logger.Warn("⚠️ [Bybit] 獲取合約信息失败: %v，使用默认精度", err)
		adapter.priceDecimals = 2
		adapter.quantityDecimals = 3
	}

	return adapter, nil
}

// GetName 獲取交易所名称
func (b *BybitAdapter) GetName() string {
	return "Bybit"
}

// GetMarketType 獲取市場類型：futures 合約
func (b *BybitAdapter) GetMarketType() string {
	return "futures"
}

// fetchInstrumentInfo 獲取合約信息
func (b *BybitAdapter) fetchInstrumentInfo(ctx context.Context) error {
	instruments, err := b.client.GetInstruments(ctx, "linear", b.symbol)
	if err != nil {
		return fmt.Errorf("獲取合約信息失败: %w", err)
	}

	if len(instruments) == 0 {
		return fmt.Errorf("未找到合約信息: %s", b.symbol)
	}

	inst := instruments[0]

	// 解析精度
	tickSize, _ := strconv.ParseFloat(inst.PriceFilter.TickSize, 64)
	qtyStep, _ := strconv.ParseFloat(inst.LotSizeFilter.QtyStep, 64)

	b.priceDecimals = getPrecision(tickSize)
	b.quantityDecimals = getPrecision(qtyStep)
	b.baseAsset = inst.BaseCoin
	b.quoteAsset = inst.QuoteCoin

	logger.Info("ℹ️ [Bybit 合約信息] %s - 數量精度:%d, 價格精度:%d, 基础币种:%s, 计價币种:%s",
		b.symbol, b.quantityDecimals, b.priceDecimals, b.baseAsset, b.quoteAsset)

	return nil
}

// getPrecision 根據最小变动單位计算精度
func getPrecision(value float64) int {
	str := strconv.FormatFloat(value, 'f', -1, 64)
	parts := strings.Split(str, ".")
	if len(parts) == 2 {
		return len(parts[1])
	}
	return 0
}

// PlaceOrder 下單
func (b *BybitAdapter) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	side := string(req.Side)
	orderType := string(req.Type)

	// 确定精度
	qDec := b.quantityDecimals
	if qDec < 0 {
		qDec = 0
	}
	pDec := req.PriceDecimals
	if pDec < 0 {
		pDec = 0
	}

	// 特殊处理：如果數量過小，自动調整為最小下單量
	if req.Quantity <= 0 {
		req.Quantity = math.Pow10(-qDec)
		logger.Warn("⚠️ [Bybit] 下單數量原始值為 0，已自动調整為最小單位: %.8f", req.Quantity)
	}

	qtyStr := fmt.Sprintf("%.*f", qDec, req.Quantity)
	// 如果截断后數量為 0，也需要兜底
	q, _ := strconv.ParseFloat(qtyStr, 64)
	if q <= 0 {
		minQty := math.Pow10(-qDec)
		qtyStr = fmt.Sprintf("%.*f", qDec, minQty)
		logger.Warn("⚠️ [Bybit] 數量截断后為 0，使用最小精度兜底: %s", qtyStr)
	}

	// 構造订單请求
	orderReq := map[string]interface{}{
		"category":  "linear",
		"symbol":    req.Symbol,
		"side":      side,
		"orderType": orderType,
		"qty":       qtyStr,
		"price":     fmt.Sprintf("%.*f", pDec, req.Price),
	}

	// 設置 TimeInForce
	if req.PostOnly {
		orderReq["timeInForce"] = "PostOnly"
	} else {
		orderReq["timeInForce"] = "GTC"
	}

	// 設置自定义订單ID
	if req.ClientOrderID != "" {
		clientOrderID := utils.AddBrokerPrefix("bybit", req.ClientOrderID)
		orderReq["orderLinkId"] = clientOrderID
	}

	// 設置 ReduceOnly
	if req.ReduceOnly {
		orderReq["reduceOnly"] = true
	}

	resp, err := b.client.PlaceOrder(ctx, orderReq)
	if err != nil {
		return nil, err
	}

	orderID, _ := strconv.ParseInt(resp.OrderId, 10, 64)

	return &Order{
		OrderID:       orderID,
		ClientOrderID: resp.OrderLinkId,
		Symbol:        req.Symbol,
		Side:          req.Side,
		Type:          req.Type,
		Price:         req.Price,
		Quantity:      req.Quantity,
		Status:        OrderStatusNew,
		CreatedAt:     time.Now(),
		UpdateTime:    time.Now().UnixMilli(),
	}, nil
}

// BatchPlaceOrders 批量下單
func (b *BybitAdapter) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	placedOrders := make([]*Order, 0, len(orders))
	hasMarginError := false

	// Bybit 支援批量下單，但為了简化實現，先使用循环
	for _, orderReq := range orders {
		order, err := b.PlaceOrder(ctx, orderReq)
		if err != nil {
			logger.Warn("⚠️ [Bybit] 下單失败 %.2f %s: %v",
				orderReq.Price, orderReq.Side, err)

			if strings.Contains(err.Error(), "110007") || strings.Contains(err.Error(), "insufficient") {
				hasMarginError = true
			}
			continue
		}
		placedOrders = append(placedOrders, order)
	}

	return placedOrders, hasMarginError
}

// CancelOrder 取消訂單
func (b *BybitAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	err := b.client.CancelOrder(ctx, "linear", symbol, strconv.FormatInt(orderID, 10), "")
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "110001") || strings.Contains(errStr, "Order does not exist") {
			logger.Info("ℹ️ [Bybit] 订單 %d 已不存在，跳過取消", orderID)
			return nil
		}
		return err
	}

	logger.Info("✅ [Bybit] 取消訂單成功: %d", orderID)
	return nil
}

// BatchCancelOrders 批量撤單
func (b *BybitAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	if len(orderIDs) == 0 {
		return nil
	}

	// Bybit 批量撤單限制：最多10個
	batchSize := 10
	for i := 0; i < len(orderIDs); i += batchSize {
		end := i + batchSize
		if end > len(orderIDs) {
			end = len(orderIDs)
		}

		batch := orderIDs[i:end]

		// 逐個撤單（Bybit V5 API 批量撤單接口较複杂）
		for _, orderID := range batch {
			if err := b.CancelOrder(ctx, symbol, orderID); err != nil {
				logger.Warn("⚠️ [Bybit] 取消訂單失败 %d: %v", orderID, err)
			}
			time.Sleep(50 * time.Millisecond)
		}

		if i+batchSize < len(orderIDs) {
			time.Sleep(100 * time.Millisecond)
		}
	}

	return nil
}

// CancelAllOrders 取消所有订單
func (b *BybitAdapter) CancelAllOrders(ctx context.Context, symbol string) error {
	// 先查詢所有未完成订單
	orders, err := b.GetOpenOrders(ctx, symbol)
	if err != nil {
		return err
	}

	if len(orders) == 0 {
		logger.Info("ℹ️ [Bybit] 没有未完成订單")
		return nil
	}

	orderIDs := make([]int64, len(orders))
	for i, order := range orders {
		orderIDs[i] = order.OrderID
	}

	return b.BatchCancelOrders(ctx, symbol, orderIDs)
}

// GetOrder 查詢訂單
func (b *BybitAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	order, err := b.client.GetOrder(ctx, "linear", symbol, strconv.FormatInt(orderID, 10), "")
	if err != nil {
		return nil, err
	}

	return b.convertOrder(order), nil
}

// GetOpenOrders 查詢未完成订單
func (b *BybitAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	orders, err := b.client.GetOpenOrders(ctx, "linear", symbol)
	if err != nil {
		return nil, err
	}

	result := make([]*Order, 0, len(orders))
	for _, order := range orders {
		result = append(result, b.convertOrder(&order))
	}

	return result, nil
}

// convertOrder 轉换订單格式
func (b *BybitAdapter) convertOrder(order *BybitOrder) *Order {
	orderID, _ := strconv.ParseInt(order.OrderId, 10, 64)
	price, _ := strconv.ParseFloat(order.Price, 64)
	quantity, _ := strconv.ParseFloat(order.Qty, 64)
	executedQty, _ := strconv.ParseFloat(order.CumExecQty, 64)
	avgPrice, _ := strconv.ParseFloat(order.AvgPrice, 64)
	updateTime, _ := strconv.ParseInt(order.UpdatedTime, 10, 64)

	var side Side
	if order.Side == "Buy" {
		side = SideBuy
	} else {
		side = SideSell
	}

	return &Order{
		OrderID:       orderID,
		ClientOrderID: order.OrderLinkId,
		Symbol:        order.Symbol,
		Side:          side,
		Type:          OrderType(order.OrderType),
		Price:         price,
		Quantity:      quantity,
		ExecutedQty:   executedQty,
		AvgPrice:      avgPrice,
		Status:        OrderStatus(order.OrderStatus),
		UpdateTime:    updateTime,
	}
}

// GetAccount 獲取帳戶信息
func (b *BybitAdapter) GetAccount(ctx context.Context) (*Account, error) {
	balance, err := b.client.GetBalance(ctx, "UNIFIED")
	if err != nil {
		return nil, err
	}

	if len(balance) == 0 {
		return &Account{
			TotalWalletBalance: 0,
			TotalMarginBalance: 0,
			AvailableBalance:   0,
			Positions:          []*Position{},
		}, nil
	}

	// Bybit 返回账戶餘額
	totalBalance, _ := strconv.ParseFloat(balance[0].TotalEquity, 64)
	availBalance, _ := strconv.ParseFloat(balance[0].TotalAvailableBalance, 64)
	marginBalance, _ := strconv.ParseFloat(balance[0].TotalMarginBalance, 64)

	// 獲取持倉
	positions, err := b.GetPositions(ctx, b.symbol)
	if err != nil {
		logger.Warn("⚠️ [Bybit] 獲取持倉失败: %v", err)
		positions = []*Position{}
	}

	return &Account{
		TotalWalletBalance: totalBalance,
		TotalMarginBalance: marginBalance,
		AvailableBalance:   availBalance,
		Positions:          positions,
	}, nil
}

// GetPositions 獲取持倉信息
func (b *BybitAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	positions, err := b.client.GetPositions(ctx, "linear", symbol)
	if err != nil {
		return nil, err
	}

	result := make([]*Position, 0)
	for _, pos := range positions {
		size, _ := strconv.ParseFloat(pos.Size, 64)
		if size == 0 {
			continue
		}

		entryPrice, _ := strconv.ParseFloat(pos.AvgPrice, 64)
		markPrice, _ := strconv.ParseFloat(pos.MarkPrice, 64)
		unrealizedPNL, _ := strconv.ParseFloat(pos.UnrealisedPnl, 64)
		leverage, _ := strconv.Atoi(pos.Leverage)

		result = append(result, &Position{
			Symbol:         pos.Symbol,
			Size:           size,
			EntryPrice:     entryPrice,
			MarkPrice:      markPrice,
			UnrealizedPNL:  unrealizedPNL,
			Leverage:       leverage,
			MarginType:     pos.TradeMode,
			IsolatedMargin: 0,
		})
	}

	return result, nil
}

// GetBalance 獲取餘額
func (b *BybitAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	account, err := b.GetAccount(ctx)
	if err != nil {
		return 0, err
	}
	return account.AvailableBalance, nil
}

// StartOrderStream 啟動訂單流
func (b *BybitAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	if b.wsManager == nil {
		b.wsManager = NewWebSocketManager(b.client.apiKey, b.client.secretKey, b.useTestnet)
	}

	localCallback := func(update OrderUpdate) {
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
			OrderID:       update.OrderID,
			ClientOrderID: update.ClientOrderID,
			Symbol:        update.Symbol,
			Side:          string(update.Side),
			Type:          string(update.Type),
			Status:        string(update.Status),
			Price:         update.Price,
			Quantity:      update.Quantity,
			ExecutedQty:   update.ExecutedQty,
			AvgPrice:      update.AvgPrice,
			UpdateTime:    update.UpdateTime,
		}
		callback(genericUpdate)
	}

	return b.wsManager.Start(ctx, b.symbol, localCallback)
}

// StopOrderStream 停止訂單流
func (b *BybitAdapter) StopOrderStream() error {
	if b.wsManager != nil {
		b.wsManager.Stop()
	}
	return nil
}

// GetLatestPrice 獲取最新價格
func (b *BybitAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	if b.wsManager != nil {
		price := b.wsManager.GetLatestPrice()
		if price > 0 {
			return price, nil
		}
	}

	return 0, fmt.Errorf("WebSocket 價格流未就绪或無價格數據")
}

// StartPriceStream 啟動價格流
func (b *BybitAdapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	if b.wsManager == nil {
		b.wsManager = NewWebSocketManager(b.client.apiKey, b.client.secretKey, b.useTestnet)
	}
	return b.wsManager.StartPriceStream(ctx, symbol, callback)
}

// StartKlineStream 啟動K線流
func (b *BybitAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	if b.klineWSManager == nil {
		b.klineWSManager = NewKlineWebSocketManager(b.useTestnet)
	}

	return b.klineWSManager.Start(ctx, symbols, interval, callback)
}

// StopKlineStream 停止K線流
func (b *BybitAdapter) StopKlineStream() error {
	if b.klineWSManager != nil {
		b.klineWSManager.Stop()
	}
	return nil
}

// GetHistoricalKlines 獲取歷史K線數據
func (b *BybitAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	klines, err := b.client.GetKlines(ctx, "linear", symbol, interval, limit)
	if err != nil {
		return nil, fmt.Errorf("獲取歷史K線失败: %w", err)
	}

	candles := make([]*Candle, 0, len(klines))
	for _, k := range klines {
		timestamp, _ := strconv.ParseInt(k.StartTime, 10, 64)
		open, _ := strconv.ParseFloat(k.OpenPrice, 64)
		high, _ := strconv.ParseFloat(k.HighPrice, 64)
		low, _ := strconv.ParseFloat(k.LowPrice, 64)
		close, _ := strconv.ParseFloat(k.ClosePrice, 64)
		volume, _ := strconv.ParseFloat(k.Volume, 64)

		candles = append(candles, &Candle{
			Symbol:    symbol,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
			Timestamp: timestamp,
			IsClosed:  true,
		})
	}

	return candles, nil
}

// GetPriceDecimals 獲取價格精度
func (b *BybitAdapter) GetPriceDecimals() int {
	return b.priceDecimals
}

// GetQuantityDecimals 獲取數量精度
func (b *BybitAdapter) GetQuantityDecimals() int {
	return b.quantityDecimals
}

// GetBaseAsset 獲取基础资產
func (b *BybitAdapter) GetBaseAsset() string {
	return b.baseAsset
}

// GetQuoteAsset 獲取计價资產
func (b *BybitAdapter) GetQuoteAsset() string {
	return b.quoteAsset
}

// GetOrderBook 獲取訂單簿深度
func (b *BybitAdapter) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	// 調用 Bybit API 獲取訂單簿
	bybitOrderBook, err := b.client.GetOrderBook(ctx, "linear", symbol, limit)
	if err != nil {
		return nil, fmt.Errorf("獲取訂單簿深度失败: %w", err)
	}

	// 轉换買盘數據（價格從高到低）
	bids := make([]OrderBookLevel, 0, len(bybitOrderBook.Bids))
	for _, bid := range bybitOrderBook.Bids {
		if len(bid) < 2 {
			continue
		}
		price, err := strconv.ParseFloat(bid[0], 64)
		if err != nil {
			logger.Warn("⚠️ [Bybit] 订單簿買盘價格解析失败: %v", err)
			continue
		}
		quantity, err := strconv.ParseFloat(bid[1], 64)
		if err != nil {
			logger.Warn("⚠️ [Bybit] 订單簿買盘數量解析失败: %v", err)
			continue
		}
		bids = append(bids, OrderBookLevel{
			Price:    price,
			Quantity: quantity,
		})
	}

	// 轉换賣盘數據（價格從低到高）
	asks := make([]OrderBookLevel, 0, len(bybitOrderBook.Asks))
	for _, ask := range bybitOrderBook.Asks {
		if len(ask) < 2 {
			continue
		}
		price, err := strconv.ParseFloat(ask[0], 64)
		if err != nil {
			logger.Warn("⚠️ [Bybit] 订單簿賣盘價格解析失败: %v", err)
			continue
		}
		quantity, err := strconv.ParseFloat(ask[1], 64)
		if err != nil {
			logger.Warn("⚠️ [Bybit] 订單簿賣盘數量解析失败: %v", err)
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
		Timestamp: bybitOrderBook.TS,
	}, nil
}

// GetFundingRate 獲取资金费率
func (b *BybitAdapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	fundingRate, err := b.client.GetFundingRate(ctx, "linear", symbol)
	if err != nil {
		return 0, fmt.Errorf("獲取资金费率失败: %w", err)
	}

	rate, _ := strconv.ParseFloat(fundingRate.FundingRate, 64)
	return rate, nil
}

// GetSpotPrice 獲取現貨市场價格
func (b *BybitAdapter) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	// Bybit 現貨使用 category=spot
	ticker, err := b.client.GetTicker(ctx, "spot", symbol)
	if err != nil {
		return 0, fmt.Errorf("獲取現貨價格失败: %w", err)
	}

	price, err := strconv.ParseFloat(ticker.LastPrice, 64)
	if err != nil {
		return 0, fmt.Errorf("解析現貨價格失败: %w", err)
	}

	return price, nil
}

// InternalTransfer 交易所內部轉帳（Bybit 暂未實現）
func (b *BybitAdapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return "", fmt.Errorf("internal transfer not implemented for Bybit")
}
