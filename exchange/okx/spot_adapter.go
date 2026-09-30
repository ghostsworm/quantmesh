package okx

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
	// okxSpotTdModeCash 現貨非槓桿交易模式
	okxSpotTdModeCash = "cash"
	// okxSpotTgtCcyBase 市價單 sz 以基礎幣計（OKX 現貨市價買單默認按計價幣計）
	okxSpotTgtCcyBase = "base_ccy"
	// okxSpotDefaultQuote 無法從交易對解析計價幣時的默認值
	okxSpotDefaultQuote = "USDT"
	// okxSpotCancelInterval 逐筆撤單間隔，避免觸發限頻
	okxSpotCancelInterval = 50 * time.Millisecond
	// okxInsufficientBalanceCode OKX 餘額不足錯誤碼
	okxInsufficientBalanceCode = "51008"
)

// symbolToSpotInstId BTCUSDT -> BTC-USDT
func symbolToSpotInstId(symbol string) string {
	symbol = strings.TrimSpace(symbol)
	if strings.HasSuffix(symbol, "USDT") {
		base := strings.TrimSuffix(symbol, "USDT")
		return base + "-USDT"
	}
	if strings.Contains(symbol, "USDT") {
		return strings.ReplaceAll(symbol, "USDT", "-USDT")
	}
	return symbol
}

// OKXSpotAdapter OKX 現貨适配器
type OKXSpotAdapter struct {
	client           *OKXClient
	symbol           string
	instId           string // 如 BTC-USDT
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
	useTestnet       bool
	wsManager        *WebSocketManager // 公共 tickers 價格流（與合約適配器相同）
	spotOrderWS      *SpotOrderWebSocketManager
	klineWS          *KlineWebSocketManager

	// 來自 instruments（用於價格/數量對齊與最小下單量校驗）
	tickSz float64
	lotSz  float64
	minSz  float64
}

// NewOKXSpotAdapter 創建 OKX 現貨适配器
func NewOKXSpotAdapter(cfg map[string]string, symbol string) (*OKXSpotAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]
	passphrase := cfg["passphrase"]
	testnet := false
	if v, ok := cfg["testnet"]; ok && (v == "true" || v == "1") {
		testnet = true
	}
	if apiKey == "" || secretKey == "" || passphrase == "" {
		return nil, fmt.Errorf("OKX API 配置不完整（現貨需要 api_key、secret_key、passphrase）")
	}
	client := NewOKXClient(apiKey, secretKey, passphrase, testnet)
	instId := symbolToSpotInstId(symbol)
	adapter := &OKXSpotAdapter{
		client:     client,
		symbol:     symbol,
		instId:     instId,
		useTestnet: testnet,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := adapter.fetchSpotInstrument(ctx); err != nil {
		// 不再寫死 tickSz/lotSz/minSz：步長按精度推算，最小下單量交給交易所校驗
		logger.Warn("⚠️ [OKX Spot] 獲取交易對信息失败: %v，使用默认精度", err)
		adapter.priceDecimals = 2
		adapter.quantityDecimals = 5
	}
	if adapter.baseAsset == "" || adapter.quoteAsset == "" {
		base, quote := splitSpotInstId(instId)
		if adapter.baseAsset == "" {
			adapter.baseAsset = base
		}
		if adapter.quoteAsset == "" {
			adapter.quoteAsset = quote
		}
	}
	return adapter, nil
}

// splitSpotInstId BTC-USDT → BTC, USDT
func splitSpotInstId(instId string) (base, quote string) {
	parts := strings.SplitN(instId, "-", 2)
	base = parts[0]
	quote = okxSpotDefaultQuote
	if len(parts) == 2 && parts[1] != "" {
		quote = parts[1]
	}
	return base, quote
}

func (o *OKXSpotAdapter) fetchSpotInstrument(ctx context.Context) error {
	instruments, err := o.client.GetInstruments(ctx, okxInstTypeSpot, o.instId)
	if err != nil {
		return fmt.Errorf("查詢現貨交易對 %s 失败: %w", o.instId, err)
	}
	if len(instruments) == 0 {
		return fmt.Errorf("未找到現貨交易對: %s", o.instId)
	}
	return o.applySpotInstrument(instruments[0])
}

// applySpotInstrument 解析現貨規格（tickSz / lotSz / minSz / baseCcy / quoteCcy）
func (o *OKXSpotAdapter) applySpotInstrument(inst Instrument) error {
	tickSz, err := strconv.ParseFloat(inst.TickSz, 64)
	if err != nil || tickSz <= 0 {
		return fmt.Errorf("現貨 %s tickSz 無效: %q", inst.InstId, inst.TickSz)
	}
	lotSz, err := strconv.ParseFloat(inst.LotSz, 64)
	if err != nil || lotSz <= 0 {
		return fmt.Errorf("現貨 %s lotSz 無效: %q", inst.InstId, inst.LotSz)
	}
	minSz, err := strconv.ParseFloat(inst.MinSz, 64)
	if err != nil || minSz < 0 {
		minSz = lotSz
	}
	o.tickSz = tickSz
	o.lotSz = lotSz
	o.minSz = minSz
	o.priceDecimals = getPrecision(tickSz)
	o.quantityDecimals = getPrecision(lotSz)
	base, quote := splitSpotInstId(o.instId)
	o.baseAsset = inst.BaseCcy
	if o.baseAsset == "" {
		o.baseAsset = base
	}
	o.quoteAsset = inst.QuoteCcy
	if o.quoteAsset == "" {
		o.quoteAsset = quote
	}
	logger.Info("ℹ️ [OKX Spot] %s - tickSz:%v lotSz:%v minSz:%v 數量精度:%d 價格精度:%d",
		o.instId, tickSz, lotSz, minSz, o.quantityDecimals, o.priceDecimals)
	return nil
}

// GetName 交易所名称
func (o *OKXSpotAdapter) GetName() string {
	return "OKX Spot"
}

// GetMarketType 市場類型
func (o *OKXSpotAdapter) GetMarketType() string {
	return "spot"
}

// effectiveLotSz 數量步長；交易對信息缺失時按數量精度推算
func (o *OKXSpotAdapter) effectiveLotSz() float64 {
	if o.lotSz > 0 {
		return o.lotSz
	}
	return math.Pow10(-o.quantityDecimals)
}

// effectiveTickSz 價格步長；交易對信息缺失時按價格精度推算
func (o *OKXSpotAdapter) effectiveTickSz(priceDecimals int) float64 {
	if o.tickSz > 0 {
		return o.tickSz
	}
	if priceDecimals <= 0 {
		priceDecimals = o.priceDecimals
	}
	return math.Pow10(-priceDecimals)
}

// alignQuantity 數量向下取整到 lotSz，低於 minSz 直接拒絕（不靜默放大）
func (o *OKXSpotAdapter) alignQuantity(qty float64) (float64, error) {
	if math.IsNaN(qty) || math.IsInf(qty, 0) || qty <= 0 {
		return 0, fmt.Errorf("OKX 現貨下單數量無效: %v", qty)
	}
	aligned := utils.FloorToStep(qty, o.effectiveLotSz())
	if aligned <= 0 || (o.minSz > 0 && aligned < o.minSz) {
		return 0, fmt.Errorf("OKX 現貨下單數量過小: %s 數量 %v（按 lotSz=%v 對齊後 %v）低於最小下單量 minSz=%v %s",
			o.instId, qty, o.effectiveLotSz(), aligned, o.minSz, o.baseAsset)
	}
	return aligned, nil
}

// alignPrice 價格按方向對齊到 tickSz：買單向下、賣單向上，避免 post only 單穿價
func (o *OKXSpotAdapter) alignPrice(price float64, side Side, priceDecimals int) (float64, error) {
	if math.IsNaN(price) || math.IsInf(price, 0) || price <= 0 {
		return 0, fmt.Errorf("OKX 現貨限價單價格無效: %v", price)
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

// PlaceOrder 下單（現貨 tdMode=cash，忽略 ReduceOnly）。
// 請求須為 OKX 原生值（buy/sell、limit/market/post_only），由 wrapper 通過映射表轉換。
func (o *OKXSpotAdapter) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	if _, err := ToInternalSide(req.Side); err != nil {
		return nil, fmt.Errorf("OKX 現貨下單失败(instId=%s): %w", o.instId, err)
	}
	// OKX 的 post only 是 ordType=post_only，不是獨立參數
	orderType := req.Type
	if req.PostOnly {
		switch orderType {
		case OrderTypeLimit:
			orderType = OrderTypePostOnly
		case OrderTypePostOnly:
		default:
			return nil, fmt.Errorf("OKX 現貨下單失败(instId=%s): 訂單類型 %q 不支援 post only", o.instId, orderType)
		}
	}
	if orderType != OrderTypeLimit && orderType != OrderTypeMarket && orderType != OrderTypePostOnly {
		return nil, fmt.Errorf("OKX 現貨下單失败(instId=%s): 不支援的訂單類型 %q", o.instId, orderType)
	}

	qty, err := o.alignQuantity(req.Quantity)
	if err != nil {
		return nil, err
	}

	orderReq := map[string]interface{}{
		"instId":  o.instId,
		"tdMode":  okxSpotTdModeCash,
		"side":    string(req.Side),
		"ordType": string(orderType),
		"sz":      strconv.FormatFloat(qty, 'f', getPrecision(o.effectiveLotSz()), 64),
	}

	price := req.Price
	if orderType == OrderTypeMarket {
		// 現貨市價買單默認 sz 為計價幣金額，統一指定按基礎幣數量
		orderReq["tgtCcy"] = okxSpotTgtCcyBase
	} else {
		price, err = o.alignPrice(req.Price, req.Side, req.PriceDecimals)
		if err != nil {
			return nil, err
		}
		orderReq["px"] = strconv.FormatFloat(price, 'f', getPrecision(o.effectiveTickSz(req.PriceDecimals)), 64)
	}
	if req.ClientOrderID != "" {
		orderReq["clOrdId"] = utils.AddBrokerPrefix("okx", req.ClientOrderID)
	}
	resp, err := o.client.PlaceOrder(ctx, orderReq)
	if err != nil {
		return nil, fmt.Errorf("OKX 現貨下單失败(instId=%s side=%s sz=%v): %w", o.instId, req.Side, qty, err)
	}
	if len(resp) == 0 {
		return nil, fmt.Errorf("OKX 現貨下單响应為空(instId=%s)", o.instId)
	}
	r := resp[0]
	if r.SCode != "0" {
		return nil, fmt.Errorf("下單失败: %s - %s", r.SCode, r.SMsg)
	}
	orderID, _ := strconv.ParseInt(r.OrdId, 10, 64)
	now := time.Now()
	return &Order{
		OrderID:       orderID,
		ClientOrderID: r.ClOrdId,
		Symbol:        o.symbol,
		Side:          req.Side,
		Type:          orderType,
		Price:         price,
		Quantity:      qty,
		Status:        OrderStatusNew,
		CreatedAt:     now,
		UpdateTime:    now.UnixMilli(),
	}, nil
}

// BatchPlaceOrders 批量下單
func (o *OKXSpotAdapter) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	placed := make([]*Order, 0, len(orders))
	hasErr := false
	for _, req := range orders {
		order, err := o.PlaceOrder(ctx, req)
		if err != nil {
			logger.Warn("⚠️ [OKX Spot] 下單失败: %v", err)
			if strings.Contains(err.Error(), okxInsufficientBalanceCode) || strings.Contains(err.Error(), "insufficient") {
				hasErr = true
			}
			continue
		}
		placed = append(placed, order)
	}
	return placed, hasErr
}

// CancelOrder 取消訂單
func (o *OKXSpotAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	if err := o.client.CancelOrder(ctx, o.instId, strconv.FormatInt(orderID, 10), ""); err != nil {
		return fmt.Errorf("OKX 現貨撤單失败(instId=%s ordId=%d): %w", o.instId, orderID, err)
	}
	return nil
}

// BatchCancelOrders 批量撤單
func (o *OKXSpotAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	if len(orderIDs) == 0 {
		return nil
	}
	ids := make([]string, len(orderIDs))
	for i, id := range orderIDs {
		ids[i] = strconv.FormatInt(id, 10)
	}
	if err := o.client.BatchCancelOrders(ctx, o.instId, ids); err != nil {
		return fmt.Errorf("OKX 現貨批量撤單失败(instId=%s 共 %d 筆): %w", o.instId, len(ids), err)
	}
	return nil
}

// CancelAllOrders 取消該交易對下所有订單（任一筆失败都匯總返回，不吞錯）
func (o *OKXSpotAdapter) CancelAllOrders(ctx context.Context, symbol string) error {
	orders, err := o.client.GetOpenOrdersByInstType(ctx, okxInstTypeSpot, o.instId)
	if err != nil {
		return fmt.Errorf("OKX 現貨查詢挂單失败(instId=%s): %w", o.instId, err)
	}
	var errs []error
	for _, ord := range orders {
		id, err := strconv.ParseInt(ord.OrdId, 10, 64)
		if err != nil {
			errs = append(errs, fmt.Errorf("OKX 現貨挂單 ordId=%q 無法解析: %w", ord.OrdId, err))
			continue
		}
		if err := o.CancelOrder(ctx, symbol, id); err != nil {
			errs = append(errs, err)
		}
		time.Sleep(okxSpotCancelInterval)
	}
	return errors.Join(errs...)
}

// convertOrder REST 訂單 → 本地訂單（Side/Type/Status 保留 OKX 原生值，由 wrapper 映射）
func (o *OKXSpotAdapter) convertOrder(ord *OKXOrder) *Order {
	orderID, _ := strconv.ParseInt(ord.OrdId, 10, 64)
	price, _ := strconv.ParseFloat(ord.Px, 64)
	qty, _ := strconv.ParseFloat(ord.Sz, 64)
	execQty, _ := strconv.ParseFloat(ord.AccFillSz, 64)
	avgPx, _ := strconv.ParseFloat(ord.AvgPx, 64)
	uTime, _ := strconv.ParseInt(ord.UTime, 10, 64)
	return &Order{
		OrderID:       orderID,
		ClientOrderID: ord.ClOrdId,
		Symbol:        o.symbol,
		Side:          Side(ord.Side),
		Type:          OrderType(ord.OrdType),
		Price:         price,
		Quantity:      qty,
		ExecutedQty:   execQty,
		AvgPrice:      avgPx,
		Status:        OrderStatus(ord.State),
		UpdateTime:    uTime,
	}
}

// GetOrder 查詢訂單
func (o *OKXSpotAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	ord, err := o.client.GetOrder(ctx, o.instId, strconv.FormatInt(orderID, 10), "")
	if err != nil {
		return nil, fmt.Errorf("OKX 現貨查單失败(instId=%s ordId=%d): %w", o.instId, orderID, err)
	}
	order := o.convertOrder(ord)
	if order.OrderID == 0 {
		order.OrderID = orderID
	}
	return order, nil
}

// GetOrderByClientOrderID 按客戶端訂單 ID 查詢現貨訂單（包含終態）。
func (o *OKXSpotAdapter) GetOrderByClientOrderID(ctx context.Context, symbol, clientOrderID string) (*Order, error) {
	clientOrderID = utils.AddBrokerPrefix("okx", clientOrderID)
	ord, err := o.client.GetOrder(ctx, o.instId, "", clientOrderID)
	if err != nil {
		return nil, fmt.Errorf("OKX 現貨按客戶端訂單 ID 查單失敗(instId=%s): %w", o.instId, err)
	}
	return o.convertOrder(ord), nil
}

// GetOpenOrders 未完成订單
func (o *OKXSpotAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	orders, err := o.client.GetOpenOrdersByInstType(ctx, okxInstTypeSpot, o.instId)
	if err != nil {
		return nil, fmt.Errorf("OKX 現貨查詢挂單失败(instId=%s): %w", o.instId, err)
	}
	result := make([]*Order, 0, len(orders))
	for i := range orders {
		result = append(result, o.convertOrder(&orders[i]))
	}
	return result, nil
}

// GetAccount 現貨账戶餘額
func (o *OKXSpotAdapter) GetAccount(ctx context.Context) (*Account, error) {
	balances, err := o.client.GetBalance(ctx)
	if err != nil {
		return nil, err
	}
	total, available, err := summarizeOKXSpotQuoteBalance(balances, o.quoteAsset)
	if err != nil {
		return nil, err
	}
	return &Account{
		TotalWalletBalance: total,
		TotalMarginBalance: total,
		AvailableBalance:   available,
		BalanceAsset:       strings.ToUpper(strings.TrimSpace(o.quoteAsset)),
		Positions:          nil,
	}, nil
}

func summarizeOKXSpotQuoteBalance(balances []Balance, quoteAsset string) (total, available float64, err error) {
	quoteAsset = strings.ToUpper(strings.TrimSpace(quoteAsset))
	if quoteAsset == "" {
		return 0, 0, fmt.Errorf("OKX spot quote asset is unavailable")
	}
	found := false
	for _, balance := range balances {
		for _, detail := range balance.Details {
			if !strings.EqualFold(strings.TrimSpace(detail.Ccy), quoteAsset) {
				continue
			}
			if found {
				return 0, 0, fmt.Errorf("duplicate OKX spot %s balance rows", quoteAsset)
			}
			found = true
			total, err = strconv.ParseFloat(detail.Eq, 64)
			if err != nil || math.IsNaN(total) || math.IsInf(total, 0) || total < 0 {
				return 0, 0, fmt.Errorf("invalid OKX spot %s equity %q", quoteAsset, detail.Eq)
			}
			available, err = strconv.ParseFloat(detail.AvailBal, 64)
			if err != nil || math.IsNaN(available) || math.IsInf(available, 0) || available < 0 || available > total {
				return 0, 0, fmt.Errorf("invalid OKX spot %s available balance %q", quoteAsset, detail.AvailBal)
			}
		}
	}
	return total, available, nil
}

func (o *OKXSpotAdapter) SpotInventoryQty(ctx context.Context) (float64, error) {
	if o == nil || o.client == nil || strings.TrimSpace(o.baseAsset) == "" {
		return 0, fmt.Errorf("OKX spot inventory requires a configured base asset")
	}
	balances, err := o.client.GetBalance(ctx)
	if err != nil {
		return 0, fmt.Errorf("query OKX spot inventory: %w", err)
	}
	total, _, err := summarizeOKXSpotQuoteBalance(balances, o.baseAsset)
	return total, err
}

// GetPositions 現貨“持倉”由基础资產餘額構成
func (o *OKXSpotAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	balances, err := o.client.GetBalance(ctx)
	if err != nil {
		return nil, err
	}
	base := o.baseAsset
	if base == "" {
		base = strings.Split(o.instId, "-")[0]
	}
	var free float64
	for _, b := range balances {
		for _, d := range b.Details {
			if d.Ccy == base {
				free, _ = strconv.ParseFloat(d.AvailBal, 64)
				eq, _ := strconv.ParseFloat(d.Eq, 64)
				if eq > free {
					free = eq
				}
				break
			}
		}
	}
	if free <= 0 {
		return nil, nil
	}
	price, _ := o.GetLatestPrice(ctx, symbol)
	if price <= 0 {
		price = 0
	}
	return []*Position{{
		Symbol:         o.symbol,
		Size:           free,
		EntryPrice:     price,
		MarkPrice:      price,
		UnrealizedPNL:  0,
		Leverage:       1,
		MarginType:     "spot",
		IsolatedMargin: 0,
	}}, nil
}

// GetBalance 某资產餘額
func (o *OKXSpotAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	balances, err := o.client.GetBalance(ctx)
	if err != nil {
		return 0, err
	}
	for _, b := range balances {
		for _, d := range b.Details {
			if d.Ccy == asset {
				return strconv.ParseFloat(d.AvailBal, 64)
			}
		}
	}
	return 0, nil
}

// spotCommissionToQuote 把現貨手續費統一換算為計價幣口徑。
// 上層（position 包）把 Commission 直接累加進 USDT 計的 BuyFee/PnL，因此：
//   - 手續費幣種為基礎幣（OKX 現貨買單常見）→ 乘以成交價換算為計價幣，CommissionAsset 改為計價幣；
//   - 手續費幣種為計價幣或為空 → 原樣返回，CommissionAsset 為計價幣；
//   - 其他幣種（如平台幣抵扣）→ 無法換算，原樣返回並保留原幣種，由上層自行判斷。
//
// commission 已是「支出為正」口徑。
func (o *OKXSpotAdapter) spotCommissionToQuote(commission float64, feeCcy string, fillPx float64) (float64, string) {
	switch {
	case feeCcy == "" || feeCcy == o.quoteAsset:
		return commission, o.quoteAsset
	case feeCcy == o.baseAsset && fillPx > 0:
		return commission * fillPx, o.quoteAsset
	default:
		return commission, feeCcy
	}
}

// StartOrderStream 現貨私有訂單流（orders，instType=SPOT），推送前統一換成內部口徑
func (o *OKXSpotAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	if o.spotOrderWS != nil {
		o.spotOrderWS.Stop()
	}
	o.spotOrderWS = NewSpotOrderWebSocketManager(o.client.apiKey, o.client.secretKey, o.client.passphrase, o.useTestnet, o.instId)
	return o.spotOrderWS.Start(ctx, func(ou OrderUpdate) {
		update, err := o.normalizeSpotStreamUpdate(ou)
		if err != nil {
			logger.Error("❌ [OKX Spot WS] 丟棄無法識別的訂單推送 ordId=%d clOrdId=%s: %v", ou.OrderID, ou.ClientOrderID, err)
			return
		}
		callback(update)
	})
}

// SpotStreamOrderUpdate 現貨訂單推送：在通用 StreamOrderUpdate 上附加基礎幣手續費數量。
// 上層按字段名反射讀取（嵌入字段會被提升），BaseFeeQty 映射到 position.OrderUpdate.BaseFeeQty。
type SpotStreamOrderUpdate struct {
	StreamOrderUpdate
	CommissionKnown bool
	// BaseFeeQty 本筆成交以基礎幣扣收的手續費（基礎幣單位，>=0）；其計價幣價值已包含在 Commission 中
	BaseFeeQty float64
}

// normalizeSpotStreamUpdate normalizeOrderUpdate + 基礎幣手續費數量（fillFeeCcy 為基礎幣且為支出時）
func (o *OKXSpotAdapter) normalizeSpotStreamUpdate(update OrderUpdate) (SpotStreamOrderUpdate, error) {
	normalized, err := o.normalizeOrderUpdate(update)
	if err != nil {
		return SpotStreamOrderUpdate{}, err
	}
	out := SpotStreamOrderUpdate{StreamOrderUpdate: normalized, CommissionKnown: update.CommissionKnown}
	if o.baseAsset != "" && update.CommissionAsset == o.baseAsset && update.Commission > 0 {
		out.BaseFeeQty = update.Commission
	}
	return out, nil
}

// normalizeOrderUpdate 將 OKX 現貨原生推送轉成內部口徑：
// Symbol 用配置的交易對（BTCUSDT 而非 BTC-USDT）、Side/Type/Status 映射為內部常量、手續費換算為計價幣。
func (o *OKXSpotAdapter) normalizeOrderUpdate(update OrderUpdate) (StreamOrderUpdate, error) {
	if update.Symbol != "" && update.Symbol != o.instId {
		return StreamOrderUpdate{}, fmt.Errorf("非本適配器交易對的推送: instId=%s（期望 %s）", update.Symbol, o.instId)
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
	commission, commissionAsset := o.spotCommissionToQuote(update.Commission, update.CommissionAsset, update.FillPrice)
	return StreamOrderUpdate{
		OrderID:         update.OrderID,
		ClientOrderID:   update.ClientOrderID,
		Symbol:          o.symbol,
		Side:            side,
		Type:            orderType,
		Status:          status,
		Price:           update.Price,
		Quantity:        update.Quantity,
		ExecutedQty:     update.ExecutedQty,
		AvgPrice:        update.AvgPrice,
		UpdateTime:      update.UpdateTime,
		Commission:      commission,
		CommissionAsset: commissionAsset,
		RealizedPnL:     update.RealizedPnL,
	}, nil
}

// StopOrderStream 停止現貨訂單流
func (o *OKXSpotAdapter) StopOrderStream() error {
	if o.spotOrderWS != nil {
		o.spotOrderWS.Stop()
		o.spotOrderWS = nil
	}
	return nil
}

// GetLatestPrice 最新價（優先使用 WebSocket 緩存，與 OKX 合約適配器一致）
func (o *OKXSpotAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	if o.wsManager != nil {
		price := o.wsManager.GetLatestPrice()
		if price > 0 {
			return price, nil
		}
	}
	ticker, err := o.client.GetTicker(ctx, o.instId)
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(ticker.Last, 64)
}

// StartPriceStream 公共 WebSocket 訂閱 tickers（現貨 instId 如 BTC-USDT，與 REST 一致）
func (o *OKXSpotAdapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	if o.wsManager == nil {
		o.wsManager = NewWebSocketManager(o.client.apiKey, o.client.secretKey, o.client.passphrase, o.useTestnet)
	}
	return o.wsManager.StartPriceStream(ctx, o.instId, callback)
}

// StartKlineStream 公共 candle 頻道（與合約相同 API，instId 為現貨如 BTC-USDT）
func (o *OKXSpotAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(interface{})) error {
	if o.klineWS != nil {
		o.klineWS.Stop()
	}
	instIds := make([]string, 0, len(symbols))
	for _, s := range symbols {
		instIds = append(instIds, symbolToSpotInstId(s))
	}
	o.klineWS = NewKlineWebSocketManager(o.useTestnet)
	return o.klineWS.Start(ctx, instIds, interval, func(c interface{}) {
		callback(c)
	})
}

// StopKlineStream 停止 K 線流
func (o *OKXSpotAdapter) StopKlineStream() error {
	if o.klineWS != nil {
		o.klineWS.Stop()
		o.klineWS = nil
	}
	return nil
}

// GetHistoricalKlines 历史K線
func (o *OKXSpotAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	bar := interval
	if bar == "1h" {
		bar = "1H"
	}
	if bar == "" {
		bar = "1m"
	}
	klines, err := o.client.GetKlines(ctx, o.instId, bar, limit)
	if err != nil {
		return nil, err
	}
	candles := make([]*Candle, 0, len(klines))
	for _, k := range klines {
		open, _ := strconv.ParseFloat(k.O, 64)
		high, _ := strconv.ParseFloat(k.H, 64)
		low, _ := strconv.ParseFloat(k.L, 64)
		closeP, _ := strconv.ParseFloat(k.C, 64)
		vol, _ := strconv.ParseFloat(k.Vol, 64)
		ts, _ := strconv.ParseInt(k.Ts, 10, 64)
		candles = append(candles, &Candle{
			Symbol:    o.symbol,
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
func (o *OKXSpotAdapter) GetPriceDecimals() int {
	return o.priceDecimals
}

// GetQuantityDecimals 數量精度
func (o *OKXSpotAdapter) GetQuantityDecimals() int {
	return o.quantityDecimals
}

// GetBaseAsset 基础资產
func (o *OKXSpotAdapter) GetBaseAsset() string {
	return o.baseAsset
}

// GetQuoteAsset 计價资產
func (o *OKXSpotAdapter) GetQuoteAsset() string {
	return o.quoteAsset
}

// EstimateFinalOrderAmount 預估订單金額
func (o *OKXSpotAdapter) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return price * quantity
}

// GetFundingRate 現貨無资金费率
func (o *OKXSpotAdapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return 0, nil
}

// GetSpotPrice 現貨最新價
func (o *OKXSpotAdapter) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return o.GetLatestPrice(ctx, symbol)
}

// GetOrderBook 订單簿
func (o *OKXSpotAdapter) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	if limit <= 0 {
		limit = 20
	}
	ob, err := o.client.GetOrderBook(ctx, o.instId, limit)
	if err != nil {
		return nil, err
	}
	bids := make([]OrderBookLevel, 0, len(ob.Bids))
	for _, b := range ob.Bids {
		if len(b) < 2 {
			continue
		}
		price, _ := strconv.ParseFloat(b[0], 64)
		qty, _ := strconv.ParseFloat(b[1], 64)
		bids = append(bids, OrderBookLevel{Price: price, Quantity: qty})
	}
	asks := make([]OrderBookLevel, 0, len(ob.Asks))
	for _, a := range ob.Asks {
		if len(a) < 2 {
			continue
		}
		price, _ := strconv.ParseFloat(a[0], 64)
		qty, _ := strconv.ParseFloat(a[1], 64)
		asks = append(asks, OrderBookLevel{Price: price, Quantity: qty})
	}
	ts, _ := strconv.ParseInt(ob.TS, 10, 64)
	return &OrderBook{
		Symbol:    symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: ts,
	}, nil
}

// InternalTransfer 內部轉帳（與合約適配器相同 REST，見 mapOKXTransferEndpoints）
func (o *OKXSpotAdapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
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

// OKXSpotOrderFill 現貨成交明細：在 OKXOrderFill 上附加基礎幣手續費數量
type OKXSpotOrderFill struct {
	OKXOrderFill
	// BaseFeeQty 本筆以基礎幣扣收的手續費（基礎幣單位，>=0）；其計價幣價值已包含在 Commission 中
	BaseFeeQty float64
}

// GetOrderFills 查詢成交明細（GET /api/v5/trade/fills，instType=SPOT）。
// Commission 為「支出為正、返佣為負」，並已換算為計價幣（規則見 spotCommissionToQuote）。
// 以基礎幣收取的手續費另在 BaseFeeQty 中給出原始數量（基礎幣單位，>=0）。
func (o *OKXSpotAdapter) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*OKXSpotOrderFill, error) {
	ord := ""
	if orderID != 0 {
		ord = strconv.FormatInt(orderID, 10)
	}
	rows, err := o.client.GetTradeFills(ctx, okxInstTypeSpot, o.instId, ord)
	if err != nil {
		return nil, fmt.Errorf("OKX 現貨查詢訂單 %d 成交明細失败(instId=%s): %w", orderID, o.instId, err)
	}
	fills := make([]*OKXSpotOrderFill, 0, len(rows))
	for _, r := range rows {
		price, err := strconv.ParseFloat(r.FillPx, 64)
		if err != nil {
			return nil, fmt.Errorf("OKX 現貨成交明細 tradeId=%s fillPx 無效 %q: %w", r.TradeId, r.FillPx, err)
		}
		sz, err := strconv.ParseFloat(r.FillSz, 64)
		if err != nil {
			return nil, fmt.Errorf("OKX 現貨成交明細 tradeId=%s fillSz 無效 %q: %w", r.TradeId, r.FillSz, err)
		}
		fee, _ := strconv.ParseFloat(r.Fee, 64)
		ts, _ := strconv.ParseInt(r.Ts, 10, 64)
		ordID := orderID
		if r.OrdId != "" {
			if v, err := strconv.ParseInt(r.OrdId, 10, 64); err == nil {
				ordID = v
			}
		}
		rawCommission := okxFeeToCommission(fee)
		commission, commissionAsset := o.spotCommissionToQuote(rawCommission, r.FeeCcy, price)
		baseFeeQty := 0.0
		if o.baseAsset != "" && r.FeeCcy == o.baseAsset && rawCommission > 0 {
			baseFeeQty = rawCommission
		}
		fills = append(fills, &OKXSpotOrderFill{
			OKXOrderFill: OKXOrderFill{
				OrderID:         ordID,
				TradeID:         r.TradeId,
				Symbol:          o.symbol,
				Side:            Side(r.Side),
				Price:           price,
				Quantity:        sz,
				Commission:      commission,
				CommissionAsset: commissionAsset,
				TradeTime:       ts,
				IsMaker:         r.ExecType == okxExecTypeMaker,
			},
			BaseFeeQty: baseFeeQty,
		})
	}
	return fills, nil
}
