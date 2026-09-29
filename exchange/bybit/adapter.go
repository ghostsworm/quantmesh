package bybit

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

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
	PositionSide   string
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
	BalanceAsset       string
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

	// 合約規格
	qtyStep     float64 // 數量步長
	minOrderQty float64 // 最小下單數量
	tickSize    float64 // 價格步長

	// 持倉模式自檢：本系統不傳 positionIdx，僅支援單向持倉
	posModeMu       sync.Mutex
	posModeVerified bool
}

const (
	// bybitCategoryLinear USDT 永續
	bybitCategoryLinear = "linear"
	// bybitOneWayPositionIdx 單向持倉模式下的 positionIdx
	bybitOneWayPositionIdx = "0"
)

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
	if adapter.baseAsset == "" || adapter.quoteAsset == "" {
		b, q := parseLinearSymbolBaseQuote(adapter.symbol)
		if adapter.baseAsset == "" {
			adapter.baseAsset = b
		}
		if adapter.quoteAsset == "" {
			adapter.quoteAsset = q
		}
	}

	return adapter, nil
}

// parseLinearSymbolBaseQuote 從線性永续符号推斷 base/quote（REST 失敗時兜底，如測試環境無外網）
func parseLinearSymbolBaseQuote(sym string) (base, quote string) {
	s := strings.TrimSpace(strings.ToUpper(sym))
	if s == "" {
		return "", ""
	}
	for _, suf := range []string{"USDT", "USDC", "BUSD"} {
		if strings.HasSuffix(s, suf) {
			return strings.TrimSuffix(s, suf), suf
		}
	}
	if strings.HasSuffix(s, "USD") {
		return strings.TrimSuffix(s, "USD"), "USD"
	}
	return "", ""
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

	return b.applyInstrument(instruments[0])
}

// applyInstrument 解析合約規格（tickSize / qtyStep / minOrderQty）
func (b *BybitAdapter) applyInstrument(inst Instrument) error {
	tickSize, err := strconv.ParseFloat(inst.PriceFilter.TickSize, 64)
	if err != nil || tickSize <= 0 {
		return fmt.Errorf("合約 %s tickSize 無效: %q", inst.Symbol, inst.PriceFilter.TickSize)
	}
	qtyStep, err := strconv.ParseFloat(inst.LotSizeFilter.QtyStep, 64)
	if err != nil || qtyStep <= 0 {
		return fmt.Errorf("合約 %s qtyStep 無效: %q", inst.Symbol, inst.LotSizeFilter.QtyStep)
	}
	minQty, err := strconv.ParseFloat(inst.LotSizeFilter.MinOrderQty, 64)
	if err != nil || minQty < 0 {
		minQty = qtyStep
	}

	b.tickSize = tickSize
	b.qtyStep = qtyStep
	b.minOrderQty = minQty
	b.priceDecimals = getPrecision(tickSize)
	b.quantityDecimals = getPrecision(qtyStep)
	b.baseAsset = inst.BaseCoin
	b.quoteAsset = inst.QuoteCoin

	logger.Info("ℹ️ [Bybit 合約信息] %s - qtyStep:%v, minOrderQty:%v, tickSize:%v, 數量精度:%d, 價格精度:%d, 基础币种:%s, 计價币种:%s",
		b.symbol, qtyStep, minQty, tickSize, b.quantityDecimals, b.priceDecimals, b.baseAsset, b.quoteAsset)

	return nil
}

// effectiveQtyStep 數量步長；合約信息缺失時按數量精度推算
func (b *BybitAdapter) effectiveQtyStep() float64 {
	if b.qtyStep > 0 {
		return b.qtyStep
	}
	return math.Pow10(-b.quantityDecimals)
}

// effectiveTickSize 價格步長；合約信息缺失時按價格精度推算
func (b *BybitAdapter) effectiveTickSize(priceDecimals int) float64 {
	if b.tickSize > 0 {
		return b.tickSize
	}
	if priceDecimals <= 0 {
		priceDecimals = b.priceDecimals
	}
	return math.Pow10(-priceDecimals)
}

// alignQuantity 數量向下取整到 qtyStep，低於最小下單量直接拒絕（不再靜默放大）
func (b *BybitAdapter) alignQuantity(qty float64) (float64, error) {
	if math.IsNaN(qty) || math.IsInf(qty, 0) || qty <= 0 {
		return 0, fmt.Errorf("Bybit 下單數量無效: %v", qty)
	}
	aligned := utils.FloorToStep(qty, b.effectiveQtyStep())
	if aligned <= 0 || (b.minOrderQty > 0 && aligned < b.minOrderQty) {
		return 0, fmt.Errorf("Bybit 下單數量過小: %s 數量 %v（對齊後 %v）低於最小下單量 %v", b.symbol, qty, aligned, b.minOrderQty)
	}
	return aligned, nil
}

// alignPrice 價格按方向對齊到 tickSize：買單向下、賣單向上，避免 PostOnly 單穿價
func (b *BybitAdapter) alignPrice(price float64, side Side, priceDecimals int) (float64, error) {
	if math.IsNaN(price) || math.IsInf(price, 0) || price <= 0 {
		return 0, fmt.Errorf("Bybit 限價單價格無效: %v", price)
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

// ensureOneWayPositionMode 下單前自檢持倉模式（只在成功後緩存）。
// 雙向持倉（hedge）下 /v5/position/list 返回 positionIdx=1/2 的記錄，本系統不傳 positionIdx，每單都會被拒。
func (b *BybitAdapter) ensureOneWayPositionMode(ctx context.Context) error {
	b.posModeMu.Lock()
	defer b.posModeMu.Unlock()
	if b.posModeVerified {
		return nil
	}
	positions, err := b.client.GetPositions(ctx, bybitCategoryLinear, b.symbol)
	if err != nil {
		return fmt.Errorf("Bybit 下單前查詢持倉模式失败(symbol=%s): %w", b.symbol, err)
	}
	for _, p := range positions {
		if idx := p.PositionIdx.String(); idx != "" && idx != bybitOneWayPositionIdx {
			return fmt.Errorf("Bybit %s 為雙向持倉模式(positionIdx=%s)，本系統僅支援單向持倉(One-Way Mode)，請在 Bybit 切換後重試", b.symbol, idx)
		}
	}
	b.posModeVerified = true
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
	if req.Side != SideBuy && req.Side != SideSell {
		return nil, fmt.Errorf("Bybit 不支援的訂單方向: %q（需為 Buy/Sell）", req.Side)
	}
	if req.Type != OrderTypeLimit && req.Type != OrderTypeMarket {
		return nil, fmt.Errorf("Bybit 不支援的訂單類型: %q（需為 Limit/Market）", req.Type)
	}
	if req.Type == OrderTypeMarket && req.PostOnly {
		return nil, fmt.Errorf("Bybit 市價單不支援 PostOnly")
	}

	if err := b.ensureOneWayPositionMode(ctx); err != nil {
		return nil, err
	}

	qty, err := b.alignQuantity(req.Quantity)
	if err != nil {
		return nil, err
	}

	// 構造订單请求
	orderReq := map[string]interface{}{
		"category":  bybitCategoryLinear,
		"symbol":    req.Symbol,
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
		if req.PostOnly || req.TimeInForce == TimeInForcePO {
			orderReq["timeInForce"] = string(TimeInForcePO)
		} else {
			orderReq["timeInForce"] = string(TimeInForceGTC)
		}
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
		Price:         price,
		Quantity:      qty,
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

// GetOrderByClientOrderID queries Bybit's order-history endpoint, which includes terminal orders.
func (b *BybitAdapter) GetOrderByClientOrderID(ctx context.Context, symbol, clientOrderID string) (*Order, error) {
	clientOrderID = utils.AddBrokerPrefix("bybit", clientOrderID)
	order, err := b.client.GetOrderHistoryByClientID(ctx, bybitCategoryLinear, symbol, clientOrderID)
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

	// Side/Type/Status 保持 Bybit 原生值，由 wrapper 通過映射表轉成內部常量（未知值報錯而非透傳）
	return &Order{
		OrderID:       orderID,
		ClientOrderID: order.OrderLinkId,
		Symbol:        order.Symbol,
		Side:          Side(order.Side),
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
	totalBalance, availableBalance, marginBalance, err := summarizeBybitUnifiedBalance(balance)
	if err != nil {
		return nil, err
	}

	// 獲取持倉
	positions, err := b.GetPositions(ctx, b.symbol)
	if err != nil {
		return nil, fmt.Errorf("query Bybit positions for account snapshot: %w", err)
	}

	return &Account{
		TotalWalletBalance: totalBalance,
		TotalMarginBalance: marginBalance,
		AvailableBalance:   availableBalance,
		BalanceAsset:       "USD",
		Positions:          positions,
	}, nil
}

func summarizeBybitUnifiedBalance(balances []Balance) (equity, available, margin float64, err error) {
	if len(balances) == 0 {
		return 0, 0, 0, nil
	}
	if len(balances) != 1 {
		return 0, 0, 0, fmt.Errorf("Bybit unified balance returned %d account rows, expected one", len(balances))
	}
	parse := func(name, value string) (float64, error) {
		parsed, parseErr := strconv.ParseFloat(value, 64)
		if parseErr != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 {
			return 0, fmt.Errorf("invalid Bybit unified %s USD value %q", name, value)
		}
		return parsed, nil
	}
	if equity, err = parse("totalEquity", balances[0].TotalEquity); err != nil {
		return 0, 0, 0, err
	}
	if available, err = parse("totalAvailableBalance", balances[0].TotalAvailableBalance); err != nil {
		return 0, 0, 0, err
	}
	if margin, err = parse("totalMarginBalance", balances[0].TotalMarginBalance); err != nil {
		return 0, 0, 0, err
	}
	return equity, available, margin, nil
}

// GetPositions 獲取持倉信息
func (b *BybitAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	positions, err := b.client.GetPositions(ctx, "linear", symbol)
	if err != nil {
		return nil, err
	}

	result := make([]*Position, 0)
	for _, pos := range positions {
		size, err := strconv.ParseFloat(pos.Size, 64)
		if err != nil {
			return nil, fmt.Errorf("Bybit returned invalid position size %q for %s", pos.Size, pos.Symbol)
		}
		size, positionSide, err := normalizeBybitPositionSide(pos.PositionIdx.String(), pos.Side, size, pos.Symbol)
		if err != nil {
			return nil, err
		}

		entryPrice, _ := strconv.ParseFloat(pos.AvgPrice, 64)
		markPrice, _ := strconv.ParseFloat(pos.MarkPrice, 64)
		unrealizedPNL, _ := strconv.ParseFloat(pos.UnrealisedPnl, 64)
		leverage, _ := strconv.Atoi(pos.Leverage)

		result = append(result, &Position{
			Symbol:         pos.Symbol,
			Size:           size,
			PositionSide:   positionSide,
			EntryPrice:     entryPrice,
			MarkPrice:      markPrice,
			UnrealizedPNL:  unrealizedPNL,
			Leverage:       leverage,
			MarginType:     pos.TradeMode.String(),
			IsolatedMargin: 0,
		})
	}

	return result, nil
}

func normalizeBybitPositionSide(positionIdx, side string, size float64, symbol string) (float64, string, error) {
	if math.IsNaN(size) || math.IsInf(size, 0) || size < 0 {
		return 0, "", fmt.Errorf("Bybit returned invalid position size %v for %s", size, symbol)
	}
	side = strings.ToUpper(strings.TrimSpace(side))
	switch positionIdx {
	case "0":
		if size == 0 {
			if side != "" {
				return 0, "", fmt.Errorf("Bybit returned side %q for empty one-way position %s", side, symbol)
			}
			return 0, "NET", nil
		}
		switch side {
		case "BUY":
			return size, "NET", nil
		case "SELL":
			return -size, "NET", nil
		default:
			return 0, "", fmt.Errorf("Bybit returned invalid one-way position side %q for non-zero position %s", side, symbol)
		}
	case "1":
		if size > 0 && side != "BUY" || size == 0 && side != "" && side != "BUY" {
			return 0, "", fmt.Errorf("Bybit LONG position has inconsistent side %q for %s", side, symbol)
		}
		return size, "LONG", nil
	case "2":
		if size > 0 && side != "SELL" || size == 0 && side != "" && side != "SELL" {
			return 0, "", fmt.Errorf("Bybit SHORT position has inconsistent side %q for %s", side, symbol)
		}
		return size, "SHORT", nil
	default:
		return 0, "", fmt.Errorf("Bybit returned unsupported positionIdx %q for %s", positionIdx, symbol)
	}
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

	// order topic 覆蓋全部品類，合約適配器只處理 linear，避免同名現貨訂單混入
	b.wsManager.SetOrderCategory(bybitCategoryLinear)

	localCallback := func(update OrderUpdate) {
		genericUpdate, err := normalizeOrderUpdate(update)
		if err != nil {
			logger.Error("❌ [Bybit WebSocket] 丟棄無法識別的訂單推送 orderId=%d orderLinkId=%s symbol=%s: %v",
				update.OrderID, update.ClientOrderID, update.Symbol, err)
			return
		}
		callback(genericUpdate)
	}

	return b.wsManager.Start(ctx, b.symbol, localCallback)
}

// StreamOrderUpdate 推給上層的訂單更新（字段名與 exchange.OrderUpdate 一致，供反射讀取）
type StreamOrderUpdate struct {
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
	CommissionKnown bool // Bybit order topic has no per-execution fee; REST execution history must verify it.
	RealizedPnL     float64
}

// normalizeOrderUpdate 將 Bybit 原生推送的 Side/Type/Status 映射為內部常量（未知值報錯）
func normalizeOrderUpdate(update OrderUpdate) (StreamOrderUpdate, error) {
	side, err := ToInternalSide(update.Side)
	if err != nil {
		return StreamOrderUpdate{}, err
	}
	orderType, err := ToInternalOrderType(update.Type)
	if err != nil {
		return StreamOrderUpdate{}, err
	}
	status, err := ToInternalStatus(update.Status)
	if err != nil {
		return StreamOrderUpdate{}, err
	}
	return StreamOrderUpdate{
		OrderID:         update.OrderID,
		ClientOrderID:   update.ClientOrderID,
		Symbol:          update.Symbol,
		Side:            side,
		Type:            orderType,
		Status:          status,
		Price:           update.Price,
		Quantity:        update.Quantity,
		ExecutedQty:     update.ExecutedQty,
		AvgPrice:        update.AvgPrice,
		UpdateTime:      update.UpdateTime,
		Commission:      update.Commission,
		CommissionAsset: update.CommissionAsset,
		RealizedPnL:     update.RealizedPnL,
	}, nil
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
	if interval == "1h" {
		interval = "60"
	} else {
		interval = strings.TrimSuffix(interval, "m")
	}
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

// FundingInfo 資金費率詳情（供 exchange wrapper 轉換）
type FundingInfo struct {
	Symbol          string
	Rate            float64
	NextFundingTime time.Time
	MarkPrice       float64
	IndexPrice      float64
}

func bybitEstimateNextFundingUTC8h(now time.Time) time.Time {
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

// GetFundingInfo 從 /v5/market/tickers 獲取資金費與下次結算時間
func (b *BybitAdapter) GetFundingInfo(ctx context.Context, symbol string) (*FundingInfo, error) {
	tk, err := b.client.GetFundingTicker(ctx, "linear", symbol)
	if err != nil {
		return nil, fmt.Errorf("獲取 funding ticker 失败: %w", err)
	}
	rate, _ := strconv.ParseFloat(tk.FundingRate, 64)
	mark, _ := strconv.ParseFloat(tk.MarkPrice, 64)
	idx, _ := strconv.ParseFloat(tk.IndexPrice, 64)
	var next time.Time
	if tk.NextFundingTime != "" {
		if ms, err := strconv.ParseInt(tk.NextFundingTime, 10, 64); err == nil && ms > 0 {
			next = time.UnixMilli(ms)
		}
	}
	if next.IsZero() {
		next = bybitEstimateNextFundingUTC8h(time.Now().UTC())
	}
	return &FundingInfo{
		Symbol:          symbol,
		Rate:            rate,
		NextFundingTime: next,
		MarkPrice:       mark,
		IndexPrice:      idx,
	}, nil
}

// GetSpotPrice 獲取現貨市场價格
// BybitOrderFill 訂單成交記錄（本地類型，避免循環導入）
type BybitOrderFill struct {
	OrderID          int64
	TradeID          string
	Symbol           string
	Side             string
	Price            float64
	Quantity         float64
	Commission       float64
	CommissionAsset  string
	TradeTime        int64
	IsMaker          bool
	RealizedPnL      float64
	RealizedPnLKnown bool
}

func (b *BybitAdapter) GetExecutionHistoryPage(ctx context.Context, symbol string, startTime, endTime int64, cursor string, limit int) ([]BybitExecution, string, error) {
	return b.client.GetExecutionHistoryPage(ctx, "linear", symbol, startTime, endTime, cursor, limit)
}

func (b *BybitAdapter) GetOrderHistoryPage(ctx context.Context, symbol string, startTime, endTime int64, cursor string, limit int) ([]*BybitOrderFill, string, error) {
	rows, next, err := b.GetExecutionHistoryPage(ctx, symbol, startTime, endTime, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	fills := make([]*BybitOrderFill, 0, len(rows))
	for _, row := range rows {
		orderID, e1 := strconv.ParseInt(row.OrderId, 10, 64)
		price, e2 := strconv.ParseFloat(row.ExecPrice, 64)
		qty, e3 := strconv.ParseFloat(row.ExecQty, 64)
		fee, e4 := strconv.ParseFloat(row.ExecFee, 64)
		tradeTime, e5 := strconv.ParseInt(row.ExecTime, 10, 64)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || orderID <= 0 || row.TradeId == "" || row.Symbol != symbol || price <= 0 || qty <= 0 || tradeTime <= 0 {
			return nil, "", fmt.Errorf("Bybit returned invalid linear execution tradeId=%s", row.TradeId)
		}
		pnl, err := strconv.ParseFloat(row.ClosedPnl, 64)
		if err != nil && row.ClosedPnl != "" {
			return nil, "", fmt.Errorf("parse Bybit closed PnL tradeId=%s: %w", row.TradeId, err)
		}
		fills = append(fills, &BybitOrderFill{OrderID: orderID, TradeID: row.TradeId, Symbol: row.Symbol, Side: row.Side, Price: price, Quantity: qty, Commission: fee, CommissionAsset: row.FeeCurrency, TradeTime: tradeTime, IsMaker: row.IsMaker, RealizedPnL: pnl, RealizedPnLKnown: row.ClosedPnl != ""})
	}
	return fills, next, nil
}

// GetOrderFills 查詢訂單成交記錄（用於獲取手續費）
func (b *BybitAdapter) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*BybitOrderFill, error) {
	executions, err := b.client.GetOrderFills(ctx, "linear", symbol, strconv.FormatInt(orderID, 10))
	if err != nil {
		return nil, err
	}

	fills := make([]*BybitOrderFill, 0, len(executions))
	for _, exec := range executions {
		price, _ := strconv.ParseFloat(exec.ExecPrice, 64)
		qty, _ := strconv.ParseFloat(exec.ExecQty, 64)
		commission, _ := strconv.ParseFloat(exec.ExecFee, 64)
		tradeTime, _ := strconv.ParseInt(exec.ExecTime, 10, 64)

		fills = append(fills, &BybitOrderFill{
			OrderID:         orderID,
			TradeID:         exec.TradeId,
			Symbol:          exec.Symbol,
			Side:            exec.Side,
			Price:           price,
			Quantity:        qty,
			Commission:      commission,
			CommissionAsset: exec.FeeCurrency,
			TradeTime:       tradeTime,
			IsMaker:         exec.IsMaker,
		})
	}

	return fills, nil
}

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

// InternalTransfer 交易所內部轉帳（POST /v5/asset/transfer：CONTRACT↔SPOT、FUND↔UNIFIED）
func (b *BybitAdapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	from, to, err := mapBybitTransferAccounts(fromAccount, toAccount)
	if err != nil {
		return "", err
	}
	coin := strings.TrimSpace(asset)
	if coin == "" {
		return "", fmt.Errorf("Bybit 劃轉需要指定資產代碼")
	}
	amt := strconv.FormatFloat(amount, 'f', 8, 64)
	tid := uuid.New().String()
	return b.client.CreateUniversalTransfer(ctx, tid, coin, amt, from, to)
}

func mapBybitTransferAccounts(fromAccount, toAccount string) (from, to string, err error) {
	f := strings.ToUpper(strings.TrimSpace(fromAccount))
	t := strings.ToUpper(strings.TrimSpace(toAccount))
	switch {
	case (f == "UMFUTURE" || f == "CONTRACT" || f == "LINEAR") && (t == "SPOT" || t == "MAIN"):
		return "CONTRACT", "SPOT", nil
	case (f == "SPOT" || f == "MAIN") && (t == "UMFUTURE" || t == "CONTRACT" || t == "LINEAR"):
		return "SPOT", "CONTRACT", nil
	case (f == "FUNDING" || f == "FUND") && (t == "UNIFIED" || t == "TRADING"):
		return "FUND", "UNIFIED", nil
	case (f == "UNIFIED" || f == "TRADING") && (t == "FUNDING" || t == "FUND"):
		return "UNIFIED", "FUND", nil
	default:
		return "", "", fmt.Errorf("Bybit 不支援的劃轉: %s -> %s（支援 UMFUTURE↔SPOT、FUND↔UNIFIED）", fromAccount, toAccount)
	}
}
