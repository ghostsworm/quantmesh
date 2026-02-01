package exchange

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/logger"
)

// DryRunWrapper 模拟运行包装器
// 包装真实的交易所适配器，拦截所有会影响账户的操作（下单、撤单等）
// 但保留所有只读操作（获取价格、持仓、余额等）
type DryRunWrapper struct {
	wrapped IExchange

	// 模拟订单 ID 生成器
	orderIDCounter int64

	// 模拟订单存储
	mu              sync.RWMutex
	simulatedOrders map[int64]*Order
}

// NewDryRunWrapper 创建 Dry Run 包装器
func NewDryRunWrapper(ex IExchange) *DryRunWrapper {
	logger.Info("🔒 [DryRun] 已启用模拟运行模式，交易所: %s（不会实际下单）", ex.GetName())
	return &DryRunWrapper{
		wrapped:         ex,
		orderIDCounter:  1000000, // 从一个大数字开始，避免与真实订单 ID 冲突
		simulatedOrders: make(map[int64]*Order),
	}
}

// GetName 获取交易所名称（添加 DryRun 标识）
func (d *DryRunWrapper) GetName() string {
	return d.wrapped.GetName() + " [DryRun]"
}

// PlaceOrder 模拟下单（不实际发送到交易所）
func (d *DryRunWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	// 生成模拟订单 ID
	orderID := atomic.AddInt64(&d.orderIDCounter, 1)

	// 创建模拟订单
	order := &Order{
		OrderID:       orderID,
		ClientOrderID: req.ClientOrderID,
		Symbol:        req.Symbol,
		Side:          req.Side,
		Type:          req.Type,
		Price:         req.Price,
		Quantity:      req.Quantity,
		Status:        OrderStatusNew,
		CreatedAt:     time.Now(),
		UpdateTime:    time.Now().UnixMilli(),
	}

	// 保存到模拟订单存储
	d.mu.Lock()
	d.simulatedOrders[orderID] = order
	d.mu.Unlock()

	logger.Info("🔒 [DryRun] 模拟下单: %s %s %.8f @ %.4f (订单ID: %d, ClientOrderID: %s)",
		req.Side, req.Symbol, req.Quantity, req.Price, orderID, req.ClientOrderID)

	return order, nil
}

// BatchPlaceOrders 批量模拟下单
func (d *DryRunWrapper) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	placedOrders := make([]*Order, 0, len(orders))

	for _, req := range orders {
		order, err := d.PlaceOrder(ctx, req)
		if err != nil {
			logger.Warn("🔒 [DryRun] 模拟下单失败: %v", err)
			continue
		}
		placedOrders = append(placedOrders, order)
	}

	logger.Info("🔒 [DryRun] 批量模拟下单完成: %d/%d 成功", len(placedOrders), len(orders))
	return placedOrders, false // 模拟模式下不会有保证金不足错误
}

// CancelOrder 模拟撤单
func (d *DryRunWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	d.mu.Lock()
	if order, exists := d.simulatedOrders[orderID]; exists {
		order.Status = OrderStatusCanceled
		order.UpdateTime = time.Now().UnixMilli()
	}
	d.mu.Unlock()

	logger.Info("🔒 [DryRun] 模拟撤单: %s 订单ID: %d", symbol, orderID)
	return nil
}

// BatchCancelOrders 批量模拟撤单
func (d *DryRunWrapper) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	d.mu.Lock()
	for _, orderID := range orderIDs {
		if order, exists := d.simulatedOrders[orderID]; exists {
			order.Status = OrderStatusCanceled
			order.UpdateTime = time.Now().UnixMilli()
		}
	}
	d.mu.Unlock()

	logger.Info("🔒 [DryRun] 批量模拟撤单: %s 共 %d 个订单", symbol, len(orderIDs))
	return nil
}

// CancelAllOrders 模拟撤销所有订单
func (d *DryRunWrapper) CancelAllOrders(ctx context.Context, symbol string) error {
	d.mu.Lock()
	count := 0
	for _, order := range d.simulatedOrders {
		if order.Symbol == symbol && order.Status == OrderStatusNew {
			order.Status = OrderStatusCanceled
			order.UpdateTime = time.Now().UnixMilli()
			count++
		}
	}
	d.mu.Unlock()

	logger.Info("🔒 [DryRun] 模拟撤销所有订单: %s 共 %d 个订单", symbol, count)
	return nil
}

// GetOrder 获取订单信息（优先返回模拟订单，否则查询真实交易所）
func (d *DryRunWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	// 先查找模拟订单
	d.mu.RLock()
	if order, exists := d.simulatedOrders[orderID]; exists {
		d.mu.RUnlock()
		return order, nil
	}
	d.mu.RUnlock()

	// 如果不是模拟订单，查询真实交易所（可能是之前的历史订单）
	return d.wrapped.GetOrder(ctx, symbol, orderID)
}

// GetOpenOrders 获取未完成订单（合并模拟订单和真实订单）
func (d *DryRunWrapper) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	// 获取模拟的未完成订单
	d.mu.RLock()
	simulatedOpen := make([]*Order, 0)
	for _, order := range d.simulatedOrders {
		if order.Symbol == symbol && order.Status == OrderStatusNew {
			simulatedOpen = append(simulatedOpen, order)
		}
	}
	d.mu.RUnlock()

	// 获取真实交易所的未完成订单
	realOrders, err := d.wrapped.GetOpenOrders(ctx, symbol)
	if err != nil {
		// 如果获取真实订单失败，只返回模拟订单
		logger.Warn("🔒 [DryRun] 获取真实未完成订单失败: %v，仅返回模拟订单", err)
		return simulatedOpen, nil
	}

	// 合并返回
	return append(realOrders, simulatedOpen...), nil
}

// ===== 以下方法直接透传到真实交易所（只读操作）=====

// GetAccount 获取账户信息（透传）
func (d *DryRunWrapper) GetAccount(ctx context.Context) (*Account, error) {
	return d.wrapped.GetAccount(ctx)
}

// GetPositions 获取持仓信息（透传）
func (d *DryRunWrapper) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	return d.wrapped.GetPositions(ctx, symbol)
}

// GetBalance 获取余额（透传）
func (d *DryRunWrapper) GetBalance(ctx context.Context, asset string) (float64, error) {
	return d.wrapped.GetBalance(ctx, asset)
}

// StartOrderStream 启动订单流（透传）
func (d *DryRunWrapper) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	logger.Info("🔒 [DryRun] 启动订单流监听（模拟模式下可能不会收到成交回报）")
	return d.wrapped.StartOrderStream(ctx, callback)
}

// StopOrderStream 停止订单流（透传）
func (d *DryRunWrapper) StopOrderStream() error {
	return d.wrapped.StopOrderStream()
}

// GetLatestPrice 获取最新价格（透传）
func (d *DryRunWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return d.wrapped.GetLatestPrice(ctx, symbol)
}

// StartPriceStream 启动价格流（透传）
func (d *DryRunWrapper) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return d.wrapped.StartPriceStream(ctx, symbol, callback)
}

// StartKlineStream 启动K线流（透传）
func (d *DryRunWrapper) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	return d.wrapped.StartKlineStream(ctx, symbols, interval, callback)
}

// StopKlineStream 停止K线流（透传）
func (d *DryRunWrapper) StopKlineStream() error {
	return d.wrapped.StopKlineStream()
}

// GetHistoricalKlines 获取历史K线数据（透传）
func (d *DryRunWrapper) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	return d.wrapped.GetHistoricalKlines(ctx, symbol, interval, limit)
}

// GetPriceDecimals 获取价格精度（透传）
func (d *DryRunWrapper) GetPriceDecimals() int {
	return d.wrapped.GetPriceDecimals()
}

// GetQuantityDecimals 获取数量精度（透传）
func (d *DryRunWrapper) GetQuantityDecimals() int {
	return d.wrapped.GetQuantityDecimals()
}

// GetBaseAsset 获取基础资产（透传）
func (d *DryRunWrapper) GetBaseAsset() string {
	return d.wrapped.GetBaseAsset()
}

// GetQuoteAsset 获取计价资产（透传）
func (d *DryRunWrapper) GetQuoteAsset() string {
	return d.wrapped.GetQuoteAsset()
}

// EstimateFinalOrderAmount 预估最终下单金额（透传）
func (d *DryRunWrapper) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return d.wrapped.EstimateFinalOrderAmount(symbol, price, quantity, reduceOnly)
}

// GetFundingRate 获取资金费率（透传）
func (d *DryRunWrapper) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return d.wrapped.GetFundingRate(ctx, symbol)
}

// GetSpotPrice 获取现货价格（透传）
func (d *DryRunWrapper) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return d.wrapped.GetSpotPrice(ctx, symbol)
}

// GetOrderBook 获取订单簿深度（透传）
func (d *DryRunWrapper) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	return d.wrapped.GetOrderBook(ctx, symbol, limit)
}

// InternalTransfer 内部转账（DryRun 模式下不执行）
func (d *DryRunWrapper) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return "", ErrNotImplemented
}

// GetWrappedExchange 获取被包装的真实交易所（用于需要访问原始交易所的场景）
func (d *DryRunWrapper) GetWrappedExchange() IExchange {
	return d.wrapped
}

// GetSimulatedOrders 获取所有模拟订单（用于调试）
func (d *DryRunWrapper) GetSimulatedOrders() map[int64]*Order {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// 返回副本
	result := make(map[int64]*Order)
	for k, v := range d.simulatedOrders {
		result[k] = v
	}
	return result
}

// ClearSimulatedOrders 清除所有模拟订单（用于测试）
func (d *DryRunWrapper) ClearSimulatedOrders() {
	d.mu.Lock()
	d.simulatedOrders = make(map[int64]*Order)
	d.mu.Unlock()
	logger.Info("🔒 [DryRun] 已清除所有模拟订单")
}
