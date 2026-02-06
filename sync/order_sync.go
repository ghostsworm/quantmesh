package sync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"quantmesh/exchange"
	"quantmesh/exchange/binance"
	"quantmesh/logger"
	"quantmesh/storage"
)

// OrderSyncService 订单同步服务
// 定期从交易所拉取历史成交记录，补全缺失的订单
type OrderSyncService struct {
	exchange      exchange.IExchange
	storage       storage.Storage
	symbol        string
	accountID     string
	exchangeName  string
	syncInterval  time.Duration
	lastSyncTime  time.Time
	mu            sync.RWMutex
	isRunning     bool
	stopC         chan struct{}
}

// NewOrderSyncService 创建订单同步服务
func NewOrderSyncService(
	ex exchange.IExchange,
	st storage.Storage,
	symbol, accountID, exchangeName string,
	syncInterval time.Duration,
) *OrderSyncService {
	return &OrderSyncService{
		exchange:     ex,
		storage:      st,
		symbol:      symbol,
		accountID:    accountID,
		exchangeName: exchangeName,
		syncInterval: syncInterval,
		stopC:        make(chan struct{}),
	}
}

// Start 启动订单同步服务
func (s *OrderSyncService) Start(ctx context.Context) {
	s.mu.Lock()
	if s.isRunning {
		s.mu.Unlock()
		logger.Warn("⚠️ [订单同步] 服务已在运行")
		return
	}
	s.isRunning = true
	s.mu.Unlock()

	logger.Info("✅ [订单同步] 启动订单同步服务 (symbol=%s, interval=%v)", s.symbol, s.syncInterval)

	go func() {
		ticker := time.NewTicker(s.syncInterval)
		defer ticker.Stop()

		// 启动时立即同步一次
		if err := s.Sync(ctx); err != nil {
			logger.Error("❌ [订单同步] 初始同步失败: %v", err)
		}

		for {
			select {
			case <-ctx.Done():
				logger.Info("⏹️ [订单同步] 服务已停止")
				return
			case <-s.stopC:
				logger.Info("⏹️ [订单同步] 服务已停止")
				return
			case <-ticker.C:
				if err := s.Sync(ctx); err != nil {
					logger.Error("❌ [订单同步] 同步失败: %v", err)
				}
			}
		}
	}()
}

// Stop 停止订单同步服务
func (s *OrderSyncService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning {
		return
	}

	close(s.stopC)
	s.isRunning = false
	logger.Info("⏹️ [订单同步] 订单同步服务已停止")
}

// Sync 执行订单同步
func (s *OrderSyncService) Sync(ctx context.Context) error {
	s.mu.Lock()
	lastSync := s.lastSyncTime
	s.mu.Unlock()

	// 同步最近1天的数据
	startTime := time.Now().Add(-1 * 24 * time.Hour).UnixMilli()
	if !lastSync.IsZero() {
		// 从上次同步时间开始，往前推10分钟（避免遗漏）
		startTime = lastSync.Add(-10 * time.Minute).UnixMilli()
	}
	endTime := time.Now().UnixMilli()

	logger.Info("🔄 [订单同步] 开始同步订单 (symbol=%s, startTime=%s, endTime=%s)",
		s.symbol, time.UnixMilli(startTime).Format("2006-01-02 15:04:05"),
		time.UnixMilli(endTime).Format("2006-01-02 15:04:05"))

	// 检查是否是币安交易所（通过类型断言获取GetAdapter方法）
	type adapterGetter interface {
		GetAdapter() interface{}
	}

	adapterGetterImpl, ok := s.exchange.(adapterGetter)
	if !ok {
		logger.Warn("⚠️ [订单同步] 当前交易所不支持订单同步: %s", s.exchangeName)
		return nil
	}

	// 获取币安adapter
	adapter := adapterGetterImpl.GetAdapter()
	if adapter == nil {
		return fmt.Errorf("无法获取币安adapter")
	}

	binanceAdapterImpl, ok := adapter.(*binance.BinanceAdapter)
	if !ok {
		return fmt.Errorf("adapter类型错误，期望 *binance.BinanceAdapter，实际: %T", adapter)
	}

	// 获取用户成交记录（限制500条）
	trades, err := binanceAdapterImpl.GetUserTrades(ctx, s.symbol, startTime, endTime, 500)
	if err != nil {
		return fmt.Errorf("获取用户成交记录失败: %w", err)
	}

	if len(trades) == 0 {
		logger.Debug("📭 [订单同步] 没有新的成交记录")
		s.mu.Lock()
		s.lastSyncTime = time.Now()
		s.mu.Unlock()
		return nil
	}

	logger.Info("📊 [订单同步] 获取到 %d 条成交记录", len(trades))

	// 获取数据库中已存在的订单ID集合
	existingOrderIDs, err := s.getExistingOrderIDs(ctx)
	if err != nil {
		logger.Warn("⚠️ [订单同步] 获取已存在订单ID失败: %v", err)
		existingOrderIDs = make(map[int64]bool)
	}

	// 同步缺失的订单
	syncedCount := 0
	for _, trade := range trades {
		// 检查订单是否已存在
		if existingOrderIDs[trade.OrderID] {
			continue
		}

		// 保存订单（包含交易所已实现盈亏）
		var realizedPnL *float64
		if trade.RealizedPnL != 0 {
			v := trade.RealizedPnL
			realizedPnL = &v
		}
		order := &storage.Order{
			OrderID:       trade.OrderID,
			ClientOrderID: "", // 成交记录中没有ClientOrderID
			Symbol:        trade.Symbol,
			Side:          string(trade.Side),
			Exchange:      "binance",
			Price:         trade.Price,
			Quantity:      trade.Quantity,
			FilledQty:     trade.Quantity, // 成交记录 = 已成交
			Status:        "FILLED",       // 成交记录都是已成交的
			RealizedPnL:   realizedPnL,
			CreatedAt:     trade.Time,
			UpdatedAt:     trade.Time,
		}

		if err := s.storage.SaveOrder(order); err != nil {
			logger.Warn("⚠️ [订单同步] 保存订单失败 (OrderID=%d): %v", trade.OrderID, err)
			continue
		}

		syncedCount++
		logger.Debug("✅ [订单同步] 同步订单: OrderID=%d, Side=%s, Price=%.2f, Quantity=%.4f, RealizedPnL=%.4f",
			trade.OrderID, trade.Side, trade.Price, trade.Quantity, trade.RealizedPnL)
	}

	s.mu.Lock()
	s.lastSyncTime = time.Now()
	s.mu.Unlock()

	if syncedCount > 0 {
		logger.Info("✅ [订单同步] 同步完成: 新增 %d 个订单", syncedCount)
	} else {
		logger.Debug("✅ [订单同步] 同步完成: 没有新订单")
	}

	return nil
}

// getExistingOrderIDs 获取数据库中已存在的订单ID集合
func (s *OrderSyncService) getExistingOrderIDs(ctx context.Context) (map[int64]bool, error) {
	// 查询最近7天的订单
	orders, err := s.storage.QueryOrdersWithTimeRange(1000, 0, "FILLED", nil, nil)
	if err != nil {
		return nil, err
	}

	orderIDs := make(map[int64]bool)
	for _, order := range orders {
		if order.Symbol == s.symbol {
			orderIDs[order.OrderID] = true
		}
	}

	return orderIDs, nil
}
