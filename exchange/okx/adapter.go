package okx

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
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
	FillPrice       float64 // 本次成交價（fillPx）；現貨按基礎幣收取的手續費需用它換算為計價幣
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

	// 合約規格：策略層以「基礎幣數量」下單，OKX 合約 sz 單位是「張」，需按 ctVal 換算
	ctVal  float64 // 每張合約面值（基礎幣）；現貨/未知時按 1 處理
	lotSz  float64 // 下單數量步長（張）
	minSz  float64 // 最小下單數量（張）
	tickSz float64 // 價格步長

	// 持倉模式自檢：本系統不傳 posSide，僅支援 net_mode（單向持倉）
	posModeMu       sync.Mutex
	posModeVerified bool
}

const (
	// defaultCtVal 未取得合約面值（或現貨品種無 ctVal）時的面值：1 張 = 1 基礎幣
	defaultCtVal = 1.0
	// maxQtyDecimals 基礎幣數量換算後保留的最大小數位
	maxQtyDecimals = 12
	// okxPosModeLongShort OKX 雙向持倉模式標識
	okxPosModeLongShort = "long_short_mode"
	// okxInstTypeSwap 永續合約 instType
	okxInstTypeSwap = "SWAP"
	// okxInstTypeSpot 現貨 instType
	okxInstTypeSpot = "SPOT"
	// okxDefaultCommissionAsset 推送中缺少手續費幣種時的默認值（USDT 本位合約）
	okxDefaultCommissionAsset = "USDT"
	// okxExecTypeMaker 成交明細中 maker 標識
	okxExecTypeMaker = "M"
)

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
	if adapter.baseAsset == "" || adapter.quoteAsset == "" {
		b, q := parseFuturesSymbolBaseQuote(adapter.symbol)
		if adapter.baseAsset == "" {
			adapter.baseAsset = b
		}
		if adapter.quoteAsset == "" {
			adapter.quoteAsset = q
		}
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

func parseFuturesSymbolBaseQuote(sym string) (base, quote string) {
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

	return o.applyInstrument(instruments[0])
}

// applyInstrument 解析合約規格（tickSz / lotSz / minSz / ctVal）
func (o *OKXAdapter) applyInstrument(inst Instrument) error {
	tickSz, err := strconv.ParseFloat(inst.TickSz, 64)
	if err != nil || tickSz <= 0 {
		return fmt.Errorf("合約 %s tickSz 無效: %q", inst.InstId, inst.TickSz)
	}
	lotSz, err := strconv.ParseFloat(inst.LotSz, 64)
	if err != nil || lotSz <= 0 {
		return fmt.Errorf("合約 %s lotSz 無效: %q", inst.InstId, inst.LotSz)
	}
	minSz, err := strconv.ParseFloat(inst.MinSz, 64)
	if err != nil || minSz < 0 {
		minSz = lotSz
	}
	// 現貨品種沒有 ctVal，按 1 張 = 1 基礎幣處理
	ctVal := defaultCtVal
	if inst.CtVal != "" {
		v, err := strconv.ParseFloat(inst.CtVal, 64)
		if err != nil || v <= 0 {
			return fmt.Errorf("合約 %s ctVal 無效: %q", inst.InstId, inst.CtVal)
		}
		ctVal = v
	}

	o.tickSz = tickSz
	o.lotSz = lotSz
	o.minSz = minSz
	o.ctVal = ctVal
	o.priceDecimals = getPrecision(tickSz)
	// 對外暴露的數量精度是「基礎幣」精度：步長 = lotSz × ctVal
	o.quantityDecimals = o.baseQtyDecimals()
	if inst.CtValCcy != "" {
		o.baseAsset = inst.CtValCcy // 基础币种
	}
	if inst.SettleCcy != "" {
		o.quoteAsset = inst.SettleCcy // 結算币种
	}

	logger.Info("ℹ️ [OKX 合約信息] %s - ctVal:%v, lotSz:%v, minSz:%v, tickSz:%v, 數量精度(基礎幣):%d, 價格精度:%d, 基础币种:%s, 计價币种:%s",
		o.instId, ctVal, lotSz, minSz, tickSz, o.quantityDecimals, o.priceDecimals, o.baseAsset, o.quoteAsset)

	return nil
}

// effectiveCtVal 返回有效合約面值（未初始化時按 1）
func (o *OKXAdapter) effectiveCtVal() float64 {
	if o.ctVal > 0 {
		return o.ctVal
	}
	return defaultCtVal
}

// effectiveLotSz 返回數量步長（張）；合約信息缺失時按 quantityDecimals 推算
func (o *OKXAdapter) effectiveLotSz() float64 {
	if o.lotSz > 0 {
		return o.lotSz
	}
	return math.Pow10(-o.quantityDecimals)
}

// effectiveTickSz 返回價格步長；合約信息缺失時按價格精度推算
func (o *OKXAdapter) effectiveTickSz(priceDecimals int) float64 {
	if o.tickSz > 0 {
		return o.tickSz
	}
	if priceDecimals <= 0 {
		priceDecimals = o.priceDecimals
	}
	return math.Pow10(-priceDecimals)
}

// baseQtyDecimals 基礎幣數量的小數位（lotSz 小數位 + ctVal 小數位）
func (o *OKXAdapter) baseQtyDecimals() int {
	d := getPrecision(o.effectiveLotSz()) + getPrecision(o.effectiveCtVal())
	if d > maxQtyDecimals {
		return maxQtyDecimals
	}
	return d
}

// baseToContracts 基礎幣數量 → 合約張數：floor(qty/ctVal/lotSz)*lotSz，並校驗 minSz
func (o *OKXAdapter) baseToContracts(baseQty float64) (float64, error) {
	if math.IsNaN(baseQty) || math.IsInf(baseQty, 0) || baseQty <= 0 {
		return 0, fmt.Errorf("OKX 下單數量無效: %v", baseQty)
	}
	ctVal := o.effectiveCtVal()
	contracts := utils.FloorToStep(baseQty/ctVal, o.effectiveLotSz())
	if contracts <= 0 || (o.minSz > 0 && contracts < o.minSz) {
		return 0, fmt.Errorf("OKX 下單數量過小: %s 數量 %v（=%v 張，ctVal=%v）低於最小下單量 %v 張（=%v 基礎幣）",
			o.instId, baseQty, contracts, ctVal, o.minSz, o.contractsToBase(o.minSz))
	}
	return contracts, nil
}

// contractsToBase 合約張數 → 基礎幣數量
func (o *OKXAdapter) contractsToBase(contracts float64) float64 {
	factor := math.Pow10(o.baseQtyDecimals())
	return math.Round(contracts*o.effectiveCtVal()*factor) / factor
}

// alignPrice 價格按方向對齊到 tickSz：買單向下、賣單向上，避免 post only 單穿價
func (o *OKXAdapter) alignPrice(price float64, side Side, priceDecimals int) (float64, error) {
	if math.IsNaN(price) || math.IsInf(price, 0) || price <= 0 {
		return 0, fmt.Errorf("OKX 限價單價格無效: %v", price)
	}
	tick := o.effectiveTickSz(priceDecimals)
	switch side {
	case SideBuy:
		return utils.FloorToStep(price, tick), nil
	case SideSell:
		return utils.CeilToStep(price, tick), nil
	default:
		return 0, fmt.Errorf("OKX 不支援的訂單方向: %q", side)
	}
}

// ensureNetPositionMode 下單前自檢帳戶持倉模式（只在成功後緩存）。
// 本系統不傳 posSide，雙向持倉模式下每一單都會被拒，直接給出明確錯誤。
func (o *OKXAdapter) ensureNetPositionMode(ctx context.Context) error {
	o.posModeMu.Lock()
	defer o.posModeMu.Unlock()
	if o.posModeVerified {
		return nil
	}
	cfg, err := o.client.GetAccountConfig(ctx)
	if err != nil {
		return fmt.Errorf("OKX 下單前查詢持倉模式失败: %w", err)
	}
	if cfg.PosMode == okxPosModeLongShort {
		return fmt.Errorf("OKX 帳戶為雙向持倉模式(%s)，本系統僅支援單向持倉(net_mode)，請在 OKX 交易設置中切換後重試", cfg.PosMode)
	}
	o.posModeVerified = true
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
	if req.Side != SideBuy && req.Side != SideSell {
		return nil, fmt.Errorf("OKX 不支援的訂單方向: %q（需為 buy/sell）", req.Side)
	}
	// OKX 的 post only 是 ordType=post_only，不是獨立參數
	orderType := req.Type
	if req.PostOnly && orderType == OrderTypeLimit {
		orderType = OrderTypePostOnly
	}
	if _, err := ToInternalOrderType(orderType); err != nil {
		return nil, fmt.Errorf("OKX 下單失败: %w", err)
	}

	if err := o.ensureNetPositionMode(ctx); err != nil {
		return nil, err
	}

	// 基礎幣數量 → 合約張數（向下取整到 lotSz，低於 minSz 直接拒絕）
	contracts, err := o.baseToContracts(req.Quantity)
	if err != nil {
		return nil, err
	}
	lotSz := o.effectiveLotSz()

	// 構造订單请求
	orderReq := map[string]interface{}{
		"instId":  o.instId,
		"tdMode":  "cross", // 全倉模式
		"side":    string(req.Side),
		"ordType": string(orderType),
		"sz":      strconv.FormatFloat(contracts, 'f', getPrecision(lotSz), 64),
	}

	price := req.Price
	if orderType != OrderTypeMarket && orderType != OrderTypeOptimalLimitIOC {
		price, err = o.alignPrice(req.Price, req.Side, req.PriceDecimals)
		if err != nil {
			return nil, err
		}
		orderReq["px"] = strconv.FormatFloat(price, 'f', getPrecision(o.effectiveTickSz(req.PriceDecimals)), 64)
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
		Type:          orderType,
		Price:         price,
		Quantity:      o.contractsToBase(contracts),
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

// GetOrderByClientOrderID 查詢含終態的訂單，供提交結果不明時恢復使用。
func (o *OKXAdapter) GetOrderByClientOrderID(ctx context.Context, symbol, clientOrderID string) (*Order, error) {
	clientOrderID = utils.AddBrokerPrefix("okx", clientOrderID)
	order, err := o.client.GetOrder(ctx, o.instId, "", clientOrderID)
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

	// Side/Type/Status 保持 OKX 原生值，由 wrapper 通過映射表轉成內部常量（未知值報錯而非透傳）
	return &Order{
		OrderID:       orderID,
		ClientOrderID: order.ClOrdId,
		Symbol:        o.symbol,
		Side:          Side(order.Side),
		Type:          OrderType(order.OrdType),
		Price:         price,
		Quantity:      o.contractsToBase(quantity),
		ExecutedQty:   o.contractsToBase(executedQty),
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
			Size:           o.contractsToBase(size), // pos 單位為張（net_mode 下帶符號），換算為基礎幣
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
		genericUpdate, err := o.normalizeOrderUpdate(update)
		if err != nil {
			logger.Error("❌ [OKX WebSocket] 丟棄無法識別的訂單推送 ordId=%d clOrdId=%s: %v",
				update.OrderID, update.ClientOrderID, err)
			return
		}
		callback(genericUpdate)
	}

	return o.wsManager.Start(ctx, o.instId, localCallback)
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
	RealizedPnL     float64
}

// normalizeOrderUpdate 將 OKX 原生推送轉成內部口徑：
// Symbol 用配置的交易對（BTCUSDT 而非 BTC-USDT-SWAP）、數量由張換算為基礎幣、Side/Type/Status 映射為內部常量。
func (o *OKXAdapter) normalizeOrderUpdate(update OrderUpdate) (StreamOrderUpdate, error) {
	if update.Symbol != "" && update.Symbol != o.instId {
		return StreamOrderUpdate{}, fmt.Errorf("非本適配器合約的推送: instId=%s（期望 %s）", update.Symbol, o.instId)
	}
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
		Symbol:          o.symbol,
		Side:            side,
		Type:            orderType,
		Status:          status,
		Price:           update.Price,
		Quantity:        o.contractsToBase(update.Quantity),
		ExecutedQty:     o.contractsToBase(update.ExecutedQty),
		AvgPrice:        update.AvgPrice,
		UpdateTime:      update.UpdateTime,
		Commission:      update.Commission,
		CommissionAsset: update.CommissionAsset,
		RealizedPnL:     update.RealizedPnL,
	}, nil
}

// OKXOrderFill 訂單成交明細（本地類型，避免循環匯入；數量已換算為基礎幣）
type OKXOrderFill struct {
	OrderID         int64
	TradeID         string
	Symbol          string
	Side            Side
	Price           float64
	Quantity        float64
	Commission      float64 // 正數為支出，負數為返佣
	CommissionAsset string
	TradeTime       int64
	IsMaker         bool
}

// okxFeeToCommission OKX 手續費符號約定：扣費為負、返佣為正；內部 Commission 以「支出為正」計，故取反
func okxFeeToCommission(fee float64) float64 {
	return -fee
}

// GetOrderFills 查詢訂單成交明細（GET /api/v5/trade/fills，用於補充手續費）
func (o *OKXAdapter) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*OKXOrderFill, error) {
	rows, err := o.client.GetTradeFills(ctx, okxInstTypeSwap, o.instId, strconv.FormatInt(orderID, 10))
	if err != nil {
		return nil, fmt.Errorf("OKX 查詢訂單 %d 成交明細失败(instId=%s): %w", orderID, o.instId, err)
	}

	fills := make([]*OKXOrderFill, 0, len(rows))
	for _, r := range rows {
		price, err := strconv.ParseFloat(r.FillPx, 64)
		if err != nil {
			return nil, fmt.Errorf("OKX 成交明細 tradeId=%s fillPx 無效 %q: %w", r.TradeId, r.FillPx, err)
		}
		sz, err := strconv.ParseFloat(r.FillSz, 64)
		if err != nil {
			return nil, fmt.Errorf("OKX 成交明細 tradeId=%s fillSz 無效 %q: %w", r.TradeId, r.FillSz, err)
		}
		fee, _ := strconv.ParseFloat(r.Fee, 64)
		ts, _ := strconv.ParseInt(r.Ts, 10, 64)
		ordID := orderID
		if r.OrdId != "" {
			if v, err := strconv.ParseInt(r.OrdId, 10, 64); err == nil {
				ordID = v
			}
		}
		fills = append(fills, &OKXOrderFill{
			OrderID:         ordID,
			TradeID:         r.TradeId,
			Symbol:          o.symbol,
			Side:            Side(r.Side),
			Price:           price,
			Quantity:        o.contractsToBase(sz),
			Commission:      okxFeeToCommission(fee),
			CommissionAsset: r.FeeCcy,
			TradeTime:       ts,
			IsMaker:         r.ExecType == okxExecTypeMaker,
		})
	}
	return fills, nil
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
	if interval == "1h" {
		interval = "1H"
	}
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

// FundingInfo 資金費率詳情（與 exchange.FundingInfo 同構，供 wrapper 轉換）
type FundingInfo struct {
	Symbol          string
	Rate            float64
	NextFundingTime time.Time
	MarkPrice       float64
	IndexPrice      float64
}

func okxEstimateNextFundingUTC8h(now time.Time) time.Time {
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

// GetFundingInfo 獲取資金費率與下次結算時間（OKX public funding-rate）
func (o *OKXAdapter) GetFundingInfo(ctx context.Context, symbol string) (*FundingInfo, error) {
	fr, err := o.client.GetFundingRate(ctx, o.instId)
	if err != nil {
		return nil, fmt.Errorf("獲取资金费率失败: %w", err)
	}
	rate, _ := strconv.ParseFloat(fr.FundingRate, 64)
	var next time.Time
	if fr.NextTime != "" {
		if ms, err := strconv.ParseInt(fr.NextTime, 10, 64); err == nil && ms > 0 {
			next = time.UnixMilli(ms)
		}
	}
	if next.IsZero() {
		next = okxEstimateNextFundingUTC8h(time.Now().UTC())
	}
	mark, err := o.GetLatestPrice(ctx, symbol)
	if err != nil {
		mark = 0
	}
	return &FundingInfo{
		Symbol:          symbol,
		Rate:            rate,
		NextFundingTime: next,
		MarkPrice:       mark,
		IndexPrice:      mark,
	}, nil
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

// InternalTransfer 交易所內部轉帳（POST /api/v5/asset/transfer，type=0）
// 支援常見標籤：FUNDING/FUND ↔ TRADING/UNIFIED（資金帳戶 18 ↔ 交易帳戶 6）
func (o *OKXAdapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	from, to, err := mapOKXTransferEndpoints(fromAccount, toAccount)
	if err != nil {
		return "", err
	}
	body := map[string]interface{}{
		"ccy":  asset,
		"amt":  fmt.Sprintf("%.*f", 8, amount),
		"type": "0",
		"from": from,
		"to":   to,
	}
	return o.client.AssetTransfer(ctx, body)
}

func mapOKXTransferEndpoints(fromAccount, toAccount string) (from, to string, err error) {
	f := strings.ToUpper(strings.TrimSpace(fromAccount))
	t := strings.ToUpper(strings.TrimSpace(toAccount))
	switch {
	case (f == "FUNDING" || f == "FUND" || f == "SPOT") && (t == "TRADING" || t == "UNIFIED" || t == "MAIN" || t == "UMFUTURE" || t == "CONTRACT"):
		return "18", "6", nil
	case (f == "TRADING" || f == "UNIFIED" || f == "MAIN" || f == "UMFUTURE" || f == "CONTRACT") && (t == "FUNDING" || t == "FUND" || t == "SPOT"):
		return "6", "18", nil
	default:
		return "", "", fmt.Errorf("OKX 不支援的劃轉: %s -> %s（僅支援 FUNDING<->TRADING）", fromAccount, toAccount)
	}
}
