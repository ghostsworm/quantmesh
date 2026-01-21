package order

import (
	"context"
	"fmt"
	"math"
	"quantmesh/exchange"
	"quantmesh/lock"
	"quantmesh/logger"
	"quantmesh/metrics"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// OrderRequest 订单请求
type OrderRequest struct {
	Symbol        string
	Side          string
	Price         float64
	Quantity      float64
	PriceDecimals int    // 价格小数位数（用于格式化价格字符串）
	ReduceOnly    bool   // 是否只减仓（平仓单）
	PostOnly      bool   // 是否只做 Maker（Post Only）
	ClientOrderID string // 自定义订单ID
	StrategyName  string // 策略名称（可选，用于日志追踪）
	StrategyType  string // 策略类型（可选，如 "grid", "dca", "martingale"）
}

// Order 订单信息
type Order struct {
	OrderID       int64
	ClientOrderID string
	Symbol        string
	Side          string
	Price         float64
	Quantity      float64
	Status        string
	CreatedAt     time.Time
}

// ExchangeOrderExecutor 基于 exchange.IExchange 的订单执行器
type ExchangeOrderExecutor struct {
	exchange    exchange.IExchange
	symbol      string
	rateLimiter *rate.Limiter
	lock        lock.DistributedLock // 分布式锁

	// 时间配置
	rateLimitRetryDelay time.Duration
	orderRetryDelay     time.Duration
}

// NewExchangeOrderExecutor 创建基于交易所接口的订单执行器
func NewExchangeOrderExecutor(ex exchange.IExchange, symbol string, rateLimitRetryDelay, orderRetryDelay int, distributedLock lock.DistributedLock) *ExchangeOrderExecutor {
	return &ExchangeOrderExecutor{
		exchange:            ex,
		symbol:              symbol,
		rateLimiter:         rate.NewLimiter(rate.Limit(25), 30), // 25单/秒，突发30
		lock:                distributedLock,
		rateLimitRetryDelay: time.Duration(rateLimitRetryDelay) * time.Second,
		orderRetryDelay:     time.Duration(orderRetryDelay) * time.Millisecond,
	}
}

// isPostOnlyError 检查是否为PostOnly错误
func isPostOnlyError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Binance: code=-5022, Bitget: Post Only order will be rejected, Gate.io: ORDER_POC_IMMEDIATE
	return strings.Contains(errStr, "-5022") ||
		strings.Contains(errStr, "Post Only") ||
		strings.Contains(errStr, "post_only") ||
		strings.Contains(errStr, "would immediately match") ||
		strings.Contains(errStr, "ORDER_POC_IMMEDIATE")
}

// isReduceOnlyError 检查是否为ReduceOnly错误（无持仓时尝试减仓）
func isReduceOnlyError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Binance: code=-2022, msg=ReduceOnly Order is rejected
	// 注意：不要直接匹配 "reduce only"，因为金额不足的报错 "-4164" 里也包含这个词
	return strings.Contains(errStr, "-2022") ||
		strings.Contains(errStr, "ReduceOnly Order is rejected") ||
		(strings.Contains(errStr, "reduce only") && !strings.Contains(errStr, "-4164"))
}

// PlaceOrder 下单（带重试）
func (oe *ExchangeOrderExecutor) PlaceOrder(req *OrderRequest) (*Order, error) {
	startTime := time.Now()
	pm := metrics.GetPrometheusMetrics()
	exchangeName := oe.exchange.GetName()

	// 分布式锁：防止多实例对同一价格位重复下单
	// 使用价格区间锁（中粒度）：每10个价格间隔一个锁
	priceLevel := math.Floor(req.Price/10) * 10
	lockKey := fmt.Sprintf("order:%s:%s:%.0f", exchangeName, req.Symbol, priceLevel)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	acquired, err := oe.lock.TryLock(ctx, lockKey, 5*time.Second)
	if err != nil {
		logger.Warn("⚠️ [%s] 获取锁失败: %v", exchangeName, err)
		// 锁获取失败不阻塞，继续执行（降级策略）
	} else if !acquired {
		logger.Debug("🔒 [%s] 价格位 %.2f 已被其他实例锁定，跳过", exchangeName, req.Price)
		return nil, nil // 返回 nil 表示跳过，不是错误
	} else {
		// 成功获取锁，defer 释放
		defer func() {
			if unlockErr := oe.lock.Unlock(ctx, lockKey); unlockErr != nil {
				logger.Warn("⚠️ [%s] 释放锁失败: %v", exchangeName, unlockErr)
			}
		}()
	}

	// 限流
	if err := oe.rateLimiter.Wait(context.Background()); err != nil {
		return nil, fmt.Errorf("速率限制等待失败: %v", err)
	}

	maxRetries := 5 // 增加重试次数:3次PostOnly + 1次降级 + 1次保险
	var lastErr error
	postOnlyFailCount := 0
	degraded := false // 是否已降级为普通单

	for i := 0; i <= maxRetries; i++ {
		// 转换为通用订单请求
		exchangeReq := &exchange.OrderRequest{
			Symbol:        req.Symbol,
			Side:          exchange.Side(req.Side),
			Type:          exchange.OrderTypeLimit,
			TimeInForce:   exchange.TimeInForceGTC,
			Quantity:      req.Quantity,
			Price:         req.Price,
			PriceDecimals: req.PriceDecimals,
			ReduceOnly:    req.ReduceOnly,
			PostOnly:      req.PostOnly && !degraded, // 如果已降级，强制为普通单
			ClientOrderID: req.ClientOrderID,         // 传递自定义订单ID
			StrategyName:  req.StrategyName,          // 传递策略名称
			StrategyType:  req.StrategyType,          // 传递策略类型
		}

		// 🔥 如果PostOnly已失败3次，降级为普通限价单
		if postOnlyFailCount >= 3 && req.PostOnly && !degraded {
			degraded = true
			logger.Warn("⚠️ [%s] PostOnly已失败3次，降级为普通限价单: %s %.2f",
				oe.exchange.GetName(), req.Side, req.Price)
			exchangeReq.PostOnly = false
		}

		// 调用交易所接口
		exchangeOrder, err := oe.exchange.PlaceOrder(context.Background(), exchangeReq)
		if err == nil {
			// 转换回 Order 格式
			order := &Order{
				OrderID:       exchangeOrder.OrderID,
				ClientOrderID: exchangeOrder.ClientOrderID,
				Symbol:        req.Symbol,
				Side:          req.Side,
				Price:         req.Price,
				Quantity:      req.Quantity,
				Status:        string(exchangeOrder.Status),
				CreatedAt:     time.Now(),
			}

			// 记录 Prometheus 指标
			duration := time.Since(startTime)
			pm.RecordOrder(exchangeName, req.Symbol, req.Side, string(exchangeOrder.Status))
			pm.RecordOrderSuccess(exchangeName, req.Symbol, req.Side, duration)

			// 根据实际使用的订单类型显示日志
			orderTypeDesc := "PostOnly"
			if !exchangeReq.PostOnly {
				orderTypeDesc = "普通单(PostOnly降级)"
			}
			logger.Info("✅ [%s] 下单成功(%s): %s %.*f 数量: %.4f 订单ID: %d",
				oe.exchange.GetName(), orderTypeDesc, req.Side, req.PriceDecimals, req.Price, req.Quantity, exchangeOrder.OrderID)
			return order, nil
		}

		lastErr = err

		// 判断错误类型
		errStr := err.Error()
		if strings.Contains(errStr, "-4061") {
			// 持仓模式不匹配：双向持仓 vs 单向持仓
			logger.Fatalf("❌ 下单失败，请在交易所将双向持仓改为单向持仓。错误码: -4061")
			return nil, fmt.Errorf("持仓模式不匹配: %w", err)
		} else if strings.Contains(errStr, "-1003") || strings.Contains(errStr, "rate limit") {
			// 速率限制，等待后重试
			pm.RecordAPIRateLimitHit(exchangeName)
			logger.Warn("⚠️ 触发速率限制，等待后重试...")
			time.Sleep(oe.rateLimitRetryDelay)
			continue
		} else if isPostOnlyError(err) && !degraded {
			// 🔥 PostOnly错误：价格会立即成交，记录失败次数(必须放在其他检查之前!)
			postOnlyFailCount++
			logger.Warn("⚠️ [%s] PostOnly被拒(%d/3): %s %.2f, 等待500ms后重试",
				oe.exchange.GetName(), postOnlyFailCount, req.Side, req.Price)

			// 如果还没达到3次，继续重试PostOnly
			if postOnlyFailCount < 3 {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			// 达到3次后，下一轮循环会触发降级
			time.Sleep(500 * time.Millisecond)
			continue
		} else if strings.Contains(errStr, "-4061") {
			// 持仓模式不匹配（已在前面处理，这里保留以防万一）
			return nil, err
		} else if strings.Contains(errStr, "-2019") || strings.Contains(errStr, "保证金不足") || strings.Contains(errStr, "insufficient") {
			// 保证金不足，不重试
			return nil, err
		} else if strings.Contains(errStr, "-4164") || strings.Contains(errStr, "Order's notional must be no smaller than 100") {
			// 🔥 币安合约最小订单金额不足：订单名义价值必须 >= 100 USDT（除非是reduce only订单）
			// 这是配置问题，重试无效，直接返回错误
			logger.Error("❌ [%s] 订单金额不足：币安合约要求订单名义价值 >= 100 USDT（除非是reduce only订单）。订单金额=%.2f × %.8f = %.2f USDT",
				oe.exchange.GetName(), req.Price, req.Quantity, req.Price*req.Quantity)
			return nil, fmt.Errorf("订单金额不足（币安合约最小订单金额为100 USDT）: %w", err)
		} else if strings.Contains(errStr, "-1021") {
			// 时间戳不同步，不重试
			return nil, err
		} else if isReduceOnlyError(err) {
			// 🔥 ReduceOnly订单被拒绝：无持仓时尝试减仓，不重试
			logger.Warn("⚠️ [%s] ReduceOnly订单被拒绝（无持仓）: %s %.2f",
				oe.exchange.GetName(), req.Side, req.Price)
			return nil, fmt.Errorf("ReduceOnly订单被拒绝（无持仓）: %w", err)
		}

		// 其他错误，短暂等待后重试
		if i < maxRetries {
			time.Sleep(oe.orderRetryDelay)
		}
	}

	// 记录失败指标
	pm.RecordOrderFailure(exchangeName, req.Symbol, req.Side, "max_retries_exceeded")
	return nil, fmt.Errorf("下单失败（重试%d次）: %w", maxRetries, lastErr)
}

// BatchPlaceOrdersResult 批量下单结果
type BatchPlaceOrdersResult struct {
	PlacedOrders     []*Order        // 成功下单的订单列表
	HasMarginError   bool            // 是否出现保证金不足错误
	ReduceOnlyErrors map[string]bool // ReduceOnly错误的订单（key为ClientOrderID）
}

// BatchPlaceOrders 批量下单
// 返回：成功下单的订单列表、是否出现保证金不足错误、ReduceOnly错误的订单
func (oe *ExchangeOrderExecutor) BatchPlaceOrders(orders []*OrderRequest) ([]*Order, bool) {
	result := oe.BatchPlaceOrdersWithDetails(orders)
	return result.PlacedOrders, result.HasMarginError
}

// BatchPlaceOrdersWithDetails 批量下单（返回详细结果）
func (oe *ExchangeOrderExecutor) BatchPlaceOrdersWithDetails(orders []*OrderRequest) *BatchPlaceOrdersResult {
	result := &BatchPlaceOrdersResult{
		PlacedOrders:     make([]*Order, 0, len(orders)),
		HasMarginError:   false,
		ReduceOnlyErrors: make(map[string]bool),
	}

	for _, orderReq := range orders {
		order, err := oe.PlaceOrder(orderReq)
		if err != nil {
			logger.Warn("⚠️ [%s] 下单失败 %.2f %s: %v",
				oe.exchange.GetName(), orderReq.Price, orderReq.Side, err)

			// 检查错误类型
			errStr := err.Error()
			if strings.Contains(errStr, "保证金不足") || strings.Contains(errStr, "-2019") || strings.Contains(errStr, "insufficient") {
				result.HasMarginError = true
				logger.Error("❌ [保证金不足] 订单 %.2f %s 因保证金不足失败", orderReq.Price, orderReq.Side)
			} else if isReduceOnlyError(err) {
				// 记录 ReduceOnly 错误
				result.ReduceOnlyErrors[orderReq.ClientOrderID] = true
				logger.Error("❌ [ReduceOnly错误] 订单 %.2f %s 无持仓，需要清空槽位", orderReq.Price, orderReq.Side)
			}
			continue
		}
		result.PlacedOrders = append(result.PlacedOrders, order)
	}

	return result
}

// CancelOrder 取消订单
func (oe *ExchangeOrderExecutor) CancelOrder(orderID int64) error {
	exchangeName := oe.exchange.GetName()

	// 分布式锁：防止多实例同时取消同一订单
	lockKey := fmt.Sprintf("cancel:%s:%d", exchangeName, orderID)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	acquired, err := oe.lock.TryLock(ctx, lockKey, 3*time.Second)
	if err != nil {
		logger.Warn("⚠️ [%s] 获取取消锁失败: %v", exchangeName, err)
		// 锁获取失败不阻塞，继续执行（降级策略）
	} else if !acquired {
		logger.Debug("🔒 [%s] 订单 %d 正在被其他实例取消，跳过", exchangeName, orderID)
		return nil // 跳过，不是错误
	} else {
		// 成功获取锁，defer 释放
		defer func() {
			if unlockErr := oe.lock.Unlock(ctx, lockKey); unlockErr != nil {
				logger.Warn("⚠️ [%s] 释放取消锁失败: %v", exchangeName, unlockErr)
			}
		}()
	}

	// 限流
	if err := oe.rateLimiter.Wait(context.Background()); err != nil {
		return fmt.Errorf("速率限制等待失败: %v", err)
	}

	err = oe.exchange.CancelOrder(context.Background(), oe.symbol, orderID)
	if err != nil {
		// 如果是"Unknown order"错误，说明订单已经不存在（可能已成交或已取消），不算错误
		errStr := err.Error()
		if strings.Contains(errStr, "-2011") || strings.Contains(errStr, "Unknown order") || strings.Contains(errStr, "does not exist") {
			logger.Info("ℹ️ [%s] 订单 %d 已不存在（可能已成交或已取消），跳过取消", oe.exchange.GetName(), orderID)
			return nil
		}
		return fmt.Errorf("取消订单失败: %v", err)
	}

	logger.Info("✅ [%s] 取消订单成功: %d", oe.exchange.GetName(), orderID)
	return nil
}

// BatchCancelOrders 批量撤单
func (oe *ExchangeOrderExecutor) BatchCancelOrders(orderIDs []int64) error {
	if len(orderIDs) == 0 {
		return nil
	}

	// 使用交易所的批量撤单接口
	err := oe.exchange.BatchCancelOrders(context.Background(), oe.symbol, orderIDs)
	if err != nil {
		logger.Warn("⚠️ [%s] 批量撤单失败: %v，尝试单个撤单", oe.exchange.GetName(), err)
		// 如果批量撤单失败，尝试单个撤单
		for _, orderID := range orderIDs {
			if err := oe.CancelOrder(orderID); err != nil {
				logger.Warn("⚠️ [%s] 取消订单 %d 失败: %v", oe.exchange.GetName(), orderID, err)
			}
		}
	}

	return nil
}

// CheckOrderStatus 检查订单状态
func (oe *ExchangeOrderExecutor) CheckOrderStatus(orderID int64) (string, float64, error) {
	order, err := oe.exchange.GetOrder(context.Background(), oe.symbol, orderID)
	if err != nil {
		return "", 0, err
	}

	return string(order.Status), order.ExecutedQty, nil
}

// GetOpenOrders 获取未完成订单
func (oe *ExchangeOrderExecutor) GetOpenOrders() ([]interface{}, error) {
	orders, err := oe.exchange.GetOpenOrders(context.Background(), oe.symbol)
	if err != nil {
		return nil, err
	}

	// 转换为 interface{} 列表（为了兼容现有代码）
	result := make([]interface{}, len(orders))
	for i, order := range orders {
		result[i] = order
	}

	return result, nil
}

// GetQuantityDecimals 获取数量精度（小数位数）
func (oe *ExchangeOrderExecutor) GetQuantityDecimals() int {
	return oe.exchange.GetQuantityDecimals()
}

// RoundQuantity 将数量按交易所精度向下取整
func (oe *ExchangeOrderExecutor) RoundQuantity(quantity float64) float64 {
	qDec := oe.exchange.GetQuantityDecimals()
	multiplier := math.Pow(10, float64(qDec))
	return math.Floor(quantity*multiplier) / multiplier
}

// EstimateFinalOrderAmount 预估最终下单金额
// 交易所可能因最小名义金额、精度对齐等原因调整数量，导致实际金额与原始金额不同
// 此方法用于资金分配器在下单前准确预留资金
func (oe *ExchangeOrderExecutor) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return oe.exchange.EstimateFinalOrderAmount(symbol, price, quantity, reduceOnly)
}

// GetSymbol 获取当前交易对
func (oe *ExchangeOrderExecutor) GetSymbol() string {
	return oe.symbol
}
