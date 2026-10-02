package binance

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"

	binancesdk "github.com/adshao/go-binance/v2"
)

// BinanceSpotAdapter 幣安現貨交易所適配器
type BinanceSpotAdapter struct {
	client           *binancesdk.Client
	symbol           string
	apiKey           string
	secretKey        string
	priceDecimals    int
	quantityDecimals int
	tickSize         float64
	stepSize         float64
	baseAsset        string
	quoteAsset       string
	useTestnet       bool

	lastAPICallTime time.Time
	apiCallMu       sync.Mutex
	minAPIInterval  time.Duration

	// WebSocket 管理器
	wsManager *SpotWebSocketManager
	orderWS   *SpotUserDataWebSocketManager
	klineWS   *KlineWebSocketManager // NewSpotKlineWebSocketManager

	// 最新行情價格缓存在资产估值用途；成交费用必须通过历史成交分钟K线折算。
	feePriceMu               sync.Mutex
	feePriceCache            map[string]spotFeeAssetPrice
	feeWarned                map[string]bool
	feePriceFetcher          func(ctx context.Context, pair string) (float64, error) // 測試注入；nil 時走 REST ticker
	feeHistoricalRateFetcher func(ctx context.Context, asset, quote string, tradeTime int64) (float64, error)
}

// NewBinanceSpotAdapter 創建币安現貨适配器
func NewBinanceSpotAdapter(cfg map[string]string, symbol string) (*BinanceSpotAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]
	testnetStr := cfg["testnet"]

	useTestnet := false
	if testnetStr == "true" {
		useTestnet = true
		logger.Info("🌐 [Binance Spot] 使用測試網模式")
	}

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("Binance API 配置不完整")
	}

	symbol = normalizeBinanceSymbolTypo(symbol)

	client := binancesdk.NewClient(apiKey, secretKey)
	if useTestnet {
		client.SetApiEndpoint("https://testnet.binance.vision")
	}

	client.NewSetServerTimeService().Do(context.Background())

	adapter := &BinanceSpotAdapter{
		client:         client,
		symbol:         symbol,
		apiKey:         apiKey,
		secretKey:      secretKey,
		useTestnet:     useTestnet,
		minAPIInterval: 200 * time.Millisecond,
		wsManager:      NewSpotWebSocketManager(useTestnet),
	}

	ctxInit, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := adapter.fetchSpotExchangeInfo(ctxInit); err != nil {
		logger.Warn("⚠️ [Binance Spot] 獲取交易對信息失败: %v，使用默认精度", err)
		adapter.priceDecimals = 2
		adapter.quantityDecimals = 5
	}

	return adapter, nil
}

// GetName 獲取交易所名称
func (b *BinanceSpotAdapter) GetName() string {
	return "Binance Spot"
}

// GetMarketType 獲取市場類型：spot 現貨
func (b *BinanceSpotAdapter) GetMarketType() string {
	return "spot"
}

func (b *BinanceSpotAdapter) fetchSpotExchangeInfo(ctx context.Context) error {
	info, err := b.client.NewExchangeInfoService().Symbol(b.symbol).Do(ctx)
	if err != nil {
		return err
	}

	for i := range info.Symbols {
		s := &info.Symbols[i]
		if s.Symbol == b.symbol {
			b.baseAsset = s.BaseAsset
			b.quoteAsset = s.QuoteAsset

			// 從 Filter 中獲取 tickSize 和 stepSize
			if pf := s.PriceFilter(); pf != nil && pf.TickSize != "" {
				b.tickSize, _ = strconv.ParseFloat(pf.TickSize, 64)
			}
			if lf := s.LotSizeFilter(); lf != nil && lf.StepSize != "" {
				b.stepSize, _ = strconv.ParseFloat(lf.StepSize, 64)
			}

			// 🔥 修復: 從 tickSize/stepSize 反推實際交易精度
			// 之前錯誤地使用 QuotePrecision/BaseAssetPrecision（通常是8），
			// 它们是資產的最大精度，不是交易對的實際精度。
			// 例如 BTCUSDT: QuotePrecision=8, 但實際 tickSize=0.01 → 價格精度應為 2
			if b.tickSize > 0 {
				b.priceDecimals = countDecimalsFromStep(b.tickSize)
			} else {
				b.priceDecimals = s.QuotePrecision // fallback
				b.tickSize = math.Pow10(-b.priceDecimals)
			}
			if b.stepSize > 0 {
				b.quantityDecimals = countDecimalsFromStep(b.stepSize)
			} else {
				b.quantityDecimals = s.BaseAssetPrecision // fallback
				b.stepSize = math.Pow10(-b.quantityDecimals)
			}

			logger.Info("ℹ️ [Binance Spot] %s - 數量精度:%d, 價格精度:%d, tickSize:%.8f, stepSize:%.8f, 基础:%s, 计價:%s",
				b.symbol, b.quantityDecimals, b.priceDecimals, b.tickSize, b.stepSize, b.baseAsset, b.quoteAsset)
			return nil
		}
	}
	return fmt.Errorf("未找到交易對信息: %s", b.symbol)
}

// countDecimalsFromStep 從步進值反推小數位數
// 例如: 0.01 → 2, 0.00001 → 5, 1 → 0, 0.1 → 1
func countDecimalsFromStep(step float64) int {
	if step <= 0 || step >= 1 {
		return 0
	}
	s := strconv.FormatFloat(step, 'f', -1, 64)
	dotIdx := strings.Index(s, ".")
	if dotIdx < 0 {
		return 0
	}
	// 去掉尾部的零
	trimmed := strings.TrimRight(s[dotIdx+1:], "0")
	return len(trimmed)
}

func (b *BinanceSpotAdapter) roundToTickSize(price float64, side Side) float64 {
	if b.tickSize <= 0 {
		return price
	}
	if side == SideBuy {
		return utils.FloorToStep(price, b.tickSize)
	}
	return utils.CeilToStep(price, b.tickSize)
}

func (b *BinanceSpotAdapter) roundToStepSize(quantity float64) float64 {
	return utils.FloorToStep(quantity, b.stepSize)
}

// PlaceOrder 下單（現貨不支援 ReduceOnly，忽略該参數）
func (b *BinanceSpotAdapter) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	if req.Price <= 0 {
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

	pDec := req.PriceDecimals
	if pDec <= 0 {
		pDec = b.priceDecimals
	}
	priceStr := fmt.Sprintf("%.*f", pDec, adjustedPrice)
	quantityStr := fmt.Sprintf("%.*f", b.quantityDecimals, adjustedQty)

	timeInForce := binancesdk.TimeInForceTypeGTC
	// 現貨 API 部分环境支援 GTX（Post Only），若無则用 GTC
	if req.PostOnly {
		timeInForce = "GTX"
	}

	orderService := b.client.NewCreateOrderService().
		Symbol(req.Symbol).
		Side(binancesdk.SideType(req.Side)).
		Type(binancesdk.OrderTypeLimit).
		TimeInForce(timeInForce).
		Quantity(quantityStr).
		Price(priceStr)

	clientOrderID := req.ClientOrderID
	if clientOrderID != "" {
		clientOrderID = utils.AddBrokerPrefix("binance", clientOrderID)
		orderService = orderService.NewClientOrderID(clientOrderID)
	}

	resp, err := orderService.Do(ctx)
	if err != nil {
		return nil, err
	}

	price, _ := strconv.ParseFloat(resp.Price, 64)
	qty, _ := strconv.ParseFloat(resp.OrigQuantity, 64)
	execQty, _ := strconv.ParseFloat(resp.ExecutedQuantity, 64)
	cumulativeQuote, _ := strconv.ParseFloat(resp.CummulativeQuoteQuantity, 64)
	avgPrice := cumulativeAveragePrice(cumulativeQuote, execQty)

	return &Order{
		OrderID:       resp.OrderID,
		ClientOrderID: resp.ClientOrderID,
		Symbol:        resp.Symbol,
		Side:          Side(resp.Side),
		Type:          OrderType(resp.Type),
		Price:         price,
		Quantity:      qty,
		ExecutedQty:   execQty,
		AvgPrice:      avgPrice,
		Status:        OrderStatus(resp.Status),
		CreatedAt:     time.Unix(0, resp.TransactTime*int64(time.Millisecond)),
		UpdateTime:    resp.TransactTime,
	}, nil
}

// BatchPlaceOrders 批量下單
func (b *BinanceSpotAdapter) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	placed := make([]*Order, 0, len(orders))
	hasBalanceError := false
	for _, req := range orders {
		order, err := b.PlaceOrder(ctx, req)
		if err != nil {
			logger.Warn("⚠️ [Binance Spot] 下單失败 %.2f %s: %v", req.Price, req.Side, err)
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
func (b *BinanceSpotAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	_, err := b.client.NewCancelOrderService().Symbol(symbol).OrderID(orderID).Do(ctx)
	if err != nil {
		if isBinanceUnknownOrderError(err) {
			logger.Info("ℹ️ [Binance Spot] 订單 %d 已不存在，跳過取消", orderID)
			return nil
		}
		return fmt.Errorf("cancel spot order %d on %s: %w", orderID, symbol, err)
	}
	return nil
}

// BatchCancelOrders 批量撤單（逐個撤銷，匯總失敗；訂單不存在視為成功）
func (b *BinanceSpotAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	return cancelOrdersSequentially(ctx, "binance spot", symbol, orderIDs, b.CancelOrder)
}

// cancelOrdersSequentially 逐個撤單並匯總錯誤（cancel 需自行把「訂單不存在」視為成功）
func cancelOrdersSequentially(ctx context.Context, market, symbol string, orderIDs []int64,
	cancel func(ctx context.Context, symbol string, orderID int64) error) error {
	var errs []error
	for i, id := range orderIDs {
		if err := cancel(ctx, symbol, id); err != nil {
			logger.Warn("⚠️ [%s] 取消訂單失败 %d: %v", market, id, err)
			errs = append(errs, err)
		}
		if i < len(orderIDs)-1 {
			time.Sleep(binanceCancelThrottleInterval) // 避免限频
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s batch cancel on %s: %d of %d orders failed: %w",
			market, symbol, len(errs), len(orderIDs), errors.Join(errs...))
	}
	return nil
}

// CancelAllOrders 取消該交易對下所有订單（無挂單時交易所返回 -2011，視為成功）
func (b *BinanceSpotAdapter) CancelAllOrders(ctx context.Context, symbol string) error {
	_, err := b.client.NewCancelOpenOrdersService().Symbol(symbol).Do(ctx)
	if err != nil && !isBinanceUnknownOrderError(err) {
		return fmt.Errorf("cancel all spot orders on %s: %w", symbol, err)
	}
	return nil
}

// GetOrder 查詢訂單
func (b *BinanceSpotAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	o, err := b.client.NewGetOrderService().Symbol(symbol).OrderID(orderID).Do(ctx)
	if err != nil {
		return nil, err
	}
	price, _ := strconv.ParseFloat(o.Price, 64)
	qty, _ := strconv.ParseFloat(o.OrigQuantity, 64)
	execQty, _ := strconv.ParseFloat(o.ExecutedQuantity, 64)
	cumulativeQuote, _ := strconv.ParseFloat(o.CummulativeQuoteQuantity, 64)
	avgPrice := cumulativeAveragePrice(cumulativeQuote, execQty)
	return &Order{
		OrderID:       o.OrderID,
		ClientOrderID: o.ClientOrderID,
		Symbol:        o.Symbol,
		Side:          Side(o.Side),
		Type:          OrderType(o.Type),
		Price:         price,
		Quantity:      qty,
		ExecutedQty:   execQty,
		AvgPrice:      avgPrice,
		Status:        OrderStatus(o.Status),
		UpdateTime:    o.UpdateTime,
	}, nil
}

// GetOpenOrders 查詢未完成订單
func (b *BinanceSpotAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	list, err := b.client.NewListOpenOrdersService().Symbol(symbol).Do(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*Order, 0, len(list))
	for _, o := range list {
		price, _ := strconv.ParseFloat(o.Price, 64)
		qty, _ := strconv.ParseFloat(o.OrigQuantity, 64)
		execQty, _ := strconv.ParseFloat(o.ExecutedQuantity, 64)
		cumulativeQuote, _ := strconv.ParseFloat(o.CummulativeQuoteQuantity, 64)
		avgPrice := cumulativeAveragePrice(cumulativeQuote, execQty)
		result = append(result, &Order{
			OrderID:       o.OrderID,
			ClientOrderID: o.ClientOrderID,
			Symbol:        o.Symbol,
			Side:          Side(o.Side),
			Type:          OrderType(o.Type),
			Price:         price,
			Quantity:      qty,
			ExecutedQty:   execQty,
			AvgPrice:      avgPrice,
			Status:        OrderStatus(o.Status),
			UpdateTime:    o.UpdateTime,
		})
	}
	return result, nil
}

// GetAccountOpenOrders returns current spot orders across all symbols.
func (b *BinanceSpotAdapter) GetAccountOpenOrders(ctx context.Context) ([]*Order, error) {
	var list []*binancesdk.Order
	err := b.withRateLimit(ctx, func() error {
		var requestErr error
		list, requestErr = b.client.NewListOpenOrdersService().Do(ctx)
		return requestErr
	})
	if err != nil {
		return nil, err
	}
	result := make([]*Order, 0, len(list))
	for _, order := range list {
		price, err := strconv.ParseFloat(order.Price, 64)
		if err != nil {
			return nil, fmt.Errorf("parse account spot order %d price: %w", order.OrderID, err)
		}
		quantity, err := strconv.ParseFloat(order.OrigQuantity, 64)
		if err != nil {
			return nil, fmt.Errorf("parse account spot order %d quantity: %w", order.OrderID, err)
		}
		executedQty, err := strconv.ParseFloat(order.ExecutedQuantity, 64)
		if err != nil {
			return nil, fmt.Errorf("parse account spot order %d executed quantity: %w", order.OrderID, err)
		}
		cumulativeQuote, err := strconv.ParseFloat(order.CummulativeQuoteQuantity, 64)
		if err != nil {
			return nil, fmt.Errorf("parse account spot order %d cumulative quote: %w", order.OrderID, err)
		}
		result = append(result, &Order{OrderID: order.OrderID, ClientOrderID: order.ClientOrderID, Symbol: order.Symbol,
			Side: Side(order.Side), Type: OrderType(order.Type), Price: price, Quantity: quantity,
			ExecutedQty: executedQty, AvgPrice: cumulativeAveragePrice(cumulativeQuote, executedQty),
			Status: OrderStatus(order.Status), UpdateTime: order.UpdateTime})
	}
	return result, nil
}

// withRateLimit 確保 API 調用間隔，避免觸發幣安限流
func (b *BinanceSpotAdapter) withRateLimit(ctx context.Context, fn func() error) error {
	b.apiCallMu.Lock()
	elapsed := time.Since(b.lastAPICallTime)
	if elapsed < b.minAPIInterval {
		waitTime := b.minAPIInterval - elapsed
		b.apiCallMu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
		}
		b.apiCallMu.Lock()
	}
	b.lastAPICallTime = time.Now()
	b.apiCallMu.Unlock()
	return fn()
}

// GetAccount 獲取現貨账戶（餘額），含限流
func (b *BinanceSpotAdapter) GetAccount(ctx context.Context) (*Account, error) {
	var acc *binancesdk.Account
	if err := b.withRateLimit(ctx, func() error {
		var err error
		acc, err = b.client.NewGetAccountService().Do(ctx)
		return err
	}); err != nil {
		return nil, err
	}
	quoteAsset := b.quoteAsset
	if quoteAsset == "" {
		quoteAsset = "USDT"
	}
	totalWallet, available, err := summarizeSpotQuoteBalance(acc.Balances, quoteAsset)
	if err != nil {
		return nil, fmt.Errorf("parse Binance spot %s balance: %w", quoteAsset, err)
	}
	return &Account{
		TotalWalletBalance: totalWallet,
		TotalMarginBalance: totalWallet,
		AvailableBalance:   available,
		BalanceAsset:       quoteAsset,
		Positions:          nil,
	}, nil
}

func summarizeSpotQuoteBalance(balances []binancesdk.Balance, quoteAsset string) (total, available float64, err error) {
	if strings.TrimSpace(quoteAsset) == "" {
		return 0, 0, fmt.Errorf("quote asset is required")
	}
	found := false
	for _, balance := range balances {
		if !strings.EqualFold(balance.Asset, quoteAsset) {
			continue
		}
		if found {
			return 0, 0, fmt.Errorf("duplicate %s balance entries", quoteAsset)
		}
		found = true
		free, parseErr := strconv.ParseFloat(balance.Free, 64)
		if parseErr != nil || math.IsNaN(free) || math.IsInf(free, 0) || free < 0 {
			return 0, 0, fmt.Errorf("invalid %s free balance %q", quoteAsset, balance.Free)
		}
		locked, parseErr := strconv.ParseFloat(balance.Locked, 64)
		if parseErr != nil || math.IsNaN(locked) || math.IsInf(locked, 0) || locked < 0 {
			return 0, 0, fmt.Errorf("invalid %s locked balance %q", quoteAsset, balance.Locked)
		}
		total, available = free+locked, free
		if math.IsInf(total, 0) {
			return 0, 0, fmt.Errorf("%s balance overflow", quoteAsset)
		}
	}
	return total, available, nil
}

// GetPositions 現貨無合約持倉，返回基础资產餘額構成的“持倉”（用於网格賣單逻辑）
func (b *BinanceSpotAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	var acc *binancesdk.Account
	if err := b.withRateLimit(ctx, func() error {
		var err error
		acc, err = b.client.NewGetAccountService().Do(ctx)
		return err
	}); err != nil {
		return nil, err
	}
	base := b.baseAsset
	if base == "" {
		// 嘗試按常見计价幣後綴推導 base asset（注意順序：長後綴優先）
		for _, suffix := range []string{"USDT", "USDC", "BUSD", "U"} {
			trimmed := strings.TrimSuffix(symbol, suffix)
			if trimmed != symbol {
				base = trimmed
				break
			}
		}
		if base == "" {
			base = symbol
		}
	}
	var free, locked float64
	for _, bal := range acc.Balances {
		if bal.Asset == base {
			free, _ = strconv.ParseFloat(bal.Free, 64)
			locked, _ = strconv.ParseFloat(bal.Locked, 64)
			break
		}
	}
	size := free + locked
	if size <= 0 {
		return []*Position{}, nil
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

// GetBalance 獲取某资產餘額，含限流
func (b *BinanceSpotAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	var acc *binancesdk.Account
	if err := b.withRateLimit(ctx, func() error {
		var err error
		acc, err = b.client.NewGetAccountService().Do(ctx)
		return err
	}); err != nil {
		return 0, err
	}
	for _, bal := range acc.Balances {
		if bal.Asset == asset {
			free, _ := strconv.ParseFloat(bal.Free, 64)
			return free, nil
		}
	}
	return 0, nil
}

// GetSpotInventoryQty returns total free+locked balance for this market's base asset.
func (b *BinanceSpotAdapter) GetSpotInventoryQty(ctx context.Context) (float64, error) {
	if b == nil || b.client == nil || b.baseAsset == "" {
		return 0, fmt.Errorf("spot inventory requires a configured market")
	}
	var acc *binancesdk.Account
	if err := b.withRateLimit(ctx, func() error {
		var err error
		acc, err = b.client.NewGetAccountService().Do(ctx)
		return err
	}); err != nil {
		return 0, fmt.Errorf("query spot account inventory: %w", err)
	}
	for _, balance := range acc.Balances {
		if balance.Asset != b.baseAsset {
			continue
		}
		free, err := strconv.ParseFloat(balance.Free, 64)
		if err != nil {
			return 0, fmt.Errorf("parse free %s balance: %w", b.baseAsset, err)
		}
		locked, err := strconv.ParseFloat(balance.Locked, 64)
		if err != nil {
			return 0, fmt.Errorf("parse locked %s balance: %w", b.baseAsset, err)
		}
		total := free + locked
		if math.IsNaN(total) || math.IsInf(total, 0) || total < 0 {
			return 0, fmt.Errorf("spot %s inventory is invalid", b.baseAsset)
		}
		return total, nil
	}
	return 0, nil
}

// StartOrderStream 現貨 User Data Stream（executionReport）
func (b *BinanceSpotAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	if b.orderWS == nil {
		b.orderWS = NewSpotUserDataWebSocketManager(b.client, b.useTestnet)
	}
	return b.orderWS.Start(ctx, func(up OrderUpdate) {
		callback(b.toSpotStreamUpdate(up))
	})
}

// SpotStreamOrderUpdate 現貨訂單推送：在 OrderUpdate 上附加基礎幣手續費數量。
// 上層按字段名反射讀取（嵌入字段會被提升），BaseFeeQty 映射到 position.OrderUpdate.BaseFeeQty。
type SpotStreamOrderUpdate struct {
	OrderUpdate
	// BaseFeeQty 本筆成交以基礎幣扣收的手續費（executionReport n，N=基礎幣時；基礎幣單位，>=0）
	BaseFeeQty float64
}

// toSpotStreamUpdate 把 executionReport 的手續費（commissionAsset 計）換算為計價幣口徑：
//   - 計價幣 → 原樣；
//   - 基礎幣 → × 本筆成交價（L，缺失時退回委託價），並把原始數量填入 BaseFeeQty；
//   - 其他幣種（如 BNB 抵扣）→ × 成交所在 UTC 分鐘的歷史K線收盤價。
//
// 降級約定：無法換算時 Commission 保留原幣種數量、CommissionAsset 保留原幣種，並按幣種只告警一次。
// 下游（position 包）把 Commission 當計價幣累加，因此此時費用口徑會有偏差，可通過 CommissionAsset != 計價幣識別。
func (b *BinanceSpotAdapter) toSpotStreamUpdate(up OrderUpdate) SpotStreamOrderUpdate {
	out := SpotStreamOrderUpdate{OrderUpdate: up}
	if up.Commission == 0 || up.CommissionAsset == "" {
		return out
	}
	asset := strings.ToUpper(strings.TrimSpace(up.CommissionAsset))
	if b.baseAsset != "" && strings.EqualFold(asset, b.baseAsset) && up.Commission > 0 {
		out.BaseFeeQty = up.Commission
	}
	if b.quoteAsset == "" {
		b.warnFeeConversionOnce(asset, "計價幣未知（交易對信息未加載）")
		return out
	}
	switch {
	case strings.EqualFold(asset, b.quoteAsset):
		return out
	case strings.EqualFold(asset, b.baseAsset):
		px := up.AvgPrice
		if px <= 0 {
			px = up.Price
		}
		if px <= 0 {
			b.warnFeeConversionOnce(asset, "成交價缺失")
			return out
		}
		out.Commission = up.Commission * px
		out.CommissionAsset = b.quoteAsset
	default:
		ctx, cancel := context.WithTimeout(context.Background(), spotFeeAssetPriceTimeout)
		px, err := b.historicalFeeAssetQuoteRate(ctx, asset, up.UpdateTime)
		cancel()
		if err != nil || px <= 0 {
			b.warnFeeConversionOnce(asset, fmt.Sprintf("查詢成交時點 %s%s 歷史價格失敗: %v", asset, b.quoteAsset, err))
			return out
		}
		out.Commission = up.Commission * px
		out.CommissionAsset = b.quoteAsset
	}
	return out
}

const (
	// spotFeeAssetPriceTTL 手續費幣種（如 BNB）對計價幣價格緩存時長（失敗結果同樣緩存，避免每筆成交都打 REST）
	spotFeeAssetPriceTTL = 30 * time.Second
	// spotFeeAssetPriceTimeout 單次 REST 查價超時（在訂單推送回調中同步執行，須保持很短）
	spotFeeAssetPriceTimeout = 3 * time.Second
)

type spotFeeAssetPrice struct {
	price float64
	err   error
	at    time.Time
}

// feeAssetQuotePrice 返回 asset 以計價幣計的最新價（ASSET+QUOTE ticker），帶 TTL 緩存
func (b *BinanceSpotAdapter) feeAssetQuotePrice(asset string) (float64, error) {
	pair := asset + b.quoteAsset
	now := time.Now()
	b.feePriceMu.Lock()
	if c, ok := b.feePriceCache[pair]; ok && now.Sub(c.at) < spotFeeAssetPriceTTL {
		b.feePriceMu.Unlock()
		return c.price, c.err
	}
	fetch := b.feePriceFetcher
	b.feePriceMu.Unlock()

	if fetch == nil {
		fetch = b.fetchTickerPrice
	}
	ctx, cancel := context.WithTimeout(context.Background(), spotFeeAssetPriceTimeout)
	defer cancel()
	price, err := fetch(ctx, pair)
	if err == nil && price <= 0 {
		err = fmt.Errorf("%s 價格無效: %v", pair, price)
	}

	b.feePriceMu.Lock()
	if b.feePriceCache == nil {
		b.feePriceCache = make(map[string]spotFeeAssetPrice)
	}
	b.feePriceCache[pair] = spotFeeAssetPrice{price: price, err: err, at: now}
	b.feePriceMu.Unlock()
	return price, err
}

// fetchTickerPrice REST 查詢交易對最新價
func (b *BinanceSpotAdapter) fetchTickerPrice(ctx context.Context, pair string) (float64, error) {
	if b.client == nil {
		return 0, fmt.Errorf("Binance 客戶端未初始化，無法查詢 %s 價格", pair)
	}
	ticker, err := b.client.NewListPricesService().Symbol(pair).Do(ctx)
	if err != nil {
		return 0, fmt.Errorf("查詢 %s 價格失敗: %w", pair, err)
	}
	if len(ticker) == 0 {
		return 0, fmt.Errorf("無價格數據: %s", pair)
	}
	return strconv.ParseFloat(ticker[0].Price, 64)
}

// warnFeeConversionOnce 手續費無法換算為計價幣時按幣種只告警一次
func (b *BinanceSpotAdapter) warnFeeConversionOnce(asset, reason string) {
	b.feePriceMu.Lock()
	if b.feeWarned == nil {
		b.feeWarned = make(map[string]bool)
	}
	warned := b.feeWarned[asset]
	b.feeWarned[asset] = true
	b.feePriceMu.Unlock()
	if !warned {
		logger.Warn("⚠️ [Binance Spot] %s 手續費幣種 %s 無法換算為計價幣（%s），Commission 保留原幣種數量，費用統計口徑可能偏差",
			b.symbol, asset, reason)
	}
}

// StopOrderStream 停止訂單流
func (b *BinanceSpotAdapter) StopOrderStream() error {
	if b.orderWS != nil {
		b.orderWS.Stop()
		b.orderWS = nil
	}
	return nil
}

// GetLatestPrice 獲取最新價（現貨，優先 WebSocket 緩存）
func (b *BinanceSpotAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	if b.wsManager != nil {
		if p := b.wsManager.GetLatestPrice(); p > 0 {
			return p, nil
		}
	}
	ticker, err := b.client.NewListPricesService().Symbol(symbol).Do(ctx)
	if err != nil {
		return 0, err
	}
	if len(ticker) == 0 {
		return 0, fmt.Errorf("無價格數據: %s", symbol)
	}
	return strconv.ParseFloat(ticker[0].Price, 64)
}

// StartPriceStream 啟動價格流（現貨 miniTicker WebSocket）
func (b *BinanceSpotAdapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return b.wsManager.StartPriceStream(ctx, symbol, callback)
}

// StartKlineStream 啟動現貨 K 線流（combined stream）
func (b *BinanceSpotAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(interface{})) error {
	if b.klineWS == nil {
		b.klineWS = NewSpotKlineWebSocketManager(b.useTestnet)
	}
	return b.klineWS.Start(ctx, symbols, interval, callback)
}

// StopKlineStream 停止K線流
func (b *BinanceSpotAdapter) StopKlineStream() error {
	if b.klineWS != nil {
		b.klineWS.Stop()
		b.klineWS = nil
	}
	return nil
}

// GetHistoricalKlines 獲取歷史K線
func (b *BinanceSpotAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	klines, err := b.client.NewKlinesService().Symbol(symbol).Interval(interval).Limit(limit).Do(ctx)
	if err != nil {
		return nil, err
	}
	candles := make([]*Candle, 0, len(klines))
	for _, k := range klines {
		open, _ := strconv.ParseFloat(k.Open, 64)
		high, _ := strconv.ParseFloat(k.High, 64)
		low, _ := strconv.ParseFloat(k.Low, 64)
		closeP, _ := strconv.ParseFloat(k.Close, 64)
		vol, _ := strconv.ParseFloat(k.Volume, 64)
		candles = append(candles, &Candle{
			Symbol:    symbol,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closeP,
			Volume:    vol,
			Timestamp: k.OpenTime,
			IsClosed:  true,
		})
	}
	return candles, nil
}

// GetPriceDecimals 價格精度
func (b *BinanceSpotAdapter) GetPriceDecimals() int {
	return b.priceDecimals
}

// GetQuantityDecimals 數量精度
func (b *BinanceSpotAdapter) GetQuantityDecimals() int {
	return b.quantityDecimals
}

// GetOrderFills returns the spot execution ledger for a single order.
func (b *BinanceSpotAdapter) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*OrderFill, error) {
	if b == nil || b.client == nil || b.apiKey == "" || b.secretKey == "" || orderID <= 0 || strings.TrimSpace(symbol) == "" {
		return nil, fmt.Errorf("Binance spot execution lookup requires a configured adapter, symbol, and order ID")
	}
	b.apiCallMu.Lock()
	wait := b.minAPIInterval - time.Since(b.lastAPICallTime)
	if wait > 0 {
		b.apiCallMu.Unlock()
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		b.apiCallMu.Lock()
	}
	b.lastAPICallTime = time.Now()
	b.apiCallMu.Unlock()

	rows, err := b.client.NewListTradesService().Symbol(symbol).OrderId(orderID).Limit(1000).Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("query Binance spot executions for order %d: %w", orderID, err)
	}
	if len(rows) == 1000 {
		return nil, fmt.Errorf("Binance spot order %d reached the execution page limit; completeness is unknown", orderID)
	}
	fills := make([]*OrderFill, 0, len(rows))
	historicalFeeRows := make([]*UserTrade, 0, len(rows))
	for _, row := range rows {
		if row == nil || row.OrderID != orderID || row.Symbol != symbol || row.ID <= 0 {
			return nil, fmt.Errorf("Binance returned an invalid spot execution for order %d", orderID)
		}
		price, priceErr := strconv.ParseFloat(row.Price, 64)
		quantity, quantityErr := strconv.ParseFloat(row.Quantity, 64)
		quoteQty, quoteErr := strconv.ParseFloat(row.QuoteQuantity, 64)
		commission, commissionErr := strconv.ParseFloat(row.Commission, 64)
		if priceErr != nil || quantityErr != nil || quoteErr != nil || commissionErr != nil || price <= 0 || quantity <= 0 || quoteQty <= 0 || commission < 0 {
			return nil, fmt.Errorf("Binance spot execution %d contains invalid economic fields", row.ID)
		}
		side := SideSell
		if row.IsBuyer {
			side = SideBuy
		}
		baseFee := 0.0
		if strings.EqualFold(row.CommissionAsset, b.baseAsset) {
			baseFee = commission
		}
		fill := &OrderFill{OrderID: row.OrderID, TradeID: strconv.FormatInt(row.ID, 10), Symbol: row.Symbol,
			Side: side, Price: price, Quantity: quantity, QuoteQuantity: quoteQty, Commission: commission, CommissionAsset: row.CommissionAsset,
			TradeTime: row.Time, IsMaker: row.IsMaker, BaseFeeQty: baseFee}
		trade := &UserTrade{ID: row.ID, OrderID: row.OrderID, Symbol: row.Symbol, Side: side, Price: price, Quantity: quantity,
			QuoteQuantity: quoteQty, Commission: commission, CommissionAsset: row.CommissionAsset, Time: time.UnixMilli(row.Time), IsMaker: row.IsMaker}
		asset := strings.ToUpper(strings.TrimSpace(row.CommissionAsset))
		switch {
		case strings.EqualFold(asset, b.quoteAsset):
			trade.CommissionQuote, trade.CommissionQuoteRate, trade.CommissionQuoteKnown = commission, 1, true
		case strings.EqualFold(asset, b.baseAsset):
			trade.CommissionQuote, trade.CommissionQuoteRate, trade.CommissionQuoteKnown = commission*price, price, true
		}
		fills = append(fills, fill)
		historicalFeeRows = append(historicalFeeRows, trade)
	}
	b.convertThirdAssetFeesAtHistoricalMinute(ctx, historicalFeeRows)
	for index, trade := range historicalFeeRows {
		fills[index].CommissionQuote = trade.CommissionQuote
		fills[index].CommissionQuoteRate = trade.CommissionQuoteRate
		fills[index].CommissionQuoteKnown = trade.CommissionQuoteKnown
	}
	return fills, nil
}

// GetUserTradesFromID 获取现货账户成交并支持成交 ID 分页。
func (b *BinanceSpotAdapter) GetUserTradesFromID(ctx context.Context, symbol string, startTime, endTime, fromID int64, limit int) ([]*UserTrade, error) {
	if b == nil || b.client == nil || b.apiKey == "" || b.secretKey == "" || strings.TrimSpace(symbol) == "" {
		return nil, fmt.Errorf("Binance spot history requires a configured adapter and symbol")
	}
	b.apiCallMu.Lock()
	wait := b.minAPIInterval - time.Since(b.lastAPICallTime)
	if wait > 0 {
		b.apiCallMu.Unlock()
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		b.apiCallMu.Lock()
	}
	b.lastAPICallTime = time.Now()
	b.apiCallMu.Unlock()

	service := b.client.NewListTradesService().Symbol(symbol)
	if fromID > 0 {
		service = service.FromID(fromID)
	} else {
		if startTime > 0 {
			service = service.StartTime(startTime)
		}
		if endTime > 0 {
			service = service.EndTime(endTime)
		}
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := service.Limit(limit).Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("query Binance spot trade history: %w", err)
	}
	trades := make([]*UserTrade, 0, len(rows))
	for _, row := range rows {
		if row == nil || row.ID <= 0 || row.Symbol != symbol {
			return nil, fmt.Errorf("Binance returned an invalid spot history row")
		}
		price, priceErr := strconv.ParseFloat(row.Price, 64)
		quantity, quantityErr := strconv.ParseFloat(row.Quantity, 64)
		quoteQty, quoteErr := strconv.ParseFloat(row.QuoteQuantity, 64)
		commission, commissionErr := strconv.ParseFloat(row.Commission, 64)
		if priceErr != nil || quantityErr != nil || quoteErr != nil || commissionErr != nil || price <= 0 || quantity <= 0 || commission < 0 {
			return nil, fmt.Errorf("Binance spot history trade %d contains invalid economic fields", row.ID)
		}
		side := SideSell
		if row.IsBuyer {
			side = SideBuy
		}
		trade := &UserTrade{ID: row.ID, OrderID: row.OrderID, Symbol: row.Symbol, Side: side,
			Price: price, Quantity: quantity, QuoteQuantity: quoteQty, Commission: commission,
			CommissionAsset: row.CommissionAsset, Time: time.UnixMilli(row.Time), IsMaker: row.IsMaker}
		asset := strings.ToUpper(strings.TrimSpace(row.CommissionAsset))
		switch asset {
		case strings.ToUpper(b.quoteAsset):
			trade.CommissionQuote, trade.CommissionQuoteRate, trade.CommissionQuoteKnown = commission, 1, true
		case strings.ToUpper(b.baseAsset):
			trade.CommissionQuote, trade.CommissionQuoteRate, trade.CommissionQuoteKnown = commission*price, price, true
		}
		trades = append(trades, trade)
	}
	b.convertThirdAssetFeesAtHistoricalMinute(ctx, trades)
	return trades, nil
}

// GetBaseAsset 基础資產
func (b *BinanceSpotAdapter) GetBaseAsset() string {
	return b.baseAsset
}

// GetQuoteAsset 计價资產
func (b *BinanceSpotAdapter) GetQuoteAsset() string {
	return b.quoteAsset
}

// EstimateFinalOrderAmount 預估订單金額（現貨無最小名义限制時即 price*quantity）
func (b *BinanceSpotAdapter) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	p := b.roundToTickSize(price, SideBuy)
	q := b.roundToStepSize(quantity)
	if q <= 0 {
		q = b.stepSize
	}
	return p * q
}

// GetFundingRate 現貨無资金费率
func (b *BinanceSpotAdapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return 0, nil
}

// GetSpotPrice 現貨最新價即 spot 價
func (b *BinanceSpotAdapter) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return b.GetLatestPrice(ctx, symbol)
}

// GetOrderBook 订單簿
func (b *BinanceSpotAdapter) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	depth, err := b.client.NewDepthService().Symbol(symbol).Limit(limit).Do(ctx)
	if err != nil {
		return nil, err
	}
	bids := make([]OrderBookLevel, 0, len(depth.Bids))
	for _, bid := range depth.Bids {
		price, _ := strconv.ParseFloat(bid.Price, 64)
		qty, _ := strconv.ParseFloat(bid.Quantity, 64)
		bids = append(bids, OrderBookLevel{Price: price, Quantity: qty})
	}
	asks := make([]OrderBookLevel, 0, len(depth.Asks))
	for _, ask := range depth.Asks {
		price, _ := strconv.ParseFloat(ask.Price, 64)
		qty, _ := strconv.ParseFloat(ask.Quantity, 64)
		asks = append(asks, OrderBookLevel{Price: price, Quantity: qty})
	}
	return &OrderBook{
		Symbol:    symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: depth.LastUpdateID,
	}, nil
}

// InternalTransfer 現貨內部轉帳（同 binance adapter 逻辑）
func (b *BinanceSpotAdapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	var transferType binancesdk.UserUniversalTransferType
	switch {
	case strings.EqualFold(fromAccount, "UMFUTURE") && (strings.EqualFold(toAccount, "SPOT") || strings.EqualFold(toAccount, "MAIN")):
		transferType = binancesdk.UserUniversalTransferTypeUmFuturesToMain
	case strings.EqualFold(fromAccount, "MAIN") && strings.EqualFold(toAccount, "UMFUTURE"):
		transferType = binancesdk.UserUniversalTransferTypeMainToUmFutures
	default:
		return "", fmt.Errorf("不支援的轉账類型: %s -> %s", fromAccount, toAccount)
	}
	res, err := b.client.NewUserUniversalTransferService().
		Type(transferType).
		Asset(asset).
		Amount(strconv.FormatFloat(amount, 'f', -1, 64)).
		Do(ctx)
	if err != nil {
		return "", err
	}
	return verifiedUniversalTransferID(res)
}
