package whitebit

import (
	"context"
	"fmt"
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

// AdapterOrder 适配器层订单信息（使用client.Order转换）
type AdapterOrder struct {
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

// AdapterPosition 适配器层持仓信息（使用client.Position转换）
type AdapterPosition struct {
	Symbol         string
	Size           float64
	EntryPrice     float64
	MarkPrice      float64
	UnrealizedPNL  float64
	Leverage       int
	MarginType     string
	IsolatedMargin float64
}

// AdapterAccount 适配器层账户信息
type AdapterAccount struct {
	TotalWalletBalance float64
	TotalMarginBalance float64
	AvailableBalance   float64
	Positions          []*AdapterPosition
}

// AdapterOrderUpdate WebSocket订单更新（使用websocket.OrderUpdate转换）
type AdapterOrderUpdate struct {
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

// AdapterOrderBookLevel 订单簿档位
type AdapterOrderBookLevel struct {
	Price    float64
	Quantity float64
}

// AdapterOrderBook 订单簿（使用client.OrderBook转换）
type AdapterOrderBook struct {
	Symbol    string
	Bids      []AdapterOrderBookLevel
	Asks      []AdapterOrderBookLevel
	Timestamp int64
}

// WhiteBITAdapter WhiteBIT 交易所适配器
type WhiteBITAdapter struct {
	client           *WhiteBITClient
	symbol           string
	market           string // WhiteBIT市场标识（如 BTC_PERP）
	wsManager        *WebSocketManager
	klineWSManager   *KlineWebSocketManager
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
	useTestnet       bool
}

// NewWhiteBITAdapter 創建 WhiteBIT 适配器
func NewWhiteBITAdapter(cfg map[string]string, symbol string) (*WhiteBITAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]
	testnetStr := cfg["testnet"]

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("WhiteBIT API 配置不完整")
	}

	useTestnet := false
	if testnetStr == "true" {
		useTestnet = true
		logger.Info("🌐 [WhiteBIT] 使用测试模式（注意：WhiteBIT没有测试网）")
	}

	client := NewWhiteBITClient(apiKey, secretKey, useTestnet)

	// 转换交易对格式：BTCUSDT -> BTC_PERP
	market := convertSymbolToMarket(symbol)

	adapter := &WhiteBITAdapter{
		client:     client,
		symbol:     symbol,
		market:     market,
		useTestnet: useTestnet,
	}

	// 获取合约信息
	ctxInit, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := adapter.fetchMarketInfo(ctxInit); err != nil {
		logger.Warn("⚠️ [WhiteBIT] 獲取市场信息失败: %v，使用默认精度", err)
		adapter.priceDecimals = 2
		adapter.quantityDecimals = 3
		adapter.baseAsset = "BTC"
		adapter.quoteAsset = "USDT"
	}

	return adapter, nil
}

// convertSymbolToMarket 轉换交易對格式
// BTCUSDT -> BTC_PERP
// ETHUSDT -> ETH_PERP
func convertSymbolToMarket(symbol string) string {
	// 移除 USDT 后缀
	base := strings.TrimSuffix(symbol, "USDT")
	return fmt.Sprintf("%s_PERP", base)
}

// fetchMarketInfo 獲取市场信息
func (w *WhiteBITAdapter) fetchMarketInfo(ctx context.Context) error {
	// 获取期货市场列表
	futuresMarkets, err := w.client.GetFuturesMarkets(ctx)
	if err != nil {
		return fmt.Errorf("獲取期货市场列表失败: %w", err)
	}

	// 查找对应的市场
	var marketInfo *FuturesMarket
	for i := range futuresMarkets {
		if futuresMarkets[i].TickerID == w.market {
			marketInfo = &futuresMarkets[i]
			break
		}
	}

	if marketInfo == nil {
		return fmt.Errorf("未找到市场信息: %s", w.market)
	}

	// 解析精度（WhiteBIT使用字符串格式，需要从市场信息中获取）
	// 由于API返回的是字符串，我们需要从markets API获取精度信息
	markets, err := w.client.GetMarkets(ctx)
	if err == nil {
		for _, m := range markets {
			if m.Name == w.market {
				// 解析精度
				if prec, err := strconv.Atoi(m.MoneyPrec); err == nil {
					w.priceDecimals = prec
				}
				if prec, err := strconv.Atoi(m.StockPrec); err == nil {
					w.quantityDecimals = prec
				}
				w.baseAsset = m.Stock
				w.quoteAsset = m.Money
				break
			}
		}
	}

	// 如果从markets API获取失败，使用默认值
	if w.priceDecimals == 0 {
		w.priceDecimals = 2
	}
	if w.quantityDecimals == 0 {
		w.quantityDecimals = 3
	}
	if w.baseAsset == "" {
		w.baseAsset = strings.TrimSuffix(w.market, "_PERP")
	}
	if w.quoteAsset == "" {
		w.quoteAsset = "USDT"
	}

	logger.Info("ℹ️ [WhiteBIT 市场信息] %s - 數量精度:%d, 價格精度:%d, 基础币种:%s, 计價币种:%s",
		w.market, w.quantityDecimals, w.priceDecimals, w.baseAsset, w.quoteAsset)

	return nil
}

// GetName 獲取交易所名称
func (w *WhiteBITAdapter) GetName() string {
	return "WhiteBIT"
}

// GetMarketType 獲取市場類型：futures 合約
func (w *WhiteBITAdapter) GetMarketType() string {
	return "futures"
}

// GetPriceDecimals 獲取價格精度
func (w *WhiteBITAdapter) GetPriceDecimals() int {
	return w.priceDecimals
}

// GetQuantityDecimals 獲取數量精度
func (w *WhiteBITAdapter) GetQuantityDecimals() int {
	return w.quantityDecimals
}

// GetBaseAsset 獲取基础资產
func (w *WhiteBITAdapter) GetBaseAsset() string {
	return w.baseAsset
}

// GetQuoteAsset 獲取计價资產
func (w *WhiteBITAdapter) GetQuoteAsset() string {
	return w.quoteAsset
}

// convertSide 轉换交易方向
func convertSide(side Side) string {
	if side == SideBuy {
		return "buy"
	}
	return "sell"
}

// convertOrderStatus 轉换订单状态
func convertOrderStatus(status string) OrderStatus {
	switch strings.ToUpper(status) {
	case "OPEN", "NEW":
		return OrderStatusNew
	case "PARTIALLY_FILLED":
		return OrderStatusPartiallyFilled
	case "FILLED":
		return OrderStatusFilled
	case "CANCELED", "CANCELLED":
		return OrderStatusCanceled
	default:
		return OrderStatusRejected
	}
}

// PlaceOrder 下單
func (w *WhiteBITAdapter) PlaceOrder(ctx context.Context, req *OrderRequest) (*AdapterOrder, error) {
	side := convertSide(req.Side)
	amount := fmt.Sprintf("%.*f", w.quantityDecimals, req.Quantity)
	price := fmt.Sprintf("%.*f", req.PriceDecimals, req.Price)

	clientOrderID := ""
	if req.ClientOrderID != "" {
		clientOrderID = utils.AddBrokerPrefix("whitebit", req.ClientOrderID)
	}

	order, err := w.client.PlaceOrder(ctx, w.market, side, amount, price, clientOrderID, req.PostOnly, req.TimeInForce == "IOC")
	if err != nil {
		return nil, err
	}

	orderID := order.OrderID
	executedQty, _ := strconv.ParseFloat(order.DealStock, 64)
	avgPrice := 0.0
	if executedQty > 0 {
		dealMoney, _ := strconv.ParseFloat(order.DealMoney, 64)
		avgPrice = dealMoney / executedQty
	}

	return &AdapterOrder{
		OrderID:       orderID,
		ClientOrderID: order.ClientOrderID,
		Symbol:        req.Symbol,
		Side:          req.Side,
		Type:          req.Type,
		Price:         req.Price,
		Quantity:      req.Quantity,
		ExecutedQty:   executedQty,
		AvgPrice:      avgPrice,
		Status:        convertOrderStatus(order.Status),
		CreatedAt:     time.Unix(int64(order.Timestamp), 0),
		UpdateTime:    int64(order.Timestamp * 1000),
	}, nil
}

// BatchPlaceOrders 批量下單
func (w *WhiteBITAdapter) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*AdapterOrder, bool) {
	placedOrders := make([]*AdapterOrder, 0, len(orders))
	hasMarginError := false

	// WhiteBIT支持批量下单（最多20个）
	if len(orders) <= 20 {
		bulkOrders := make([]BulkOrderRequest, 0, len(orders))
		for _, orderReq := range orders {
			side := convertSide(orderReq.Side)
			amount := fmt.Sprintf("%.*f", w.quantityDecimals, orderReq.Quantity)
			price := fmt.Sprintf("%.*f", orderReq.PriceDecimals, orderReq.Price)

			clientOrderID := ""
			if orderReq.ClientOrderID != "" {
				clientOrderID = utils.AddBrokerPrefix("whitebit", orderReq.ClientOrderID)
			}

			bulkOrders = append(bulkOrders, BulkOrderRequest{
				Side:         side,
				Amount:       amount,
				Price:        price,
				Market:       w.market,
				PostOnly:     orderReq.PostOnly,
				IOC:          orderReq.TimeInForce == "IOC",
				ClientOrderID: clientOrderID,
			})
		}

		results, err := w.client.BatchPlaceOrders(ctx, bulkOrders, false)
		if err == nil {
			for i, result := range results {
				if result.Error == nil && result.Result != nil {
					order := result.Result
					executedQty, _ := strconv.ParseFloat(order.DealStock, 64)
					avgPrice := 0.0
					if executedQty > 0 {
						dealMoney, _ := strconv.ParseFloat(order.DealMoney, 64)
						avgPrice = dealMoney / executedQty
					}

					placedOrders = append(placedOrders, &AdapterOrder{
						OrderID:       order.OrderID,
						ClientOrderID: order.ClientOrderID,
						Symbol:        orders[i].Symbol,
						Side:          orders[i].Side,
						Type:          orders[i].Type,
						Price:         orders[i].Price,
						Quantity:      orders[i].Quantity,
						ExecutedQty:   executedQty,
						AvgPrice:      avgPrice,
						Status:        convertOrderStatus(order.Status),
						CreatedAt:     time.Unix(int64(order.Timestamp), 0),
						UpdateTime:    int64(order.Timestamp * 1000),
					})
				} else if result.Error != nil {
					errMsg := fmt.Sprintf("%v", result.Error)
					if strings.Contains(errMsg, "balance") || strings.Contains(errMsg, "margin") {
						hasMarginError = true
					}
				}
			}
			return placedOrders, hasMarginError
		}
	}

	// 如果批量下单失败，回退到逐个下单
	for _, orderReq := range orders {
		order, err := w.PlaceOrder(ctx, orderReq)
		if err != nil {
			logger.Warn("⚠️ [WhiteBIT] 下單失败 %.2f %s: %v",
				orderReq.Price, orderReq.Side, err)

			if strings.Contains(err.Error(), "balance") || strings.Contains(err.Error(), "margin") {
				hasMarginError = true
			}
			continue
		}
		placedOrders = append(placedOrders, order)
	}

	return placedOrders, hasMarginError
}

// CancelOrder 取消訂單
func (w *WhiteBITAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	order, err := w.client.CancelOrder(ctx, w.market, orderID, "")
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "不存在") {
			logger.Info("ℹ️ [WhiteBIT] 订單 %d 已不存在，跳過取消", orderID)
			return nil
		}
		return err
	}

	// 检查订单是否已取消
	if order.Status == "CANCELED" || order.Status == "CANCELLED" {
		return nil
	}

	return nil
}

// BatchCancelOrders 批量取消訂單
func (w *WhiteBITAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	// WhiteBIT不支持批量取消，逐个取消
	for _, orderID := range orderIDs {
		if err := w.CancelOrder(ctx, symbol, orderID); err != nil {
			logger.Warn("⚠️ [WhiteBIT] 取消訂單 %d 失败: %v", orderID, err)
		}
	}
	return nil
}

// CancelAllOrders 取消所有訂單
func (w *WhiteBITAdapter) CancelAllOrders(ctx context.Context, symbol string) error {
	// WhiteBIT支持取消所有订单
	orderTypes := []string{"Futures", "Margin", "Spot"}
	return w.client.CancelAllOrders(ctx, w.market, orderTypes)
}

// GetOrder 查詢訂單
func (w *WhiteBITAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*AdapterOrder, error) {
	order, err := w.client.GetOrder(ctx, w.market, orderID, "")
	if err != nil {
		return nil, err
	}

	quantity, _ := strconv.ParseFloat(order.Amount, 64)
	price, _ := strconv.ParseFloat(order.Price, 64)
	executedQty, _ := strconv.ParseFloat(order.DealStock, 64)
	avgPrice := 0.0
	if executedQty > 0 {
		dealMoney, _ := strconv.ParseFloat(order.DealMoney, 64)
		avgPrice = dealMoney / executedQty
	}

	var side Side
	if order.Side == "buy" {
		side = SideBuy
	} else {
		side = SideSell
	}

	var orderType OrderType
	if order.Type == "limit" {
		orderType = OrderTypeLimit
	} else {
		orderType = OrderTypeMarket
	}

	return &AdapterOrder{
		OrderID:       order.OrderID,
		ClientOrderID: order.ClientOrderID,
		Symbol:        symbol,
		Side:          side,
		Type:          orderType,
		Price:         price,
		Quantity:      quantity,
		ExecutedQty:   executedQty,
		AvgPrice:      avgPrice,
		Status:        convertOrderStatus(order.Status),
		CreatedAt:     time.Unix(int64(order.Timestamp), 0),
		UpdateTime:    int64(order.Timestamp * 1000),
	}, nil
}

// GetOpenOrders 查詢未完成訂單
func (w *WhiteBITAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*AdapterOrder, error) {
	orders, err := w.client.GetOpenOrders(ctx, w.market, 100, 0)
	if err != nil {
		return nil, err
	}

	result := make([]*AdapterOrder, 0, len(orders))
	for _, o := range orders {
		quantity, _ := strconv.ParseFloat(o.Amount, 64)
		price, _ := strconv.ParseFloat(o.Price, 64)
		executedQty, _ := strconv.ParseFloat(o.DealStock, 64)

		var side Side
		if o.Side == "buy" || o.Side == "2" {
			side = SideBuy
		} else {
			side = SideSell
		}

		var orderType OrderType
		if o.Type == "limit" || o.Type == "1" {
			orderType = OrderTypeLimit
		} else {
			orderType = OrderTypeMarket
		}

		result = append(result, &AdapterOrder{
			OrderID:       o.OrderID,
			ClientOrderID: o.ClientOrderID,
			Symbol:        symbol,
			Side:          side,
			Type:          orderType,
			Price:         price,
			Quantity:      quantity,
			ExecutedQty:   executedQty,
			Status:        convertOrderStatus(o.Status),
			CreatedAt:     time.Unix(int64(o.Timestamp), 0),
			UpdateTime:    int64(o.Timestamp * 1000),
		})
	}

	return result, nil
}

// GetBalance 獲取餘額
func (w *WhiteBITAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	balances, err := w.client.GetCollateralBalance(ctx, asset)
	if err != nil {
		return 0, err
	}

	if balance, ok := balances[asset]; ok {
		available, _ := strconv.ParseFloat(balance.AvailableWithBorrow, 64)
		return available, nil
	}

	return 0, fmt.Errorf("未找到资产余额: %s", asset)
}

// GetAccount 獲取帳戶信息
func (w *WhiteBITAdapter) GetAccount(ctx context.Context) (*AdapterAccount, error) {
	// 获取保证金账户余额
	balances, err := w.client.GetCollateralBalance(ctx, "")
	if err != nil {
		return nil, err
	}

	// 计算总余额（USDT）
	totalBalance := 0.0
	availableBalance := 0.0

	if usdtBalance, ok := balances["USDT"]; ok {
		totalBalance, _ = strconv.ParseFloat(usdtBalance.Balance, 64)
		availableBalance, _ = strconv.ParseFloat(usdtBalance.AvailableWithBorrow, 64)
	}

	// 获取持仓
	positions, err := w.GetPositions(ctx, w.symbol)
	if err != nil {
		logger.Warn("⚠️ [WhiteBIT] 獲取持倉失败: %v", err)
		positions = []*AdapterPosition{}
	}

	adapterPositions := make([]*AdapterPosition, len(positions))
	for i, pos := range positions {
		adapterPositions[i] = &AdapterPosition{
			Symbol:         pos.Symbol,
			Size:           pos.Size,
			EntryPrice:     pos.EntryPrice,
			MarkPrice:      pos.MarkPrice,
			UnrealizedPNL:  pos.UnrealizedPNL,
			Leverage:       pos.Leverage,
			MarginType:     pos.MarginType,
			IsolatedMargin: pos.IsolatedMargin,
		}
	}

	return &AdapterAccount{
		TotalWalletBalance: totalBalance,
		TotalMarginBalance: totalBalance,
		AvailableBalance:   availableBalance,
		Positions:          adapterPositions,
	}, nil
}

// GetPositions 獲取持倉信息
func (w *WhiteBITAdapter) GetPositions(ctx context.Context, symbol string) ([]*AdapterPosition, error) {
	positions, err := w.client.GetPositions(ctx, w.market)
	if err != nil {
		return nil, err
	}

	result := make([]*AdapterPosition, 0, len(positions))
	for _, p := range positions {
		size, _ := strconv.ParseFloat(p.Amount, 64)
		entryPrice, _ := strconv.ParseFloat(p.BasePrice, 64)
		markPrice := entryPrice // WhiteBIT可能不返回标记价格，使用开仓价
		unrealizedPnL, _ := strconv.ParseFloat(p.PnL, 64)

		result = append(result, &AdapterPosition{
			Symbol:         symbol,
			Size:           size,
			EntryPrice:     entryPrice,
			MarkPrice:      markPrice,
			UnrealizedPNL:  unrealizedPnL,
			Leverage:       1, // WhiteBIT杠杆需要单独查询
			MarginType:     "cross",
			IsolatedMargin: 0,
		})
	}

	return result, nil
}

// FundingInfo 資金費率詳情（供 exchange wrapper 轉換）
type FundingInfo struct {
	Symbol          string
	Rate            float64
	NextFundingTime time.Time
	MarkPrice       float64
	IndexPrice      float64
}

func parseWhiteBITNextFundingTimestamp(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty next_funding_rate_timestamp")
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		if f > 1e12 {
			return time.UnixMilli(int64(f)).UTC(), nil
		}
		if f > 1e9 {
			return time.Unix(int64(f), 0).UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse next_funding_rate_timestamp: %q", s)
}

// GetFundingInfo 從 /api/v4/public/futures 獲取 funding_rate、index_price、next_funding_rate_timestamp
func (w *WhiteBITAdapter) GetFundingInfo(ctx context.Context, symbol string) (*FundingInfo, error) {
	m, err := w.client.GetFuturesMarketByTicker(ctx, w.market)
	if err != nil {
		return nil, err
	}
	next, err := parseWhiteBITNextFundingTimestamp(m.NextFundingRateTimestamp)
	if err != nil {
		return nil, err
	}
	rate, err := strconv.ParseFloat(m.FundingRate, 64)
	if err != nil {
		return nil, fmt.Errorf("解析 funding_rate: %w", err)
	}
	mark, _ := strconv.ParseFloat(m.LastPrice, 64)
	idx, _ := strconv.ParseFloat(m.IndexPrice, 64)
	if mark == 0 {
		mark = idx
	}
	out := strings.TrimSpace(symbol)
	if out == "" {
		out = w.symbol
	}
	return &FundingInfo{
		Symbol:          out,
		Rate:            rate,
		NextFundingTime: next,
		MarkPrice:       mark,
		IndexPrice:      idx,
	}, nil
}

// GetFundingRate 獲取资金费率
func (w *WhiteBITAdapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return w.client.GetFundingRate(ctx, w.market)
}

// GetLatestPrice 獲取最新價格
func (w *WhiteBITAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	tickers, err := w.client.GetTicker(ctx, w.market)
	if err != nil {
		return 0, err
	}

	if ticker, ok := tickers[w.market]; ok {
		price, err := strconv.ParseFloat(ticker.LastPrice, 64)
		if err != nil {
			return 0, err
		}
		return price, nil
	}

	return 0, fmt.Errorf("未找到价格数据: %s", w.market)
}

// StartOrderStream 啟動訂單流
func (w *WhiteBITAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	if w.wsManager == nil {
		w.wsManager = NewWebSocketManager(w.client.apiKey, w.client.secretKey, w.client, w.useTestnet)
	}

	orderCallback := func(update WSOrderUpdate) {
		// 转换为适配器层格式
		price, _ := strconv.ParseFloat(update.Price, 64)
		quantity, _ := strconv.ParseFloat(update.Amount, 64)
		executedQty, _ := strconv.ParseFloat(update.DealStock, 64)
		dealMoney, _ := strconv.ParseFloat(update.DealMoney, 64)
		avgPrice := 0.0
		if executedQty > 0 {
			avgPrice = dealMoney / executedQty
		}
		commission, _ := strconv.ParseFloat(update.DealFee, 64)

		var side Side
		if update.Side == 2 { // 2 = buy
			side = SideBuy
		} else {
			side = SideSell
		}

		var orderType OrderType
		if update.Type == 1 { // 1 = limit
			orderType = OrderTypeLimit
		} else {
			orderType = OrderTypeMarket
		}

		adapterUpdate := AdapterOrderUpdate{
			OrderID:         update.ID,
			ClientOrderID:   update.ClientOrderID,
			Symbol:          update.Market,
			Side:            side,
			Type:            orderType,
			Status:          convertOrderStatus(update.Status),
			Price:           price,
			Quantity:        quantity,
			ExecutedQty:     executedQty,
			AvgPrice:        avgPrice,
			UpdateTime:      int64(update.Mtime * 1000),
			Commission:      commission,
			CommissionAsset: "USDT", // WhiteBIT默认使用USDT作为手续费资产
		}
		callback(adapterUpdate)
	}

	return w.wsManager.Start(ctx, w.market, orderCallback)
}

// StopOrderStream 停止訂單流
func (w *WhiteBITAdapter) StopOrderStream() error {
	if w.wsManager != nil {
		return w.wsManager.Stop()
	}
	return nil
}

// StartPriceStream 啟動價格流
func (w *WhiteBITAdapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	if w.wsManager == nil {
		w.wsManager = NewWebSocketManager(w.client.apiKey, w.client.secretKey, w.client, w.useTestnet)
	}

	return w.wsManager.StartPriceStream(ctx, w.market, callback)
}

// StartKlineStream 啟動K線流
func (w *WhiteBITAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	// TODO: 实现K线流
	return fmt.Errorf("K线流功能待实现")
}

// StopKlineStream 停止K線流
func (w *WhiteBITAdapter) StopKlineStream() error {
	// TODO: 实现K线流停止
	return nil
}

// GetHistoricalKlines 獲取歷史K線數據
func (w *WhiteBITAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	// TODO: 实现历史K线获取
	return nil, fmt.Errorf("历史K线功能待实现")
}

// EstimateFinalOrderAmount 預估最终下單金額
func (w *WhiteBITAdapter) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	// WhiteBIT使用名义金额
	return price * quantity
}

// GetOrderFills 查詢訂單成交記錄（暂未实现）
func (w *WhiteBITAdapter) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]interface{}, error) {
	// WhiteBIT API支持查询订单成交记录，但需要进一步实现
	// 可以使用 /api/v4/trade-account/order 端点
	return nil, nil
}

// GetIncomeHistory 獲取收入歷史（暂未实现）
func (w *WhiteBITAdapter) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]interface{}, error) {
	// WhiteBIT API支持查询资金费用历史，但需要进一步实现
	// 可以使用 /api/v4/collateral-account/funding-history 端点
	return nil, nil
}

// GetSpotPrice 獲取現貨市场價格
func (w *WhiteBITAdapter) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	// WhiteBIT期货和现货价格可能不同，这里返回期货价格
	return w.GetLatestPrice(ctx, symbol)
}

// GetOrderBook 獲取訂單簿深度
func (w *WhiteBITAdapter) GetOrderBook(ctx context.Context, symbol string, limit int) (*AdapterOrderBook, error) {
	orderBook, err := w.client.GetOrderBook(ctx, w.market, limit)
	if err != nil {
		return nil, err
	}

	bids := make([]AdapterOrderBookLevel, 0, len(orderBook.Bids))
	asks := make([]AdapterOrderBookLevel, 0, len(orderBook.Asks))

	for _, bid := range orderBook.Bids {
		if len(bid) >= 2 {
			price, _ := strconv.ParseFloat(bid[0], 64)
			quantity, _ := strconv.ParseFloat(bid[1], 64)
			bids = append(bids, AdapterOrderBookLevel{Price: price, Quantity: quantity})
		}
	}

	for _, ask := range orderBook.Asks {
		if len(ask) >= 2 {
			price, _ := strconv.ParseFloat(ask[0], 64)
			quantity, _ := strconv.ParseFloat(ask[1], 64)
			asks = append(asks, AdapterOrderBookLevel{Price: price, Quantity: quantity})
		}
	}

	return &AdapterOrderBook{
		Symbol:    symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: orderBook.Timestamp,
	}, nil
}

// InternalTransfer 交易所內部轉帳
func (w *WhiteBITAdapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	// TODO: 实现内部转账
	return "", fmt.Errorf("内部转账功能待实现")
}
