package exchange

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/exchange/income"
	"quantmesh/logger"
)

// DryRunWrapper 模拟运行包装器
// 包装真實的交易所适配器，拦截所有會影响账戶的操作（下單、撤單等）
// 但保留所有只读操作（獲取價格、持倉、餘額等）
type DryRunWrapper struct {
	wrapped IExchange

	// 模拟订單 ID 生成器
	orderIDCounter int64

	// 模拟订單存儲
	mu              sync.RWMutex
	simulatedOrders map[int64]*Order
}

// NewDryRunWrapper 創建 Dry Run 包装器
func NewDryRunWrapper(ex IExchange) *DryRunWrapper {
	logger.Info("🔒 [DryRun] 已啟用模拟运行模式，交易所: %s（不會實際下單）", ex.GetName())
	return &DryRunWrapper{
		wrapped:         ex,
		orderIDCounter:  1000000, // 從一個大數字开始，避免與真實订單 ID 冲突
		simulatedOrders: make(map[int64]*Order),
	}
}

// GetName 獲取交易所名称（添加 DryRun 標识）
func (d *DryRunWrapper) GetName() string {
	return d.wrapped.GetName() + " [DryRun]"
}

// GetMarketType 獲取市場類型
func (d *DryRunWrapper) GetMarketType() string {
	return d.wrapped.GetMarketType()
}

// PlaceOrder 模拟下單（不實際发送到交易所）
func (d *DryRunWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	// 生成模拟订單 ID
	orderID := atomic.AddInt64(&d.orderIDCounter, 1)

	// 創建模拟订單
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

	// 保存到模拟订單存儲
	d.mu.Lock()
	d.simulatedOrders[orderID] = order
	d.mu.Unlock()

	logger.Info("🔒 [DryRun] 模拟下單: %s %s %.8f @ %.4f (订單ID: %d, ClientOrderID: %s)",
		req.Side, req.Symbol, req.Quantity, req.Price, orderID, req.ClientOrderID)

	return order, nil
}

// BatchPlaceOrders 批量模拟下單
func (d *DryRunWrapper) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	placedOrders := make([]*Order, 0, len(orders))

	for _, req := range orders {
		order, err := d.PlaceOrder(ctx, req)
		if err != nil {
			logger.Warn("🔒 [DryRun] 模拟下單失败: %v", err)
			continue
		}
		placedOrders = append(placedOrders, order)
	}

	logger.Info("🔒 [DryRun] 批量模拟下單完成: %d/%d 成功", len(placedOrders), len(orders))
	return placedOrders, false // 模拟模式下不會有保证金不足錯误
}

// CancelOrder 模拟撤單
func (d *DryRunWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	d.mu.Lock()
	if order, exists := d.simulatedOrders[orderID]; exists {
		order.Status = OrderStatusCanceled
		order.UpdateTime = time.Now().UnixMilli()
	}
	d.mu.Unlock()

	logger.Info("🔒 [DryRun] 模拟撤單: %s 订單ID: %d", symbol, orderID)
	return nil
}

// BatchCancelOrders 批量模拟撤單
func (d *DryRunWrapper) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	d.mu.Lock()
	for _, orderID := range orderIDs {
		if order, exists := d.simulatedOrders[orderID]; exists {
			order.Status = OrderStatusCanceled
			order.UpdateTime = time.Now().UnixMilli()
		}
	}
	d.mu.Unlock()

	logger.Info("🔒 [DryRun] 批量模拟撤單: %s 共 %d 個订單", symbol, len(orderIDs))
	return nil
}

// CancelAllOrders 模拟撤销所有订單
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

	logger.Info("🔒 [DryRun] 模拟撤销所有订單: %s 共 %d 個订單", symbol, count)
	return nil
}

// GetOrder 獲取訂單信息（优先返回模拟订單，否则查詢真實交易所）
func (d *DryRunWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	// 先查找模拟订單
	d.mu.RLock()
	if order, exists := d.simulatedOrders[orderID]; exists {
		d.mu.RUnlock()
		return order, nil
	}
	d.mu.RUnlock()

	// 如果不是模拟订單，查詢真實交易所（可能是之前的历史订單）
	return d.wrapped.GetOrder(ctx, symbol, orderID)
}

// GetOpenOrders 獲取未完成订單（合並模拟订單和真實订單）
func (d *DryRunWrapper) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	// 獲取模拟的未完成订單
	d.mu.RLock()
	simulatedOpen := make([]*Order, 0)
	for _, order := range d.simulatedOrders {
		if order.Symbol == symbol && order.Status == OrderStatusNew {
			simulatedOpen = append(simulatedOpen, order)
		}
	}
	d.mu.RUnlock()

	// 獲取真實交易所的未完成订單
	realOrders, err := d.wrapped.GetOpenOrders(ctx, symbol)
	if err != nil {
		// 如果獲取真實订單失败，只返回模拟订單
		logger.Warn("🔒 [DryRun] 獲取真實未完成订單失败: %v，僅返回模拟订單", err)
		return simulatedOpen, nil
	}

	// 合並返回
	return append(realOrders, simulatedOpen...), nil
}

// ===== 以下方法直接透傳到真實交易所（只读操作）=====

// GetAccount 獲取帳戶信息（透傳）
func (d *DryRunWrapper) GetAccount(ctx context.Context) (*Account, error) {
	return d.wrapped.GetAccount(ctx)
}

// GetPositions 獲取持倉信息（透傳）
func (d *DryRunWrapper) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	return d.wrapped.GetPositions(ctx, symbol)
}

// GetBalance 獲取餘額（透傳）
func (d *DryRunWrapper) GetBalance(ctx context.Context, asset string) (float64, error) {
	return d.wrapped.GetBalance(ctx, asset)
}

// StartOrderStream 啟動訂單流（透傳）
func (d *DryRunWrapper) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	logger.Info("🔒 [DryRun] 啟動訂單流監听（模拟模式下可能不會收到成交回报）")
	return d.wrapped.StartOrderStream(ctx, callback)
}

// StopOrderStream 停止訂單流（透傳）
func (d *DryRunWrapper) StopOrderStream() error {
	return d.wrapped.StopOrderStream()
}

// GetLatestPrice 獲取最新價格（透傳）
func (d *DryRunWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return d.wrapped.GetLatestPrice(ctx, symbol)
}

// StartPriceStream 啟動價格流（透傳）
func (d *DryRunWrapper) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return d.wrapped.StartPriceStream(ctx, symbol, callback)
}

// StartKlineStream 啟動K線流（透傳）
func (d *DryRunWrapper) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	return d.wrapped.StartKlineStream(ctx, symbols, interval, callback)
}

// StopKlineStream 停止K線流（透傳）
func (d *DryRunWrapper) StopKlineStream() error {
	return d.wrapped.StopKlineStream()
}

// GetHistoricalKlines 獲取歷史K線數據（透傳）
func (d *DryRunWrapper) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	return d.wrapped.GetHistoricalKlines(ctx, symbol, interval, limit)
}

// GetPriceDecimals 獲取價格精度（透傳）
func (d *DryRunWrapper) GetPriceDecimals() int {
	return d.wrapped.GetPriceDecimals()
}

// GetQuantityDecimals 獲取數量精度（透傳）
func (d *DryRunWrapper) GetQuantityDecimals() int {
	return d.wrapped.GetQuantityDecimals()
}

// GetBaseAsset 獲取基础资產（透傳）
func (d *DryRunWrapper) GetBaseAsset() string {
	return d.wrapped.GetBaseAsset()
}

// GetQuoteAsset 獲取计價资產（透傳）
func (d *DryRunWrapper) GetQuoteAsset() string {
	return d.wrapped.GetQuoteAsset()
}

// EstimateFinalOrderAmount 預估最终下單金額（透傳）
func (d *DryRunWrapper) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return d.wrapped.EstimateFinalOrderAmount(symbol, price, quantity, reduceOnly)
}

// GetFundingRate 獲取资金费率（透傳）
func (d *DryRunWrapper) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return d.wrapped.GetFundingRate(ctx, symbol)
}

func (d *DryRunWrapper) GetFundingInfo(ctx context.Context, symbol string) (*FundingInfo, error) {
	return d.wrapped.GetFundingInfo(ctx, symbol)
}

// GetIncomeHistory 獲取收入歷史（透傳）
func (d *DryRunWrapper) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error) {
	return d.wrapped.GetIncomeHistory(ctx, symbol, incomeType, startTime, endTime)
}

// GetOrderFills 查詢訂單成交記錄（透傳）
func (d *DryRunWrapper) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*OrderFill, error) {
	return d.wrapped.GetOrderFills(ctx, symbol, orderID)
}

// GetSpotPrice 獲取現貨價格（透傳）
func (d *DryRunWrapper) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return d.wrapped.GetSpotPrice(ctx, symbol)
}

// GetOrderBook 獲取訂單簿深度（透傳）
func (d *DryRunWrapper) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	return d.wrapped.GetOrderBook(ctx, symbol, limit)
}

// InternalTransfer 內部轉帳（DryRun 模式下不執行）
func (d *DryRunWrapper) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return "", ErrNotImplemented
}

// GetWrappedExchange 獲取被包装的真實交易所（用於需要访问原始交易所的场景）
func (d *DryRunWrapper) GetWrappedExchange() IExchange {
	return d.wrapped
}

// GetSimulatedOrders 獲取所有模拟订單（用於調試）
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

// ClearSimulatedOrders 清除所有模拟订單（用於测試）
func (d *DryRunWrapper) ClearSimulatedOrders() {
	d.mu.Lock()
	d.simulatedOrders = make(map[int64]*Order)
	d.mu.Unlock()
	logger.Info("🔒 [DryRun] 已清除所有模拟订單")
}
