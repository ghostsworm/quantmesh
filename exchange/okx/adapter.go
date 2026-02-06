package okx

import (
	"context"
	"fmt"
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
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

const (
	OrderTypeLimit  OrderType = "limit"
	OrderTypeMarket OrderType = "market"
)

const (
	OrderStatusNew             OrderStatus = "live"
	OrderStatusPartiallyFilled OrderStatus = "partially_filled"
	OrderStatusFilled          OrderStatus = "filled"
	OrderStatusCanceled        OrderStatus = "canceled"
	OrderStatusRejected        OrderStatus = "rejected"
	OrderStatusExpired         OrderStatus = "expired"
)

const (
	TimeInForceGTC TimeInForce = "GTC" // Good Till Cancel
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
	Commission      float64 // 本次成交手續費
	CommissionAsset string  // 手續費幣種
	RealizedPnL     float64 // 已實現盈虧（交易所計算）
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

// OKXAdapter OKX 交易所适配器
type OKXAdapter struct {
	client           *OKXClient
	symbol           string
	instId           string // OKX 的合約標识（如 BTC-USDT-SWAP）
	wsManager        *WebSocketManager
	klineWSManager   *KlineWebSocketManager
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
	useTestnet       bool
}

// NewOKXAdapter 創建 OKX 适配器
func NewOKXAdapter(cfg map[string]string, symbol string) (*OKXAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]
	passphrase := cfg["passphrase"]
	testnetStr := cfg["testnet"]

	if apiKey == "" || secretKey == "" || passphrase == "" {
		return nil, fmt.Errorf("OKX API 配置不完整")
	}

	useTestnet := false
	if testnetStr == "true" {
		useTestnet = true
		logger.Info("🌐 [OKX] 使用模拟盘模式")
	}

	client := NewOKXClient(apiKey, secretKey, passphrase, useTestnet)

	// 轉换交易對格式：BTCUSDT -> BTC-USDT-SWAP
	instId := convertSymbolToInstId(symbol)

	adapter := &OKXAdapter{
		client:     client,
		symbol:     symbol,
		instId:     instId,
		useTestnet: useTestnet,
	}

	// 獲取合約信息
	ctxInit, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := adapter.fetchInstrumentInfo(ctxInit); err != nil {
		logger.Warn("⚠️ [OKX] 獲取合約信息失败: %v，使用默认精度", err)
		adapter.priceDecimals = 2
		adapter.quantityDecimals = 3
	}

	return adapter, nil
}

// GetName 獲取交易所名称
func (o *OKXAdapter) GetName() string {
	return "OKX"
}

// GetMarketType 獲取市場類型：futures 合約
func (o *OKXAdapter) GetMarketType() string {
	return "futures"
}

// convertSymbolToInstId 轉换交易對格式
// BTCUSDT -> BTC-USDT-SWAP
// ETHUSDT -> ETH-USDT-SWAP
func convertSymbolToInstId(symbol string) string {
	// 移除 USDT 后缀
	base := strings.TrimSuffix(symbol, "USDT")
	return fmt.Sprintf("%s-USDT-SWAP", base)
}

// fetchInstrumentInfo 獲取合約信息
func (o *OKXAdapter) fetchInstrumentInfo(ctx context.Context) error {
	instruments, err := o.client.GetInstruments(ctx, "SWAP", o.instId)
	if err != nil {
		return fmt.Errorf("獲取合約信息失败: %w", err)
	}

	if len(instruments) == 0 {
		return fmt.Errorf("未找到合約信息: %s", o.instId)
	}

	inst := instruments[0]

	// 解析精度
	tickSz, _ := strconv.ParseFloat(inst.TickSz, 64)
	lotSz, _ := strconv.ParseFloat(inst.LotSz, 64)

	o.priceDecimals = getPrecision(tickSz)
	o.quantityDecimals = getPrecision(lotSz)
	o.baseAsset = inst.CtValCcy   // 基础币种
	o.quoteAsset = inst.SettleCcy // 結算币种

	logger.Info("ℹ️ [OKX 合約信息] %s - 數量精度:%d, 價格精度:%d, 基础币种:%s, 计價币种:%s",
		o.instId, o.quantityDecimals, o.priceDecimals, o.baseAsset, o.quoteAsset)

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
func (o *OKXAdapter) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	side := string(req.Side)
	orderType := string(req.Type)

	// OKX 使用 post_only 作為 TimeInForce
	var tdMode string
	if req.PostOnly {
		tdMode = "post_only"
	} else {
		tdMode = ""
	}

	// 構造订單请求
	orderReq := map[string]interface{}{
		"instId":  o.instId,
		"tdMode":  "cross", // 全倉模式
		"side":    side,
		"ordType": orderType,
		"sz":      fmt.Sprintf("%.*f", o.quantityDecimals, req.Quantity),
		"px":      fmt.Sprintf("%.*f", req.PriceDecimals, req.Price),
	}

	// 設置 post_only
	if tdMode != "" {
		orderReq["postOnly"] = true
	}

	// 設置自定义订單ID
	if req.ClientOrderID != "" {
		clientOrderID := utils.AddBrokerPrefix("okx", req.ClientOrderID)
		orderReq["clOrdId"] = clientOrderID
	}

	// 設置 ReduceOnly
	if req.ReduceOnly {
		orderReq["reduceOnly"] = true
	}

	resp, err := o.client.PlaceOrder(ctx, orderReq)
	if err != nil {
		return nil, err
	}

	if len(resp) == 0 {
		return nil, fmt.Errorf("下單响应為空")
	}

	result := resp[0]
	if result.SCode != "0" {
		return nil, fmt.Errorf("下單失败: %s - %s", result.SCode, result.SMsg)
	}

	orderID, _ := strconv.ParseInt(result.OrdId, 10, 64)

	return &Order{
		OrderID:       orderID,
		ClientOrderID: result.ClOrdId,
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
func (o *OKXAdapter) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	placedOrders := make([]*Order, 0, len(orders))
	hasMarginError := false

	// OKX 支援批量下單，但為了简化實現，先使用循环
	for _, orderReq := range orders {
		order, err := o.PlaceOrder(ctx, orderReq)
		if err != nil {
			logger.Warn("⚠️ [OKX] 下單失败 %.2f %s: %v",
				orderReq.Price, orderReq.Side, err)

			if strings.Contains(err.Error(), "51008") || strings.Contains(err.Error(), "insufficient") {
				hasMarginError = true
			}
			continue
		}
		placedOrders = append(placedOrders, order)
	}

	return placedOrders, hasMarginError
}

// CancelOrder 取消訂單
func (o *OKXAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	err := o.client.CancelOrder(ctx, o.instId, strconv.FormatInt(orderID, 10), "")
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "51400") || strings.Contains(errStr, "Order does not exist") {
			logger.Info("ℹ️ [OKX] 订單 %d 已不存在，跳過取消", orderID)
			return nil
		}
		return err
	}

	logger.Info("✅ [OKX] 取消訂單成功: %d", orderID)
	return nil
}

// BatchCancelOrders 批量撤單
func (o *OKXAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	if len(orderIDs) == 0 {
		return nil
	}

	// OKX 批量撤單限制：最多20個
	batchSize := 20
	for i := 0; i < len(orderIDs); i += batchSize {
		end := i + batchSize
		if end > len(orderIDs) {
			end = len(orderIDs)
		}

		batch := orderIDs[i:end]

		// 轉换為字符串數组
		orderIDStrs := make([]string, len(batch))
		for j, id := range batch {
			orderIDStrs[j] = strconv.FormatInt(id, 10)
		}

		err := o.client.BatchCancelOrders(ctx, o.instId, orderIDStrs)
		if err != nil {
			logger.Warn("⚠️ [OKX] 批量撤單失败 (共%d個): %v", len(batch), err)
			// 失败時尝試單個撤單
			logger.Info("🔄 [OKX] 改為逐個撤單...")
			for _, orderID := range batch {
				_ = o.CancelOrder(ctx, symbol, orderID)
				time.Sleep(100 * time.Millisecond)
			}
		} else {
			logger.Info("✅ [OKX] 批量撤單成功: %d 個订單", len(batch))
		}

		if i+batchSize < len(orderIDs) {
			time.Sleep(100 * time.Millisecond)
		}
	}

	return nil
}

// CancelAllOrders 取消所有订單
func (o *OKXAdapter) CancelAllOrders(ctx context.Context, symbol string) error {
	// 先查詢所有未完成订單
	orders, err := o.GetOpenOrders(ctx, symbol)
	if err != nil {
		return err
	}

	if len(orders) == 0 {
		logger.Info("ℹ️ [OKX] 没有未完成订單")
		return nil
	}

	orderIDs := make([]int64, len(orders))
	for i, order := range orders {
		orderIDs[i] = order.OrderID
	}

	return o.BatchCancelOrders(ctx, symbol, orderIDs)
}

// GetOrder 查詢訂單
func (o *OKXAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	order, err := o.client.GetOrder(ctx, o.instId, strconv.FormatInt(orderID, 10), "")
	if err != nil {
		return nil, err
	}

	return o.convertOrder(order), nil
}

// GetOpenOrders 查詢未完成订單
func (o *OKXAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	orders, err := o.client.GetOpenOrders(ctx, o.instId)
	if err != nil {
		return nil, err
	}

	result := make([]*Order, 0, len(orders))
	for _, order := range orders {
		result = append(result, o.convertOrder(&order))
	}

	return result, nil
}

// convertOrder 轉换订單格式
func (o *OKXAdapter) convertOrder(order *OKXOrder) *Order {
	orderID, _ := strconv.ParseInt(order.OrdId, 10, 64)
	price, _ := strconv.ParseFloat(order.Px, 64)
	quantity, _ := strconv.ParseFloat(order.Sz, 64)
	executedQty, _ := strconv.ParseFloat(order.AccFillSz, 64)
	avgPrice, _ := strconv.ParseFloat(order.AvgPx, 64)
	updateTime, _ := strconv.ParseInt(order.UTime, 10, 64)

	var side Side
	if order.Side == "buy" {
		side = SideBuy
	} else {
		side = SideSell
	}

	return &Order{
		OrderID:       orderID,
		ClientOrderID: order.ClOrdId,
		Symbol:        o.symbol,
		Side:          side,
		Type:          OrderType(order.OrdType),
		Price:         price,
		Quantity:      quantity,
		ExecutedQty:   executedQty,
		AvgPrice:      avgPrice,
		Status:        OrderStatus(order.State),
		UpdateTime:    updateTime,
	}
}

// GetAccount 獲取帳戶信息
func (o *OKXAdapter) GetAccount(ctx context.Context) (*Account, error) {
	balance, err := o.client.GetBalance(ctx)
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

	// OKX 返回多币种餘額，取 USDT
	var totalBalance, availBalance float64
	for _, detail := range balance[0].Details {
		if detail.Ccy == "USDT" {
			totalBalance, _ = strconv.ParseFloat(detail.Eq, 64)
			availBalance, _ = strconv.ParseFloat(detail.AvailBal, 64)
			break
		}
	}

	// 獲取持倉
	positions, err := o.GetPositions(ctx, o.symbol)
	if err != nil {
		logger.Warn("⚠️ [OKX] 獲取持倉失败: %v", err)
		positions = []*Position{}
	}

	return &Account{
		TotalWalletBalance: totalBalance,
		TotalMarginBalance: totalBalance,
		AvailableBalance:   availBalance,
		Positions:          positions,
	}, nil
}

// GetPositions 獲取持倉信息
func (o *OKXAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	positions, err := o.client.GetPositions(ctx, o.instId)
	if err != nil {
		return nil, err
	}

	result := make([]*Position, 0)
	for _, pos := range positions {
		size, _ := strconv.ParseFloat(pos.Pos, 64)
		if size == 0 {
			continue
		}

		entryPrice, _ := strconv.ParseFloat(pos.AvgPx, 64)
		markPrice, _ := strconv.ParseFloat(pos.MarkPx, 64)
		unrealizedPNL, _ := strconv.ParseFloat(pos.Upl, 64)
		leverage, _ := strconv.Atoi(pos.Lever)

		result = append(result, &Position{
			Symbol:         o.symbol,
			Size:           size,
			EntryPrice:     entryPrice,
			MarkPrice:      markPrice,
			UnrealizedPNL:  unrealizedPNL,
			Leverage:       leverage,
			MarginType:     pos.MgnMode,
			IsolatedMargin: 0,
		})
	}

	return result, nil
}

// GetBalance 獲取餘額
func (o *OKXAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	account, err := o.GetAccount(ctx)
	if err != nil {
		return 0, err
	}
	return account.AvailableBalance, nil
}

// StartOrderStream 啟動訂單流
func (o *OKXAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	if o.wsManager == nil {
		o.wsManager = NewWebSocketManager(o.client.apiKey, o.client.secretKey, o.client.passphrase, o.useTestnet)
	}

	localCallback := func(update OrderUpdate) {
		genericUpdate := struct {
			OrderID         int64
			ClientOrderID   string
			Symbol          string
			Side            string
			Type            string
			Status          string
			Price           float64
			Quantity        float64
			ExecutedQty     float64
			AvgPrice        float64
			UpdateTime      int64
			Commission      float64
			CommissionAsset string
			RealizedPnL     float64
		}{
			OrderID:         update.OrderID,
			ClientOrderID:   update.ClientOrderID,
			Symbol:          update.Symbol,
			Side:            string(update.Side),
			Type:            string(update.Type),
			Status:          string(update.Status),
			Price:           update.Price,
			Quantity:        update.Quantity,
			ExecutedQty:     update.ExecutedQty,
			AvgPrice:        update.AvgPrice,
			UpdateTime:      update.UpdateTime,
			Commission:      update.Commission,
			CommissionAsset: update.CommissionAsset,
			RealizedPnL:     update.RealizedPnL,
		}
		callback(genericUpdate)
	}

	return o.wsManager.Start(ctx, o.instId, localCallback)
}

// StopOrderStream 停止訂單流
func (o *OKXAdapter) StopOrderStream() error {
	if o.wsManager != nil {
		o.wsManager.Stop()
	}
	return nil
}

// GetLatestPrice 獲取最新價格
func (o *OKXAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	if o.wsManager != nil {
		price := o.wsManager.GetLatestPrice()
		if price > 0 {
			return price, nil
		}
	}

	return 0, fmt.Errorf("WebSocket 價格流未就绪或無價格數據")
}

// StartPriceStream 啟動價格流
func (o *OKXAdapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	if o.wsManager == nil {
		o.wsManager = NewWebSocketManager(o.client.apiKey, o.client.secretKey, o.client.passphrase, o.useTestnet)
	}
	return o.wsManager.StartPriceStream(ctx, o.instId, callback)
}

// StartKlineStream 啟動K線流
func (o *OKXAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	if o.klineWSManager == nil {
		o.klineWSManager = NewKlineWebSocketManager(o.useTestnet)
	}

	// 轉换交易對格式
	instIds := make([]string, len(symbols))
	for i, sym := range symbols {
		instIds[i] = convertSymbolToInstId(sym)
	}

	return o.klineWSManager.Start(ctx, instIds, interval, callback)
}

// StopKlineStream 停止K線流
func (o *OKXAdapter) StopKlineStream() error {
	if o.klineWSManager != nil {
		o.klineWSManager.Stop()
	}
	return nil
}

// GetHistoricalKlines 獲取歷史K線數據
func (o *OKXAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	klines, err := o.client.GetKlines(ctx, o.instId, interval, limit)
	if err != nil {
		return nil, fmt.Errorf("獲取歷史K線失败: %w", err)
	}

	candles := make([]*Candle, 0, len(klines))
	for _, k := range klines {
		timestamp, _ := strconv.ParseInt(k.Ts, 10, 64)
		open, _ := strconv.ParseFloat(k.O, 64)
		high, _ := strconv.ParseFloat(k.H, 64)
		low, _ := strconv.ParseFloat(k.L, 64)
		close, _ := strconv.ParseFloat(k.C, 64)
		volume, _ := strconv.ParseFloat(k.Vol, 64)

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
func (o *OKXAdapter) GetPriceDecimals() int {
	return o.priceDecimals
}

// GetQuantityDecimals 獲取數量精度
func (o *OKXAdapter) GetQuantityDecimals() int {
	return o.quantityDecimals
}

// GetBaseAsset 獲取基础资產
func (o *OKXAdapter) GetBaseAsset() string {
	return o.baseAsset
}

// GetQuoteAsset 獲取计價资產
func (o *OKXAdapter) GetQuoteAsset() string {
	return o.quoteAsset
}

// GetFundingRate 獲取资金费率
func (o *OKXAdapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	fundingRate, err := o.client.GetFundingRate(ctx, o.instId)
	if err != nil {
		return 0, fmt.Errorf("獲取资金费率失败: %w", err)
	}

	rate, _ := strconv.ParseFloat(fundingRate.FundingRate, 64)
	return rate, nil
}

// GetSpotPrice 獲取現貨市场價格
func (o *OKXAdapter) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	// 將合約交易對轉换為現貨交易對
	// BTC-USDT-SWAP -> BTC-USDT
	spotInstId := strings.Replace(symbol, "-SWAP", "", 1)
	spotInstId = strings.Replace(spotInstId, "-PERP", "", 1)

	// 調用 OKX 現貨 ticker API
	ticker, err := o.client.GetTicker(ctx, spotInstId)
	if err != nil {
		return 0, fmt.Errorf("獲取現貨價格失败: %w", err)
	}

	price, err := strconv.ParseFloat(ticker.Last, 64)
	if err != nil {
		return 0, fmt.Errorf("解析現貨價格失败: %w", err)
	}

	return price, nil
}

// GetOrderBook 獲取訂單簿深度
func (o *OKXAdapter) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	// OKX API: GET /api/v5/market/books
	okxOrderBook, err := o.client.GetOrderBook(ctx, o.instId, limit)
	if err != nil {
		return nil, fmt.Errorf("獲取訂單簿深度失败: %w", err)
	}

	// 轉换買盘數據（價格從高到低）
	bids := make([]OrderBookLevel, 0, len(okxOrderBook.Bids))
	for _, bid := range okxOrderBook.Bids {
		if len(bid) < 2 {
			continue
		}
		price, err := strconv.ParseFloat(bid[0], 64)
		if err != nil {
			logger.Warn("⚠️ [OKX] 订單簿買盘價格解析失败: %v", err)
			continue
		}
		quantity, err := strconv.ParseFloat(bid[1], 64)
		if err != nil {
			logger.Warn("⚠️ [OKX] 订單簿買盘數量解析失败: %v", err)
			continue
		}
		bids = append(bids, OrderBookLevel{
			Price:    price,
			Quantity: quantity,
		})
	}

	// 轉换賣盘數據（價格從低到高）
	asks := make([]OrderBookLevel, 0, len(okxOrderBook.Asks))
	for _, ask := range okxOrderBook.Asks {
		if len(ask) < 2 {
			continue
		}
		price, err := strconv.ParseFloat(ask[0], 64)
		if err != nil {
			logger.Warn("⚠️ [OKX] 订單簿賣盘價格解析失败: %v", err)
			continue
		}
		quantity, err := strconv.ParseFloat(ask[1], 64)
		if err != nil {
			logger.Warn("⚠️ [OKX] 订單簿賣盘數量解析失败: %v", err)
			continue
		}
		asks = append(asks, OrderBookLevel{
			Price:    price,
			Quantity: quantity,
		})
	}

	// 解析時间戳
	timestamp, _ := strconv.ParseInt(okxOrderBook.TS, 10, 64)

	return &OrderBook{
		Symbol:    symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: timestamp,
	}, nil
}

// InternalTransfer 交易所內部轉帳（OKX 暂未實現）
func (o *OKXAdapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return "", fmt.Errorf("internal transfer not implemented for OKX")
}
