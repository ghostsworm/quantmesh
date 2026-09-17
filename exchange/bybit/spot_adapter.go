package bybit

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"
)

const (
	// bybitCategorySpot 現貨 category
	bybitCategorySpot = "spot"
	// bybitSpotMarketUnitBase 現貨市價單 qty 以基礎幣計（Bybit 現貨市價買單默認按計價幣計）
	bybitSpotMarketUnitBase = "baseCoin"
	// bybitSpotCancelInterval 逐筆撤單間隔，避免觸發限頻
	bybitSpotCancelInterval = 50 * time.Millisecond
	// bybitOrderNotExistsCode 撤單時訂單已不存在
	bybitOrderNotExistsCode = "110001"
	// bybitInsufficientBalanceCode 餘額不足
	bybitInsufficientBalanceCode = "110007"
)

// BybitSpotAdapter Bybit 現貨交易所适配器
type BybitSpotAdapter struct {
	client           *BybitClient
	symbol           string
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
	useTestnet       bool
	wsManager        *WebSocketManager // 公共現貨 tickers + 私有訂單流
	spotKlineWS      *KlineWebSocketManager

	// 現貨規格（instruments-info，category=spot）
	tickSize    float64 // 價格步長
	qtyStep     float64 // 數量步長（basePrecision）
	minOrderQty float64 // 最小下單數量（基礎幣）
	minOrderAmt float64 // 最小下單金額（計價幣）
}

// NewBybitSpotAdapter 創建 Bybit 現貨适配器
func NewBybitSpotAdapter(cfg map[string]string, symbol string) (*BybitSpotAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]
	testnetStr := cfg["testnet"]

	useTestnet := false
	if testnetStr == "true" {
		useTestnet = true
		logger.Info("🌐 [Bybit Spot] 使用測試網模式")
	}

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("Bybit API 配置不完整")
	}

	client := NewBybitClient(apiKey, secretKey, useTestnet)
	adapter := &BybitSpotAdapter{
		client:     client,
		symbol:     symbol,
		useTestnet: useTestnet,
	}

	ctxInit, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := adapter.fetchSpotInstrument(ctxInit); err != nil {
		logger.Warn("⚠️ [Bybit Spot] 獲取交易對信息失败: %v，使用默认精度", err)
		adapter.priceDecimals = 2
		adapter.quantityDecimals = 5
	}
	if adapter.baseAsset == "" || adapter.quoteAsset == "" {
		base, quote := parseLinearSymbolBaseQuote(symbol)
		if adapter.baseAsset == "" {
			adapter.baseAsset = base
		}
		if adapter.quoteAsset == "" {
			adapter.quoteAsset = quote
		}
	}

	return adapter, nil
}

func (b *BybitSpotAdapter) fetchSpotInstrument(ctx context.Context) error {
	instruments, err := b.client.GetInstruments(ctx, bybitCategorySpot, b.symbol)
	if err != nil {
		return fmt.Errorf("查詢現貨交易對 %s 失败: %w", b.symbol, err)
	}
	if len(instruments) == 0 {
		return fmt.Errorf("未找到現貨交易對: %s", b.symbol)
	}
	return b.applySpotInstrument(instruments[0])
}

// applySpotInstrument 解析現貨規格：tickSize、basePrecision（數量步長）、minOrderQty、minOrderAmt
func (b *BybitSpotAdapter) applySpotInstrument(inst Instrument) error {
	tickSize, err := strconv.ParseFloat(inst.PriceFilter.TickSize, 64)
	if err != nil || tickSize <= 0 {
		return fmt.Errorf("現貨 %s tickSize 無效: %q", inst.Symbol, inst.PriceFilter.TickSize)
	}
	stepStr := inst.LotSizeFilter.BasePrecision
	if stepStr == "" {
		stepStr = inst.LotSizeFilter.QtyStep
	}
	qtyStep, err := strconv.ParseFloat(stepStr, 64)
	if err != nil || qtyStep <= 0 {
		return fmt.Errorf("現貨 %s basePrecision 無效: %q", inst.Symbol, stepStr)
	}
	minQty, err := strconv.ParseFloat(inst.LotSizeFilter.MinOrderQty, 64)
	if err != nil || minQty < 0 {
		minQty = qtyStep
	}
	minAmt, err := strconv.ParseFloat(inst.LotSizeFilter.MinOrderAmt, 64)
	if err != nil || minAmt < 0 {
		minAmt = 0
	}

	b.tickSize = tickSize
	b.qtyStep = qtyStep
	b.minOrderQty = minQty
	b.minOrderAmt = minAmt
	b.priceDecimals = getPrecision(tickSize)
	b.quantityDecimals = getPrecision(qtyStep)
	b.baseAsset = inst.BaseCoin
	b.quoteAsset = inst.QuoteCoin
	logger.Info("ℹ️ [Bybit Spot] %s - basePrecision:%v minOrderQty:%v minOrderAmt:%v tickSize:%v 數量精度:%d 價格精度:%d 基础:%s 计價:%s",
		b.symbol, qtyStep, minQty, minAmt, tickSize, b.quantityDecimals, b.priceDecimals, b.baseAsset, b.quoteAsset)
	return nil
}

// GetName 交易所名称
func (b *BybitSpotAdapter) GetName() string {
	return "Bybit Spot"
}

// GetMarketType 市場類型
func (b *BybitSpotAdapter) GetMarketType() string {
	return "spot"
}

// effectiveQtyStep 數量步長；交易對信息缺失時按數量精度推算
func (b *BybitSpotAdapter) effectiveQtyStep() float64 {
	if b.qtyStep > 0 {
		return b.qtyStep
	}
	return math.Pow10(-b.quantityDecimals)
}

// effectiveTickSize 價格步長；交易對信息缺失時按價格精度推算
func (b *BybitSpotAdapter) effectiveTickSize(priceDecimals int) float64 {
	if b.tickSize > 0 {
		return b.tickSize
	}
	if priceDecimals <= 0 {
		priceDecimals = b.priceDecimals
	}
	return math.Pow10(-priceDecimals)
}

// alignQuantity 數量向下取整到 basePrecision，低於 minOrderQty 直接拒絕（不靜默放大）
func (b *BybitSpotAdapter) alignQuantity(qty float64) (float64, error) {
	if math.IsNaN(qty) || math.IsInf(qty, 0) || qty <= 0 {
		return 0, fmt.Errorf("Bybit 現貨下單數量無效: %v", qty)
	}
	aligned := utils.FloorToStep(qty, b.effectiveQtyStep())
	if aligned <= 0 || (b.minOrderQty > 0 && aligned < b.minOrderQty) {
		return 0, fmt.Errorf("Bybit 現貨下單數量過小: %s 數量 %v（按 basePrecision=%v 對齊後 %v）低於最小下單量 %v %s",
			b.symbol, qty, b.effectiveQtyStep(), aligned, b.minOrderQty, b.baseAsset)
	}
	return aligned, nil
}

// alignPrice 價格按方向對齊到 tickSize：買單向下、賣單向上，避免 PostOnly 單穿價
func (b *BybitSpotAdapter) alignPrice(price float64, side Side, priceDecimals int) (float64, error) {
	if math.IsNaN(price) || math.IsInf(price, 0) || price <= 0 {
		return 0, fmt.Errorf("Bybit 現貨限價單價格無效: %v", price)
	}
	tick := b.effectiveTickSize(priceDecimals)
	switch side {
	case SideBuy:
		return utils.FloorToStep(price, tick), nil
	case SideSell:
		return utils.CeilToStep(price, tick), nil
	default:
		return 0, fmt.Errorf("Bybit 不支援的訂單方向: %q", side)
	}
}

// checkMinNotional 校驗下單金額不低於 minOrderAmt（price<=0 時無法估算，交給交易所校驗）
func (b *BybitSpotAdapter) checkMinNotional(qty, price float64) error {
	if b.minOrderAmt <= 0 || price <= 0 {
		return nil
	}
	if notional := qty * price; notional < b.minOrderAmt {
		return fmt.Errorf("Bybit 現貨下單金額過小: %s 數量 %v × 價格 %v = %v %s，低於最小下單金額 minOrderAmt=%v",
			b.symbol, qty, price, notional, b.quoteAsset, b.minOrderAmt)
	}
	return nil
}

// PlaceOrder 下單（現貨忽略 ReduceOnly）。請求須為 Bybit 原生值（Buy/Sell、Limit/Market），由 wrapper 映射。
func (b *BybitSpotAdapter) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	if req.Side != SideBuy && req.Side != SideSell {
		return nil, fmt.Errorf("Bybit 現貨不支援的訂單方向: %q（需為 Buy/Sell）", req.Side)
	}
	if req.Type != OrderTypeLimit && req.Type != OrderTypeMarket {
		return nil, fmt.Errorf("Bybit 現貨不支援的訂單類型: %q（需為 Limit/Market）", req.Type)
	}
	postOnly := req.PostOnly || req.TimeInForce == TimeInForcePO
	if req.Type == OrderTypeMarket && postOnly {
		return nil, fmt.Errorf("Bybit 現貨市價單不支援 PostOnly")
	}

	qty, err := b.alignQuantity(req.Quantity)
	if err != nil {
		return nil, err
	}

	orderReq := map[string]interface{}{
		"category":  bybitCategorySpot,
		"symbol":    b.symbol,
		"side":      string(req.Side),
		"orderType": string(req.Type),
		"qty":       strconv.FormatFloat(qty, 'f', getPrecision(b.effectiveQtyStep()), 64),
	}

	price := req.Price
	if req.Type == OrderTypeLimit {
		price, err = b.alignPrice(req.Price, req.Side, req.PriceDecimals)
		if err != nil {
			return nil, err
		}
		orderReq["price"] = strconv.FormatFloat(price, 'f', getPrecision(b.effectiveTickSize(req.PriceDecimals)), 64)
		if postOnly {
			orderReq["timeInForce"] = string(TimeInForcePO)
		} else {
			orderReq["timeInForce"] = string(TimeInForceGTC)
		}
	} else {
		// 現貨市價買單默認 qty 為計價幣金額，統一指定按基礎幣數量
		orderReq["marketUnit"] = bybitSpotMarketUnitBase
	}
	if err := b.checkMinNotional(qty, price); err != nil {
		return nil, err
	}
	if req.ClientOrderID != "" {
		orderReq["orderLinkId"] = utils.AddBrokerPrefix("bybit", req.ClientOrderID)
	}

	resp, err := b.client.PlaceOrder(ctx, orderReq)
	if err != nil {
		return nil, fmt.Errorf("Bybit 現貨下單失败(symbol=%s side=%s qty=%v): %w", b.symbol, req.Side, qty, err)
	}
	orderID, _ := strconv.ParseInt(resp.OrderId, 10, 64)
	now := time.Now()
	return &Order{
		OrderID:       orderID,
		ClientOrderID: resp.OrderLinkId,
		Symbol:        b.symbol,
		Side:          req.Side,
		Type:          req.Type,
		Price:         price,
		Quantity:      qty,
		Status:        OrderStatusNew,
		CreatedAt:     now,
		UpdateTime:    now.UnixMilli(),
	}, nil
}

// BatchPlaceOrders 批量下單
func (b *BybitSpotAdapter) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	placed := make([]*Order, 0, len(orders))
	hasBalanceError := false
	for _, req := range orders {
		order, err := b.PlaceOrder(ctx, req)
		if err != nil {
			logger.Warn("⚠️ [Bybit Spot] 下單失败 %.2f %s: %v", req.Price, req.Side, err)
			if strings.Contains(err.Error(), bybitInsufficientBalanceCode) || strings.Contains(err.Error(), "insufficient") {
				hasBalanceError = true
			}
			continue
		}
		placed = append(placed, order)
	}
	return placed, hasBalanceError
}

// CancelOrder 取消訂單（訂單已不存在視為成功）
func (b *BybitSpotAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	err := b.client.CancelOrder(ctx, bybitCategorySpot, b.symbol, strconv.FormatInt(orderID, 10), "")
	if err != nil {
		if strings.Contains(err.Error(), bybitOrderNotExistsCode) || strings.Contains(err.Error(), "Order does not exist") {
			logger.Info("ℹ️ [Bybit Spot] 订單 %d 已不存在，跳過取消", orderID)
			return nil
		}
		return fmt.Errorf("Bybit 現貨撤單失败(symbol=%s orderId=%d): %w", b.symbol, orderID, err)
	}
	return nil
}

// BatchCancelOrders 批量撤單（逐筆，失败匯總返回，不吞錯）
func (b *BybitSpotAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	var errs []error
	for _, id := range orderIDs {
		if err := b.CancelOrder(ctx, symbol, id); err != nil {
			errs = append(errs, err)
		}
		time.Sleep(bybitSpotCancelInterval)
	}
	return errors.Join(errs...)
}

// CancelAllOrders 取消該交易對下所有订單
func (b *BybitSpotAdapter) CancelAllOrders(ctx context.Context, symbol string) error {
	orders, err := b.client.GetOpenOrders(ctx, bybitCategorySpot, b.symbol)
	if err != nil {
		return fmt.Errorf("Bybit 現貨查詢挂單失败(symbol=%s): %w", b.symbol, err)
	}
	var errs []error
	for _, o := range orders {
		orderID, err := strconv.ParseInt(o.OrderId, 10, 64)
		if err != nil {
			errs = append(errs, fmt.Errorf("Bybit 現貨挂單 orderId=%q 無法解析: %w", o.OrderId, err))
			continue
		}
		if err := b.CancelOrder(ctx, symbol, orderID); err != nil {
			errs = append(errs, err)
		}
		time.Sleep(bybitSpotCancelInterval)
	}
	return errors.Join(errs...)
}

// GetOrder 查詢訂單
func (b *BybitSpotAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	order, err := b.client.GetOrder(ctx, bybitCategorySpot, b.symbol, strconv.FormatInt(orderID, 10), "")
	if err != nil {
		return nil, fmt.Errorf("Bybit 現貨查單失败(symbol=%s orderId=%d): %w", b.symbol, orderID, err)
	}
	return b.convertOrder(order), nil
}

// GetOpenOrders 未完成订單
func (b *BybitSpotAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	orders, err := b.client.GetOpenOrders(ctx, bybitCategorySpot, b.symbol)
	if err != nil {
		return nil, fmt.Errorf("Bybit 現貨查詢挂單失败(symbol=%s): %w", b.symbol, err)
	}
	result := make([]*Order, 0, len(orders))
	for i := range orders {
		result = append(result, b.convertOrder(&orders[i]))
	}
	return result, nil
}

// convertOrder REST 訂單 → 本地訂單（Side/Type/Status 保留 Bybit 原生值，由 wrapper 映射，未知值報錯而非透傳）
func (b *BybitSpotAdapter) convertOrder(order *BybitOrder) *Order {
	orderID, _ := strconv.ParseInt(order.OrderId, 10, 64)
	price, _ := strconv.ParseFloat(order.Price, 64)
	qty, _ := strconv.ParseFloat(order.Qty, 64)
	execQty, _ := strconv.ParseFloat(order.CumExecQty, 64)
	avgPrice, _ := strconv.ParseFloat(order.AvgPrice, 64)
	updateTime, _ := strconv.ParseInt(order.UpdatedTime, 10, 64)
	return &Order{
		OrderID:       orderID,
		ClientOrderID: order.OrderLinkId,
		Symbol:        b.symbol,
		Side:          Side(order.Side),
		Type:          OrderType(order.OrderType),
		Price:         price,
		Quantity:      qty,
		ExecutedQty:   execQty,
		AvgPrice:      avgPrice,
		Status:        OrderStatus(order.OrderStatus),
		UpdateTime:    updateTime,
	}
}

// GetAccount 現貨账戶餘額
func (b *BybitSpotAdapter) GetAccount(ctx context.Context) (*Account, error) {
	balances, err := b.client.GetBalance(ctx, "SPOT")
	if err != nil {
		return nil, err
	}
	var totalEquity, totalAvail float64
	for _, bal := range balances {
		eq, _ := strconv.ParseFloat(bal.TotalEquity, 64)
		avail, _ := strconv.ParseFloat(bal.TotalAvailableBalance, 64)
		totalEquity += eq
		totalAvail += avail
	}
	return &Account{
		TotalWalletBalance: totalEquity,
		TotalMarginBalance: totalEquity,
		AvailableBalance:   totalAvail,
		Positions:          nil,
	}, nil
}

// GetPositions 現貨“持倉”由基础资產餘額構成
func (b *BybitSpotAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	balances, err := b.client.GetBalance(ctx, "SPOT")
	if err != nil {
		return nil, err
	}
	base := b.baseAsset
	if base == "" {
		base = strings.TrimSuffix(symbol, "USDT")
	}
	var size float64
	for _, bal := range balances {
		for _, c := range bal.Coin {
			if c.Coin == base {
				wb, _ := strconv.ParseFloat(c.WalletBalance, 64)
				aw, _ := strconv.ParseFloat(c.AvailableToWithdraw, 64)
				if wb > size {
					size = wb
				}
				if aw > size {
					size = aw
				}
				break
			}
		}
	}
	if size <= 0 {
		return nil, nil
	}
	price, _ := b.GetLatestPrice(ctx, symbol)
	if price <= 0 {
		price = 0
	}
	return []*Position{{
		Symbol:         symbol,
		Size:           size,
		EntryPrice:     price,
		MarkPrice:      price,
		UnrealizedPNL:  0,
		Leverage:       1,
		MarginType:     "spot",
		IsolatedMargin: 0,
	}}, nil
}

// GetBalance 某资產餘額
func (b *BybitSpotAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	balances, err := b.client.GetBalance(ctx, "SPOT")
	if err != nil {
		return 0, err
	}
	for _, bal := range balances {
		for _, c := range bal.Coin {
			if c.Coin == asset {
				return strconv.ParseFloat(c.AvailableToWithdraw, 64)
			}
		}
	}
	return 0, nil
}

// StartOrderStream 現貨訂單流（v5/private，topic order）。
// order topic 覆蓋全部品類：只轉發 category=spot 且 symbol 為本交易對的推送，並映射為內部常量。
// 連接層複用合約的 WebSocketManager（認證確認後訂閱、斷線指數退避重連）。
func (b *BybitSpotAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	if b.wsManager == nil {
		b.wsManager = NewWebSocketManager(b.client.apiKey, b.client.secretKey, b.useTestnet)
	}
	b.wsManager.SetOrderCategory(bybitCategorySpot)
	return b.wsManager.Start(ctx, b.symbol, func(ou OrderUpdate) {
		update, err := b.normalizeOrderUpdate(ou)
		if err != nil {
			logger.Error("❌ [Bybit Spot WS] 丟棄無法識別的訂單推送 orderId=%d orderLinkId=%s symbol=%s: %v",
				ou.OrderID, ou.ClientOrderID, ou.Symbol, err)
			return
		}
		callback(update)
	})
}

// normalizeOrderUpdate 現貨推送 → 內部口徑：校驗交易對、Symbol 統一為配置值、Side/Type/Status 映射。
// Bybit order topic 無「本次成交」手續費，Commission 為 0，由上層經 GetOrderFills 補查（已換算為計價幣）。
func (b *BybitSpotAdapter) normalizeOrderUpdate(update OrderUpdate) (StreamOrderUpdate, error) {
	if update.Symbol != "" && !strings.EqualFold(update.Symbol, b.symbol) {
		return StreamOrderUpdate{}, fmt.Errorf("非本適配器交易對的推送: symbol=%s（期望 %s）", update.Symbol, b.symbol)
	}
	normalized, err := normalizeOrderUpdate(update)
	if err != nil {
		return StreamOrderUpdate{}, err
	}
	normalized.Symbol = b.symbol
	if normalized.Commission == 0 {
		normalized.CommissionAsset = b.quoteAsset
	}
	return normalized, nil
}

// StopOrderStream 停止私有訂單流
func (b *BybitSpotAdapter) StopOrderStream() error {
	if b.wsManager != nil {
		b.wsManager.Stop()
	}
	return nil
}

// GetLatestPrice 最新價（優先 WebSocket 緩存）
func (b *BybitSpotAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	if b.wsManager != nil {
		if p := b.wsManager.GetLatestPrice(); p > 0 {
			return p, nil
		}
	}
	ticker, err := b.client.GetTicker(ctx, bybitCategorySpot, symbol)
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(ticker.LastPrice, 64)
}

// StartPriceStream 公共現貨 WebSocket tickers.{symbol}
func (b *BybitSpotAdapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	if b.wsManager == nil {
		b.wsManager = NewWebSocketManager(b.client.apiKey, b.client.secretKey, b.useTestnet)
	}
	return b.wsManager.StartSpotPriceStream(ctx, b.symbol, callback)
}

// StartKlineStream 現貨公共 K 線（v5/public/spot）
func (b *BybitSpotAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(interface{})) error {
	if b.spotKlineWS != nil {
		b.spotKlineWS.Stop()
	}
	b.spotKlineWS = NewSpotKlineWebSocketManager(b.useTestnet)
	return b.spotKlineWS.Start(ctx, symbols, interval, callback)
}

// StopKlineStream 停止 K 線流
func (b *BybitSpotAdapter) StopKlineStream() error {
	if b.spotKlineWS != nil {
		b.spotKlineWS.Stop()
		b.spotKlineWS = nil
	}
	return nil
}

// GetHistoricalKlines 历史K線
func (b *BybitSpotAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	klines, err := b.client.GetKlines(ctx, bybitCategorySpot, symbol, interval, limit)
	if err != nil {
		return nil, err
	}
	candles := make([]*Candle, 0, len(klines))
	for _, k := range klines {
		open, _ := strconv.ParseFloat(k.OpenPrice, 64)
		high, _ := strconv.ParseFloat(k.HighPrice, 64)
		low, _ := strconv.ParseFloat(k.LowPrice, 64)
		closeP, _ := strconv.ParseFloat(k.ClosePrice, 64)
		vol, _ := strconv.ParseFloat(k.Volume, 64)
		ts, _ := strconv.ParseInt(k.StartTime, 10, 64)
		candles = append(candles, &Candle{
			Symbol:    symbol,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closeP,
			Volume:    vol,
			Timestamp: ts,
			IsClosed:  true,
		})
	}
	return candles, nil
}

// GetPriceDecimals 價格精度
func (b *BybitSpotAdapter) GetPriceDecimals() int {
	return b.priceDecimals
}

// GetQuantityDecimals 數量精度
func (b *BybitSpotAdapter) GetQuantityDecimals() int {
	return b.quantityDecimals
}

// GetBaseAsset 基础资產
func (b *BybitSpotAdapter) GetBaseAsset() string {
	return b.baseAsset
}

// GetQuoteAsset 计價资產
func (b *BybitSpotAdapter) GetQuoteAsset() string {
	return b.quoteAsset
}

// EstimateFinalOrderAmount 預估订單金額
func (b *BybitSpotAdapter) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return price * quantity
}

// GetFundingRate 現貨無资金费率
func (b *BybitSpotAdapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return 0, nil
}

// GetSpotPrice 現貨最新價
func (b *BybitSpotAdapter) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return b.GetLatestPrice(ctx, symbol)
}

// GetOrderBook 订單簿
func (b *BybitSpotAdapter) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	ob, err := b.client.GetOrderBook(ctx, bybitCategorySpot, symbol, limit)
	if err != nil {
		return nil, err
	}
	bids := make([]OrderBookLevel, 0, len(ob.Bids))
	for _, b := range ob.Bids {
		if len(b) >= 2 {
			price, _ := strconv.ParseFloat(b[0], 64)
			qty, _ := strconv.ParseFloat(b[1], 64)
			bids = append(bids, OrderBookLevel{Price: price, Quantity: qty})
		}
	}
	asks := make([]OrderBookLevel, 0, len(ob.Asks))
	for _, a := range ob.Asks {
		if len(a) >= 2 {
			price, _ := strconv.ParseFloat(a[0], 64)
			qty, _ := strconv.ParseFloat(a[1], 64)
			asks = append(asks, OrderBookLevel{Price: price, Quantity: qty})
		}
	}
	return &OrderBook{
		Symbol:    symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: ob.TS,
	}, nil
}

// InternalTransfer 現貨适配器暫不支援內部轉帳
func (b *BybitSpotAdapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return "", fmt.Errorf("Bybit 現貨适配器暫不支援內部轉帳，请在网页端操作")
}

// spotCommissionToQuote 把現貨手續費統一換算為計價幣口徑（position 包把 Commission 直接累加進 USDT 計的費用/盈虧）：
//   - 手續費幣種為基礎幣（Bybit 現貨買單常見）→ 乘以成交價，CommissionAsset 改為計價幣；
//   - 手續費幣種為計價幣或為空 → 原樣返回；
//   - 其他幣種無法換算 → 原樣返回並保留原幣種。
func (b *BybitSpotAdapter) spotCommissionToQuote(fee float64, feeCcy string, price float64) (float64, string) {
	switch {
	case feeCcy == "" || feeCcy == b.quoteAsset:
		return fee, b.quoteAsset
	case feeCcy == b.baseAsset && price > 0:
		return fee * price, b.quoteAsset
	default:
		return fee, feeCcy
	}
}

// BybitSpotOrderFill 現貨成交明細：在 BybitOrderFill 上附加基礎幣手續費數量
type BybitSpotOrderFill struct {
	BybitOrderFill
	// BaseFeeQty 本筆以基礎幣扣收的手續費（基礎幣單位，>=0）；其計價幣價值已包含在 Commission 中
	BaseFeeQty float64
}

// spotBaseFeeQty 手續費幣種為基礎幣且為支出時返回基礎幣數量，否則為 0
func spotBaseFeeQty(fee float64, feeCcy, baseAsset string) float64 {
	if baseAsset == "" || feeCcy != baseAsset || fee <= 0 {
		return 0
	}
	return fee
}

// GetOrderFills 查詢成交明細（category=spot）。Commission 支出為正、返佣為負（Bybit execFee 原生口徑），已換算為計價幣；
// 以基礎幣收取的手續費另在 BaseFeeQty 中給出原始數量。
func (b *BybitSpotAdapter) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*BybitSpotOrderFill, error) {
	oid := ""
	if orderID != 0 {
		oid = strconv.FormatInt(orderID, 10)
	}
	executions, err := b.client.GetOrderFills(ctx, bybitCategorySpot, b.symbol, oid)
	if err != nil {
		return nil, fmt.Errorf("Bybit 現貨查詢訂單 %d 成交明細失败(symbol=%s): %w", orderID, b.symbol, err)
	}
	fills := make([]*BybitSpotOrderFill, 0, len(executions))
	for _, exec := range executions {
		price, err := strconv.ParseFloat(exec.ExecPrice, 64)
		if err != nil {
			return nil, fmt.Errorf("Bybit 現貨成交明細 tradeId=%s execPrice 無效 %q: %w", exec.TradeId, exec.ExecPrice, err)
		}
		qty, err := strconv.ParseFloat(exec.ExecQty, 64)
		if err != nil {
			return nil, fmt.Errorf("Bybit 現貨成交明細 tradeId=%s execQty 無效 %q: %w", exec.TradeId, exec.ExecQty, err)
		}
		fee, _ := strconv.ParseFloat(exec.ExecFee, 64)
		tradeTime, _ := strconv.ParseInt(exec.ExecTime, 10, 64)
		ordID, _ := strconv.ParseInt(exec.OrderId, 10, 64)
		if ordID == 0 {
			ordID = orderID
		}
		commission, commissionAsset := b.spotCommissionToQuote(fee, exec.FeeCurrency, price)
		fills = append(fills, &BybitSpotOrderFill{
			BybitOrderFill: BybitOrderFill{
				OrderID:         ordID,
				TradeID:         exec.TradeId,
				Symbol:          b.symbol,
				Side:            exec.Side,
				Price:           price,
				Quantity:        qty,
				Commission:      commission,
				CommissionAsset: commissionAsset,
				TradeTime:       tradeTime,
				IsMaker:         exec.IsMaker,
			},
			BaseFeeQty: spotBaseFeeQty(fee, exec.FeeCurrency, b.baseAsset),
		})
	}
	return fills, nil
}
