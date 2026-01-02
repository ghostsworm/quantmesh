package huobi

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"
)

// 为了避免循环导入，在这里定义需要的类型
type Side string
type OrderType string
type OrderStatus string
type TimeInForce string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

const (
	OrderTypeLimit  OrderType = "limit"
	OrderTypeMarket OrderType = "market"
)

const (
	OrderStatusNew             OrderStatus = "3" // 未成交
	OrderStatusPartiallyFilled OrderStatus = "4" // 部分成交
	OrderStatusFilled          OrderStatus = "6" // 完全成交
	OrderStatusCanceled        OrderStatus = "7" // 已撤销
	OrderStatusRejected        OrderStatus = "5" // 下单失败
)

const (
	TimeInForceGTC TimeInForce = "GTC"
	TimeInForcePO  TimeInForce = "post_only"
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

// HuobiAdapter Huobi 交易所适配器
type HuobiAdapter struct {
	client           *HuobiClient
	symbol           string
	contractCode     string // Huobi 的合约代码（如 BTC-USDT）
	wsManager        *WebSocketManager
	klineWSManager   *KlineWebSocketManager
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
}

// NewHuobiAdapter 创建 Huobi 适配器
func NewHuobiAdapter(cfg map[string]string, symbol string) (*HuobiAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("Huobi API 配置不完整")
	}

	client := NewHuobiClient(apiKey, secretKey)

	// 转换交易对格式：BTCUSDT -> BTC-USDT
	contractCode := convertSymbolToContractCode(symbol)

	adapter := &HuobiAdapter{
		client:       client,
		symbol:       symbol,
		contractCode: contractCode,
	}

	// 获取合约信息
	ctxInit, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := adapter.fetchContractInfo(ctxInit); err != nil {
		logger.Warn("⚠️ [Huobi] 获取合约信息失败: %v，使用默认精度", err)
		adapter.priceDecimals = 2
		adapter.quantityDecimals = 0
	}

	return adapter, nil
}

// GetName 获取交易所名称
func (h *HuobiAdapter) GetName() string {
	return "Huobi"
}

// convertSymbolToContractCode 转换交易对格式
// BTCUSDT -> BTC-USDT
func convertSymbolToContractCode(symbol string) string {
	base := strings.TrimSuffix(symbol, "USDT")
	return fmt.Sprintf("%s-USDT", base)
}

// fetchContractInfo 获取合约信息
func (h *HuobiAdapter) fetchContractInfo(ctx context.Context) error {
	contracts, err := h.client.GetContractInfo(ctx, h.contractCode)
	if err != nil {
		return fmt.Errorf("获取合约信息失败: %w", err)
	}

	if len(contracts) == 0 {
		return fmt.Errorf("未找到合约信息: %s", h.contractCode)
	}

	contract := contracts[0]

	// 解析精度
	priceTick, _ := strconv.ParseFloat(contract.PriceTick, 64)
	h.priceDecimals = getPrecision(priceTick)
	h.quantityDecimals = 0 // Huobi 使用张数，通常为整数

	// 解析币种
	parts := strings.Split(h.contractCode, "-")
	if len(parts) == 2 {
		h.baseAsset = parts[0]
		h.quoteAsset = parts[1]
	}

	logger.Info("ℹ️ [Huobi 合约信息] %s - 价格精度:%d, 基础币种:%s, 计价币种:%s",
		h.contractCode, h.priceDecimals, h.baseAsset, h.quoteAsset)

	return nil
}

// getPrecision 根据最小变动单位计算精度
func getPrecision(value float64) int {
	str := strconv.FormatFloat(value, 'f', -1, 64)
	parts := strings.Split(str, ".")
	if len(parts) == 2 {
		return len(parts[1])
	}
	return 0
}

// PlaceOrder 下单
func (h *HuobiAdapter) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	direction := string(req.Side)
	offset := "open"
	if req.ReduceOnly {
		offset = "close"
	}

	// 构造订单请求
	orderReq := map[string]interface{}{
		"contract_code":    h.contractCode,
		"direction":        direction,
		"offset":           offset,
		"order_price_type": "limit",
		"price":            req.Price,
		"volume":           int(req.Quantity), // Huobi 使用张数
		"lever_rate":       10,                // 默认10倍杠杆
	}

	if req.PostOnly {
		orderReq["order_price_type"] = "post_only"
	}

	if req.ClientOrderID != "" {
		clientOrderID := utils.AddBrokerPrefix("huobi", req.ClientOrderID)
		orderReq["client_order_id"] = clientOrderID
	}

	resp, err := h.client.PlaceOrder(ctx, orderReq)
	if err != nil {
		return nil, err
	}

	return &Order{
		OrderID:       resp.OrderId,
		ClientOrderID: resp.ClientOrderId,
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

// BatchPlaceOrders 批量下单
func (h *HuobiAdapter) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	placedOrders := make([]*Order, 0, len(orders))
	hasMarginError := false

	for _, orderReq := range orders {
		order, err := h.PlaceOrder(ctx, orderReq)
		if err != nil {
			logger.Warn("⚠️ [Huobi] 下单失败 %.2f %s: %v",
				orderReq.Price, orderReq.Side, err)

			if strings.Contains(err.Error(), "1030") || strings.Contains(err.Error(), "insufficient") {
				hasMarginError = true
			}
			continue
		}
		placedOrders = append(placedOrders, order)
	}

	return placedOrders, hasMarginError
}

// CancelOrder 取消订单
func (h *HuobiAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	err := h.client.CancelOrder(ctx, h.contractCode, strconv.FormatInt(orderID, 10), "")
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "1061") || strings.Contains(errStr, "Order does not exist") {
			logger.Info("ℹ️ [Huobi] 订单 %d 已不存在，跳过取消", orderID)
			return nil
		}
		return err
	}

	logger.Info("✅ [Huobi] 取消订单成功: %d", orderID)
	return nil
}

// BatchCancelOrders 批量撤单
func (h *HuobiAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	if len(orderIDs) == 0 {
		return nil
	}

	for _, orderID := range orderIDs {
		if err := h.CancelOrder(ctx, symbol, orderID); err != nil {
			logger.Warn("⚠️ [Huobi] 取消订单失败 %d: %v", orderID, err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	return nil
}

// CancelAllOrders 取消所有订单
func (h *HuobiAdapter) CancelAllOrders(ctx context.Context, symbol string) error {
	orders, err := h.GetOpenOrders(ctx, symbol)
	if err != nil {
		return err
	}

	if len(orders) == 0 {
		logger.Info("ℹ️ [Huobi] 没有未完成订单")
		return nil
	}

	orderIDs := make([]int64, len(orders))
	for i, order := range orders {
		orderIDs[i] = order.OrderID
	}

	return h.BatchCancelOrders(ctx, symbol, orderIDs)
}

// GetOrder 查询订单
func (h *HuobiAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	order, err := h.client.GetOrder(ctx, h.contractCode, strconv.FormatInt(orderID, 10), "")
	if err != nil {
		return nil, err
	}

	return h.convertOrder(order), nil
}

// GetOpenOrders 查询未完成订单
func (h *HuobiAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	orders, err := h.client.GetOpenOrders(ctx, h.contractCode)
	if err != nil {
		return nil, err
	}

	result := make([]*Order, 0, len(orders))
	for _, order := range orders {
		result = append(result, h.convertOrder(&order))
	}

	return result, nil
}

// convertOrder 转换订单格式
func (h *HuobiAdapter) convertOrder(order *HuobiOrder) *Order {
	var side Side
	if order.Direction == "buy" {
		side = SideBuy
	} else {
		side = SideSell
	}

	return &Order{
		OrderID:       order.OrderId,
		ClientOrderID: order.ClientOrderId,
		Symbol:        h.symbol,
		Side:          side,
		Type:          OrderTypeLimit,
		Price:         order.Price,
		Quantity:      order.Volume,
		ExecutedQty:   order.TradeVolume,
		AvgPrice:      order.TradeAvgPrice,
		Status:        OrderStatus(strconv.Itoa(order.Status)),
		UpdateTime:    order.CreatedAt,
	}
}

// GetAccount 获取账户信息
func (h *HuobiAdapter) GetAccount(ctx context.Context) (*Account, error) {
	accounts, err := h.client.GetAccountInfo(ctx, h.contractCode)
	if err != nil {
		return nil, err
	}

	if len(accounts) == 0 {
		return &Account{
			TotalWalletBalance: 0,
			TotalMarginBalance: 0,
			AvailableBalance:   0,
			Positions:          []*Position{},
		}, nil
	}

	account := accounts[0]

	positions, err := h.GetPositions(ctx, h.symbol)
	if err != nil {
		logger.Warn("⚠️ [Huobi] 获取持仓失败: %v", err)
		positions = []*Position{}
	}

	return &Account{
		TotalWalletBalance: account.MarginBalance,
		TotalMarginBalance: account.MarginBalance,
		AvailableBalance:   account.MarginAvailable,
		Positions:          positions,
	}, nil
}

// GetPositions 获取持仓信息
func (h *HuobiAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	positions, err := h.client.GetPositionInfo(ctx, h.contractCode)
	if err != nil {
		return nil, err
	}

	result := make([]*Position, 0)
	for _, pos := range positions {
		if pos.Volume == 0 {
			continue
		}

		size := pos.Volume
		if pos.Direction == "sell" {
			size = -size
		}

		result = append(result, &Position{
			Symbol:         h.symbol,
			Size:           size,
			EntryPrice:     pos.CostOpen,
			MarkPrice:      pos.CostHold,
			UnrealizedPNL:  pos.ProfitUnreal,
			Leverage:       pos.LeverRate,
			MarginType:     "cross",
			IsolatedMargin: 0,
		})
	}

	return result, nil
}

// GetBalance 获取余额
func (h *HuobiAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	account, err := h.GetAccount(ctx)
	if err != nil {
		return 0, err
	}
	return account.AvailableBalance, nil
}

// StartOrderStream 启动订单流
func (h *HuobiAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	if h.wsManager == nil {
		h.wsManager = NewWebSocketManager(h.client.apiKey, h.client.secretKey)
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

	return h.wsManager.Start(ctx, h.contractCode, localCallback)
}

// StopOrderStream 停止订单流
func (h *HuobiAdapter) StopOrderStream() error {
	if h.wsManager != nil {
		h.wsManager.Stop()
	}
	return nil
}

// GetLatestPrice 获取最新价格
func (h *HuobiAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	if h.wsManager != nil {
		price := h.wsManager.GetLatestPrice()
		if price > 0 {
			return price, nil
		}
	}

	return 0, fmt.Errorf("WebSocket 价格流未就绪或无价格数据")
}

// StartPriceStream 启动价格流
func (h *HuobiAdapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	if h.wsManager == nil {
		h.wsManager = NewWebSocketManager(h.client.apiKey, h.client.secretKey)
	}
	return h.wsManager.StartPriceStream(ctx, h.contractCode, callback)
}

// StartKlineStream 启动K线流
func (h *HuobiAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	if h.klineWSManager == nil {
		h.klineWSManager = NewKlineWebSocketManager()
	}

	contractCodes := make([]string, len(symbols))
	for i, sym := range symbols {
		contractCodes[i] = convertSymbolToContractCode(sym)
	}

	return h.klineWSManager.Start(ctx, contractCodes, interval, callback)
}

// StopKlineStream 停止K线流
func (h *HuobiAdapter) StopKlineStream() error {
	if h.klineWSManager != nil {
		h.klineWSManager.Stop()
	}
	return nil
}

// GetHistoricalKlines 获取历史K线数据
func (h *HuobiAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	klines, err := h.client.GetKlines(ctx, h.contractCode, interval, limit)
	if err != nil {
		return nil, fmt.Errorf("获取历史K线失败: %w", err)
	}

	candles := make([]*Candle, 0, len(klines))
	for _, k := range klines {
		candles = append(candles, &Candle{
			Symbol:    symbol,
			Open:      k.Open,
			High:      k.High,
			Low:       k.Low,
			Close:     k.Close,
			Volume:    k.Vol,
			Timestamp: k.Id * 1000,
			IsClosed:  true,
		})
	}

	return candles, nil
}

// GetPriceDecimals 获取价格精度
func (h *HuobiAdapter) GetPriceDecimals() int {
	return h.priceDecimals
}

// GetQuantityDecimals 获取数量精度
func (h *HuobiAdapter) GetQuantityDecimals() int {
	return h.quantityDecimals
}

// GetBaseAsset 获取基础资产
func (h *HuobiAdapter) GetBaseAsset() string {
	return h.baseAsset
}

// GetQuoteAsset 获取计价资产
func (h *HuobiAdapter) GetQuoteAsset() string {
	return h.quoteAsset
}

// GetFundingRate 获取资金费率
func (h *HuobiAdapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	fundingRate, err := h.client.GetFundingRate(ctx, h.contractCode)
	if err != nil {
		return 0, fmt.Errorf("获取资金费率失败: %w", err)
	}

	rate, _ := strconv.ParseFloat(fundingRate.FundingRate, 64)
	return rate, nil
}
