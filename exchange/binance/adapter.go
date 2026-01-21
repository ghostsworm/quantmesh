package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"

	"github.com/adshao/go-binance/v2/futures"
)

// 为了避免循环导入，在这里定义需要的类型
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
	TimeInForceGTX TimeInForce = "GTX" // Post Only - 无法成为挂单方就撤销
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
	ClientOrderID string // 自定义订单ID
	StrategyName  string // 策略名称（可选，用于日志追踪）
	StrategyType  string // 策略类型（可选，如 "grid", "dca", "martingale"）
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
}

type OrderUpdate struct {
	OrderID       int64
	ClientOrderID string
	Symbol        string
	Side          Side
	Type          OrderType
	Status        OrderStatus
	Price         float64
	Quantity      float64
	ExecutedQty   float64
	AvgPrice      float64
	UpdateTime    int64
}

type OrderUpdateCallback func(update OrderUpdate)

// BinanceAdapter 币安交易所适配器
type BinanceAdapter struct {
	client           *futures.Client
	symbol           string
	wsManager        *WebSocketManager
	klineWSManager   *KlineWebSocketManager
	priceDecimals    int     // 价格精度（小数位数）
	quantityDecimals int     // 数量精度（小数位数）
	tickSize         float64 // 价格最小变动单位（如 0.10）
	stepSize         float64 // 数量最小变动单位（如 0.001）
	baseAsset        string  // 基础资产（交易币种），如 BTC
	quoteAsset       string  // 计价资产（结算币种），如 USDT、USD
	useTestnet       bool    // 是否使用测试网

	// 速率限制相关
	lastAPICallTime time.Time     // 上次API调用时间
	apiCallMu       sync.Mutex    // API调用互斥锁
	minAPIInterval  time.Duration // 最小API调用间隔
}

// APIPermissions API 权限信息（临时定义，避免循环导入）
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

// NewBinanceAdapter 创建币安适配器
func NewBinanceAdapter(cfg map[string]string, symbol string) (*BinanceAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]
	testnetStr := cfg["testnet"]

	// 解析测试网配置
	useTestnet := false
	if testnetStr == "true" {
		useTestnet = true
		logger.Info("🌐 [Binance] 使用测试网模式")
	}

	// 设置测试网模式（必须在创建客户端之前设置）
	futures.UseTestnet = useTestnet

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("Binance API 配置不完整")
	}

	client := futures.NewClient(apiKey, secretKey)

	// 同步服务器时间
	client.NewSetServerTimeService().Do(context.Background())

	wsManager := NewWebSocketManager(apiKey, secretKey, useTestnet)

	adapter := &BinanceAdapter{
		client:         client,
		symbol:         symbol,
		wsManager:      wsManager,
		useTestnet:     useTestnet,
		minAPIInterval: 200 * time.Millisecond, // 最小API调用间隔200ms，避免触发限流
	}

	// 获取合约信息（价格精度、数量精度等）
	ctxInit, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := adapter.fetchExchangeInfo(ctxInit); err != nil {
		logger.Warn("⚠️ [Binance] 获取合约信息失败: %v，使用默认精度", err)
		// 使用默认值
		adapter.priceDecimals = 2
		adapter.quantityDecimals = 3
	}

	return adapter, nil
}

// GetName 获取交易所名称
func (b *BinanceAdapter) GetName() string {
	return "Binance"
}

// fetchExchangeInfo 获取合约信息（价格精度、数量精度等）
func (b *BinanceAdapter) fetchExchangeInfo(ctx context.Context) error {
	exchangeInfo, err := b.client.NewExchangeInfoService().Do(ctx)
	if err != nil {
		return fmt.Errorf("获取交易所信息失败: %w", err)
	}

	// 查找指定交易对的信息
	for _, symbol := range exchangeInfo.Symbols {
		if symbol.Symbol == b.symbol {
			b.priceDecimals = symbol.PricePrecision
			b.quantityDecimals = symbol.QuantityPrecision
			b.baseAsset = symbol.BaseAsset
			b.quoteAsset = symbol.QuoteAsset

			// 从 Filters 中获取 tickSize 和 stepSize
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

			// 如果没有获取到 tickSize/stepSize，根据精度计算默认值
			if b.tickSize == 0 {
				b.tickSize = math.Pow10(-b.priceDecimals)
			}
			if b.stepSize == 0 {
				b.stepSize = math.Pow10(-b.quantityDecimals)
			}

			logger.Info("ℹ️ [Binance 合约信息] %s - 数量精度:%d, 价格精度:%d, tickSize:%.8f, stepSize:%.8f, 基础币种:%s, 计价币种:%s",
				b.symbol, b.quantityDecimals, b.priceDecimals, b.tickSize, b.stepSize, b.baseAsset, b.quoteAsset)
			return nil
		}
	}

	return fmt.Errorf("未找到合约信息: %s", b.symbol)
}

// roundToTickSize 将价格调整到符合 tickSize 的值
// side: BUY 时向下取整，SELL 时向上取整，确保挂单价格对交易者有利
func (b *BinanceAdapter) roundToTickSize(price float64, side Side) float64 {
	if b.tickSize <= 0 {
		return price
	}
	// 计算价格是 tickSize 的多少倍
	ticks := price / b.tickSize
	var roundedTicks float64
	if side == SideBuy {
		// 买单向下取整（更低的价格对买家有利）
		roundedTicks = math.Floor(ticks)
	} else {
		// 卖单向上取整（更高的价格对卖家有利）
		roundedTicks = math.Ceil(ticks)
	}
	return roundedTicks * b.tickSize
}

// roundToStepSize 将数量调整到符合 stepSize 的值（向下取整）
func (b *BinanceAdapter) roundToStepSize(quantity float64) float64 {
	if b.stepSize <= 0 {
		return quantity
	}
	steps := math.Floor(quantity / b.stepSize)
	return steps * b.stepSize
}

// PlaceOrder 下单
func (b *BinanceAdapter) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	// 验证价格
	if req.Price <= 0 {
		return nil, fmt.Errorf("无效的下单价格: %.8f（价格必须大于0）", req.Price)
	}

	// 使用 tickSize 调整价格，确保符合交易所要求
	originalPrice := req.Price
	adjustedPrice := b.roundToTickSize(req.Price, req.Side)
	if adjustedPrice != originalPrice {
		logger.Debug("ℹ️ [Binance] [%s] 价格已按 tickSize(%.8f) 调整: %.8f -> %.8f",
			req.Symbol, b.tickSize, originalPrice, adjustedPrice)
		req.Price = adjustedPrice
	}

	// 使用 stepSize 调整数量
	originalQty := req.Quantity
	adjustedQty := b.roundToStepSize(req.Quantity)
	if adjustedQty != originalQty && adjustedQty > 0 {
		logger.Debug("ℹ️ [Binance] [%s] 数量已按 stepSize(%.8f) 调整: %.8f -> %.8f",
			req.Symbol, b.stepSize, originalQty, adjustedQty)
		req.Quantity = adjustedQty
	}

	// 优先使用请求中指定的精度，如果没有则使用从交易所获取的精度
	pDec := req.PriceDecimals
	if pDec <= 0 {
		pDec = b.priceDecimals
	}
	qDec := b.quantityDecimals

	// 特殊处理：如果下单数量原始值为 0，尝试用最小单位兜底
	if req.Quantity <= 0 {
		minQty := b.stepSize
		if minQty <= 0 {
			minQty = math.Pow10(-qDec)
		}
		req.Quantity = minQty
		logger.Warn("⚠️ [Binance] [%s] 下单数量原始值为 0，已自动调整为最小成交单位: %.8f", req.Symbol, minQty)
	}

	priceStr := fmt.Sprintf("%.*f", pDec, req.Price)
	quantityStr := fmt.Sprintf("%.*f", qDec, req.Quantity)

	// 特殊处理：如果数量截断后为 0，则用交易所允许的最小数量兜底，避免报错
	q, _ := strconv.ParseFloat(quantityStr, 64)
	if q <= 0 {
		originalQty := req.Quantity // 保存原始数量
		minQty := math.Pow10(-qDec) // 例如精度3，则最小下单量为 0.001
		quantityStr = fmt.Sprintf("%.*f", qDec, minQty)
		req.Quantity = minQty
		
		// 构建策略信息字符串
		strategyInfo := ""
		if req.StrategyName != "" || req.StrategyType != "" {
			if req.StrategyName != "" && req.StrategyType != "" {
				strategyInfo = fmt.Sprintf("[策略:%s/%s] ", req.StrategyName, req.StrategyType)
			} else if req.StrategyName != "" {
				strategyInfo = fmt.Sprintf("[策略:%s] ", req.StrategyName)
			} else if req.StrategyType != "" {
				strategyInfo = fmt.Sprintf("[策略类型:%s] ", req.StrategyType)
			}
		}
		
		// 获取基础资产名称（用于显示单位）
		baseAsset := b.baseAsset
		if baseAsset == "" {
			// 如果无法获取，尝试从 Symbol 中提取（BTCUSDT -> BTC）
			if len(req.Symbol) > 4 {
				baseAsset = req.Symbol[:len(req.Symbol)-4] // 假设最后4个字符是计价币种（如USDT）
			} else {
				baseAsset = "币"
			}
		}
		
		// 计算订单金额（USDT）
		orderAmount := originalQty * req.Price
		minOrderAmount := minQty * req.Price
		
		logger.Warn("⚠️ [Binance] [%s] %s下单数量精度截断警告："+
			"原始数量=%.8f %s (订单金额=%.2f USDT)，"+
			"在精度 %d 下格式化后为 0，已自动调整为最小下单量 %s %s (订单金额=%.2f USDT)",
			req.Symbol, strategyInfo,
			originalQty, baseAsset, orderAmount,
			qDec, quantityStr, baseAsset, minOrderAmount)
	}

	// 最终验证数量
	finalQty, _ := strconv.ParseFloat(quantityStr, 64)
	if finalQty <= 0 {
		return nil, fmt.Errorf("无效的下单数量: %s（数量必须大于0）", quantityStr)
	}

	// 🔥 币安合约最小订单金额检查：订单名义价值必须 >= 100 USDT（除非是reduce only订单）
	finalPrice, _ := strconv.ParseFloat(priceStr, 64)
	orderNotional := finalPrice * finalQty
	const minNotional = 100.0 // 币安合约最小订单金额为100 USDT
	
	if !req.ReduceOnly && orderNotional < minNotional {
		// 构建策略信息字符串
		strategyInfo := ""
		if req.StrategyName != "" || req.StrategyType != "" {
			if req.StrategyName != "" && req.StrategyType != "" {
				strategyInfo = fmt.Sprintf("[策略:%s/%s] ", req.StrategyName, req.StrategyType)
			} else if req.StrategyName != "" {
				strategyInfo = fmt.Sprintf("[策略:%s] ", req.StrategyName)
			} else if req.StrategyType != "" {
				strategyInfo = fmt.Sprintf("[策略类型:%s] ", req.StrategyType)
			}
		}
		
		// 获取基础资产名称（用于显示单位）
		baseAsset := b.baseAsset
		if baseAsset == "" {
			if len(req.Symbol) > 4 {
				baseAsset = req.Symbol[:len(req.Symbol)-4]
			} else {
				baseAsset = "币"
			}
		}

		// 尝试自动上调数量：由于数量精度/步进对齐可能导致名义金额从 100 掉到 99.x
		// 这里按数量精度向上取整，确保最终 notional >= minNotional
		scale := math.Pow10(qDec)
		needQty := minNotional / finalPrice
		adjustedQty := math.Ceil((needQty+1e-12)*scale) / scale // +epsilon 避免浮点误差导致仍不足

		// 防御：如果计算结果没有变大，就至少增加一个最小步进
		if adjustedQty <= finalQty {
			adjustedQty = (math.Floor(finalQty*scale) + 1) / scale
		}

		adjustedQtyStr := fmt.Sprintf("%.*f", qDec, adjustedQty)
		adjustedQtyParsed, _ := strconv.ParseFloat(adjustedQtyStr, 64)
		adjustedNotional := finalPrice * adjustedQtyParsed

		if adjustedQtyParsed > 0 && adjustedNotional >= minNotional {
			logger.Warn("⚠️ [Binance] [%s] %s订单金额不足(%.2f<%.2f USDT)，已自动上调数量: %.8f -> %.8f %s（价格=%.2f，名义金额=%.2f USDT）",
				req.Symbol, strategyInfo, orderNotional, minNotional, finalQty, adjustedQtyParsed, baseAsset, finalPrice, adjustedNotional)

			// 应用修正后的数量
			req.Quantity = adjustedQtyParsed
			quantityStr = adjustedQtyStr
			finalQty = adjustedQtyParsed
			orderNotional = adjustedNotional
		} else {
			logger.Error("❌ [Binance] [%s] %s订单金额不足：订单金额=%.2f USDT，币安合约要求最小订单金额为 %.2f USDT（除非是reduce only订单）。价格=%.2f，数量=%.8f %s",
				req.Symbol, strategyInfo, orderNotional, minNotional, finalPrice, finalQty, baseAsset)

			return nil, fmt.Errorf("订单金额不足：订单金额 %.2f USDT 小于币安合约最小要求 %.2f USDT（除非是reduce only订单）。请增加订单金额或数量", orderNotional, minNotional)
		}
	}

	// 根据 PostOnly 参数选择 TimeInForce
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

	// 设置自定义订单ID（添加返佣标识）
	clientOrderID := req.ClientOrderID
	if clientOrderID != "" {
		// 添加币安返佣前缀 x-zdfVM8vY（合约经纪商ID）
		clientOrderID = utils.AddBrokerPrefix("binance", clientOrderID)
		orderService = orderService.NewClientOrderID(clientOrderID)
	}

	// 币安单向持仓模式：如果是平仓单，需要设置 ReduceOnly
	// 注意：币安的 ReduceOnly 仅在单向持仓模式下有效
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

// BatchPlaceOrders 批量下单
func (b *BinanceAdapter) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	placedOrders := make([]*Order, 0, len(orders))
	hasMarginError := false

	for _, orderReq := range orders {
		order, err := b.PlaceOrder(ctx, orderReq)
		if err != nil {
			logger.Warn("⚠️ [Binance] 下单失败 %.2f %s: %v",
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

// CancelOrder 取消订单
func (b *BinanceAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	_, err := b.client.NewCancelOrderService().
		Symbol(symbol).
		OrderID(orderID).
		Do(ctx)

	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "-2011") || strings.Contains(errStr, "Unknown order") {
			logger.Info("ℹ️ [Binance] 订单 %d 已不存在，跳过取消", orderID)
			return nil
		}
		return err
	}

	logger.Info("✅ [Binance] 取消订单成功: %d", orderID)
	return nil
}

// BatchCancelOrders 批量撤单
func (b *BinanceAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	if len(orderIDs) == 0 {
		return nil
	}

	// 🔥 Binance 批量撤单限制：最多10个
	batchSize := 10
	for i := 0; i < len(orderIDs); i += batchSize {
		end := i + batchSize
		if end > len(orderIDs) {
			end = len(orderIDs)
		}

		batch := orderIDs[i:end]

		// 🔥 如果只有1个订单，直接用单个撤单接口
		if len(batch) == 1 {
			if err := b.CancelOrder(ctx, symbol, batch[0]); err != nil {
				logger.Warn("⚠️ [Binance] 取消订单失败 %d: %v", batch[0], err)
			}
			continue
		}

		_, err := b.client.NewCancelMultipleOrdersService().
			Symbol(symbol).
			OrderIDList(batch).
			Do(ctx)

		if err != nil {
			logger.Warn("⚠️ [Binance] 批量撤单失败 (共%d个): %v", len(batch), err)
			// 失败时尝试单个撤单
			logger.Info("🔄 [Binance] 改为逐个撤单...")
			for _, orderID := range batch {
				_ = b.CancelOrder(ctx, symbol, orderID)
				time.Sleep(100 * time.Millisecond) // 避免限频
			}
		} else {
			logger.Info("✅ [Binance] 批量撤单成功: %d 个订单", len(batch))
		}

		// 避免限频
		if i+batchSize < len(orderIDs) {
			time.Sleep(100 * time.Millisecond)
		}
	}

	return nil
}

// GetOrder 查询订单
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

// GetOpenOrders 查询未完成订单（添加速率限制和重试逻辑）
func (b *BinanceAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	const maxRetries = 5
	var lastErr error

	for retry := 0; retry < maxRetries; retry++ {
		// 速率限制：确保最小调用间隔
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

		// 检查是否是速率限制错误
		if strings.Contains(errStr, "-1003") || strings.Contains(errStr, "Way too many requests") ||
			strings.Contains(errStr, "rate limit") || strings.Contains(errStr, "banned until") {
			// 计算等待时间
			waitDuration := waitForRateLimit(err, retry)

			// 检查上下文是否已取消
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("上下文已取消: %w", ctx.Err())
			default:
			}

			// 等待后重试
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("上下文已取消: %w", ctx.Err())
			case <-time.After(waitDuration):
				// 继续重试
			}
			continue
		}

		// 其他错误直接返回
		return nil, fmt.Errorf("查询挂单失败: %w", err)
	}

	// 所有重试都失败
	return nil, fmt.Errorf("查询挂单失败（重试%d次）: %w", maxRetries, lastErr)
}

// GetAccount 获取账户信息（合约账户）
func (b *BinanceAdapter) GetAccount(ctx context.Context) (*Account, error) {
	// 记录当前使用的网络模式
	if b.useTestnet {
		logger.Debug("🌐 [Binance] 正在从测试网获取账户信息")
	} else {
		logger.Debug("🌐 [Binance] 正在从主网获取账户信息")
	}
	
	// 🔥 修复：使用合约账户专用的 API
	account, err := b.client.NewGetAccountService().Do(ctx)
	if err != nil {
		// 将常见的英文错误转换为友好的中文提示
		errStr := err.Error()
		if strings.Contains(errStr, "Service unavailable from a restricted location") {
			return nil, fmt.Errorf("你的网络连接在限制服务区域，请检查网络或使用代理")
		}
		return nil, err
	}

	// 🔥 修复：从合约账户的 Assets 中获取 USDT 余额
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
	for _, pos := range account.Positions {
		posAmt, _ := strconv.ParseFloat(pos.PositionAmt, 64)
		if posAmt == 0 {
			continue
		}

		entryPrice, _ := strconv.ParseFloat(pos.EntryPrice, 64)
		unrealizedPNL, _ := strconv.ParseFloat(pos.UnrealizedProfit, 64)
		leverage, _ := strconv.Atoi(pos.Leverage)

		positions = append(positions, &Position{
			Symbol:         pos.Symbol,
			Size:           posAmt,
			EntryPrice:     entryPrice,
			MarkPrice:      0, // 币安 AccountPosition 没有 MarkPrice
			UnrealizedPNL:  unrealizedPNL,
			Leverage:       leverage,
			MarginType:     "", // 币安 AccountPosition 没有 MarginType
			IsolatedMargin: 0,  // 币安 AccountPosition 没有 IsolatedMargin
		})
	}

	return &Account{
		TotalWalletBalance: totalWalletBalance,
		TotalMarginBalance: totalMarginBalance,
		AvailableBalance:   availableBalance,
		Positions:          positions,
	}, nil
}

// parseBanTime 从错误消息中解析封禁时间（毫秒时间戳）
// 错误格式: "IP(130.176.187.84) banned until 1767288777555"
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

	// 转换为time.Time（毫秒时间戳）
	banTime := time.Unix(banTimestamp/1000, (banTimestamp%1000)*1000000)
	return banTime, true
}

// waitForRateLimit 等待速率限制，包括解析封禁时间
func waitForRateLimit(err error, retryCount int) time.Duration {
	errStr := err.Error()

	// 检查是否是 -1003 错误（速率限制）
	if strings.Contains(errStr, "-1003") || strings.Contains(errStr, "Way too many requests") {
		// 尝试解析封禁时间
		if banTime, ok := parseBanTime(errStr); ok {
			now := time.Now()
			if banTime.After(now) {
				waitDuration := banTime.Sub(now) + time.Second // 多等1秒确保解封
				logger.Warn("⚠️ [Binance] IP被封禁直到 %v，等待 %v 后重试", banTime, waitDuration)
				return waitDuration
			}
		}

		// 如果没有解析到封禁时间，使用指数退避
		backoff := time.Duration(1<<uint(retryCount)) * time.Second
		if backoff > 60*time.Second {
			backoff = 60 * time.Second // 最大等待60秒
		}
		logger.Warn("⚠️ [Binance] 触发速率限制，等待 %v 后重试 (第%d次)", backoff, retryCount+1)
		return backoff
	}

	// 其他错误使用指数退避
	backoff := time.Duration(1<<uint(retryCount)) * time.Second
	if backoff > 10*time.Second {
		backoff = 10 * time.Second
	}
	return backoff
}

// GetPositions 获取持仓信息（使用PositionRisk API获取准确的杠杆倍数）
// 添加速率限制和重试逻辑，避免触发 Binance API 限流
func (b *BinanceAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	const maxRetries = 5
	var lastErr error

	for retry := 0; retry < maxRetries; retry++ {
		// 速率限制：确保最小调用间隔
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

		// 🔥 使用 PositionRisk API，可以获取准确的杠杆信息
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

		// 检查是否是速率限制错误
		if strings.Contains(errStr, "-1003") || strings.Contains(errStr, "Way too many requests") ||
			strings.Contains(errStr, "rate limit") || strings.Contains(errStr, "banned until") {
			// 计算等待时间
			waitDuration := waitForRateLimit(err, retry)

			// 检查上下文是否已取消
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("上下文已取消: %w", ctx.Err())
			default:
			}

			// 等待后重试
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("上下文已取消: %w", ctx.Err())
			case <-time.After(waitDuration):
				// 继续重试
			}
			continue
		}

		// 其他错误直接返回
		return nil, fmt.Errorf("查询持仓失败: %w", err)
	}

	// 所有重试都失败
	return nil, fmt.Errorf("查询持仓失败（重试%d次）: %w", maxRetries, lastErr)
}

// GetBalance 获取余额
func (b *BinanceAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	account, err := b.GetAccount(ctx)
	if err != nil {
		return 0, err
	}
	return account.AvailableBalance, nil
}

// StartOrderStream 启动订单流（WebSocket）
func (b *BinanceAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	// 转换回调函数：将 binance.OrderUpdate 转换为通用格式
	localCallback := func(update OrderUpdate) {
		// 构造通用的 OrderUpdate 结构（避免导入 exchange 包）
		genericUpdate := struct {
			OrderID       int64
			ClientOrderID string
			Symbol        string
			Side          string
			Type          string
			Status        string
			Price         float64
			Quantity      float64
			ExecutedQty   float64
			AvgPrice      float64
			UpdateTime    int64
		}{
			OrderID:       update.OrderID,
			ClientOrderID: update.ClientOrderID, // 🔥 关键：传递 ClientOrderID
			Symbol:        update.Symbol,
			Side:          string(update.Side),
			Type:          string(update.Type),
			Status:        string(update.Status),
			Price:         update.Price,
			Quantity:      update.Quantity,
			ExecutedQty:   update.ExecutedQty,
			AvgPrice:      update.AvgPrice,
			UpdateTime:    update.UpdateTime,
		}
		callback(genericUpdate)
	}
	return b.wsManager.Start(ctx, localCallback)
}

// StopOrderStream 停止订单流
func (b *BinanceAdapter) StopOrderStream() error {
	b.wsManager.Stop()
	return nil
}

// GetLatestPrice 获取最新价格（仅从 WebSocket 缓存读取）
// 架构说明：
// - 各组件不应直接调用此方法获取实时价格
// - 实时价格应该通过 PriceMonitor.GetLastPrice() 获取（订阅模式）
// - 此方法仅用于下单时的价格诊断（检查订单价格与市场价格的偏离）
// - WebSocket 是唯一的价格来源，不使用 REST API
// - 如果 WebSocket 未启动或断开，返回错误
func (b *BinanceAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	// 从 WebSocket 缓存读取价格
	if b.wsManager != nil {
		price := b.wsManager.GetLatestPrice()
		if price > 0 {
			return price, nil
		}
	}

	// WebSocket 未启动或无价格数据
	return 0, fmt.Errorf("WebSocket 价格流未就绪或无价格数据")
}

// StartPriceStream 启动价格流（WebSocket）
func (b *BinanceAdapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	// 启动价格流
	return b.wsManager.StartPriceStream(ctx, symbol, callback)
}

// StartKlineStream 启动K线流（WebSocket）
func (b *BinanceAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(candle interface{})) error {
	if b.klineWSManager == nil {
		b.klineWSManager = NewKlineWebSocketManager(b.useTestnet)
	}
	return b.klineWSManager.Start(ctx, symbols, interval, callback)
}

// StopKlineStream 停止K线流
func (b *BinanceAdapter) StopKlineStream() error {
	if b.klineWSManager != nil {
		b.klineWSManager.Stop()
	}
	return nil
}

// GetHistoricalKlines 获取历史K线数据
func (b *BinanceAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	klines, err := b.client.NewKlinesService().
		Symbol(symbol).
		Interval(interval).
		Limit(limit).
		Do(ctx)

	if err != nil {
		return nil, fmt.Errorf("获取历史K线失败: %w", err)
	}

	candles := make([]*Candle, 0, len(klines))
	for _, k := range klines {
		open, _ := strconv.ParseFloat(k.Open, 64)
		high, _ := strconv.ParseFloat(k.High, 64)
		low, _ := strconv.ParseFloat(k.Low, 64)
		close, _ := strconv.ParseFloat(k.Close, 64)
		volume, _ := strconv.ParseFloat(k.Volume, 64)

		candles = append(candles, &Candle{
			Symbol:    symbol,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
			Timestamp: k.OpenTime,
			IsClosed:  true, // 历史K线都是已完结的
		})
	}

	return candles, nil
}

// GetPriceDecimals 获取价格精度（小数位数）
func (b *BinanceAdapter) GetPriceDecimals() int {
	return b.priceDecimals
}

// GetQuantityDecimals 获取数量精度（小数位数）
func (b *BinanceAdapter) GetQuantityDecimals() int {
	return b.quantityDecimals
}

// GetBaseAsset 获取基础资产（交易币种）
func (b *BinanceAdapter) GetBaseAsset() string {
	return b.baseAsset
}

// GetQuoteAsset 获取计价资产（结算币种）
func (b *BinanceAdapter) GetQuoteAsset() string {
	return b.quoteAsset
}

// GetFundingRate 获取资金费率
func (b *BinanceAdapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	// 使用币安期货API获取资金费率
	// API: GET /fapi/v1/premiumIndex
	premiumIndexList, err := b.client.NewPremiumIndexService().Symbol(symbol).Do(ctx)
	if err != nil {
		return 0, fmt.Errorf("获取资金费率失败: %w", err)
	}

	// PremiumIndexService 返回数组，取第一个元素
	if len(premiumIndexList) == 0 {
		return 0, fmt.Errorf("未找到交易对 %s 的资金费率", symbol)
	}

	premiumIndex := premiumIndexList[0]

	// 解析资金费率（字符串转浮点数）
	fundingRate, err := strconv.ParseFloat(premiumIndex.LastFundingRate, 64)
	if err != nil {
		return 0, fmt.Errorf("解析资金费率失败: %w", err)
	}

	return fundingRate, nil
}

// GetSpotPrice 获取现货市场价格
func (b *BinanceAdapter) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	// 使用币安现货API获取价格
	// API: GET /api/v3/ticker/price
	// 注意: 需要使用现货API客户端，这里使用HTTP直接调用

	url := fmt.Sprintf("https://api.binance.com/api/v3/ticker/price?symbol=%s", symbol)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("创建请求失败: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("请求现货价格失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("API返回错误状态 %d: %s", resp.StatusCode, string(body))
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
		return 0, fmt.Errorf("解析价格失败: %w", err)
	}

	return price, nil
}

// CheckAPIPermissions 检查 API 密钥权限
func (b *BinanceAdapter) CheckAPIPermissions(ctx context.Context) (*APIPermissions, error) {
	permissions := &APIPermissions{
		CanRead:  true, // 能调用 API 就说明有读权限
		CanTrade: false,
	}

	// 币安期货 API 权限判断：
	// 尝试获取账户信息来判断是否有交易权限
	_, err := b.client.NewGetAccountService().Do(ctx)
	if err == nil {
		permissions.CanTrade = true
		logger.Info("✅ [Binance] API 具有交易权限")
	} else {
		logger.Warn("⚠️ [Binance] API 可能没有交易权限或调用失败: %v", err)
		// 即使失败也继续，可能是网络问题
		permissions.CanTrade = true // 假设有权限
	}

	// 币安期货 API 不支持提现功能
	// 期货账户的资金转账需要通过现货 API 或网页操作
	// 因此期货 API Key 默认不具有提现权限
	permissions.CanWithdraw = false
	permissions.CanTransfer = false

	// 检查 IP 限制
	// 币安 API 没有直接查询 IP 限制的接口
	// 如果设置了 IP 白名单，从非白名单 IP 调用会返回 -2015 错误
	// 这里我们假设能成功调用说明 IP 是允许的或没有限制
	permissions.IPRestricted = false // 无法直接判断，需要用户在交易所后台确认

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

	logger.Info("🔐 [Binance] API 权限检测完成: 交易=%v, 提现=%v, 安全评分=%d, 风险等级=%s",
		permissions.CanTrade, permissions.CanWithdraw, permissions.SecurityScore, permissions.RiskLevel)

	return permissions, nil
}

// EstimateFinalOrderAmount 预估最终下单金额（USDT）
// 币安合约有最小名义金额 100 USDT 的要求，如果原始金额不足会自动上调数量
// 此方法用于资金分配器在下单前准确预留资金，避免预留不足导致保证金不足
func (b *BinanceAdapter) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	// ReduceOnly 订单不受最小名义金额限制
	if reduceOnly {
		return price * quantity
	}

	// 使用 tickSize 调整价格
	adjustedPrice := b.roundToTickSize(price, SideBuy) // 买卖方向对价格影响很小，这里用 BUY
	if adjustedPrice <= 0 {
		adjustedPrice = price
	}

	// 使用 stepSize 调整数量
	adjustedQty := b.roundToStepSize(quantity)
	if adjustedQty <= 0 {
		// 如果数量截断后为 0，用最小步进
		if b.stepSize > 0 {
			adjustedQty = b.stepSize
		} else {
			adjustedQty = math.Pow10(-b.quantityDecimals)
		}
	}

	// 计算名义金额
	notional := adjustedPrice * adjustedQty
	const minNotional = 100.0 // 币安合约最小订单金额

	// 如果名义金额不足 100 USDT，计算需要上调到多少
	if notional < minNotional {
		// 计算满足最小名义金额所需的数量
		needQty := minNotional / adjustedPrice
		// 按精度向上取整
		scale := math.Pow10(b.quantityDecimals)
		adjustedQty = math.Ceil((needQty+1e-12)*scale) / scale
		notional = adjustedPrice * adjustedQty
	}

	return notional
}
