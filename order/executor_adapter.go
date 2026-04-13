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

// OrderRequest 订單请求
type OrderRequest struct {
	Symbol        string
	Side          string
	Price         float64
	Quantity      float64
	PriceDecimals int    // 價格小數位數（用於格式化價格字符串）
	ReduceOnly    bool   // 是否只减倉（平倉單）
	PostOnly      bool   // 是否只做 Maker（Post Only）
	ClientOrderID string // 自定义订單ID
	StrategyName  string // 策略名称（可選，用於日志追踪）
	StrategyType  string // 策略類型（可選，如 "grid", "dca", "martingale"）
	OrderSource   string // 订單來源（"normal"=正常限價, "stop_loss"=止損平倉, "liquidation"=強制平倉）
}

// Order 订單信息
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

// ExchangeOrderExecutor 基於 exchange.IExchange 的订單執行器
type ExchangeOrderExecutor struct {
	exchange    exchange.IExchange
	symbol      string
	botID       string // 寫入 SQLite 日誌時附帶 logs.bot_id（空則不注入）
	rateLimiter *rate.Limiter
	lock        lock.DistributedLock // 分布式鎖

	// 時间配置
	rateLimitRetryDelay time.Duration
	orderRetryDelay     time.Duration
}

// NewExchangeOrderExecutor 創建基於交易所接口的订單執行器
func NewExchangeOrderExecutor(ex exchange.IExchange, symbol string, rateLimitRetryDelay, orderRetryDelay int, distributedLock lock.DistributedLock, botID string) *ExchangeOrderExecutor {
	return &ExchangeOrderExecutor{
		exchange:            ex,
		symbol:              symbol,
		botID:               strings.TrimSpace(botID),
		rateLimiter:         rate.NewLimiter(rate.Limit(25), 30), // 25單/秒，突发30
		lock:                distributedLock,
		rateLimitRetryDelay: time.Duration(rateLimitRetryDelay) * time.Second,
		orderRetryDelay:     time.Duration(orderRetryDelay) * time.Millisecond,
	}
}

func (oe *ExchangeOrderExecutor) logCtx() context.Context {
	if oe.botID == "" {
		return context.Background()
	}
	return logger.WithBotID(context.Background(), oe.botID)
}

// isPostOnlyError 检查是否為PostOnly錯误
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

// isReduceOnlyError 检查是否為ReduceOnly錯误（無持倉時尝試减倉）
func isReduceOnlyError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Binance: code=-2022, msg=ReduceOnly Order is rejected
	// 注意：不要直接匹配 "reduce only"，因為金額不足的报錯 "-4164" 里也包含這個词
	return strings.Contains(errStr, "-2022") ||
		strings.Contains(errStr, "ReduceOnly Order is rejected") ||
		(strings.Contains(errStr, "reduce only") && !strings.Contains(errStr, "-4164"))
}

// PlaceOrder 下單（带重試）
func (oe *ExchangeOrderExecutor) PlaceOrder(req *OrderRequest) (*Order, error) {
	startTime := time.Now()
	pm := metrics.GetPrometheusMetrics()
	exchangeName := oe.exchange.GetName()

	// 分布式鎖：防止多實例對同一價格位重複下單
	// 使用價格区间鎖（中粒度）：每10個價格間隔一個鎖
	priceLevel := math.Floor(req.Price/10) * 10
	lockKey := fmt.Sprintf("order:%s:%s:%.0f", exchangeName, req.Symbol, priceLevel)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	acquired, err := oe.lock.TryLock(ctx, lockKey, 5*time.Second)
	if err != nil {
		logger.WarnCtx(oe.logCtx(), "⚠️ [%s] 獲取鎖失败: %v", exchangeName, err)
		// 鎖獲取失败不阻塞，继续執行（降级策略）
	} else if !acquired {
		logger.DebugCtx(oe.logCtx(), "🔒 [%s] 價格位 %.2f 已被其他實例鎖定，跳過", exchangeName, req.Price)
		return nil, nil // 回傳 nil 表示跳過，不是錯误
	} else {
		// 成功獲取鎖，defer 释放
		defer func() {
			if unlockErr := oe.lock.Unlock(ctx, lockKey); unlockErr != nil {
				logger.WarnCtx(oe.logCtx(), "⚠️ [%s] 释放鎖失败: %v", exchangeName, unlockErr)
			}
		}()
	}

	// 限流
	if err := oe.rateLimiter.Wait(context.Background()); err != nil {
		return nil, fmt.Errorf("速率限制等待失败: %v", err)
	}

	maxRetries := 5 // 增加重試次數:3次PostOnly + 1次降级 + 1次保險
	var lastErr error
	postOnlyFailCount := 0
	degraded := false // 是否已降级為普通單

	for i := 0; i <= maxRetries; i++ {
		// 轉换為通用订單请求
		exchangeReq := &exchange.OrderRequest{
			Symbol:        req.Symbol,
			Side:          exchange.Side(req.Side),
			Type:          exchange.OrderTypeLimit,
			TimeInForce:   exchange.TimeInForceGTC,
			Quantity:      req.Quantity,
			Price:         req.Price,
			PriceDecimals: req.PriceDecimals,
			ReduceOnly:    req.ReduceOnly,
			PostOnly:      req.PostOnly && !degraded, // 如果已降级，强制為普通單
			ClientOrderID: req.ClientOrderID,         // 傳遞自定义订單ID
			StrategyName:  req.StrategyName,          // 傳遞策略名称
			StrategyType:  req.StrategyType,          // 傳遞策略類型
		}

		// 🔥 如果PostOnly已失败3次，降级為普通限價單
		if postOnlyFailCount >= 3 && req.PostOnly && !degraded {
			degraded = true
			logger.WarnCtx(oe.logCtx(), "⚠️ [%s] PostOnly已失败3次，降级為普通限價單: %s %.2f",
				oe.exchange.GetName(), req.Side, req.Price)
			exchangeReq.PostOnly = false
		}

		// 呼叫交易所接口
		exchangeOrder, err := oe.exchange.PlaceOrder(context.Background(), exchangeReq)
		if err == nil {
			// 轉换回 Order 格式
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

			// 記錄 Prometheus 指標
			duration := time.Since(startTime)
			pm.RecordOrder(exchangeName, req.Symbol, req.Side, string(exchangeOrder.Status))
			pm.RecordOrderSuccess(exchangeName, req.Symbol, req.Side, duration)

			// 根據實際使用的订單類型显示日志
			orderTypeDesc := "PostOnly"
			if !exchangeReq.PostOnly {
				orderTypeDesc = "普通單(PostOnly降级)"
			}
			logger.InfoCtx(oe.logCtx(), "✅ [%s] 下單成功(%s): %s %.*f 數量: %.4f 订單ID: %d",
				oe.exchange.GetName(), orderTypeDesc, req.Side, req.PriceDecimals, req.Price, req.Quantity, exchangeOrder.OrderID)
			return order, nil
		}

		lastErr = err

		// 判断錯误類型
		errStr := err.Error()
		if strings.Contains(errStr, "-4061") {
			// 持倉模式不匹配：双向持倉 vs 單向持倉。不退出進程，僅記錄錯誤並返回，由上層決定是否重試或告警
			logger.ErrorCtx(oe.logCtx(), "❌ 下單失败，请在交易所將双向持倉改為單向持倉。錯误碼: -4061（進程繼續運行，請手動修改後重試）")
			return nil, fmt.Errorf("持倉模式不匹配: %w", err)
		} else if strings.Contains(errStr, "-1003") || strings.Contains(errStr, "rate limit") {
			// 速率限制，等待后重試
			pm.RecordAPIRateLimitHit(exchangeName)
			logger.WarnCtx(oe.logCtx(), "⚠️ 触发速率限制，等待后重試...")
			time.Sleep(oe.rateLimitRetryDelay)
			continue
		} else if isPostOnlyError(err) && !degraded {
			// 🔥 PostOnly錯误：價格會立即成交，記錄失败次數(必須放在其他检查之前!)
			postOnlyFailCount++
			logger.WarnCtx(oe.logCtx(), "⚠️ [%s] PostOnly被拒(%d/3): %s %.2f, 等待500ms后重試",
				oe.exchange.GetName(), postOnlyFailCount, req.Side, req.Price)

			// 如果还没达到3次，继续重試PostOnly
			if postOnlyFailCount < 3 {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			// 达到3次后，下一輪循环會触发降级
			time.Sleep(500 * time.Millisecond)
			continue
		} else if strings.Contains(errStr, "-2019") || strings.Contains(errStr, "保证金不足") || strings.Contains(errStr, "insufficient") {
			// 保证金不足，不重試
			return nil, err
		} else if strings.Contains(errStr, "-4164") || strings.Contains(errStr, "Order's notional must be no smaller than 100") {
			// 🔥 币安合約最小訂單金額不足：订單名义價值必須 >= 100 USDT（除非是reduce only订單）
			// 这是配置问题，重試無效，直接返回錯误
			logger.ErrorCtx(oe.logCtx(), "❌ [%s] 订單金額不足：币安合約要求订單名义價值 >= 100 USDT（除非是reduce only订單）。订單金額=%.2f × %.8f = %.2f USDT",
				oe.exchange.GetName(), req.Price, req.Quantity, req.Price*req.Quantity)
			return nil, fmt.Errorf("订單金額不足（币安合約最小訂單金額為100 USDT）: %w", err)
		} else if strings.Contains(errStr, "-1021") {
			// 時间戳不同步，不重試
			return nil, err
		} else if isReduceOnlyError(err) {
			// 🔥 ReduceOnly订單被拒绝：無持倉時尝試减倉，不重試
			logger.WarnCtx(oe.logCtx(), "⚠️ [%s] ReduceOnly订單被拒绝（無持倉）: %s %.2f",
				oe.exchange.GetName(), req.Side, req.Price)
			return nil, fmt.Errorf("ReduceOnly订單被拒绝（無持倉）: %w", err)
		}

		// 其他錯误，短暂等待后重試
		if i < maxRetries {
			time.Sleep(oe.orderRetryDelay)
		}
	}

	// 記錄失败指標
	pm.RecordOrderFailure(exchangeName, req.Symbol, req.Side, "max_retries_exceeded")
	return nil, fmt.Errorf("下單失败（重試%d次）: %w", maxRetries, lastErr)
}

// BatchPlaceOrdersResult 批量下單結果
type BatchPlaceOrdersResult struct {
	PlacedOrders     []*Order        // 成功下單的订單列表
	HasMarginError   bool            // 是否出現保证金不足錯误
	ReduceOnlyErrors map[string]bool // ReduceOnly錯误的订單（key為ClientOrderID）
}

// BatchPlaceOrders 批量下單
// 回傳：成功下單的订單列表、是否出現保证金不足錯误、ReduceOnly錯误的订單
func (oe *ExchangeOrderExecutor) BatchPlaceOrders(orders []*OrderRequest) ([]*Order, bool) {
	result := oe.BatchPlaceOrdersWithDetails(orders)
	return result.PlacedOrders, result.HasMarginError
}

// BatchPlaceOrdersWithDetails 批量下單（返回详细結果）
func (oe *ExchangeOrderExecutor) BatchPlaceOrdersWithDetails(orders []*OrderRequest) *BatchPlaceOrdersResult {
	result := &BatchPlaceOrdersResult{
		PlacedOrders:     make([]*Order, 0, len(orders)),
		HasMarginError:   false,
		ReduceOnlyErrors: make(map[string]bool),
	}

	for _, orderReq := range orders {
		order, err := oe.PlaceOrder(orderReq)
		if err != nil {
			notionalUSDT := orderReq.Price * orderReq.Quantity
			logger.ErrorCtx(oe.logCtx(), "❌ [%s] %s 下單失败 price=%.*f side=%s qty=%.8f 名义≈%.2f USDT: %v",
				oe.exchange.GetName(), orderReq.Symbol, orderReq.PriceDecimals, orderReq.Price, orderReq.Side, orderReq.Quantity, notionalUSDT, err)

			// 检查錯误類型
			errStr := err.Error()
			if strings.Contains(errStr, "保证金不足") || strings.Contains(errStr, "-2019") || strings.Contains(errStr, "insufficient") {
				result.HasMarginError = true
				logger.ErrorCtx(oe.logCtx(), "❌ [保证金不足] 订單 price=%.*f side=%s qty=%.8f 名义≈%.2f USDT 因保证金不足失败",
					orderReq.PriceDecimals, orderReq.Price, orderReq.Side, orderReq.Quantity, notionalUSDT)
			} else if isReduceOnlyError(err) {
				// 記錄 ReduceOnly 錯误（系統會自動清空槽位，降級為 WARN 減少告警噪音）
				result.ReduceOnlyErrors[orderReq.ClientOrderID] = true
				logger.WarnCtx(oe.logCtx(), "⚠️ [ReduceOnly] 订單 %.2f %s 無持倉，將清空槽位", orderReq.Price, orderReq.Side)
			}
			continue
		}
		result.PlacedOrders = append(result.PlacedOrders, order)
	}

	return result
}

// CancelOrder 取消訂單
func (oe *ExchangeOrderExecutor) CancelOrder(orderID int64) error {
	exchangeName := oe.exchange.GetName()

	// 分布式鎖：防止多實例同時取消同一订單
	lockKey := fmt.Sprintf("cancel:%s:%d", exchangeName, orderID)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	acquired, err := oe.lock.TryLock(ctx, lockKey, 3*time.Second)
	if err != nil {
		logger.WarnCtx(oe.logCtx(), "⚠️ [%s] 獲取取消鎖失败: %v", exchangeName, err)
		// 鎖獲取失败不阻塞，继续執行（降级策略）
	} else if !acquired {
		logger.DebugCtx(oe.logCtx(), "🔒 [%s] 订單 %d 正在被其他實例取消，跳過", exchangeName, orderID)
		return nil // 跳過，不是錯误
	} else {
		// 成功獲取鎖，defer 释放
		defer func() {
			if unlockErr := oe.lock.Unlock(ctx, lockKey); unlockErr != nil {
				logger.WarnCtx(oe.logCtx(), "⚠️ [%s] 释放取消鎖失败: %v", exchangeName, unlockErr)
			}
		}()
	}

	// 限流
	if err := oe.rateLimiter.Wait(context.Background()); err != nil {
		return fmt.Errorf("速率限制等待失败: %v", err)
	}

	err = oe.exchange.CancelOrder(context.Background(), oe.symbol, orderID)
	if err != nil {
		// 如果是"Unknown order"錯误，說明订單已經不存在（可能已成交或已取消），不算錯误
		errStr := err.Error()
		if strings.Contains(errStr, "-2011") || strings.Contains(errStr, "Unknown order") || strings.Contains(errStr, "does not exist") {
			logger.InfoCtx(oe.logCtx(), "ℹ️ [%s] 订單 %d 已不存在（可能已成交或已取消），跳過取消", oe.exchange.GetName(), orderID)
			return nil
		}
		return fmt.Errorf("取消訂單失败: %v", err)
	}

	logger.InfoCtx(oe.logCtx(), "✅ [%s] 取消訂單成功: %d", oe.exchange.GetName(), orderID)
	return nil
}

// BatchCancelOrders 批量撤單
func (oe *ExchangeOrderExecutor) BatchCancelOrders(orderIDs []int64) error {
	if len(orderIDs) == 0 {
		return nil
	}

	// 使用交易所的批量撤單接口
	err := oe.exchange.BatchCancelOrders(context.Background(), oe.symbol, orderIDs)
	if err != nil {
		logger.WarnCtx(oe.logCtx(), "⚠️ [%s] 批量撤單失败: %v，尝試單個撤單", oe.exchange.GetName(), err)
		// 如果批量撤單失败，尝試單個撤單
		for _, orderID := range orderIDs {
			if err := oe.CancelOrder(orderID); err != nil {
				logger.WarnCtx(oe.logCtx(), "⚠️ [%s] 取消訂單 %d 失败: %v", oe.exchange.GetName(), orderID, err)
			}
		}
	}

	return nil
}

// CheckOrderStatus 检查订單状態
func (oe *ExchangeOrderExecutor) CheckOrderStatus(orderID int64) (string, float64, error) {
	order, err := oe.exchange.GetOrder(context.Background(), oe.symbol, orderID)
	if err != nil {
		return "", 0, err
	}

	return string(order.Status), order.ExecutedQty, nil
}

// GetOpenOrders 獲取未完成订單
func (oe *ExchangeOrderExecutor) GetOpenOrders() ([]interface{}, error) {
	orders, err := oe.exchange.GetOpenOrders(context.Background(), oe.symbol)
	if err != nil {
		return nil, err
	}

	// 轉换為 interface{} 列表（為了兼容現有代碼）
	result := make([]interface{}, len(orders))
	for i, order := range orders {
		result[i] = order
	}

	return result, nil
}

// GetQuantityDecimals 獲取數量精度（小數位數）
func (oe *ExchangeOrderExecutor) GetQuantityDecimals() int {
	return oe.exchange.GetQuantityDecimals()
}

// RoundQuantity 將數量按交易所精度向下取整
func (oe *ExchangeOrderExecutor) RoundQuantity(quantity float64) float64 {
	qDec := oe.exchange.GetQuantityDecimals()
	multiplier := math.Pow(10, float64(qDec))
	return math.Floor(quantity*multiplier) / multiplier
}

// EstimateFinalOrderAmount 預估最终下單金額
// 交易所可能因最小名义金額、精度對齐等原因調整數量，導致實際金額與原始金額不同
// 此方法用於资金分配器在下單前准确預留资金
func (oe *ExchangeOrderExecutor) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return oe.exchange.EstimateFinalOrderAmount(symbol, price, quantity, reduceOnly)
}

// GetSymbol 獲取當前交易對
func (oe *ExchangeOrderExecutor) GetSymbol() string {
	return oe.symbol
}
