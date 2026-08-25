package position

import (
	"context"
	"fmt"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
)

// CloseMethod 平仓方式
type CloseMethod string

const (
	CloseMethodMarket CloseMethod = "market" // 市价平仓
	CloseMethodLimit  CloseMethod = "limit"  // 限价平仓
)

// ClosePositionStatus 平仓状态常量
const (
	CloseStatusPending  = "PENDING"   // 等待成交
	CloseStatusFilled   = "FILLED"    // 已成交
	CloseStatusTimeout  = "TIMEOUT"   // 已超时
	CloseStatusFailed   = "FAILED"    // 失败
	CloseStatusCanceled = "CANCELED"  // 已取消
)

// ClosePositionRecord 平仓记录（用于状态追踪）
type ClosePositionRecord struct {
	RecordID     string       `json:"record_id"`      // 记录唯一ID
	BotID        string       `json:"bot_id"`         // 所属Bot
	Symbol       string       `json:"symbol"`         // 交易对
	Side         string       `json:"side"`           // 方向：BUY/SELL
	TargetQty    float64      `json:"target_qty"`     // 目标平仓数量
	FilledQty    float64      `json:"filled_qty"`     // 已平仓数量
	Method       CloseMethod  `json:"method"`         // 平仓方式
	Price        float64      `json:"price"`          // 限价（仅限价单）
	OrderID      int64        `json:"order_id"`       // 订单ID
	Status       string       `json:"status"`         // 状态：PENDING/FILLED/TIMEOUT/FAILED
	CreatedAt    time.Time    `json:"created_at"`     // 创建时间
	UpdatedAt    time.Time    `json:"updated_at"`     // 更新时间
	TimeoutAt    time.Time    `json:"timeout_at"`     // 超时时间
	RetryCount   int          `json:"retry_count"`    // 重试次数
	ErrorMessage string       `json:"error_message"`  // 错误信息
	mu           sync.RWMutex `json:"-"`              // 内部锁
}

// ClosePositionManager 平仓管理器
type ClosePositionManager struct {
	exchange ExchangeWrapper
	botID    string
	symbol   string

	// 平仓记录存储
	records map[string]*ClosePositionRecord
	recordsMu sync.RWMutex

	// 超时检查
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// ExchangeWrapper 交易所包装接口（简化版，避免循环依赖）
type ExchangeWrapper interface {
	GetName() string
	PlaceOrder(ctx context.Context, req *ExchangeOrderRequest) (*ExchangeOrder, error)
	GetOrder(ctx context.Context, symbol string, orderID int64) (*ExchangeOrder, error)
	CancelOrder(ctx context.Context, symbol string, orderID int64) error
	GetLatestPrice(ctx context.Context, symbol string) (float64, error)
}

// ExchangeOrderRequest 交易所订单请求
type ExchangeOrderRequest struct {
	Symbol        string
	Side          string
	Type          string
	Quantity      float64
	Price         float64
	ReduceOnly    bool
	PostOnly      bool   // 限价单时使用，获取 Maker 手续费
	TimeInForce   string
	PriceDecimals int
}

// ExchangeOrder 交易所订单响应
type ExchangeOrder struct {
	OrderID     int64
	Status      string
	ExecutedQty float64
}

// NewClosePositionManager 创建平仓管理器
func NewClosePositionManager(exchange ExchangeWrapper, botID, symbol string) *ClosePositionManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &ClosePositionManager{
		exchange: exchange,
		botID:    botID,
		symbol:   symbol,
		records:  make(map[string]*ClosePositionRecord),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// ClosePositions 平仓（支持市价/限价）
func (cpm *ClosePositionManager) ClosePositions(
	ctx context.Context,
	side string,
	quantity float64,
	cfg config.ClosePositionConfig,
) (*ClosePositionRecord, error) {
	record := &ClosePositionRecord{
		RecordID:  generateRecordID(),
		BotID:     cpm.botID,
		Symbol:    cpm.symbol,
		Side:      side,
		TargetQty: quantity,
		Method:    CloseMethod(cfg.Method),
		Status:    CloseStatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if cfg.TimeoutSec > 0 {
		record.TimeoutAt = record.CreatedAt.Add(time.Duration(cfg.TimeoutSec) * time.Second)
	}

	// 构建订单请求
	orderReq := &ExchangeOrderRequest{
		Symbol:        cpm.symbol,
		Side:          side,
		Quantity:      quantity,
		ReduceOnly:    true,
		PriceDecimals: 2, // 默认精度
	}

	if CloseMethod(cfg.Method) == CloseMethodMarket {
		orderReq.Type = "MARKET"
	} else {
		// 限价单：根据当前价计算限价
		currentPrice, err := cpm.exchange.GetLatestPrice(ctx, cpm.symbol)
		if err != nil {
			record.Status = CloseStatusFailed
			record.ErrorMessage = fmt.Sprintf("获取价格失败: %v", err)
			record.UpdatedAt = time.Now()
			return record, err
		}
		price := calculateLimitPrice(currentPrice, side, cfg.PriceOffset)
		orderReq.Type = "LIMIT"
		orderReq.Price = price
		orderReq.TimeInForce = "GTC"
		orderReq.PostOnly = true // 限价平仓使用 PostOnly 获取 Maker 手续费
		record.Price = price
	}

	// 下单
	order, err := cpm.exchange.PlaceOrder(ctx, orderReq)
	if err != nil {
		record.Status = CloseStatusFailed
		record.ErrorMessage = err.Error()
		record.UpdatedAt = time.Now()
		cpm.saveRecord(record)
		return record, err
	}

	record.OrderID = order.OrderID

	// 检查订单状态（立即成交的情况）
	if order.Status == "FILLED" {
		record.Status = CloseStatusFilled
		record.FilledQty = order.ExecutedQty
		record.UpdatedAt = time.Now()
	} else {
		record.Status = CloseStatusPending
	}

	// 保存记录
	cpm.saveRecord(record)

	// 启动超时检查（仅限价单且设置了超时）
	if cfg.TimeoutSec > 0 && CloseMethod(cfg.Method) == CloseMethodLimit {
		cpm.wg.Add(1)
		go cpm.watchTimeout(record, cfg)
	}

	logger.Info("📤 [平仓] 已下单: %s %s %.4f 方法:%s 订单ID:%d",
		cpm.symbol, side, quantity, cfg.Method, order.OrderID)

	return record, nil
}

// watchTimeout 监控超时
func (cpm *ClosePositionManager) watchTimeout(
	record *ClosePositionRecord,
	cfg config.ClosePositionConfig,
) {
	defer cpm.wg.Done()

	for {
		select {
		case <-cpm.ctx.Done():
			return
		case <-time.After(time.Until(record.TimeoutAt)):
			record.mu.Lock()
			if record.Status != CloseStatusPending {
				record.mu.Unlock()
				return
			}

			// 检查订单状态
			order, err := cpm.exchange.GetOrder(context.Background(), cpm.symbol, record.OrderID)
			if err == nil && (order.Status == "FILLED" || order.Status == "PARTIALLY_FILLED") {
				record.FilledQty = order.ExecutedQty
				if order.Status == "FILLED" {
					record.Status = CloseStatusFilled
				}
				record.UpdatedAt = time.Now()
				record.mu.Unlock()
				return
			}

			// 确实超时
			if cfg.AutoRetry && record.Method == CloseMethodLimit && record.RetryCount < cfg.MaxRetries {
				// 取消原订单
				_ = cpm.exchange.CancelOrder(context.Background(), cpm.symbol, record.OrderID)

				// 重试为市价单
				record.Method = CloseMethodMarket
				record.RetryCount++
				record.UpdatedAt = time.Now()
				// 在鎖內取出重试所需的字段：UpdateRecord 會併發改寫 FilledQty，
				// 放到 goroutine 裡再讀就是數據競態，可能按錯誤的剩餘量下單
				remainingQty := record.TargetQty - record.FilledQty
				side := record.Side
				record.mu.Unlock()

				go func() {
					if remainingQty > 0 {
						_, err := cpm.ClosePositions(context.Background(), side,
							remainingQty, config.ClosePositionConfig{
								Method:     string(CloseMethodMarket),
								TimeoutSec: 0, // 重试的市价单不设置超时
							})
						if err != nil {
							logger.Warn("⚠️ [平仓] 重试失败: %v", err)
						}
					}
				}()

				return
			}

			record.Status = CloseStatusTimeout
			record.UpdatedAt = time.Now()
			record.mu.Unlock()

			logger.Warn("⏰ [平仓] 订单超时: RecordID=%s OrderID=%d",
				record.RecordID, record.OrderID)
			return
		}
	}
}

// saveRecord 保存记录（内部方法）
func (cpm *ClosePositionManager) saveRecord(record *ClosePositionRecord) {
	cpm.recordsMu.Lock()
	defer cpm.recordsMu.Unlock()
	cpm.records[record.RecordID] = record
}

// GetRecord 获取平仓记录
func (cpm *ClosePositionManager) GetRecord(recordID string) (*ClosePositionRecord, bool) {
	cpm.recordsMu.RLock()
	defer cpm.recordsMu.RUnlock()
	record, ok := cpm.records[recordID]
	return record, ok
}

// ListRecords 获取所有平仓记录
func (cpm *ClosePositionManager) ListRecords() []*ClosePositionRecord {
	cpm.recordsMu.RLock()
	defer cpm.recordsMu.RUnlock()

	records := make([]*ClosePositionRecord, 0, len(cpm.records))
	for _, r := range cpm.records {
		records = append(records, r)
	}
	return records
}

// UpdateRecord 更新记录（用于外部订单更新回调）
func (cpm *ClosePositionManager) UpdateRecord(orderID int64, status string, executedQty float64) {
	cpm.recordsMu.RLock()
	defer cpm.recordsMu.RUnlock()

	for _, record := range cpm.records {
		record.mu.Lock()
		if record.OrderID == orderID && record.Status == CloseStatusPending {
			record.Status = status
			record.FilledQty = executedQty
			record.UpdatedAt = time.Now()
			record.mu.Unlock()
			break
		}
		record.mu.Unlock()
	}
}

// Stop 停止管理器
func (cpm *ClosePositionManager) Stop() {
	cpm.cancel()
	cpm.wg.Wait()
}

// calculateLimitPrice 计算限价
func calculateLimitPrice(currentPrice float64, side string, offsetPercent float64) float64 {
	// offsetPercent: 负数=更激进（卖低价/买高价），正数=更保守
	if side == "SELL" {
		return currentPrice * (1 + offsetPercent/100)
	}
	return currentPrice * (1 - offsetPercent/100)
}

func generateRecordID() string {
	return fmt.Sprintf("close_%d", time.Now().UnixNano())
}
