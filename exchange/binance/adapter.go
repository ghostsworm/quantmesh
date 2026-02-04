package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"quantmesh/exchange/income"
	"quantmesh/logger"
	"quantmesh/utils"

	binancesdk "github.com/adshao/go-binance/v2"
	"github.com/adshao/go-binance/v2/futures"
)

// 為了避免循環匯入，在这里定义需要的類型
type Side string
type OrderType string
type OrderStatus string
type TimeInForce string

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
	OrderStatusExpired         OrderStatus = "EXPIRED"
)

const (
	TimeInForceGTC TimeInForce = "GTC"
	TimeInForceGTX TimeInForce = "GTX" // Post Only - 無法成為挂單方就撤销
)

type OrderRequest struct {
	Symbol        string
	Side          Side
	Type          OrderType
	TimeInForce   TimeInForce
	Quantity      float64
	Price         float64
	ReduceOnly    bool
	PostOnly      bool // 是否只做 Maker（使用 GTX）
	PriceDecimals int
	ClientOrderID string // 自定义订單ID
	StrategyName  string // 策略名称（可選，用於日志追踪）
	StrategyType  string // 策略類型（可選，如 "grid", "dca", "martingale"）
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

type Position struct {
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
	AccountLeverage    int // 账戶级别的杠杆倍數（從持倉中提取）
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

type OrderUpdateCallback func(update OrderUpdate)

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

// BinanceAdapter 币安交易所适配器
type BinanceAdapter struct {
	client           *futures.Client
	symbol           string
	apiKey           string // 用於 SAPI 內部轉帳等
	secretKey        string
	wsManager        *WebSocketManager
	klineWSManager   *KlineWebSocketManager
	priceDecimals    int     // 價格精度（小數位數）
	quantityDecimals int     // 數量精度（小數位數）
	tickSize         float64 // 價格最小变动單位（如 0.10）
	stepSize         float64 // 數量最小变动單位（如 0.001）
	baseAsset        string  // 基础资產（交易币种），如 BTC
	quoteAsset       string  // 计價资產（結算币种），如 USDT、USD
	useTestnet       bool    // 是否使用測試網

	// 速率限制相关
	lastAPICallTime time.Time     // 上次API調用時间
	apiCallMu       sync.Mutex    // API調用互斥鎖
	minAPIInterval  time.Duration // 最小API調用间隔
}

// APIPermissions API 权限信息（临時定义，避免循環匯入）
type APIPermissions struct {
	CanTrade      bool
	CanWithdraw   bool
	CanTransfer   bool
	CanRead       bool
	IPRestricted  bool
	AllowedIPs    []string
	APIKeyName    string
	CreateTime    int64
	SecurityScore int
	RiskLevel     string
}

// NewBinanceAdapter 創建币安适配器
func NewBinanceAdapter(cfg map[string]string, symbol string) (*BinanceAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]
	testnetStr := cfg["testnet"]

	// 解析測試網配置
	useTestnet := false
	if testnetStr == "true" {
		useTestnet = true
		logger.Info("🌐 [Binance] 使用測試網模式")
	}

	// 設置測試網模式（必須在創建客戶端之前設置）
	futures.UseTestnet = useTestnet

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("Binance API 配置不完整")
	}

	return newBinanceAdapterWithKeys(apiKey, secretKey, symbol, useTestnet)
}

// NewBinanceAdapterForPublicData 創建僅用於獲取公開數據（K 線、交易所信息）的適配器。
// 當 apiKey/secretKey 為空時使用占位符，適用於回測等無需交易權限的場景。Binance K 線為公開 API，無需認證。
func NewBinanceAdapterForPublicData(cfg map[string]string, symbol string) (*BinanceAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]
	testnetStr := cfg["testnet"]

	useTestnet := false
	if testnetStr == "true" {
		useTestnet = true
		logger.Info("🌐 [Binance] 使用測試網模式（公開數據）")
	}

	futures.UseTestnet = useTestnet

	// 公開 API 無需認證，使用占位符通過客戶端構造
	if apiKey == "" {
		apiKey = "backtest_public"
	}
	if secretKey == "" {
		secretKey = "backtest_public"
	}

	return newBinanceAdapterWithKeys(apiKey, secretKey, symbol, useTestnet)
}

// newBinanceAdapterWithKeys 內部實現，支持占位密鑰（用於僅拉取公開數據如 K 線）
func newBinanceAdapterWithKeys(apiKey, secretKey, symbol string, useTestnet bool) (*BinanceAdapter, error) {
	client := futures.NewClient(apiKey, secretKey)

	// 同步服務器時间
	client.NewSetServerTimeService().Do(context.Background())

	wsManager := NewWebSocketManager(apiKey, secretKey, useTestnet)

	adapter := &BinanceAdapter{
		client:         client,
		symbol:         symbol,
		apiKey:         apiKey,
		secretKey:      secretKey,
		wsManager:      wsManager,
		useTestnet:     useTestnet,
		minAPIInterval: 200 * time.Millisecond, // 最小API調用间隔200ms，避免触发限流
	}

	// 獲取合約信息（價格精度、數量精度等）
	ctxInit, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := adapter.fetchExchangeInfo(ctxInit); err != nil {
		logger.Warn("⚠️ [Binance] 獲取合約信息失败: %v，使用默认精度", err)
		// 使用默认值
		adapter.priceDecimals = 2
		adapter.quantityDecimals = 3
	}

	return adapter, nil
}

// GetName 獲取交易所名称
func (b *BinanceAdapter) GetName() string {
	return "Binance"
}

// GetMarketType 獲取市場類型：futures 合約
func (b *BinanceAdapter) GetMarketType() string {
	return "futures"
}

// fetchExchangeInfo 獲取合約信息（價格精度、數量精度等）
func (b *BinanceAdapter) fetchExchangeInfo(ctx context.Context) error {
	exchangeInfo, err := b.client.NewExchangeInfoService().Do(ctx)
	if err != nil {
		return fmt.Errorf("獲取交易所信息失败: %w", err)
	}

	// 查找指定交易對的信息
	for _, symbol := range exchangeInfo.Symbols {
		if symbol.Symbol == b.symbol {
			b.priceDecimals = symbol.PricePrecision
			b.quantityDecimals = symbol.QuantityPrecision
			b.baseAsset = symbol.BaseAsset
			b.quoteAsset = symbol.QuoteAsset

			// 從 Filters 中獲取 tickSize 和 stepSize
			if priceFilter := symbol.PriceFilter(); priceFilter != nil {
				if ts, err := strconv.ParseFloat(priceFilter.TickSize, 64); err == nil && ts > 0 {
					b.tickSize = ts
				}
			}
			if lotSizeFilter := symbol.LotSizeFilter(); lotSizeFilter != nil {
				if ss, err := strconv.ParseFloat(lotSizeFilter.StepSize, 64); err == nil && ss > 0 {
					b.stepSize = ss
				}
			}

			// 如果没有獲取到 tickSize/stepSize，根據精度计算默认值
			if b.tickSize == 0 {
				b.tickSize = math.Pow10(-b.priceDecimals)
			}
			if b.stepSize == 0 {
				b.stepSize = math.Pow10(-b.quantityDecimals)
			}

			logger.Info("ℹ️ [Binance 合約信息] %s - 數量精度:%d, 價格精度:%d, tickSize:%.8f, stepSize:%.8f, 基础币种:%s, 计價币种:%s",
				b.symbol, b.quantityDecimals, b.priceDecimals, b.tickSize, b.stepSize, b.baseAsset, b.quoteAsset)
			return nil
		}
	}

	return fmt.Errorf("未找到合約信息: %s", b.symbol)
}

// roundToTickSize 將價格調整到符合 tickSize 的值
// side: BUY 時向下取整，SELL 時向上取整，确保挂單價格對交易者有利
func (b *BinanceAdapter) roundToTickSize(price float64, side Side) float64 {
	if b.tickSize <= 0 {
		return price
	}
	// 计算價格是 tickSize 的多少倍
	ticks := price / b.tickSize
	var roundedTicks float64
	if side == SideBuy {
		// 買單向下取整（更低的價格對買家有利）
		roundedTicks = math.Floor(ticks)
	} else {
		// 賣單向上取整（更高的價格對賣家有利）
		roundedTicks = math.Ceil(ticks)
	}
	return roundedTicks * b.tickSize
}

// roundToStepSize 將數量調整到符合 stepSize 的值（向下取整）
func (b *BinanceAdapter) roundToStepSize(quantity float64) float64 {
	if b.stepSize <= 0 {
		return quantity
	}
	steps := math.Floor(quantity / b.stepSize)
	return steps * b.stepSize
}

// PlaceOrder 下單
func (b *BinanceAdapter) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	// 驗证價格
	if req.Price <= 0 {
		return nil, fmt.Errorf("無效的下單價格: %.8f（價格必須大於0）", req.Price)
	}

	// 使用 tickSize 調整價格，确保符合交易所要求
	originalPrice := req.Price
	adjustedPrice := b.roundToTickSize(req.Price, req.Side)
	if adjustedPrice != originalPrice {
		logger.Debug("ℹ️ [Binance] [%s] 價格已按 tickSize(%.8f) 調整: %.8f -> %.8f",
			req.Symbol, b.tickSize, originalPrice, adjustedPrice)
		req.Price = adjustedPrice
	}

	// 使用 stepSize 調整數量
	originalQty := req.Quantity
	adjustedQty := b.roundToStepSize(req.Quantity)
	if adjustedQty != originalQty && adjustedQty > 0 {
		logger.Debug("ℹ️ [Binance] [%s] 數量已按 stepSize(%.8f) 調整: %.8f -> %.8f",
			req.Symbol, b.stepSize, originalQty, adjustedQty)
		req.Quantity = adjustedQty
	}

	// 优先使用请求中指定的精度，如果没有则使用從交易所獲取的精度
	pDec := req.PriceDecimals
	if pDec <= 0 {
		pDec = b.priceDecimals
	}
	qDec := b.quantityDecimals

	// 特殊处理：如果下單數量原始值為 0，尝試用最小單位兜底
	if req.Quantity <= 0 {
		minQty := b.stepSize
		if minQty <= 0 {
			minQty = math.Pow10(-qDec)
		}
		req.Quantity = minQty
		logger.Warn("⚠️ [Binance] [%s] 下單數量原始值為 0，已自动調整為最小成交單位: %.8f", req.Symbol, minQty)
	}

	priceStr := fmt.Sprintf("%.*f", pDec, req.Price)
	quantityStr := fmt.Sprintf("%.*f", qDec, req.Quantity)

	// 特殊处理：如果數量截断后為 0，则用交易所允許的最小數量兜底，避免报錯
	q, _ := strconv.ParseFloat(quantityStr, 64)
	if q <= 0 {
		originalQty := req.Quantity // 保存原始數量
		minQty := math.Pow10(-qDec) // 例如精度3，则最小下單量為 0.001
		quantityStr = fmt.Sprintf("%.*f", qDec, minQty)
		req.Quantity = minQty

		// 構建策略信息字符串
		strategyInfo := ""
		if req.StrategyName != "" || req.StrategyType != "" {
			if req.StrategyName != "" && req.StrategyType != "" {
				strategyInfo = fmt.Sprintf("[策略:%s/%s] ", req.StrategyName, req.StrategyType)
			} else if req.StrategyName != "" {
				strategyInfo = fmt.Sprintf("[策略:%s] ", req.StrategyName)
			} else if req.StrategyType != "" {
				strategyInfo = fmt.Sprintf("[策略類型:%s] ", req.StrategyType)
			}
		}

		// 獲取基础资產名称（用於显示單位）
		baseAsset := b.baseAsset
		if baseAsset == "" {
			// 如果無法獲取，尝試從 Symbol 中提取（BTCUSDT -> BTC）
			if len(req.Symbol) > 4 {
				baseAsset = req.Symbol[:len(req.Symbol)-4] // 假設最后4個字符是计價币种（如USDT）
			} else {
				baseAsset = "币"
			}
		}

		// 计算订單金額（USDT）
		orderAmount := originalQty * req.Price
		minOrderAmount := minQty * req.Price

		logger.Warn("⚠️ [Binance] [%s] %s下單數量精度截断警告："+
			"原始數量=%.8f %s (订單金額=%.2f USDT)，"+
			"在精度 %d 下格式化后為 0，已自动調整為最小下單量 %s %s (订單金額=%.2f USDT)",
			req.Symbol, strategyInfo,
			originalQty, baseAsset, orderAmount,
			qDec, quantityStr, baseAsset, minOrderAmount)
	}

	// 最终驗证數量
	finalQty, _ := strconv.ParseFloat(quantityStr, 64)
	if finalQty <= 0 {
		return nil, fmt.Errorf("無效的下單數量: %s（數量必須大於0）", quantityStr)
	}

	// 🔥 币安合約最小訂單金額检查：订單名义價值必須 >= 100 USDT（除非是reduce only订單）
	finalPrice, _ := strconv.ParseFloat(priceStr, 64)
	orderNotional := finalPrice * finalQty
	const minNotional = 100.0 // 币安合約最小訂單金額為100 USDT

	if !req.ReduceOnly && orderNotional < minNotional {
		// 構建策略信息字符串
		strategyInfo := ""
		if req.StrategyName != "" || req.StrategyType != "" {
			if req.StrategyName != "" && req.StrategyType != "" {
				strategyInfo = fmt.Sprintf("[策略:%s/%s] ", req.StrategyName, req.StrategyType)
			} else if req.StrategyName != "" {
				strategyInfo = fmt.Sprintf("[策略:%s] ", req.StrategyName)
			} else if req.StrategyType != "" {
				strategyInfo = fmt.Sprintf("[策略類型:%s] ", req.StrategyType)
			}
		}

		// 獲取基础资產名称（用於显示單位）
		baseAsset := b.baseAsset
		if baseAsset == "" {
			if len(req.Symbol) > 4 {
				baseAsset = req.Symbol[:len(req.Symbol)-4]
			} else {
				baseAsset = "币"
			}
		}

		// 尝試自动上調數量：由於數量精度/步進對齐可能導致名义金額從 100 掉到 99.x
		// 这里按數量精度向上取整，确保最终 notional >= minNotional
		scale := math.Pow10(qDec)
		needQty := minNotional / finalPrice
		adjustedQty := math.Ceil((needQty+1e-12)*scale) / scale // +epsilon 避免浮点误差導致仍不足

		// 防御：如果计算結果没有变大，就至少增加一個最小步進
		if adjustedQty <= finalQty {
			adjustedQty = (math.Floor(finalQty*scale) + 1) / scale
		}

		adjustedQtyStr := fmt.Sprintf("%.*f", qDec, adjustedQty)
		adjustedQtyParsed, _ := strconv.ParseFloat(adjustedQtyStr, 64)
		adjustedNotional := finalPrice * adjustedQtyParsed

		if adjustedQtyParsed > 0 && adjustedNotional >= minNotional {
			logger.Warn("⚠️ [Binance] [%s] %s订單金額不足(%.2f<%.2f USDT)，已自动上調數量: %.8f -> %.8f %s（價格=%.2f，名义金額=%.2f USDT）",
				req.Symbol, strategyInfo, orderNotional, minNotional, finalQty, adjustedQtyParsed, baseAsset, finalPrice, adjustedNotional)

			// 应用修正后的數量
			req.Quantity = adjustedQtyParsed
			quantityStr = adjustedQtyStr
			finalQty = adjustedQtyParsed
			orderNotional = adjustedNotional
		} else {
			logger.Error("❌ [Binance] [%s] %s订單金額不足：订單金額=%.2f USDT，币安合約要求最小訂單金額為 %.2f USDT（除非是reduce only订單）。價格=%.2f，數量=%.8f %s",
				req.Symbol, strategyInfo, orderNotional, minNotional, finalPrice, finalQty, baseAsset)

			return nil, fmt.Errorf("订單金額不足：订單金額 %.2f USDT 小於币安合約最小要求 %.2f USDT（除非是reduce only订單）。请增加订單金額或數量", orderNotional, minNotional)
		}
	}

	// 根據 PostOnly 参數选擇 TimeInForce
	timeInForce := futures.TimeInForceTypeGTC
	if req.PostOnly {
		timeInForce = futures.TimeInForceTypeGTX // Post Only - 只做 Maker
	}

	orderService := b.client.NewCreateOrderService().
		Symbol(req.Symbol).
		Side(futures.SideType(req.Side)).
		Type(futures.OrderTypeLimit).
		TimeInForce(timeInForce).
		Quantity(quantityStr).
		Price(priceStr)

	// 設置自定义订單ID（添加返佣標识）
	clientOrderID := req.ClientOrderID
	if clientOrderID != "" {
		// 添加币安返佣前缀 x-zdfVM8vY（合約經纪商ID）
		clientOrderID = utils.AddBrokerPrefix("binance", clientOrderID)
		orderService = orderService.NewClientOrderID(clientOrderID)
	}

	// 币安單向持倉模式：如果是平倉單，需要設置 ReduceOnly
	// 注意：币安的 ReduceOnly 僅在單向持倉模式下有效
	if req.ReduceOnly {
		orderService = orderService.ReduceOnly(true)
	}

	resp, err := orderService.Do(ctx)

	if err != nil {
		return nil, err
	}

	return &Order{
		OrderID:       resp.OrderID,
		ClientOrderID: resp.ClientOrderID,
		Symbol:        req.Symbol,
		Side:          req.Side,
		Type:          req.Type,
		Price:         req.Price,
		Quantity:      req.Quantity,
		Status:        OrderStatus(resp.Status),
		CreatedAt:     time.Now(),
		UpdateTime:    resp.UpdateTime,
	}, nil
}

// BatchPlaceOrders 批量下單
func (b *BinanceAdapter) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	placedOrders := make([]*Order, 0, len(orders))
	hasMarginError := false

	for _, orderReq := range orders {
		order, err := b.PlaceOrder(ctx, orderReq)
		if err != nil {
			logger.Warn("⚠️ [Binance] 下單失败 %.2f %s: %v",
				orderReq.Price, orderReq.Side, err)

			if strings.Contains(err.Error(), "-2019") || strings.Contains(err.Error(), "insufficient") {
				hasMarginError = true
			}
			continue
		}
		placedOrders = append(placedOrders, order)
	}

	return placedOrders, hasMarginError
}

// CancelOrder 取消訂單
func (b *BinanceAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	_, err := b.client.NewCancelOrderService().
		Symbol(symbol).
		OrderID(orderID).
		Do(ctx)

	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "-2011") || strings.Contains(errStr, "Unknown order") {
			logger.Info("ℹ️ [Binance] 订單 %d 已不存在，跳過取消", orderID)
			return nil
		}
		return err
	}

	logger.Info("✅ [Binance] 取消訂單成功: %d", orderID)
	return nil
}

// BatchCancelOrders 批量撤單
func (b *BinanceAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	if len(orderIDs) == 0 {
		return nil
	}

	// 🔥 Binance 批量撤單限制：最多10個
	batchSize := 10
	for i := 0; i < len(orderIDs); i += batchSize {
		end := i + batchSize
		if end > len(orderIDs) {
			end = len(orderIDs)
		}

		batch := orderIDs[i:end]

		// 🔥 如果只有1個订單，直接用單個撤單接口
		if len(batch) == 1 {
			if err := b.CancelOrder(ctx, symbol, batch[0]); err != nil {
				logger.Warn("⚠️ [Binance] 取消訂單失败 %d: %v", batch[0], err)
			}
			continue
		}

		_, err := b.client.NewCancelMultipleOrdersService().
			Symbol(symbol).
			OrderIDList(batch).
			Do(ctx)

		if err != nil {
			logger.Warn("⚠️ [Binance] 批量撤單失败 (共%d個): %v", len(batch), err)
			// 失败時尝試單個撤單
			logger.Info("🔄 [Binance] 改為逐個撤單...")
			for _, orderID := range batch {
				_ = b.CancelOrder(ctx, symbol, orderID)
				time.Sleep(100 * time.Millisecond) // 避免限频
			}
		} else {
			logger.Info("✅ [Binance] 批量撤單成功: %d 個订單", len(batch))
		}

		// 避免限频
		if i+batchSize < len(orderIDs) {
			time.Sleep(100 * time.Millisecond)
		}
	}

	return nil
}

// GetOrder 查詢訂單
func (b *BinanceAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	order, err := b.client.NewGetOrderService().
		Symbol(symbol).
		OrderID(orderID).
		Do(ctx)

	if err != nil {
		return nil, err
	}

	price, _ := strconv.ParseFloat(order.Price, 64)
	quantity, _ := strconv.ParseFloat(order.OrigQuantity, 64)
	executedQty, _ := strconv.ParseFloat(order.ExecutedQuantity, 64)
	avgPrice, _ := strconv.ParseFloat(order.AvgPrice, 64)

	return &Order{
		OrderID:       order.OrderID,
		ClientOrderID: order.ClientOrderID,
		Symbol:        order.Symbol,
		Side:          Side(order.Side),
		Type:          OrderType(order.Type),
		Price:         price,
		Quantity:      quantity,
		ExecutedQty:   executedQty,
		AvgPrice:      avgPrice,
		Status:        OrderStatus(order.Status),
		UpdateTime:    order.UpdateTime,
	}, nil
}

// GetOpenOrders 查詢未完成订單（添加速率限制和重試逻辑）
func (b *BinanceAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	const maxRetries = 5
	var lastErr error

	for retry := 0; retry < maxRetries; retry++ {
		// 速率限制：确保最小調用间隔
		b.apiCallMu.Lock()
		elapsed := time.Since(b.lastAPICallTime)
		if elapsed < b.minAPIInterval {
			waitTime := b.minAPIInterval - elapsed
			b.apiCallMu.Unlock()
			time.Sleep(waitTime)
			b.apiCallMu.Lock()
		}
		b.lastAPICallTime = time.Now()
		b.apiCallMu.Unlock()

		orders, err := b.client.NewListOpenOrdersService().
			Symbol(symbol).
			Do(ctx)

		if err == nil {
			result := make([]*Order, 0, len(orders))
			for _, order := range orders {
				price, _ := strconv.ParseFloat(order.Price, 64)
				quantity, _ := strconv.ParseFloat(order.OrigQuantity, 64)
				executedQty, _ := strconv.ParseFloat(order.ExecutedQuantity, 64)
				avgPrice, _ := strconv.ParseFloat(order.AvgPrice, 64)

				result = append(result, &Order{
					OrderID:       order.OrderID,
					ClientOrderID: order.ClientOrderID,
					Symbol:        order.Symbol,
					Side:          Side(order.Side),
					Type:          OrderType(order.Type),
					Price:         price,
					Quantity:      quantity,
					ExecutedQty:   executedQty,
					AvgPrice:      avgPrice,
					Status:        OrderStatus(order.Status),
					UpdateTime:    order.UpdateTime,
				})
			}
			return result, nil
		}

		lastErr = err
		errStr := err.Error()

		// 检查是否是速率限制錯误
		if strings.Contains(errStr, "-1003") || strings.Contains(errStr, "Way too many requests") ||
			strings.Contains(errStr, "rate limit") || strings.Contains(errStr, "banned until") {
			// 计算等待時间
			waitDuration := waitForRateLimit(err, retry)

			// 检查上下文是否已取消
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("上下文已取消: %w", ctx.Err())
			default:
			}

			// 等待后重試
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("上下文已取消: %w", ctx.Err())
			case <-time.After(waitDuration):
				// 继续重試
			}
			continue
		}

		// 其他錯误直接返回
		return nil, fmt.Errorf("查詢挂單失败: %w", err)
	}

	// 所有重試都失败
	return nil, fmt.Errorf("查詢挂單失败（重試%d次）: %w", maxRetries, lastErr)
}

// GetAccount 獲取帳戶信息（合約账戶）
func (b *BinanceAdapter) GetAccount(ctx context.Context) (*Account, error) {
	// 記錄當前使用的网络模式
	if b.useTestnet {
		logger.Debug("🌐 [Binance] 正在從測試網獲取帳戶信息")
	} else {
		logger.Debug("🌐 [Binance] 正在從主网獲取帳戶信息")
	}

	// 🔥 修複：使用合約账戶专用的 API
	account, err := b.client.NewGetAccountService().Do(ctx)
	if err != nil {
		// 將常见的英文錯误轉换為友好的中文提示
		errStr := err.Error()
		if strings.Contains(errStr, "Service unavailable from a restricted location") {
			return nil, fmt.Errorf("你的网络连接在限制服務区域，请检查网络或使用代理")
		}
		return nil, err
	}

	// 🔥 修複：從合約账戶的 Assets 中獲取 USDT 餘額
	availableBalance := 0.0
	totalWalletBalance := 0.0
	totalMarginBalance := 0.0

	for _, asset := range account.Assets {
		if asset.Asset == "USDT" || asset.Asset == "USDC" || asset.Asset == "BUSD" {
			balance, _ := strconv.ParseFloat(asset.WalletBalance, 64)
			available, _ := strconv.ParseFloat(asset.AvailableBalance, 64)
			marginBalance, _ := strconv.ParseFloat(asset.MarginBalance, 64)

			totalWalletBalance += balance
			availableBalance += available
			totalMarginBalance += marginBalance
		}
	}

	positions := make([]*Position, 0, len(account.Positions))
	accountLeverage := 1 // 默認 1 倍杠杆
	for _, pos := range account.Positions {
		posAmt, _ := strconv.ParseFloat(pos.PositionAmt, 64)
		entryPrice, _ := strconv.ParseFloat(pos.EntryPrice, 64)
		unrealizedPNL, _ := strconv.ParseFloat(pos.UnrealizedProfit, 64)
		leverage, _ := strconv.Atoi(pos.Leverage)

		// 提取杠杆倍數（取第一個非 1 的杠杆值，因為帳戶級別杠杆通常一致）
		if leverage > accountLeverage {
			accountLeverage = leverage
		}

		// 只添加有持倉的記錄
		if posAmt == 0 {
			continue
		}

		positions = append(positions, &Position{
			Symbol:         pos.Symbol,
			Size:           posAmt,
			EntryPrice:     entryPrice,
			MarkPrice:      0, // 幣安 AccountPosition 没有 MarkPrice
			UnrealizedPNL:  unrealizedPNL,
			Leverage:       leverage,
			MarginType:     "", // 幣安 AccountPosition 没有 MarginType
			IsolatedMargin: 0,  // 幣安 AccountPosition 没有 IsolatedMargin
		})
	}

	return &Account{
		TotalWalletBalance: totalWalletBalance,
		TotalMarginBalance: totalMarginBalance,
		AvailableBalance:   availableBalance,
		Positions:          positions,
		AccountLeverage:    accountLeverage,
	}, nil
}

// parseBanTime 從錯误消息中解析封禁時间（毫秒時间戳）
// 錯误格式: "IP(130.176.187.84) banned until 1767288777555"
func parseBanTime(errMsg string) (time.Time, bool) {
	re := regexp.MustCompile(`banned until (\d+)`)
	matches := re.FindStringSubmatch(errMsg)
	if len(matches) < 2 {
		return time.Time{}, false
	}

	banTimestamp, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return time.Time{}, false
	}

	// 轉换為time.Time（毫秒時间戳）
	banTime := time.Unix(banTimestamp/1000, (banTimestamp%1000)*1000000)
	return banTime, true
}

// waitForRateLimit 等待速率限制，包括解析封禁時间
func waitForRateLimit(err error, retryCount int) time.Duration {
	errStr := err.Error()

	// 检查是否是 -1003 錯误（速率限制）
	if strings.Contains(errStr, "-1003") || strings.Contains(errStr, "Way too many requests") {
		// 尝試解析封禁時间
		if banTime, ok := parseBanTime(errStr); ok {
			now := time.Now()
			if banTime.After(now) {
				waitDuration := banTime.Sub(now) + time.Second // 多等1秒确保解封
				logger.Warn("⚠️ [Binance] IP被封禁直到 %v，等待 %v 后重試", banTime, waitDuration)
				return waitDuration
			}
		}

		// 如果没有解析到封禁時间，使用指數退避
		backoff := time.Duration(1<<uint(retryCount)) * time.Second
		if backoff > 60*time.Second {
			backoff = 60 * time.Second // 最大等待60秒
		}
		logger.Warn("⚠️ [Binance] 触发速率限制，等待 %v 后重試 (第%d次)", backoff, retryCount+1)
		return backoff
	}

	// 其他錯误使用指數退避
	backoff := time.Duration(1<<uint(retryCount)) * time.Second
	if backoff > 10*time.Second {
		backoff = 10 * time.Second
	}
	return backoff
}

// GetPositions 獲取持倉信息（使用PositionRisk API獲取准确的杠杆倍數）
// 添加速率限制和重試逻辑，避免触发 Binance API 限流
func (b *BinanceAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	const maxRetries = 5
	var lastErr error

	for retry := 0; retry < maxRetries; retry++ {
		// 速率限制：确保最小調用间隔
		b.apiCallMu.Lock()
		elapsed := time.Since(b.lastAPICallTime)
		if elapsed < b.minAPIInterval {
			waitTime := b.minAPIInterval - elapsed
			b.apiCallMu.Unlock()
			time.Sleep(waitTime)
			b.apiCallMu.Lock()
		}
		b.lastAPICallTime = time.Now()
		b.apiCallMu.Unlock()

		// 🔥 使用 PositionRisk API，可以獲取准确的杠杆信息
		positionRisks, err := b.client.NewGetPositionRiskService().Symbol(symbol).Do(ctx)
		if err == nil {
			result := make([]*Position, 0)
			for _, pos := range positionRisks {
				posAmt, _ := strconv.ParseFloat(pos.PositionAmt, 64)
				entryPrice, _ := strconv.ParseFloat(pos.EntryPrice, 64)
				unrealizedPNL, _ := strconv.ParseFloat(pos.UnRealizedProfit, 64)
				markPrice, _ := strconv.ParseFloat(pos.MarkPrice, 64)
				isolatedMargin, _ := strconv.ParseFloat(pos.IsolatedMargin, 64)
				leverage, _ := strconv.Atoi(pos.Leverage)

				result = append(result, &Position{
					Symbol:         pos.Symbol,
					Size:           posAmt,
					EntryPrice:     entryPrice,
					MarkPrice:      markPrice,
					UnrealizedPNL:  unrealizedPNL,
					Leverage:       leverage,
					MarginType:     pos.MarginType,
					IsolatedMargin: isolatedMargin,
				})
			}
			return result, nil
		}

		lastErr = err
		errStr := err.Error()

		// 检查是否是速率限制錯误
		if strings.Contains(errStr, "-1003") || strings.Contains(errStr, "Way too many requests") ||
			strings.Contains(errStr, "rate limit") || strings.Contains(errStr, "banned until") {
			// 计算等待時间
			waitDuration := waitForRateLimit(err, retry)

			// 检查上下文是否已取消
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("上下文已取消: %w", ctx.Err())
			default:
			}

			// 等待后重試
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("上下文已取消: %w", ctx.Err())
			case <-time.After(waitDuration):
				// 继续重試
			}
			continue
		}

		// 其他錯误直接返回
		return nil, fmt.Errorf("查詢持倉失败: %w", err)
	}

	// 所有重試都失败
	return nil, fmt.Errorf("查詢持倉失败（重試%d次）: %w", maxRetries, lastErr)
}

// GetBalance 獲取餘額
func (b *BinanceAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	account, err := b.GetAccount(ctx)
	if err != nil {
		return 0, err
	}
	return account.AvailableBalance, nil
}

// StartOrderStream 啟動訂單流（WebSocket）
func (b *BinanceAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	// 轉换回呼函數：將 binance.OrderUpdate 轉换為通用格式
	localCallback := func(update OrderUpdate) {
		// 構造通用的 OrderUpdate 結構（避免導入 exchange 包）
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
	return b.wsManager.Start(ctx, localCallback)
}

// StopOrderStream 停止訂單流
func (b *BinanceAdapter) StopOrderStream() error {
	b.wsManager.Stop()
	return nil
}

// GetLatestPrice 獲取最新價格（合約）
// - 當請求的 symbol 與本適配器訂閱的 b.symbol 相同時，優先從 WebSocket 緩存讀取
// - 當 symbol 不同（例如價差監控查詢多個交易對）或 WebSocket 無數據時，通過 REST premiumIndex 獲取該 symbol 的標記價格
// - 確保價差監控等場景下每個交易對都能拿到對應的合約價格，而非單一訂閱的緩存
func (b *BinanceAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	// 僅當請求的 symbol 與本適配器訂閱一致且 WebSocket 有數據時，使用緩存
	if symbol == b.symbol && b.wsManager != nil {
		price := b.wsManager.GetLatestPrice()
		if price > 0 {
			return price, nil
		}
	}

	// 其他 symbol 或無緩存時：用 REST 獲取該 symbol 的合約標記價格（GET /fapi/v1/premiumIndex）
	premiumIndexList, err := b.client.NewPremiumIndexService().Symbol(symbol).Do(ctx)
	if err != nil {
		return 0, fmt.Errorf("獲取合約價格失敗: %w", err)
	}
	if len(premiumIndexList) == 0 {
		return 0, fmt.Errorf("未找到交易對 %s 的合約價格", symbol)
	}
	markPrice, err := strconv.ParseFloat(premiumIndexList[0].MarkPrice, 64)
	if err != nil {
		return 0, fmt.Errorf("解析合約價格失敗: %w", err)
	}
	return markPrice, nil
}

// StartPriceStream 啟動價格流（WebSocket）
func (b *BinanceAdapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	// 啟動價格流
	return b.wsManager.StartPriceStream(ctx, symbol, callback)
}

// StartKlineStream 啟動K線流（WebSocket）
func (b *BinanceAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(candle interface{})) error {
	if b.klineWSManager == nil {
		b.klineWSManager = NewKlineWebSocketManager(b.useTestnet)
	}
	return b.klineWSManager.Start(ctx, symbols, interval, callback)
}

// StopKlineStream 停止K線流
func (b *BinanceAdapter) StopKlineStream() error {
	if b.klineWSManager != nil {
		b.klineWSManager.Stop()
	}
	return nil
}

// GetHistoricalKlines 獲取歷史K線數據（不帶時間範圍，返回最近 limit 根）
func (b *BinanceAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	klines, err := b.client.NewKlinesService().
		Symbol(symbol).
		Interval(interval).
		Limit(limit).
		Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("獲取歷史K線失败: %w", err)
	}
	return b.klinesToCandles(klines, symbol)
}

// GetHistoricalKlinesFrom 按起始時間獲取歷史K線（用於回測按時間範圍分批拉取）
// startTimeMs 為毫秒時間戳，0 表示不限制（等同 GetHistoricalKlines）
func (b *BinanceAdapter) GetHistoricalKlinesFrom(ctx context.Context, symbol string, interval string, startTimeMs int64, limit int) ([]*Candle, error) {
	svc := b.client.NewKlinesService().
		Symbol(symbol).
		Interval(interval).
		Limit(limit)
	if startTimeMs > 0 {
		svc = svc.StartTime(startTimeMs)
	}
	klines, err := svc.Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("獲取歷史K線失败: %w", err)
	}
	return b.klinesToCandles(klines, symbol)
}

func (b *BinanceAdapter) klinesToCandles(klines []*futures.Kline, symbol string) ([]*Candle, error) {
	candles := make([]*Candle, 0, len(klines))
	for _, k := range klines {
		// 正确解析價格數據，处理解析錯误
		open, err := strconv.ParseFloat(k.Open, 64)
		if err != nil {
			logger.Warn("⚠️ [Binance] K線數據解析失败 (Open): %v, 跳過該K線", err)
			continue
		}
		high, err := strconv.ParseFloat(k.High, 64)
		if err != nil {
			logger.Warn("⚠️ [Binance] K線數據解析失败 (High): %v, 跳過該K線", err)
			continue
		}
		low, err := strconv.ParseFloat(k.Low, 64)
		if err != nil {
			logger.Warn("⚠️ [Binance] K線數據解析失败 (Low): %v, 跳過該K線", err)
			continue
		}
		close, err := strconv.ParseFloat(k.Close, 64)
		if err != nil {
			logger.Warn("⚠️ [Binance] K線數據解析失败 (Close): %v, 跳過該K線", err)
			continue
		}
		volume, err := strconv.ParseFloat(k.Volume, 64)
		if err != nil {
			logger.Warn("⚠️ [Binance] K線數據解析失败 (Volume): %v, 使用0", err)
			volume = 0
		}

		candle := &Candle{
			Symbol:    symbol,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
			Timestamp: k.OpenTime,
			IsClosed:  true, // 历史K線都是已完結的
		}

		// 驗证K線數據
		if err := b.validateCandle(candle); err != nil {
			logger.Warn("⚠️ [Binance] K線數據驗证失败: %v, 跳過該K線", err)
			continue
		}

		candles = append(candles, candle)
	}

	// 检测並記錄异常價格波動（插針）
	candles = b.detectPriceSpikes(candles, 0.20) // 20% 價格變化阈值
	// 插針裁剪已統一在 web/api getKlines 中對所有交易所調用 exchange.ClipKlineSpikes

	return candles, nil
}

// validateCandle 驗证K線數據的合理性
func (b *BinanceAdapter) validateCandle(c *Candle) error {
	// 價格必須為正數
	if c.Open <= 0 || c.High <= 0 || c.Low <= 0 || c.Close <= 0 {
		return fmt.Errorf("價格必須為正數: Open=%.2f, High=%.2f, Low=%.2f, Close=%.2f", c.Open, c.High, c.Low, c.Close)
	}

	// OHLC 关系驗证
	if c.High < c.Low {
		return fmt.Errorf("最高價不能低於最低價: High=%.2f, Low=%.2f", c.High, c.Low)
	}
	if c.High < c.Open || c.High < c.Close {
		return fmt.Errorf("最高價必須大於等於开盘價和收盘價: High=%.2f, Open=%.2f, Close=%.2f", c.High, c.Open, c.Close)
	}
	if c.Low > c.Open || c.Low > c.Close {
		return fmt.Errorf("最低價必須小於等於开盘價和收盘價: Low=%.2f, Open=%.2f, Close=%.2f", c.Low, c.Open, c.Close)
	}

	// 成交量不能為负數
	if c.Volume < 0 {
		return fmt.Errorf("成交量不能為负數: Volume=%.2f", c.Volume)
	}

	return nil
}

// detectPriceSpikes 检测並記錄异常價格波動（插針），供日志排查
// threshold: 價格變化阈值（如 0.20 表示 20%）
func (b *BinanceAdapter) detectPriceSpikes(candles []*Candle, threshold float64) []*Candle {
	if len(candles) < 2 {
		return candles
	}
	for i := 1; i < len(candles); i++ {
		prev := candles[i-1]
		curr := candles[i]
		if prev.Close <= 0 {
			continue
		}
		priceChange := math.Abs(curr.Close-prev.Close) / prev.Close
		if priceChange > threshold {
			logger.Warn("⚠️ [Binance] 检测到异常價格變化: %s, 時间: %d, 变化幅度: %.2f%%, 前一根收盘價: %.2f, 當前收盘價: %.2f",
				curr.Symbol, curr.Timestamp, priceChange*100, prev.Close, curr.Close)
		}
		highChange := (curr.High - prev.Close) / prev.Close
		lowChange := (prev.Close - curr.Low) / prev.Close
		if highChange > threshold || lowChange > threshold {
			logger.Warn("⚠️ [Binance] 检测到异常價格波動（插針）: %s, 時间: %d, High变化: %.2f%%, Low变化: %.2f%%",
				curr.Symbol, curr.Timestamp, highChange*100, lowChange*100)
		}
	}
	return candles
}

// clipPriceSpikes 裁剪 K 線插針：將單根 K 線的 High/Low 限制在「鄰近收盤價與本根開收」的合理區間內，
// 避免交易所壞 tick 或異常數據導致圖表出現不合理的長影線。
// bandPct: 允許的影線幅度，如 0.03 表示相對參考價上下 3%。
func (b *BinanceAdapter) clipPriceSpikes(candles []*Candle, bandPct float64) []*Candle {
	if len(candles) == 0 || bandPct <= 0 {
		return candles
	}
	for i := range candles {
		curr := candles[i]
		refHigh := math.Max(curr.Open, curr.Close)
		refLow := math.Min(curr.Open, curr.Close)
		if i > 0 && candles[i-1].Close > 0 {
			refHigh = math.Max(refHigh, candles[i-1].Close)
			refLow = math.Min(refLow, candles[i-1].Close)
		}
		if i+1 < len(candles) && candles[i+1].Close > 0 {
			refHigh = math.Max(refHigh, candles[i+1].Close)
			refLow = math.Min(refLow, candles[i+1].Close)
		}
		allowedHigh := refHigh * (1 + bandPct)
		allowedLow := refLow * (1 - bandPct)
		if allowedLow <= 0 {
			allowedLow = refLow * 0.99
		}
		clipped := false
		if curr.High > allowedHigh {
			logger.Warn("⚠️ [Binance] K線插針已裁剪: %s, 時间: %d, High %.2f -> %.2f (上限 %.2f)",
				curr.Symbol, curr.Timestamp, curr.High, allowedHigh, allowedHigh)
			curr.High = allowedHigh
			clipped = true
		}
		if curr.Low < allowedLow {
			logger.Warn("⚠️ [Binance] K線插針已裁剪: %s, 時间: %d, Low %.2f -> %.2f (下限 %.2f)",
				curr.Symbol, curr.Timestamp, curr.Low, allowedLow, allowedLow)
			curr.Low = allowedLow
			clipped = true
		}
		if clipped {
			// 裁剪後需保證 OHLC 關係：High >= Open,Close >= Low
			if curr.High < curr.Low {
				curr.High = math.Max(curr.Open, curr.Close)
				curr.Low = math.Min(curr.Open, curr.Close)
			}
		}
	}
	return candles
}

// GetPriceDecimals 獲取價格精度（小數位數）
func (b *BinanceAdapter) GetPriceDecimals() int {
	return b.priceDecimals
}

// GetQuantityDecimals 獲取數量精度（小數位數）
func (b *BinanceAdapter) GetQuantityDecimals() int {
	return b.quantityDecimals
}

// GetBaseAsset 獲取基础资產（交易币种）
func (b *BinanceAdapter) GetBaseAsset() string {
	return b.baseAsset
}

// GetQuoteAsset 獲取计價资產（結算币种）
func (b *BinanceAdapter) GetQuoteAsset() string {
	return b.quoteAsset
}

// GetFundingRate 獲取资金费率
func (b *BinanceAdapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	// 使用币安期货API獲取资金费率
	// API: GET /fapi/v1/premiumIndex
	premiumIndexList, err := b.client.NewPremiumIndexService().Symbol(symbol).Do(ctx)
	if err != nil {
		return 0, fmt.Errorf("獲取资金费率失败: %w", err)
	}

	// PremiumIndexService 返回數组，取第一個元素
	if len(premiumIndexList) == 0 {
		return 0, fmt.Errorf("未找到交易對 %s 的资金费率", symbol)
	}

	premiumIndex := premiumIndexList[0]

	// 解析资金费率（字符串轉浮点數）
	fundingRate, err := strconv.ParseFloat(premiumIndex.LastFundingRate, 64)
	if err != nil {
		return 0, fmt.Errorf("解析资金费率失败: %w", err)
	}

	return fundingRate, nil
}

// GetIncomeHistory 獲取收入歷史（資金費用等）
func (b *BinanceAdapter) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error) {
	svc := b.client.NewGetIncomeHistoryService()
	if symbol != "" {
		svc = svc.Symbol(symbol)
	}
	if incomeType != "" {
		svc = svc.IncomeType(incomeType)
	}
	if startTime > 0 {
		svc = svc.StartTime(startTime)
	}
	if endTime > 0 {
		svc = svc.EndTime(endTime)
	}
	svc = svc.Limit(1000)

	list, err := svc.Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("獲取收入歷史失败: %w", err)
	}

	out := make([]*income.Income, 0, len(list))
	for _, h := range list {
		incomeVal, _ := strconv.ParseFloat(h.Income, 64)
		out = append(out, &income.Income{
			Symbol:        h.Symbol,
			IncomeType:    h.IncomeType,
			Income:        incomeVal,
			Asset:         h.Asset,
			Info:          h.Info,
			TransactionID: h.TranID,
			TradeTime:     time.UnixMilli(h.Time),
		})
	}
	return out, nil
}

// FundingInfo 資金費率詳細信息（本地類型，避免循環引用）
type FundingInfo struct {
	Symbol          string
	Rate            float64
	NextFundingTime time.Time
	MarkPrice       float64
	IndexPrice      float64
}

// GetFundingInfo 獲取資金費率詳細信息
// 包含資金費率、下次結算時間、標記價格、指數價格等
func (b *BinanceAdapter) GetFundingInfo(ctx context.Context, symbol string) (*FundingInfo, error) {
	// 使用币安期货API獲取完整的 premium index 信息
	// API: GET /fapi/v1/premiumIndex
	premiumIndexList, err := b.client.NewPremiumIndexService().Symbol(symbol).Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("獲取資金費率信息失敗: %w", err)
	}

	if len(premiumIndexList) == 0 {
		return nil, fmt.Errorf("未找到交易對 %s 的資金費率信息", symbol)
	}

	pi := premiumIndexList[0]

	// 解析資金費率
	rate, err := strconv.ParseFloat(pi.LastFundingRate, 64)
	if err != nil {
		return nil, fmt.Errorf("解析資金費率失敗: %w", err)
	}

	// 解析標記價格
	markPrice, err := strconv.ParseFloat(pi.MarkPrice, 64)
	if err != nil {
		return nil, fmt.Errorf("解析標記價格失敗: %w", err)
	}

	// 解析指數價格
	indexPrice, err := strconv.ParseFloat(pi.IndexPrice, 64)
	if err != nil {
		return nil, fmt.Errorf("解析指數價格失敗: %w", err)
	}

	// 解析下次結算時間（Unix 毫秒時間戳）
	nextFundingTime := time.UnixMilli(pi.NextFundingTime)

	return &FundingInfo{
		Symbol:          symbol,
		Rate:            rate,
		NextFundingTime: nextFundingTime,
		MarkPrice:       markPrice,
		IndexPrice:      indexPrice,
	}, nil
}

// GetSpotPrice 獲取現貨市场價格
func (b *BinanceAdapter) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	// 使用币安現貨API獲取價格
	// API: GET /api/v3/ticker/price
	// 注意: 需要使用現貨API客戶端，这里使用HTTP直接調用

	url := fmt.Sprintf("https://api.binance.com/api/v3/ticker/price?symbol=%s", symbol)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("創建请求失败: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("请求現貨價格失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("API返回錯误状態 %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Symbol string `json:"symbol"`
		Price  string `json:"price"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("解析响应失败: %w", err)
	}

	price, err := strconv.ParseFloat(result.Price, 64)
	if err != nil {
		return 0, fmt.Errorf("解析價格失败: %w", err)
	}

	return price, nil
}

// CheckAPIPermissions 检查 API 密钥权限
func (b *BinanceAdapter) CheckAPIPermissions(ctx context.Context) (*APIPermissions, error) {
	permissions := &APIPermissions{
		CanRead:  true, // 能調用 API 就說明有读权限
		CanTrade: false,
	}

	// 币安期货 API 权限判断：
	// 尝試獲取帳戶信息来判断是否有交易权限
	_, err := b.client.NewGetAccountService().Do(ctx)
	if err == nil {
		permissions.CanTrade = true
		logger.Info("✅ [Binance] API 具有交易权限")
	} else {
		logger.Warn("⚠️ [Binance] API 可能没有交易权限或調用失败: %v", err)
		// 即使失败也继续，可能是网络问题
		permissions.CanTrade = true // 假設有权限
	}

	// 币安期货 API 不支援提現功能
	// 期貨帳戶的资金轉账需要通過現貨 API 或网页操作
	// 因此期货 API Key 默认不具有提現权限
	permissions.CanWithdraw = false
	permissions.CanTransfer = false

	// 检查 IP 限制
	// 币安 API 没有直接查詢 IP 限制的接口
	// 如果設置了 IP 白名單，從非白名單 IP 調用會回傳 -2015 錯误
	// 这里我们假設能成功調用說明 IP 是允許的或没有限制
	permissions.IPRestricted = false // 無法直接判断，需要用戶在交易所后台确认

	// 计算安全评分
	permissions.SecurityScore = 100
	if permissions.CanWithdraw {
		permissions.SecurityScore -= 50
	}
	if permissions.CanTransfer {
		permissions.SecurityScore -= 30
	}
	if !permissions.IPRestricted {
		permissions.SecurityScore -= 20
	}

	if permissions.SecurityScore >= 80 {
		permissions.RiskLevel = "low"
	} else if permissions.SecurityScore >= 50 {
		permissions.RiskLevel = "medium"
	} else {
		permissions.RiskLevel = "high"
	}

	logger.Info("🔐 [Binance] API 权限检测完成: 交易=%v, 提現=%v, 安全评分=%d, 风險等级=%s",
		permissions.CanTrade, permissions.CanWithdraw, permissions.SecurityScore, permissions.RiskLevel)

	return permissions, nil
}

// EstimateFinalOrderAmount 預估最终下單金額（USDT）
// 币安合約有最小名义金額 100 USDT 的要求，如果原始金額不足會自动上調數量
// 此方法用於资金分配器在下單前准确預留资金，避免預留不足導致保证金不足
func (b *BinanceAdapter) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	// ReduceOnly 订單不受最小名义金額限制
	if reduceOnly {
		return price * quantity
	}

	// 使用 tickSize 調整價格
	adjustedPrice := b.roundToTickSize(price, SideBuy) // 買賣方向對價格影响很小，这里用 BUY
	if adjustedPrice <= 0 {
		adjustedPrice = price
	}

	// 使用 stepSize 調整數量
	adjustedQty := b.roundToStepSize(quantity)
	if adjustedQty <= 0 {
		// 如果數量截断后為 0，用最小步進
		if b.stepSize > 0 {
			adjustedQty = b.stepSize
		} else {
			adjustedQty = math.Pow10(-b.quantityDecimals)
		}
	}

	// 计算名义金額
	notional := adjustedPrice * adjustedQty
	const minNotional = 100.0 // 币安合約最小訂單金額

	// 如果名义金額不足 100 USDT，计算需要上調到多少
	if notional < minNotional {
		// 计算满足最小名义金額所需的數量
		needQty := minNotional / adjustedPrice
		// 按精度向上取整
		scale := math.Pow10(b.quantityDecimals)
		adjustedQty = math.Ceil((needQty+1e-12)*scale) / scale
		notional = adjustedPrice * adjustedQty
	}

	return notional
}

// GetOrderBook 獲取訂單簿深度
func (b *BinanceAdapter) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	// 速率限制
	b.apiCallMu.Lock()
	elapsed := time.Since(b.lastAPICallTime)
	if elapsed < b.minAPIInterval {
		waitTime := b.minAPIInterval - elapsed
		b.apiCallMu.Unlock()
		time.Sleep(waitTime)
		b.apiCallMu.Lock()
	}
	b.lastAPICallTime = time.Now()
	b.apiCallMu.Unlock()

	// 調用 Binance Depth API
	depth, err := b.client.NewDepthService().
		Symbol(symbol).
		Limit(limit).
		Do(ctx)

	if err != nil {
		return nil, fmt.Errorf("獲取訂單簿深度失败: %w", err)
	}

	// 轉换買盘數據（價格從高到低）
	bids := make([]OrderBookLevel, 0, len(depth.Bids))
	for _, bid := range depth.Bids {
		price, err := strconv.ParseFloat(bid.Price, 64)
		if err != nil {
			logger.Warn("⚠️ [Binance] 订單簿買盘價格解析失败: %v", err)
			continue
		}
		quantity, err := strconv.ParseFloat(bid.Quantity, 64)
		if err != nil {
			logger.Warn("⚠️ [Binance] 订單簿買盘數量解析失败: %v", err)
			continue
		}
		bids = append(bids, OrderBookLevel{
			Price:    price,
			Quantity: quantity,
		})
	}

	// 轉换賣盘數據（價格從低到高）
	asks := make([]OrderBookLevel, 0, len(depth.Asks))
	for _, ask := range depth.Asks {
		price, err := strconv.ParseFloat(ask.Price, 64)
		if err != nil {
			logger.Warn("⚠️ [Binance] 订單簿賣盘價格解析失败: %v", err)
			continue
		}
		quantity, err := strconv.ParseFloat(ask.Quantity, 64)
		if err != nil {
			logger.Warn("⚠️ [Binance] 订單簿賣盘數量解析失败: %v", err)
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
		Timestamp: depth.LastUpdateID,
	}, nil
}

// InternalTransfer 交易所內部轉帳（期货帳戶轉現貨等，調用 SAPI）
func (b *BinanceAdapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	if b.apiKey == "" || b.secretKey == "" {
		return "", fmt.Errorf("API 密钥未配置，無法執行內部轉账")
	}
	spotClient := binancesdk.NewClient(b.apiKey, b.secretKey)
	if b.useTestnet {
		spotClient.SetApiEndpoint("https://testnet.binance.vision")
	}
	// 映射為币安 Universal Transfer 類型
	var transferType binancesdk.UserUniversalTransferType
	switch {
	case strings.EqualFold(fromAccount, "UMFUTURE") && (strings.EqualFold(toAccount, "SPOT") || strings.EqualFold(toAccount, "MAIN")):
		transferType = binancesdk.UserUniversalTransferTypeUmFuturesToMain
	case strings.EqualFold(fromAccount, "MAIN") && strings.EqualFold(toAccount, "UMFUTURE"):
		transferType = binancesdk.UserUniversalTransferTypeMainToUmFutures
	default:
		return "", fmt.Errorf("不支援的轉账類型: %s -> %s", fromAccount, toAccount)
	}
	res, err := spotClient.NewUserUniversalTransferService().
		Type(transferType).
		Asset(asset).
		Amount(strconv.FormatFloat(amount, 'f', -1, 64)).
		Do(ctx)
	if err != nil {
		return "", fmt.Errorf("內部轉帳失败: %w", err)
	}
	return strconv.FormatInt(res.ID, 10), nil
}

// UserTrade 用户成交记录
type UserTrade struct {
	ID             int64     // 成交ID
	OrderID        int64     // 订单ID
	Symbol         string    // 交易对
	Side           Side      // 买卖方向
	Price          float64   // 成交价格
	Quantity       float64   // 成交数量
	QuoteQuantity  float64   // 成交金额
	Commission     float64   // 手续费
	CommissionAsset string   // 手续费资产
	RealizedPnL    float64   // 已实现盈亏（仅平仓时有效）
	Time           time.Time // 成交时间
	IsMaker        bool      // 是否为Maker
	PositionSide   string    // 持仓方向（LONG/SHORT）
}

// GetUserTrades 获取用户成交记录（历史成交）
// symbol: 交易对
// startTime, endTime: 毫秒时间戳，0表示不限制
// limit: 返回数量限制，最大1000
func (b *BinanceAdapter) GetUserTrades(ctx context.Context, symbol string, startTime, endTime int64, limit int) ([]*UserTrade, error) {
	if b.apiKey == "" || b.secretKey == "" {
		return nil, fmt.Errorf("API 密钥未配置")
	}

	// 速率限制
	b.apiCallMu.Lock()
	elapsed := time.Since(b.lastAPICallTime)
	if elapsed < b.minAPIInterval {
		waitTime := b.minAPIInterval - elapsed
		b.apiCallMu.Unlock()
		time.Sleep(waitTime)
		b.apiCallMu.Lock()
	}
	b.lastAPICallTime = time.Now()
	b.apiCallMu.Unlock()

	// 使用币安SDK的NewListAccountTradeService获取用户成交记录
	svc := b.client.NewListAccountTradeService().Symbol(symbol)
	if startTime > 0 {
		svc = svc.StartTime(startTime)
	}
	if endTime > 0 {
		svc = svc.EndTime(endTime)
	}
	if limit > 0 {
		if limit > 1000 {
			limit = 1000
		}
		svc = svc.Limit(limit)
	} else {
		svc = svc.Limit(500) // 默认500
	}

	trades, err := svc.Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("獲取用户成交記錄失败: %w", err)
	}

	result := make([]*UserTrade, 0, len(trades))
	for _, trade := range trades {
		price, _ := strconv.ParseFloat(trade.Price, 64)
		qty, _ := strconv.ParseFloat(trade.Quantity, 64)
		quoteQty, _ := strconv.ParseFloat(trade.QuoteQuantity, 64)
		commission, _ := strconv.ParseFloat(trade.Commission, 64)
		realizedPnL, _ := strconv.ParseFloat(trade.RealizedPnl, 64)

		result = append(result, &UserTrade{
			ID:              trade.ID,
			OrderID:         trade.OrderID,
			Symbol:          trade.Symbol,
			Side:            Side(trade.Side),
			Price:           price,
			Quantity:        qty,
			QuoteQuantity:   quoteQty,
			Commission:      commission,
			CommissionAsset: trade.CommissionAsset,
			RealizedPnL:     realizedPnL,
			Time:            time.UnixMilli(trade.Time),
			IsMaker:         trade.Maker,
			PositionSide:    string(trade.PositionSide),
		})
	}

	return result, nil
}
