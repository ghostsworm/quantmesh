package bitkub

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

// 本地类型定义，避免循环导入
type Side string
type OrderType string
type OrderStatus string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

const (
	OrderTypeLimit  OrderType = "LIMIT"
	OrderTypeMarket OrderType = "MARKET"
)

const (
	OrderStatusNew             OrderStatus = "NEW"
	OrderStatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	OrderStatusFilled          OrderStatus = "FILLED"
	OrderStatusCanceled        OrderStatus = "CANCELED"
	OrderStatusRejected        OrderStatus = "REJECTED"
)

// OrderRequest 下單请求
type OrderRequest struct {
	Symbol        string
	Side          Side
	Type          OrderType
	TimeInForce   string
	Quantity      float64
	Price         float64
	ReduceOnly    bool
	PostOnly      bool
	PriceDecimals int
	ClientOrderID string
}

// Order 订单信息
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

// Position 持仓信息（现货没有持仓）
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

// Account 账户信息
type Account struct {
	TotalWalletBalance float64
	TotalMarginBalance float64
	AvailableBalance   float64
	Positions          []*Position
}

// OrderUpdate WebSocket订单更新
type OrderUpdate struct {
	OrderID         int64
	ClientOrderID   string
	Symbol          string
	Side            Side
	Type            OrderType
	Status          OrderStatus
	Price           float64
	Quantity        float64
	ExecutedQty     float64
	AvgPrice        float64
	UpdateTime      int64
	Commission      float64
	CommissionAsset string
	RealizedPnL     float64 // 已實現盈虧（交易所計算）
}

// Candle K线数据
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

// CandleUpdateCallback K线更新回调
type CandleUpdateCallback func(candle interface{})

// OrderBookLevel 订单簿档位
type OrderBookLevel struct {
	Price    float64
	Quantity float64
}

// OrderBook 订单簿
type OrderBook struct {
	Symbol    string
	Bids      []OrderBookLevel
	Asks      []OrderBookLevel
	Timestamp int64
}

// BitkubSpotAdapter Bitkub 現貨交易所適配器
type BitkubSpotAdapter struct {
	client           *BitkubClient
	symbol           string
	bitkubSymbol     string // Bitkub格式的交易對（如 BTC_THB）
	wsManager        *WebSocketManager
	priceDecimals    int
	quantityDecimals int
	tickSize         float64
	stepSize         float64
	baseAsset        string
	quoteAsset       string
	useTestnet       bool
}

// NewBitkubSpotAdapter 創建 Bitkub 現貨适配器
func NewBitkubSpotAdapter(cfg map[string]string, symbol string) (*BitkubSpotAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]
	testnetStr := cfg["testnet"]

	useTestnet := false
	if testnetStr == "true" {
		useTestnet = true
		logger.Info("🌐 [Bitkub Spot] 使用測試模式（注意：Bitkub没有测试网）")
	}

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("Bitkub API 配置不完整")
	}

	client := NewBitkubClient(apiKey, secretKey, useTestnet)

	// 轉换交易對格式：BTCUSDT -> BTC_THB
	bitkubSymbol := convertSymbolToBitkub(symbol)

	adapter := &BitkubSpotAdapter{
		client:       client,
		symbol:       symbol,
		bitkubSymbol: bitkubSymbol,
		useTestnet:   useTestnet,
		wsManager:    NewWebSocketManager(useTestnet),
	}

	ctxInit, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := adapter.fetchSymbolInfo(ctxInit); err != nil {
		logger.Warn("⚠️ [Bitkub Spot] 獲取交易對信息失败: %v，使用默认精度", err)
		adapter.priceDecimals = 2
		adapter.quantityDecimals = 8
		adapter.baseAsset = "BTC"
		adapter.quoteAsset = "THB"
	}

	return adapter, nil
}

// convertSymbolToBitkub 轉换交易對格式
// BTCUSDT -> BTC_THB
// ETHUSDT -> ETH_THB
func convertSymbolToBitkub(symbol string) string {
	// 移除 USDT 后缀，假设Bitkub主要使用THB作为计价货币
	base := strings.TrimSuffix(symbol, "USDT")
	return fmt.Sprintf("%s_THB", base)
}

// fetchSymbolInfo 獲取交易對信息
func (b *BitkubSpotAdapter) fetchSymbolInfo(ctx context.Context) error {
	symbols, err := b.client.GetSymbols(ctx)
	if err != nil {
		return fmt.Errorf("獲取交易對列表失败: %w", err)
	}

	// 查找对应的交易對
	var symbolInfo *Symbol
	for i := range symbols {
		if strings.EqualFold(symbols[i].Symbol, b.bitkubSymbol) {
			symbolInfo = &symbols[i]
			break
		}
	}

	if symbolInfo == nil {
		return fmt.Errorf("未找到交易對信息: %s", b.bitkubSymbol)
	}

	b.priceDecimals = symbolInfo.PriceScale
	b.quantityDecimals = symbolInfo.QuantityScale
	b.baseAsset = symbolInfo.BaseAsset
	b.quoteAsset = symbolInfo.QuoteAsset

	// 解析步长
	if symbolInfo.PriceStep != "" {
		b.tickSize, _ = strconv.ParseFloat(symbolInfo.PriceStep, 64)
	}
	if symbolInfo.QuantityStep != "" {
		b.stepSize, _ = strconv.ParseFloat(symbolInfo.QuantityStep, 64)
	}

	if b.tickSize <= 0 {
		b.tickSize = math.Pow10(-b.priceDecimals)
	}
	if b.stepSize <= 0 {
		b.stepSize = math.Pow10(-b.quantityDecimals)
	}

	logger.Info("ℹ️ [Bitkub Spot] %s (%s) - 數量精度:%d, 價格精度:%d, 基础:%s, 计價:%s",
		b.symbol, b.bitkubSymbol, b.quantityDecimals, b.priceDecimals, b.baseAsset, b.quoteAsset)

	return nil
}

// roundToTickSize 按價格步長四捨五入
func (b *BitkubSpotAdapter) roundToTickSize(price float64, side Side) float64 {
	if b.tickSize <= 0 {
		return price
	}
	ticks := price / b.tickSize
	var roundedTicks float64
	if side == SideBuy {
		roundedTicks = math.Floor(ticks)
	} else {
		roundedTicks = math.Ceil(ticks)
	}
	return roundedTicks * b.tickSize
}

// roundToStepSize 按數量步長四捨五入
func (b *BitkubSpotAdapter) roundToStepSize(quantity float64) float64 {
	if b.stepSize <= 0 {
		return quantity
	}
	steps := math.Floor(quantity / b.stepSize)
	return steps * b.stepSize
}

// formatAmount 格式化金額（移除尾隨零）
func formatAmount(value float64) string {
	// Bitkub要求不能有尾隨零，如 1000.00 无效，1000 有效
	str := strconv.FormatFloat(value, 'f', -1, 64)
	// 移除尾隨零和小數點
	if strings.Contains(str, ".") {
		str = strings.TrimRight(str, "0")
		str = strings.TrimRight(str, ".")
	}
	return str
}

// GetName 獲取交易所名称
func (b *BitkubSpotAdapter) GetName() string {
	return "Bitkub Spot"
}

// GetMarketType 獲取市場類型：spot 現貨
func (b *BitkubSpotAdapter) GetMarketType() string {
	return "spot"
}

// PlaceOrder 下單
func (b *BitkubSpotAdapter) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	if req.Price <= 0 && req.Type != OrderTypeMarket {
		return nil, fmt.Errorf("無效的下單價格: %.8f", req.Price)
	}

	adjustedPrice := b.roundToTickSize(req.Price, req.Side)
	adjustedQty := b.roundToStepSize(req.Quantity)
	if adjustedQty <= 0 {
		adjustedQty = b.stepSize
		if adjustedQty <= 0 {
			adjustedQty = math.Pow10(-b.quantityDecimals)
		}
	}

	orderType := "limit"
	if req.Type == OrderTypeMarket {
		orderType = "market"
		adjustedPrice = 0 // 市價單價格為0
	}

	clientOrderID := ""
	if req.ClientOrderID != "" {
		clientOrderID = utils.AddBrokerPrefix("bitkub", req.ClientOrderID)
	}

	var orderID int64
	var executedQty float64
	var avgPrice float64
	var status OrderStatus

	if req.Side == SideBuy {
		// 買單：amt是花費的THB金額
		amt := adjustedPrice * adjustedQty
		if orderType == "market" {
			amt = adjustedQty // 市價單時，amt就是花費金額
		}

		bidReq := PlaceBidRequest{
			Sym:      strings.ToLower(b.bitkubSymbol),
			Amt:      amt,
			Rat:      adjustedPrice,
			Typ:      orderType,
			ClientID: clientOrderID,
			PostOnly: req.PostOnly,
		}

		resp, err := b.client.PlaceBid(ctx, bidReq)
		if err != nil {
			return nil, err
		}

		orderID, _ = strconv.ParseInt(resp.ID, 10, 64)
		executedQty, _ = strconv.ParseFloat(formatAmount(resp.Rec), 64) // 收到的加密貨幣數量
		avgPrice = resp.Rat
		if executedQty > 0 && amt > 0 {
			avgPrice = amt / executedQty
		}
		status = OrderStatusNew
	} else {
		// 賣單：amt是賣出的加密貨幣數量
		askReq := PlaceAskRequest{
			Sym:      strings.ToLower(b.bitkubSymbol),
			Amt:      adjustedQty,
			Rat:      adjustedPrice,
			Typ:      orderType,
			ClientID: clientOrderID,
			PostOnly: req.PostOnly,
		}

		resp, err := b.client.PlaceAsk(ctx, askReq)
		if err != nil {
			return nil, err
		}

		orderID, _ = strconv.ParseInt(resp.ID, 10, 64)
		executedQty, _ = strconv.ParseFloat(formatAmount(resp.Amt), 64)
		avgPrice = resp.Rat
		status = OrderStatusNew
	}

	return &Order{
		OrderID:       orderID,
		ClientOrderID: clientOrderID,
		Symbol:        req.Symbol,
		Side:          req.Side,
		Type:          req.Type,
		Price:         adjustedPrice,
		Quantity:      adjustedQty,
		ExecutedQty:   executedQty,
		AvgPrice:      avgPrice,
		Status:        status,
		CreatedAt:     time.Now(),
		UpdateTime:    time.Now().UnixMilli(),
	}, nil
}

// BatchPlaceOrders 批量下單
func (b *BitkubSpotAdapter) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	placed := make([]*Order, 0, len(orders))
	hasBalanceError := false
	for _, req := range orders {
		order, err := b.PlaceOrder(ctx, req)
		if err != nil {
			logger.Warn("⚠️ [Bitkub Spot] 下單失败 %.2f %s: %v", req.Price, req.Side, err)
			if strings.Contains(err.Error(), "insufficient") || strings.Contains(err.Error(), "balance") {
				hasBalanceError = true
			}
			continue
		}
		placed = append(placed, order)
	}
	return placed, hasBalanceError
}

// CancelOrder 取消訂單
func (b *BitkubSpotAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	// 需要先查詢訂單以獲取side信息
	orderInfo, err := b.GetOrder(ctx, symbol, orderID)
	if err != nil {
		// 如果訂單不存在，視為已取消
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "不存在") {
			logger.Info("ℹ️ [Bitkub Spot] 订單 %d 已不存在，跳過取消", orderID)
			return nil
		}
		return err
	}

	side := "buy"
	if orderInfo.Side == SideSell {
		side = "sell"
	}

	return b.client.CancelOrder(ctx, strings.ToLower(b.bitkubSymbol), strconv.FormatInt(orderID, 10), side)
}

// BatchCancelOrders 批量取消訂單
func (b *BitkubSpotAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	for _, orderID := range orderIDs {
		if err := b.CancelOrder(ctx, symbol, orderID); err != nil {
			logger.Warn("⚠️ [Bitkub Spot] 取消訂單 %d 失败: %v", orderID, err)
		}
	}
	return nil
}

// CancelAllOrders 取消所有訂單
func (b *BitkubSpotAdapter) CancelAllOrders(ctx context.Context, symbol string) error {
	// 查詢所有未完成訂單，然後批量取消
	openOrders, err := b.GetOpenOrders(ctx, symbol)
	if err != nil {
		return err
	}

	for _, order := range openOrders {
		if err := b.CancelOrder(ctx, symbol, order.OrderID); err != nil {
			logger.Warn("⚠️ [Bitkub Spot] 取消訂單 %d 失败: %v", order.OrderID, err)
		}
	}

	return nil
}

// GetOrder 查詢訂單
func (b *BitkubSpotAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	// 先查詢未完成訂單
	openOrders, err := b.client.GetOpenOrders(ctx, strings.ToLower(b.bitkubSymbol))
	if err == nil {
		for _, o := range openOrders {
			id, _ := strconv.ParseInt(o.ID, 10, 64)
			if id == orderID {
				return b.convertOpenOrderToOrder(&o, symbol), nil
			}
		}
	}

	// 如果未完成訂單中沒有，查詢訂單詳情（需要side信息）
	// 嘗試buy和sell兩種情況
	orderInfo, err := b.client.GetOrderInfo(ctx, strings.ToLower(b.bitkubSymbol), strconv.FormatInt(orderID, 10), "buy")
	if err != nil {
		orderInfo, err = b.client.GetOrderInfo(ctx, strings.ToLower(b.bitkubSymbol), strconv.FormatInt(orderID, 10), "sell")
		if err != nil {
			return nil, err
		}
	}

	return b.convertOrderInfoToOrder(orderInfo, symbol), nil
}

// convertOpenOrderToOrder 轉换OpenOrder為Order
func (b *BitkubSpotAdapter) convertOpenOrderToOrder(o *OpenOrder, symbol string) *Order {
	rate, _ := strconv.ParseFloat(o.Rate, 64)
	amount, _ := strconv.ParseFloat(o.Amount, 64)
	receive, _ := strconv.ParseFloat(o.Receive, 64)

	var side Side
	if o.Side == "buy" {
		side = SideBuy
	} else {
		side = SideSell
	}

	var orderType OrderType
	if o.Type == "limit" {
		orderType = OrderTypeLimit
	} else {
		orderType = OrderTypeMarket
	}

	executedQty := 0.0
	if side == SideBuy {
		executedQty = receive // 買單：receive是收到的加密貨幣
	} else {
		executedQty = amount // 賣單：amount是賣出的加密貨幣
	}

	return &Order{
		OrderID:       func() int64 { id, _ := strconv.ParseInt(o.ID, 10, 64); return id }(),
		ClientOrderID: o.ClientID,
		Symbol:        symbol,
		Side:          side,
		Type:          orderType,
		Price:         rate,
		Quantity:      amount,
		ExecutedQty:   executedQty,
		Status:        OrderStatusNew,
		CreatedAt:     time.Unix(o.TS/1000, 0),
		UpdateTime:    o.TS,
	}
}

// convertOrderInfoToOrder 轉换OrderInfo為Order
func (b *BitkubSpotAdapter) convertOrderInfoToOrder(info *OrderInfo, symbol string) *Order {
	amount, _ := strconv.ParseFloat(info.Amount, 64)
	filled, _ := strconv.ParseFloat(info.Filled, 64)

	var status OrderStatus
	switch info.Status {
	case "filled":
		status = OrderStatusFilled
	case "unfilled":
		status = OrderStatusNew
	case "cancelled":
		status = OrderStatusCanceled
	default:
		status = OrderStatusRejected
	}

	if info.PartialFilled && status == OrderStatusNew {
		status = OrderStatusPartiallyFilled
	}

	avgPrice := info.Rate
	if filled > 0 && len(info.History) > 0 {
		totalCost := 0.0
		for _, fill := range info.History {
			amt, _ := strconv.ParseFloat(fill.Amount, 64)
			totalCost += fill.Rate * amt
		}
		avgPrice = totalCost / filled
	}

	return &Order{
		OrderID:       func() int64 { id, _ := strconv.ParseInt(info.ID, 10, 64); return id }(),
		ClientOrderID: info.ClientID,
		Symbol:        symbol,
		Side:          SideBuy, // 需要從其他地方獲取，這裡假設為buy
		Type:          OrderTypeLimit,
		Price:         info.Rate,
		Quantity:      amount,
		ExecutedQty:   filled,
		AvgPrice:      avgPrice,
		Status:        status,
		CreatedAt:     time.Now(),
		UpdateTime:    time.Now().UnixMilli(),
	}
}

// GetOpenOrders 查詢未完成訂單
func (b *BitkubSpotAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	openOrders, err := b.client.GetOpenOrders(ctx, strings.ToLower(b.bitkubSymbol))
	if err != nil {
		return nil, err
	}

	result := make([]*Order, 0, len(openOrders))
	for i := range openOrders {
		result = append(result, b.convertOpenOrderToOrder(&openOrders[i], symbol))
	}

	return result, nil
}

// GetAccount 獲取帳戶信息
func (b *BitkubSpotAdapter) GetAccount(ctx context.Context) (*Account, error) {
	balances, err := b.client.GetBalances(ctx)
	if err != nil {
		return nil, err
	}

	// 計算總餘額（THB）
	totalBalance := 0.0
	availableBalance := 0.0

	if thbBalance, ok := balances["THB"]; ok {
		totalBalance = thbBalance.Available + thbBalance.Reserved
		availableBalance = thbBalance.Available
	}

	return &Account{
		TotalWalletBalance: totalBalance,
		TotalMarginBalance: totalBalance,
		AvailableBalance:   availableBalance,
		Positions:          []*Position{}, // 現貨沒有持倉
	}, nil
}

// GetPositions 獲取持倉信息（現貨沒有持倉）
func (b *BitkubSpotAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	return []*Position{}, nil
}

// GetBalance 獲取餘額
func (b *BitkubSpotAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	balances, err := b.client.GetBalances(ctx)
	if err != nil {
		return 0, err
	}

	if balance, ok := balances[asset]; ok {
		return balance.Available, nil
	}

	return 0, fmt.Errorf("未找到资产余额: %s", asset)
}

// StartOrderStream 啟動訂單流（Bitkub現貨不支持）
func (b *BitkubSpotAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return fmt.Errorf("Bitkub現貨不支持訂單流")
}

// StopOrderStream 停止訂單流
func (b *BitkubSpotAdapter) StopOrderStream() error {
	return nil
}

// GetLatestPrice 獲取最新價格
func (b *BitkubSpotAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	ticker, err := b.client.GetTicker(ctx, strings.ToLower(b.bitkubSymbol))
	if err != nil {
		return 0, err
	}

	price, err := strconv.ParseFloat(ticker.Last, 64)
	if err != nil {
		return 0, err
	}

	return price, nil
}

// StartPriceStream 啟動價格流
func (b *BitkubSpotAdapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return b.wsManager.StartPriceStream(ctx, b.bitkubSymbol, callback)
}

// StartKlineStream 啟動K線流（Bitkub現貨不支持）
func (b *BitkubSpotAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	return fmt.Errorf("Bitkub現貨K線流功能待实现")
}

// StopKlineStream 停止K線流
func (b *BitkubSpotAdapter) StopKlineStream() error {
	return b.wsManager.Stop()
}

// GetHistoricalKlines 獲取歷史K線數據（Bitkub現貨不支持）
func (b *BitkubSpotAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	return nil, fmt.Errorf("Bitkub現貨歷史K線功能待实现")
}

// GetPriceDecimals 獲取價格精度
func (b *BitkubSpotAdapter) GetPriceDecimals() int {
	return b.priceDecimals
}

// GetQuantityDecimals 獲取數量精度
func (b *BitkubSpotAdapter) GetQuantityDecimals() int {
	return b.quantityDecimals
}

// GetBaseAsset 獲取基础资產
func (b *BitkubSpotAdapter) GetBaseAsset() string {
	return b.baseAsset
}

// GetQuoteAsset 獲取计價资產
func (b *BitkubSpotAdapter) GetQuoteAsset() string {
	return b.quoteAsset
}

// GetFundingRate 獲取资金费率（現貨沒有資金費率）
func (b *BitkubSpotAdapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return 0, fmt.Errorf("現貨交易沒有資金費率")
}

// GetSpotPrice 獲取現貨市场價格
func (b *BitkubSpotAdapter) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return b.GetLatestPrice(ctx, symbol)
}

// EstimateFinalOrderAmount 預估最终下單金額
func (b *BitkubSpotAdapter) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return price * quantity
}

// GetOrderBook 獲取訂單簿深度（Bitkub現貨不支持）
func (b *BitkubSpotAdapter) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	return nil, fmt.Errorf("Bitkub現貨訂單簿功能待实现")
}

// GetOrderFills 查詢訂單成交記錄
func (b *BitkubSpotAdapter) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]interface{}, error) {
	orderInfo, err := b.client.GetOrderInfo(ctx, strings.ToLower(b.bitkubSymbol), strconv.FormatInt(orderID, 10), "buy")
	if err != nil {
		orderInfo, err = b.client.GetOrderInfo(ctx, strings.ToLower(b.bitkubSymbol), strconv.FormatInt(orderID, 10), "sell")
		if err != nil {
			return nil, err
		}
	}

	// 轉换成交記錄
	fills := make([]interface{}, len(orderInfo.History))
	for i, fill := range orderInfo.History {
		fills[i] = fill
	}

	return fills, nil
}

// GetIncomeHistory 獲取收入歷史（現貨沒有）
func (b *BitkubSpotAdapter) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]interface{}, error) {
	return nil, nil
}

// InternalTransfer 交易所內部轉帳（Bitkub現貨不支持）
func (b *BitkubSpotAdapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return "", fmt.Errorf("Bitkub現貨內部轉帳功能待实现")
}
