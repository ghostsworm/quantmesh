package kucoin

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
)

// Adapter KuCoin 适配器
type Adapter struct {
	client           *KuCoinClient
	wsManager        *WebSocketManager
	klineWSManager   *KlineWebSocketManager
	symbol           string
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
}

// NewKuCoinAdapter 創建 KuCoin 适配器
func NewKuCoinAdapter(config map[string]string, symbol string) (*Adapter, error) {
	apiKey := config["api_key"]
	secretKey := config["secret_key"]
	passphrase := config["passphrase"]

	if apiKey == "" || secretKey == "" || passphrase == "" {
		return nil, fmt.Errorf("KuCoin API key, secret key, and passphrase are required")
	}

	client := NewKuCoinClient(apiKey, secretKey, passphrase)

	// 解析交易對：KuCoin 使用 BTCUSDT 格式，需要轉换為 BTC-USDT
	var parts []string
	if strings.Contains(symbol, "-") {
		parts = strings.Split(symbol, "-")
	} else {
		// 尝試解析 BTCUSDT 格式
		if strings.HasSuffix(symbol, "USDT") {
			base := strings.TrimSuffix(symbol, "USDT")
			parts = []string{base, "USDT"}
			symbol = base + "-USDT" // 轉换為 KuCoin 格式
		} else {
			return nil, fmt.Errorf("invalid symbol format: %s, expected format: BTC-USDT or BTCUSDT", symbol)
		}
	}

	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid symbol format: %s, expected format: BTC-USDT", symbol)
	}

	adapter := &Adapter{
		client:           client,
		symbol:           symbol,
		priceDecimals:    2,
		quantityDecimals: 0, // KuCoin 期货使用整數张數
		baseAsset:        parts[0],
		quoteAsset:       parts[1],
	}

	// 獲取交易對精度信息
	ctx := context.Background()
	exchangeInfo, err := client.GetExchangeInfo(ctx)
	if err != nil {
		logger.Warn("Failed to get KuCoin exchange info: %v", err)
	} else {
		if info, exists := exchangeInfo.Symbols[symbol]; exists {
			// 根據 tickSize 计算價格精度
			tickSize := info.TickSize
			if tickSize > 0 {
				adapter.priceDecimals = getPrecision(tickSize)
			}
			logger.Info("KuCoin symbol %s precision: price=%d, quantity=%d", symbol, adapter.priceDecimals, adapter.quantityDecimals)
		}
	}

	return adapter, nil
}

// getPrecision 根據 tickSize 计算精度
func getPrecision(tickSize float64) int {
	str := fmt.Sprintf("%.10f", tickSize)
	str = strings.TrimRight(str, "0")
	parts := strings.Split(str, ".")
	if len(parts) == 2 {
		return len(parts[1])
	}
	return 0
}

// GetName 獲取交易所名称
func (a *Adapter) GetName() string {
	return "KuCoin"
}

// GetMarketType 獲取市場類型：futures 合約
func (a *Adapter) GetMarketType() string {
	return "futures"
}

// PlaceOrder 下單
func (a *Adapter) PlaceOrder(ctx context.Context, req *KuCoinOrderRequest) (*Order, error) {
	clientOrderID := fmt.Sprintf("order_%d", req.Timestamp)

	orderReq := &OrderRequest{
		ClientOrderID:    clientOrderID,
		Symbol:           a.symbol,
		Side:             strings.ToLower(string(req.Side)),
		Type:             strings.ToLower(string(req.Type)),
		Price:            req.Price,
		Quantity:         req.Quantity,
		Leverage:         int(req.Leverage),
		PriceDecimals:    a.priceDecimals,
		QuantityDecimals: a.quantityDecimals,
	}

	resp, err := a.client.PlaceOrder(ctx, orderReq)
	if err != nil {
		return nil, fmt.Errorf("place order error: %w", err)
	}

	order := &Order{
		OrderID:       resp.OrderID,
		ClientOrderID: clientOrderID,
		Symbol:        a.symbol,
		Side:          string(req.Side),
		Type:          string(req.Type),
		Price:         req.Price,
		Quantity:      req.Quantity,
		Status:        "NEW",
		Timestamp:     req.Timestamp,
	}

	logger.Info("KuCoin order placed: %s, side: %s, price: %.2f, quantity: %.2f", order.OrderID, order.Side, order.Price, order.Quantity)
	return order, nil
}

// BatchPlaceOrders 批量下單
func (a *Adapter) BatchPlaceOrders(ctx context.Context, orders []*KuCoinOrderRequest) ([]*Order, bool) {
	results := make([]*Order, 0, len(orders))
	allSuccess := true

	for _, orderReq := range orders {
		order, err := a.PlaceOrder(ctx, orderReq)
		if err != nil {
			logger.Error("Batch place order failed: %v", err)
			allSuccess = false
			continue
		}
		results = append(results, order)
	}

	return results, allSuccess
}

// CancelOrder 取消訂單
func (a *Adapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	orderIDStr := strconv.FormatInt(orderID, 10)
	_, err := a.client.CancelOrder(ctx, orderIDStr)
	if err != nil {
		return fmt.Errorf("cancel order error: %w", err)
	}

	logger.Info("KuCoin order cancelled: %s", orderIDStr)
	return nil
}

// BatchCancelOrders 批量取消訂單
func (a *Adapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	for _, orderID := range orderIDs {
		if err := a.CancelOrder(ctx, symbol, orderID); err != nil {
			logger.Error("Batch cancel order %d failed: %v", orderID, err)
		}
	}
	return nil
}

// CancelAllOrders 取消所有订單
func (a *Adapter) CancelAllOrders(ctx context.Context, symbol string) error {
	orders, err := a.client.GetOpenOrders(ctx, symbol)
	if err != nil {
		return fmt.Errorf("get open orders error: %w", err)
	}

	for _, order := range orders {
		if _, err := a.client.CancelOrder(ctx, order.ID); err != nil {
			logger.Error("Cancel order %s failed: %v", order.ID, err)
		}
	}

	logger.Info("KuCoin all orders cancelled for symbol: %s", symbol)
	return nil
}

// GetOrder 查詢訂單
func (a *Adapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	orderIDStr := strconv.FormatInt(orderID, 10)
	orderInfo, err := a.client.GetOrderInfo(ctx, orderIDStr)
	if err != nil {
		return nil, fmt.Errorf("get order error: %w", err)
	}

	return a.convertToOrder(orderInfo), nil
}

// GetOpenOrders 查詢未完成订單
func (a *Adapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	orders, err := a.client.GetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("get open orders error: %w", err)
	}

	result := make([]*Order, 0, len(orders))
	for _, order := range orders {
		result = append(result, a.convertToOrder(&order))
	}

	return result, nil
}

// GetAccount 獲取帳戶信息
func (a *Adapter) GetAccount(ctx context.Context) (*Account, error) {
	accountInfo, err := a.client.GetAccountInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("get account error: %w", err)
	}

	account := &Account{
		TotalBalance:     accountInfo.AccountEquity,
		AvailableBalance: accountInfo.AvailableBalance,
		UnrealizedPnL:    accountInfo.UnrealisedPNL,
		MarginBalance:    accountInfo.MarginBalance,
	}

	return account, nil
}

// GetPositions 獲取持倉信息
func (a *Adapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	positions, err := a.client.GetPositionInfo(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("get positions error: %w", err)
	}

	result := make([]*Position, 0, len(positions))
	for _, pos := range positions {
		if !pos.IsOpen {
			continue
		}

		side := "LONG"
		if pos.CurrentQty < 0 {
			side = "SHORT"
		}

		position := &Position{
			Symbol:           pos.Symbol,
			Side:             side,
			Size:             float64(abs(pos.CurrentQty)),
			EntryPrice:       pos.AvgEntryPrice,
			MarkPrice:        pos.MarkPrice,
			UnrealizedPnL:    pos.UnrealisedPnl,
			Leverage:         pos.RealLeverage,
			LiquidationPrice: pos.LiquidationPrice,
		}
		result = append(result, position)
	}

	return result, nil
}

// GetBalance 獲取餘額
func (a *Adapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	accountInfo, err := a.client.GetAccountInfo(ctx)
	if err != nil {
		return 0, fmt.Errorf("get balance error: %w", err)
	}

	return accountInfo.AvailableBalance, nil
}

// StartOrderStream 啟動訂單流
func (a *Adapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	if a.wsManager != nil {
		return fmt.Errorf("order stream already started")
	}

	wsManager, err := NewWebSocketManager(a.client, a.symbol)
	if err != nil {
		return fmt.Errorf("create websocket manager error: %w", err)
	}

	a.wsManager = wsManager
	return wsManager.StartOrderStream(ctx, callback)
}

// StopOrderStream 停止訂單流
func (a *Adapter) StopOrderStream() error {
	if a.wsManager != nil {
		a.wsManager.Stop()
		a.wsManager = nil
	}
	return nil
}

// GetLatestPrice 獲取最新價格
func (a *Adapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	// KuCoin 没有單独的獲取最新價格的 API，这里通過持倉資訊獲取標記價格
	positions, err := a.client.GetPositionInfo(ctx, symbol)
	if err != nil {
		return 0, fmt.Errorf("get latest price error: %w", err)
	}

	if len(positions) > 0 {
		return positions[0].MarkPrice, nil
	}

	return 0, fmt.Errorf("no position found for symbol: %s", symbol)
}

// StartPriceStream 啟動價格流
func (a *Adapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	if a.wsManager == nil {
		wsManager, err := NewWebSocketManager(a.client, symbol)
		if err != nil {
			return fmt.Errorf("create websocket manager error: %w", err)
		}
		a.wsManager = wsManager
	}

	return a.wsManager.StartPriceStream(ctx, callback)
}

// StartKlineStream 啟動K線流
func (a *Adapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	if a.klineWSManager != nil {
		return fmt.Errorf("kline stream already started")
	}

	klineWSManager, err := NewKlineWebSocketManager(a.client, symbols, interval)
	if err != nil {
		return fmt.Errorf("create kline websocket manager error: %w", err)
	}

	a.klineWSManager = klineWSManager
	return klineWSManager.Start(ctx, callback)
}

// StopKlineStream 停止K線流
func (a *Adapter) StopKlineStream() error {
	if a.klineWSManager != nil {
		a.klineWSManager.Stop()
		a.klineWSManager = nil
	}
	return nil
}

// GetHistoricalKlines 獲取歷史K線數據
func (a *Adapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*KuCoinCandle, error) {
	granularity := convertIntervalToGranularity(interval)
	candles, err := a.client.GetHistoricalKlines(ctx, symbol, granularity, limit)
	if err != nil {
		return nil, fmt.Errorf("get historical klines error: %w", err)
	}

	result := make([]*KuCoinCandle, 0, len(candles))
	for _, candle := range candles {
		result = append(result, &KuCoinCandle{
			OpenTime:  candle.Time,
			Open:      candle.Open,
			High:      candle.High,
			Low:       candle.Low,
			Close:     candle.Close,
			Volume:    candle.Volume,
			CloseTime: candle.Time,
		})
	}

	return result, nil
}

// GetPriceDecimals 獲取價格精度
func (a *Adapter) GetPriceDecimals() int {
	return a.priceDecimals
}

// GetQuantityDecimals 獲取數量精度
func (a *Adapter) GetQuantityDecimals() int {
	return a.quantityDecimals
}

// GetBaseAsset 獲取基础资產
func (a *Adapter) GetBaseAsset() string {
	return a.baseAsset
}

// GetQuoteAsset 獲取报價资產
func (a *Adapter) GetQuoteAsset() string {
	return a.quoteAsset
}

// FundingInfo 資金費率詳情（供 exchange wrapper 轉換）
type FundingInfo struct {
	Symbol          string
	Rate            float64
	NextFundingTime time.Time
	MarkPrice       float64
	IndexPrice      float64
}

// normalizeUnifiedSymbol 將 BTCUSDT / BTC-USDT 統一為 BASE-QUOTE
func normalizeUnifiedSymbol(sym string) string {
	sym = strings.TrimSpace(sym)
	if sym == "" {
		return ""
	}
	if strings.Contains(sym, "-") {
		return sym
	}
	sym = strings.ToUpper(sym)
	if strings.HasSuffix(sym, "USDT") {
		return strings.TrimSuffix(sym, "USDT") + "-USDT"
	}
	return sym
}

// kucoinContractSymbolForFutures 將 BTC-USDT 等映射為 KuCoin 合約代碼（如 XBTUSDTM）
func kucoinContractSymbolForFutures(unified string) string {
	parts := strings.Split(unified, "-")
	if len(parts) != 2 {
		return ""
	}
	base, quote := strings.ToUpper(parts[0]), strings.ToUpper(parts[1])
	if quote != "USDT" {
		return ""
	}
	if base == "BTC" {
		base = "XBT"
	}
	return base + "USDTM"
}

// GetFundingInfo 從 /api/v1/contracts/{symbol} 獲取費率、標記/指數價與下次結算時間
func (a *Adapter) GetFundingInfo(ctx context.Context, symbol string) (*FundingInfo, error) {
	unified := normalizeUnifiedSymbol(symbol)
	if unified == "" {
		unified = a.symbol
	}
	contractID := kucoinContractSymbolForFutures(unified)
	if contractID == "" {
		return nil, fmt.Errorf("kucoin: unsupported symbol for contract funding info: %s", symbol)
	}
	detail, err := a.client.GetContractDetail(ctx, contractID)
	if err != nil {
		return nil, err
	}
	if detail.NextFundingRateDateTime <= 0 {
		return nil, fmt.Errorf("kucoin: empty nextFundingRateDateTime for %s", contractID)
	}
	next := time.UnixMilli(detail.NextFundingRateDateTime).UTC()
	return &FundingInfo{
		Symbol:          unified,
		Rate:            detail.FundingFeeRate,
		NextFundingTime: next,
		MarkPrice:       detail.MarkPrice,
		IndexPrice:      detail.IndexPrice,
	}, nil
}

// GetFundingRate 獲取资金费率
func (a *Adapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return a.client.GetFundingRate(ctx, symbol)
}

// convertToOrder 將 KuCoin 订單轉换為通用订單
func (a *Adapter) convertToOrder(orderInfo *OrderInfo) *Order {
	price, _ := strconv.ParseFloat(orderInfo.Price, 64)

	status := "NEW"
	if orderInfo.IsActive {
		status = "PARTIALLY_FILLED"
	}
	if orderInfo.Status == "done" {
		status = "FILLED"
	}
	if orderInfo.CancelExist {
		status = "CANCELED"
	}

	return &Order{
		OrderID:       orderInfo.ID,
		ClientOrderID: orderInfo.ClientOid,
		Symbol:        orderInfo.Symbol,
		Side:          strings.ToUpper(orderInfo.Side),
		Type:          strings.ToUpper(orderInfo.Type),
		Price:         price,
		Quantity:      float64(orderInfo.Size),
		ExecutedQty:   float64(orderInfo.FilledSize),
		Status:        status,
		Timestamp:     orderInfo.CreatedAt,
	}
}

// convertIntervalToGranularity 將時间间隔轉换為 KuCoin 的 granularity
func convertIntervalToGranularity(interval string) int {
	switch interval {
	case "1m":
		return 1
	case "5m":
		return 5
	case "15m":
		return 15
	case "30m":
		return 30
	case "1h":
		return 60
	case "2h":
		return 120
	case "4h":
		return 240
	case "8h":
		return 480
	case "12h":
		return 720
	case "1d":
		return 1440
	case "1w":
		return 10080
	default:
		return 60 // 預設 1 小時
	}
}

// abs 返回绝對值
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// KuCoinBookLevel 訂單簿檔位
type KuCoinBookLevel struct {
	Price    float64
	Quantity float64
}

// KuCoinOrderBook 訂單簿（供 exchange wrapper 轉換）
type KuCoinOrderBook struct {
	Symbol    string
	Bids      []KuCoinBookLevel
	Asks      []KuCoinBookLevel
	Timestamp int64
}

// GetSpotPrice 合約最新成交價（公共 ticker，作為「現貨參考」兜底）
func (a *Adapter) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	sym := contractSymbolForPublicAPI(a, symbol)
	if sym == "" {
		return 0, fmt.Errorf("kucoin: invalid contract symbol")
	}
	return a.client.GetFuturesTickerPrice(ctx, sym)
}

// GetOrderBook 訂單簿深度（公共 level2）
func (a *Adapter) GetOrderBook(ctx context.Context, symbol string, limit int) (*KuCoinOrderBook, error) {
	sym := contractSymbolForPublicAPI(a, symbol)
	if sym == "" {
		return nil, fmt.Errorf("kucoin: invalid contract symbol")
	}
	depth := "depth20"
	if limit > 50 {
		depth = "depth100"
	}
	bidsRaw, asksRaw, ts, err := a.client.GetFuturesOrderBookDepth(ctx, sym, depth)
	if err != nil {
		return nil, err
	}
	maxLevels := limit
	if maxLevels <= 0 {
		maxLevels = 20
	}
	parseSide := func(raw [][]string) []KuCoinBookLevel {
		out := make([]KuCoinBookLevel, 0, len(raw))
		for i, row := range raw {
			if i >= maxLevels {
				break
			}
			if len(row) < 2 {
				continue
			}
			p, _ := strconv.ParseFloat(row[0], 64)
			q, _ := strconv.ParseFloat(row[1], 64)
			out = append(out, KuCoinBookLevel{Price: p, Quantity: q})
		}
		return out
	}
	symDisp := symbol
	if strings.TrimSpace(symDisp) == "" {
		symDisp = a.symbol
	}
	return &KuCoinOrderBook{
		Symbol:    symDisp,
		Bids:      parseSide(bidsRaw),
		Asks:      parseSide(asksRaw),
		Timestamp: ts,
	}, nil
}

func contractSymbolForPublicAPI(a *Adapter, symbol string) string {
	s := strings.TrimSpace(symbol)
	if s == "" {
		return kucoinContractSymbolForFutures(a.symbol)
	}
	u := normalizeUnifiedSymbol(s)
	if u == "" {
		u = a.symbol
	}
	return kucoinContractSymbolForFutures(u)
}

// InternalTransfer 交易所內部轉帳（KuCoin 暂未實現）
func (a *Adapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return "", fmt.Errorf("internal transfer not implemented for KuCoin")
}
