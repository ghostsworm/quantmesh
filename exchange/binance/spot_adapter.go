package binance

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

	binancesdk "github.com/adshao/go-binance/v2"
)

// BinanceSpotAdapter 币安現貨交易所适配器
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

	client := binancesdk.NewClient(apiKey, secretKey)
	if useTestnet {
		client.SetApiEndpoint("https://testnet.binance.vision")
	}

	client.NewSetServerTimeService().Do(context.Background())

	adapter := &BinanceSpotAdapter{
		client:          client,
		symbol:          symbol,
		apiKey:          apiKey,
		secretKey:       secretKey,
		useTestnet:      useTestnet,
		minAPIInterval:  200 * time.Millisecond,
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
			b.priceDecimals = s.QuotePrecision
			b.quantityDecimals = s.BaseAssetPrecision
			b.baseAsset = s.BaseAsset
			b.quoteAsset = s.QuoteAsset

			if pf := s.PriceFilter(); pf != nil && pf.TickSize != "" {
				b.tickSize, _ = strconv.ParseFloat(pf.TickSize, 64)
			}
			if lf := s.LotSizeFilter(); lf != nil && lf.StepSize != "" {
				b.stepSize, _ = strconv.ParseFloat(lf.StepSize, 64)
			}
			if b.tickSize <= 0 {
				b.tickSize = math.Pow10(-b.priceDecimals)
			}
			if b.stepSize <= 0 {
				b.stepSize = math.Pow10(-b.quantityDecimals)
			}
			logger.Info("ℹ️ [Binance Spot] %s - 數量精度:%d, 價格精度:%d, 基础:%s, 计價:%s",
				b.symbol, b.quantityDecimals, b.priceDecimals, b.baseAsset, b.quoteAsset)
			return nil
		}
	}
	return fmt.Errorf("未找到交易對信息: %s", b.symbol)
}

func (b *BinanceSpotAdapter) roundToTickSize(price float64, side Side) float64 {
	if b.tickSize <= 0 {
		return price
	}
	ticks := price / b.tickSize
	var roundedTicks float64
	if side == SideBuy {
		roundedTicks = math.Floor(ticks)
	} else {
		roundedTicks = math.Ceil(ticks)
	}
	return roundedTicks * b.tickSize
}

func (b *BinanceSpotAdapter) roundToStepSize(quantity float64) float64 {
	if b.stepSize <= 0 {
		return quantity
	}
	steps := math.Floor(quantity / b.stepSize)
	return steps * b.stepSize
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
	avgPrice := price
	if resp.ExecutedQuantity != "0" && resp.ExecutedQuantity != "" && resp.CummulativeQuoteQuantity != "" {
		if cumQuote, err := strconv.ParseFloat(resp.CummulativeQuoteQuantity, 64); err == nil && execQty > 0 {
			avgPrice = cumQuote / execQty
		}
	}

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
		if strings.Contains(err.Error(), "-2011") || strings.Contains(err.Error(), "Unknown order") {
			logger.Info("ℹ️ [Binance Spot] 订單 %d 已不存在，跳過取消", orderID)
			return nil
		}
		return err
	}
	return nil
}

// BatchCancelOrders 批量撤單
func (b *BinanceSpotAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	for _, id := range orderIDs {
		_ = b.CancelOrder(ctx, symbol, id)
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

// CancelAllOrders 取消該交易對下所有订單
func (b *BinanceSpotAdapter) CancelAllOrders(ctx context.Context, symbol string) error {
	_, err := b.client.NewCancelOpenOrdersService().Symbol(symbol).Do(ctx)
	return err
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
	avgPrice, _ := strconv.ParseFloat(o.Price, 64)
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
		avgPrice, _ := strconv.ParseFloat(o.Price, 64)
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

// GetAccount 獲取現貨账戶（餘額）
func (b *BinanceSpotAdapter) GetAccount(ctx context.Context) (*Account, error) {
	acc, err := b.client.NewGetAccountService().Do(ctx)
	if err != nil {
		return nil, err
	}
	var totalWallet, available float64
	for _, bal := range acc.Balances {
		free, _ := strconv.ParseFloat(bal.Free, 64)
		locked, _ := strconv.ParseFloat(bal.Locked, 64)
		totalWallet += free + locked
		available += free
	}
	// 僅统计 USDT/USDC/BUSD 等常用计價资產作為可用餘額
	available = 0
	for _, bal := range acc.Balances {
		if bal.Asset == "USDT" || bal.Asset == "USDC" || bal.Asset == "BUSD" {
			f, _ := strconv.ParseFloat(bal.Free, 64)
			available += f
		}
	}
	return &Account{
		TotalWalletBalance: totalWallet,
		TotalMarginBalance:  totalWallet,
		AvailableBalance:   available,
		Positions:          nil,
	}, nil
}

// GetPositions 現貨無合約持倉，返回基础资產餘額構成的“持倉”（用於网格賣單逻辑）
func (b *BinanceSpotAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	acc, err := b.client.NewGetAccountService().Do(ctx)
	if err != nil {
		return nil, err
	}
	base := b.baseAsset
	if base == "" {
		base = strings.TrimSuffix(symbol, "USDT")
		if base == symbol {
			base = strings.TrimSuffix(symbol, "BUSD")
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

// GetBalance 獲取某资產餘額
func (b *BinanceSpotAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	acc, err := b.client.NewGetAccountService().Do(ctx)
	if err != nil {
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

// StartOrderStream 現貨訂單流（暂用輪詢，可后续接 User Data Stream）
func (b *BinanceSpotAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return fmt.Errorf("現貨訂單流暂未實現，请依赖對账輪詢")
}

// StopOrderStream 停止訂單流
func (b *BinanceSpotAdapter) StopOrderStream() error {
	return nil
}

// GetLatestPrice 獲取最新價（現貨）
func (b *BinanceSpotAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	ticker, err := b.client.NewListPricesService().Symbol(symbol).Do(ctx)
	if err != nil {
		return 0, err
	}
	if len(ticker) == 0 {
		return 0, fmt.Errorf("無價格數據: %s", symbol)
	}
	return strconv.ParseFloat(ticker[0].Price, 64)
}

// StartPriceStream 啟動價格流（現貨可接 BookTicker WS，此处暂不實現）
func (b *BinanceSpotAdapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return fmt.Errorf("現貨價格流暂未實現")
}

// StartKlineStream 啟動K線流
func (b *BinanceSpotAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(interface{})) error {
	return fmt.Errorf("現貨K線流暂未實現")
}

// StopKlineStream 停止K線流
func (b *BinanceSpotAdapter) StopKlineStream() error {
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

// GetBaseAsset 基础资產
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
	return strconv.FormatInt(res.ID, 10), nil
}
