package mexc

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"quantmesh/logger"
)

// Adapter MEXC 适配器
type Adapter struct {
	client           *MEXCClient
	wsManager        *WebSocketManager
	klineWSManager   *KlineWebSocketManager
	symbol           string
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
	settleAsset      string
}

type MEXCOrderFill struct {
	OrderID          int64
	TradeID          string
	Symbol           string
	Side             OrderSide
	Price            float64
	Quantity         float64
	Commission       float64
	CommissionAsset  string
	TradeTime        int64
	RealizedPnL      float64
	RealizedPnLKnown bool
	RealizedPnLAsset string
	IsMaker          bool
}

// NewAdapter 創建 MEXC 适配器
func NewAdapter(config map[string]string, symbol string) (*Adapter, error) {
	apiKey := config["api_key"]
	secretKey := config["secret_key"]
	isTestnet := config["testnet"] == "true"

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("MEXC API key or secret key is empty")
	}

	client := NewMEXCClient(apiKey, secretKey, isTestnet)

	// 解析交易對
	parts := strings.Split(symbol, "USDT")
	baseAsset := "BTC"
	if len(parts) > 0 && parts[0] != "" {
		baseAsset = parts[0]
	}

	adapter := &Adapter{
		client:           client,
		symbol:           convertSymbolToMEXC(symbol),
		priceDecimals:    2,
		quantityDecimals: 3,
		baseAsset:        baseAsset,
		quoteAsset:       "USDT",
	}

	// 獲取交易對信息
	ctx := context.Background()
	exchangeInfo, err := client.GetExchangeInfo(ctx)
	if err != nil {
		logger.Warn("Failed to get MEXC exchange info: %v", err)
	} else {
		if detail, ok := exchangeInfo.Symbols[adapter.symbol]; ok {
			adapter.settleAsset = strings.ToUpper(strings.TrimSpace(detail.SettleCoin))
			if detail.PriceScale > 0 {
				adapter.priceDecimals = detail.PriceScale
			}
			if detail.VolScale > 0 {
				adapter.quantityDecimals = detail.VolScale
			}
		}
	}

	return adapter, nil
}

// convertSymbolToMEXC 轉换交易對格式：BTCUSDT -> BTC_USDT
func convertSymbolToMEXC(symbol string) string {
	if strings.Contains(symbol, "_") {
		return symbol
	}
	// BTCUSDT -> BTC_USDT
	if strings.HasSuffix(symbol, "USDT") {
		base := strings.TrimSuffix(symbol, "USDT")
		return base + "_USDT"
	}
	return symbol
}

// convertSymbolFromMEXC 轉换交易對格式：BTC_USDT -> BTCUSDT
func convertSymbolFromMEXC(symbol string) string {
	return strings.ReplaceAll(symbol, "_", "")
}

// GetName 獲取交易所名称
func (a *Adapter) GetName() string {
	return "MEXC"
}

// GetMarketType 獲取市場類型：futures 合約
func (a *Adapter) GetMarketType() string {
	return "futures"
}

// PlaceOrder 下單
func (a *Adapter) PlaceOrder(ctx context.Context, side OrderSide, price, quantity float64, clientOrderID string) (*OrderLocal, error) {
	return a.PlaceOrderWithOptions(ctx, side, price, quantity, clientOrderID, false)
}

func (a *Adapter) PlaceOrderWithOptions(ctx context.Context, side OrderSide, price, quantity float64, clientOrderID string, reduceOnly bool) (*OrderLocal, error) {
	// 轉换订單方向
	var mexcSide int
	if side == SideBuy {
		mexcSide = int(MEXCOrderSideOpenLong)
		if reduceOnly {
			mexcSide = int(MEXCOrderSideCloseShort)
		}
	} else {
		mexcSide = int(MEXCOrderSideOpenShort)
		if reduceOnly {
			mexcSide = int(MEXCOrderSideCloseLong)
		}
	}

	// 構造 MEXC 订單请求
	mexcReq := &OrderRequest{
		Symbol:        a.symbol,
		Price:         price,
		Volume:        quantity,
		Side:          mexcSide,
		Type:          int(MEXCOrderTypeLimit),
		OpenType:      int(MEXCOpenTypeCross), // 默认全倉
		Leverage:      20,                     // 默认杠杆
		ClientOrderID: clientOrderID,
	}

	resp, err := a.client.PlaceOrder(ctx, mexcReq)
	if err != nil {
		return nil, fmt.Errorf("MEXC place order error: %w", err)
	}

	orderID, _ := strconv.ParseInt(resp.OrderID, 10, 64)

	return &OrderLocal{
		OrderID:       orderID,
		ClientOrderID: clientOrderID,
		Symbol:        convertSymbolFromMEXC(a.symbol),
		Side:          side,
		Price:         price,
		Quantity:      quantity,
		Status:        OrderStatusNew,
	}, nil
}

// CancelOrder 取消訂單
func (a *Adapter) CancelOrder(ctx context.Context, orderID int64) error {
	return a.client.CancelOrder(ctx, a.symbol, strconv.FormatInt(orderID, 10))
}

// GetOrder 查詢訂單
func (a *Adapter) GetOrder(ctx context.Context, orderID int64) (*OrderLocal, error) {
	orderInfo, err := a.client.GetOrderInfo(ctx, a.symbol, strconv.FormatInt(orderID, 10))
	if err != nil {
		return nil, err
	}

	return a.convertOrder(orderInfo), nil
}

// GetOrderFills reads MEXC's authenticated execution ledger for one order.
func (a *Adapter) GetOrderFills(ctx context.Context, orderID int64) ([]MEXCOrderFill, error) {
	if orderID <= 0 {
		return nil, fmt.Errorf("MEXC order ID must be positive")
	}
	rows, err := a.client.GetOrderDealDetails(ctx, strconv.FormatInt(orderID, 10))
	if err != nil {
		return nil, err
	}
	fills := make([]MEXCOrderFill, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		rowOrderID, err := strconv.ParseInt(row.OrderID.String(), 10, 64)
		if err != nil || rowOrderID != orderID || row.Symbol != a.symbol || row.ID.String() == "" {
			return nil, fmt.Errorf("MEXC returned mismatched execution identity for order %d", orderID)
		}
		tradeID := row.ID.String()
		if _, ok := seen[tradeID]; ok {
			return nil, fmt.Errorf("MEXC returned duplicate execution %s for order %d", tradeID, orderID)
		}
		seen[tradeID] = struct{}{}
		price, priceErr := strconv.ParseFloat(row.Price.String(), 64)
		volume, volumeErr := strconv.ParseFloat(row.Volume.String(), 64)
		fee, feeErr := strconv.ParseFloat(row.Fee.String(), 64)
		profit, profitErr := strconv.ParseFloat(row.Profit.String(), 64)
		timestamp, timeErr := strconv.ParseInt(row.Timestamp.String(), 10, 64)
		if priceErr != nil || volumeErr != nil || feeErr != nil || profitErr != nil || timeErr != nil ||
			!finiteMEXCValue(price) || !finiteMEXCValue(volume) || !finiteMEXCValue(fee) || !finiteMEXCValue(profit) ||
			price <= 0 || volume <= 0 || timestamp <= 0 || strings.TrimSpace(row.FeeCurrency) == "" {
			return nil, fmt.Errorf("MEXC execution %s contains invalid financial or timestamp fields", tradeID)
		}
		var side OrderSide
		switch MEXCOrderSide(row.Side) {
		case MEXCOrderSideOpenLong, MEXCOrderSideCloseShort:
			side = SideBuy
		case MEXCOrderSideOpenShort, MEXCOrderSideCloseLong:
			side = SideSell
		default:
			return nil, fmt.Errorf("MEXC execution %s has unsupported side %d", tradeID, row.Side)
		}
		fill := MEXCOrderFill{OrderID: orderID, TradeID: tradeID, Symbol: row.Symbol, Side: side, Price: price,
			Quantity: volume, Commission: fee, CommissionAsset: strings.ToUpper(row.FeeCurrency), TradeTime: timestamp,
			RealizedPnL: profit, RealizedPnLKnown: strings.TrimSpace(a.settleAsset) != "",
			RealizedPnLAsset: strings.ToUpper(strings.TrimSpace(a.settleAsset)), IsMaker: !row.IsTaker}
		fills = append(fills, fill)
	}
	return fills, nil
}

func finiteMEXCValue(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

// GetOpenOrders 獲取活跃订單
func (a *Adapter) GetOpenOrders(ctx context.Context) ([]*OrderLocal, error) {
	orders, err := a.client.GetOpenOrders(ctx, a.symbol)
	if err != nil {
		return nil, err
	}

	result := make([]*OrderLocal, 0, len(orders))
	for _, order := range orders {
		result = append(result, a.convertOrder(&order))
	}

	return result, nil
}

// GetAccount 獲取帳戶信息
func (a *Adapter) GetAccount(ctx context.Context) (*AccountLocal, error) {
	accountInfo, err := a.getSettlementAccount(ctx)
	if err != nil {
		return nil, err
	}

	return &AccountLocal{
		TotalWalletBalance: accountInfo.Equity,
		TotalMarginBalance: accountInfo.Equity,
		AvailableBalance:   accountInfo.AvailableBalance,
		BalanceAsset:       strings.ToUpper(strings.TrimSpace(accountInfo.Currency)),
	}, nil
}

func (a *Adapter) getSettlementAccount(ctx context.Context) (*AccountInfo, error) {
	settleAsset := strings.ToUpper(strings.TrimSpace(a.settleAsset))
	if settleAsset == "" {
		return nil, fmt.Errorf("MEXC contract settlement asset is unavailable for %s", a.symbol)
	}
	accounts, err := a.client.GetAccount(ctx)
	if err != nil {
		return nil, err
	}
	var match *AccountInfo
	for i := range accounts {
		if !strings.EqualFold(strings.TrimSpace(accounts[i].Currency), settleAsset) {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("MEXC returned duplicate %s account asset rows", settleAsset)
		}
		match = &accounts[i]
	}
	if match == nil {
		return nil, fmt.Errorf("MEXC account response has no %s settlement asset row", settleAsset)
	}
	return match, nil
}

// GetPositions 獲取持倉
func (a *Adapter) GetPositions(ctx context.Context) ([]*PositionLocal, error) {
	positions, err := a.client.GetPositions(ctx, a.symbol)
	if err != nil {
		return nil, err
	}

	result := make([]*PositionLocal, 0, len(positions))
	for _, pos := range positions {
		if pos.State != int(MEXCPositionStateHolding) {
			continue
		}

		size := pos.HoldVol
		if pos.PositionType == int(MEXCPositionTypeShort) {
			size = -size // 空倉用负數表示
		}

		result = append(result, &PositionLocal{
			Symbol:        convertSymbolFromMEXC(pos.Symbol),
			Size:          size,
			EntryPrice:    pos.OpenAvgPrice,
			MarkPrice:     pos.HoldAvgPrice,
			UnrealizedPNL: pos.UnrealizedPNL,
			Leverage:      pos.Leverage,
		})
	}

	return result, nil
}

// GetBalance 獲取餘額
func (a *Adapter) GetBalance(ctx context.Context) (float64, error) {
	accountInfo, err := a.getSettlementAccount(ctx)
	if err != nil {
		return 0, err
	}

	return accountInfo.AvailableBalance, nil
}

// StartOrderStream 啟動訂單流
func (a *Adapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	if a.wsManager != nil {
		return fmt.Errorf("order stream already started")
	}

	a.wsManager = NewWebSocketManager(a.client.apiKey, a.client.secretKey, a.client.isTestnet)
	return a.wsManager.Start(ctx, a.symbol, callback)
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
	// 如果傳入 symbol,轉换格式並使用;否则使用預設 symbol
	targetSymbol := a.symbol
	if symbol != "" {
		targetSymbol = convertSymbolToMEXC(symbol)
	}

	ticker, err := a.client.GetTicker(ctx, targetSymbol)
	if err != nil {
		return 0, err
	}

	return ticker.LastPrice, nil
}

// StartKlineStream 啟动 K線流
func (a *Adapter) StartKlineStream(ctx context.Context, interval string, callback CandleUpdateCallbackLocal) error {
	if a.klineWSManager != nil {
		return fmt.Errorf("kline stream already started")
	}

	mexcInterval := string(ConvertInterval(interval))
	a.klineWSManager = NewKlineWebSocketManager(a.client.isTestnet)

	return a.klineWSManager.Start(ctx, a.symbol, mexcInterval, func(kline *Kline) {
		candle := &CandleLocal{
			Symbol:    convertSymbolFromMEXC(a.symbol),
			Timestamp: kline.Time,
			Open:      kline.Open,
			High:      kline.High,
			Low:       kline.Low,
			Close:     kline.Close,
			Volume:    kline.Vol,
		}
		callback(candle)
	})
}

// StopKlineStream 停止 K線流
func (a *Adapter) StopKlineStream() error {
	if a.klineWSManager != nil {
		a.klineWSManager.Stop()
		a.klineWSManager = nil
	}
	return nil
}

// GetHistoricalKlines 獲取歷史 K線
func (a *Adapter) GetHistoricalKlines(ctx context.Context, interval string, limit int) ([]*CandleLocal, error) {
	mexcInterval := string(ConvertInterval(interval))
	klines, err := a.client.GetKlines(ctx, a.symbol, mexcInterval, limit)
	if err != nil {
		return nil, err
	}

	result := make([]*CandleLocal, 0, len(klines))
	for _, kline := range klines {
		result = append(result, &CandleLocal{
			Symbol:    convertSymbolFromMEXC(a.symbol),
			Timestamp: kline.Time,
			Open:      kline.Open,
			High:      kline.High,
			Low:       kline.Low,
			Close:     kline.Close,
			Volume:    kline.Vol,
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

// MEXCBookLevel 訂單簿檔位
type MEXCBookLevel struct {
	Price    float64
	Quantity float64
}

// MEXCOrderBook 訂單簿（供 exchange wrapper 轉換）
type MEXCOrderBook struct {
	Symbol    string
	Bids      []MEXCBookLevel
	Asks      []MEXCBookLevel
	Timestamp int64
}

// GetSpotPrice 指數／最新成交價兜底（公共 ticker）
func (a *Adapter) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	sym := a.symbol
	if strings.TrimSpace(symbol) != "" {
		sym = convertSymbolToMEXC(symbol)
	}
	t, err := a.client.GetTicker(ctx, sym)
	if err != nil {
		return 0, err
	}
	if t.IndexPrice > 0 {
		return t.IndexPrice, nil
	}
	return t.LastPrice, nil
}

// GetOrderBook 公共深度 REST
func (a *Adapter) GetOrderBook(ctx context.Context, symbol string, limit int) (*MEXCOrderBook, error) {
	sym := a.symbol
	if strings.TrimSpace(symbol) != "" {
		sym = convertSymbolToMEXC(symbol)
	}
	bidsRaw, asksRaw, ts, err := a.client.GetContractDepth(ctx, sym)
	if err != nil {
		return nil, err
	}
	maxLevels := limit
	if maxLevels <= 0 {
		maxLevels = 20
	}
	parseSide := func(raw [][]float64) []MEXCBookLevel {
		out := make([]MEXCBookLevel, 0, len(raw))
		for i, row := range raw {
			if i >= maxLevels {
				break
			}
			if len(row) < 2 {
				continue
			}
			out = append(out, MEXCBookLevel{Price: row[0], Quantity: row[1]})
		}
		return out
	}
	disp := symbol
	if strings.TrimSpace(disp) == "" {
		disp = convertSymbolFromMEXC(a.symbol)
	}
	return &MEXCOrderBook{
		Symbol:    disp,
		Bids:      parseSide(bidsRaw),
		Asks:      parseSide(asksRaw),
		Timestamp: ts,
	}, nil
}

// GetFundingRate 獲取资金费率
func (a *Adapter) GetFundingRate(ctx context.Context) (float64, error) {
	ticker, err := a.client.GetTicker(ctx, a.symbol)
	if err != nil {
		return 0, err
	}

	return ticker.FundingRate, nil
}

// convertOrder 轉换订單
func (a *Adapter) convertOrder(order *OrderInfo) *OrderLocal {
	var side OrderSide
	if order.Side == int(MEXCOrderSideOpenLong) || order.Side == int(MEXCOrderSideCloseShort) {
		side = SideBuy
	} else {
		side = SideSell
	}

	var status OrderStatus
	switch MEXCOrderState(order.State) {
	case MEXCOrderStateNew:
		status = OrderStatusNew
	case MEXCOrderStatePartiallyFilled:
		status = OrderStatusPartiallyFilled
	case MEXCOrderStateFilled:
		status = OrderStatusFilled
	case MEXCOrderStateCanceled, MEXCOrderStatePartialCanceled:
		status = OrderStatusCanceled
	default:
		status = OrderStatusNew
	}

	orderID, _ := strconv.ParseInt(order.OrderID, 10, 64)

	return &OrderLocal{
		OrderID:       orderID,
		ClientOrderID: order.ExternalOid,
		Symbol:        convertSymbolFromMEXC(order.Symbol),
		Side:          side,
		Price:         order.Price,
		Quantity:      order.Vol,
		ExecutedQty:   order.DealVol,
		Status:        status,
		UpdateTime:    order.UpdateTime,
	}
}

// InternalTransfer 交易所內部轉帳（MEXC 暂未實現）
func (a *Adapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return "", fmt.Errorf("internal transfer not implemented for MEXC")
}
