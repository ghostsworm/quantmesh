package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/utils"
)

// Storage 存储接口
type Storage interface {
	SaveOrder(order *Order) error
	SavePosition(position *Position) error
	SaveTrade(trade *Trade) error
	SaveEvent(eventType string, data map[string]interface{}) error
	SaveStatistics(stats *Statistics) error
	SaveSystemMetrics(metrics *SystemMetrics) error
	SaveDailySystemMetrics(metrics *DailySystemMetrics) error
	QuerySystemMetrics(startTime, endTime time.Time) ([]*SystemMetrics, error)
	QueryDailySystemMetrics(days int) ([]*DailySystemMetrics, error)
	GetLatestSystemMetrics() (*SystemMetrics, error)
	CleanupSystemMetrics(beforeTime time.Time) error
	CleanupDailySystemMetrics(beforeDate time.Time) error
	QueryOrders(limit, offset int, status string) ([]*Order, error)
	QueryTrades(startTime, endTime time.Time, limit, offset int) ([]*Trade, error)
	QueryStatistics(startDate, endDate time.Time) ([]*Statistics, error)
	GetStatisticsSummary() (*Statistics, error)
	SaveReconciliationHistory(history *ReconciliationHistory) error
	QueryReconciliationHistory(symbol string, startTime, endTime time.Time, limit, offset int) ([]*ReconciliationHistory, error)
	GetPnLBySymbol(symbol string, startTime, endTime time.Time) (*PnLSummary, error)
	GetPnLByTimeRange(startTime, endTime time.Time) ([]*PnLBySymbol, error)
	GetActualProfitBySymbol(symbol string, beforeTime time.Time) (float64, error)
	SaveRiskCheck(record *RiskCheckRecord) error
	QueryRiskCheckHistory(startTime, endTime time.Time) ([]*RiskCheckHistory, error)
	CleanupRiskCheckHistory(beforeTime time.Time) error
	Close() error
}

// storageEvent 存储事件
type storageEvent struct {
	eventType string
	data      interface{}
}

// StorageService 存储服务
type StorageService struct {
	storage      Storage
	cfg          *config.Config
	eventCh      chan *storageEvent
	buffer       []*storageEvent
	mu           sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	fallbackPath string
	stopped      bool
	stopMu       sync.Mutex
}

// NewStorageService 创建存储服务
func NewStorageService(cfg *config.Config, ctx context.Context) (*StorageService, error) {
	if !cfg.Storage.Enabled {
		return &StorageService{}, nil
	}

	ctx, cancel := context.WithCancel(ctx)

	ss := &StorageService{
		cfg:          cfg,
		eventCh:      make(chan *storageEvent, cfg.Storage.BufferSize),
		buffer:       make([]*storageEvent, 0, cfg.Storage.BatchSize),
		ctx:          ctx,
		cancel:       cancel,
		fallbackPath: "./data/storage_fallback.log",
	}

	// 创建数据目录
	dataDir := filepath.Dir(cfg.Storage.Path)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}

	// 初始化存储实现
	switch cfg.Storage.Type {
	case "sqlite":
		sqliteStorage, err := NewSQLiteStorage(cfg.Storage.Path)
		if err != nil {
			return nil, fmt.Errorf("初始化 SQLite 存储失败: %w", err)
		}
		ss.storage = sqliteStorage
	default:
		return nil, fmt.Errorf("不支持的存储类型: %s", cfg.Storage.Type)
	}

	return ss, nil
}

// GetStorage 获取底层存储接口（用于直接调用存储方法）
func (ss *StorageService) GetStorage() Storage {
	return ss.storage
}

// SaveReconciliationHistoryDirect 直接保存对账历史（用于 Reconciler）
func (ss *StorageService) SaveReconciliationHistoryDirect(symbol string, reconcileTime time.Time, localPosition, exchangePosition, positionDiff float64,
	activeBuyOrders, activeSellOrders int, pendingSellQty, totalBuyQty, totalSellQty, estimatedProfit float64) error {
	if ss.storage == nil {
		return nil
	}
	
	// 计算实际盈利（从 trades 表统计截止到对账时间的累计盈亏）
	actualProfit, err := ss.storage.GetActualProfitBySymbol(symbol, reconcileTime)
	if err != nil {
		logger.Warn("⚠️ 计算实际盈利失败: %v，使用 0 作为默认值", err)
		actualProfit = 0
	}
	
	history := &ReconciliationHistory{
		Symbol:            symbol,
		ReconcileTime:     utils.ToUTC(reconcileTime),
		LocalPosition:     localPosition,
		ExchangePosition:  exchangePosition,
		PositionDiff:      positionDiff,
		ActiveBuyOrders:   activeBuyOrders,
		ActiveSellOrders:  activeSellOrders,
		PendingSellQty:    pendingSellQty,
		TotalBuyQty:       totalBuyQty,
		TotalSellQty:      totalSellQty,
		EstimatedProfit:   estimatedProfit,
		ActualProfit:      actualProfit,
		CreatedAt:         utils.NowUTC(),
	}
	return ss.storage.SaveReconciliationHistory(history)
}

// Start 启动存储服务
func (ss *StorageService) Start() {
	if ss.storage == nil {
		return
	}

	go ss.processEvents()
	logger.Info("✅ 存储服务已启动 (类型: %s, 路径: %s)", ss.cfg.Storage.Type, ss.cfg.Storage.Path)
}

// Stop 停止存储服务
func (ss *StorageService) Stop() {
	ss.stopMu.Lock()
	if ss.stopped {
		ss.stopMu.Unlock()
		return
	}
	ss.stopped = true
	ss.stopMu.Unlock()

	// 取消 context（通知 processEvents 协程退出）
	if ss.cancel != nil {
		ss.cancel()
	}

	// 等待 processEvents 协程处理完队列中的事件
	time.Sleep(100 * time.Millisecond)

	// 最后刷新缓冲区（确保所有事件都被处理）
	ss.flush()

	// 关闭存储（关闭数据库连接）
	if ss.storage != nil {
		ss.storage.Close()
	}
}

// Save 保存数据（完全异步，不阻塞）
func (ss *StorageService) Save(eventType string, data interface{}) {
	if ss.storage == nil {
		return
	}

	// 检查服务是否已停止
	ss.stopMu.Lock()
	stopped := ss.stopped
	ss.stopMu.Unlock()

	if stopped {
		// 服务已停止，不再接受新事件
		return
	}

	select {
	case ss.eventCh <- &storageEvent{eventType: eventType, data: data}:
		// 成功加入队列
	default:
		// Channel 满了，记录警告但不阻塞
		logger.Warn("⚠️ 存储队列已满，丢弃事件: %s", eventType)
	}
}

// processEvents 处理事件（在独立 goroutine 中运行）
func (ss *StorageService) processEvents() {
	flushInterval := time.Duration(ss.cfg.Storage.FlushInterval) * time.Second
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ss.ctx.Done():
			// 退出前刷新缓冲区
			ss.flush()
			return

		case event := <-ss.eventCh:
			// 添加到缓冲区
			ss.mu.Lock()
			ss.buffer = append(ss.buffer, event)
			bufferSize := len(ss.buffer)
			ss.mu.Unlock()

			// 达到批量大小时立即刷新
			if bufferSize >= ss.cfg.Storage.BatchSize {
				ss.flush()
			}

		case <-ticker.C:
			// 定期刷新
			ss.flush()
		}
	}
}

// flush 刷新缓冲区到数据库
func (ss *StorageService) flush() {
	ss.mu.Lock()
	if len(ss.buffer) == 0 {
		ss.mu.Unlock()
		return
	}

	events := make([]*storageEvent, len(ss.buffer))
	copy(events, ss.buffer)
	ss.buffer = ss.buffer[:0]
	ss.mu.Unlock()

	// 批量写入数据库（带重试和保底方案）
	if err := ss.batchSave(events); err != nil {
		logger.Error("❌ 数据库写入失败: %v", err)
		// 保底方案：写入日志文件
		ss.fallbackToLog(events)
	}
}

// batchSave 批量保存
func (ss *StorageService) batchSave(events []*storageEvent) error {
	// 检查存储是否可用
	if ss.storage == nil {
		return fmt.Errorf("存储服务未初始化")
	}

	// 使用事务批量写入
	for _, event := range events {
		var err error
		switch event.eventType {
		case "order_placed", "order_filled", "order_canceled":
			if order, ok := event.data.(map[string]interface{}); ok {
				err = ss.saveOrderFromMap(order)
			}
		case "position_opened", "position_closed":
			if position, ok := event.data.(map[string]interface{}); ok {
				err = ss.savePositionFromMap(position)
			}
		case "system_metrics":
			// 系统监控数据直接通过SaveEvent处理（已在sqlite中实现）
			if data, ok := event.data.(map[string]interface{}); ok {
				err = ss.storage.SaveEvent(event.eventType, data)
			}
		default:
			// 保存为事件
			if data, ok := event.data.(map[string]interface{}); ok {
				err = ss.storage.SaveEvent(event.eventType, data)
			}
		}

		if err != nil {
			// 检查是否是数据库关闭错误
			if err.Error() == "sql: database is closed" {
				return fmt.Errorf("数据库已关闭，停止保存")
			}
			return fmt.Errorf("保存 %s 失败: %w", event.eventType, err)
		}
	}

	return nil
}

// saveOrderFromMap 从 map 保存订单
func (ss *StorageService) saveOrderFromMap(data map[string]interface{}) error {
	order := &Order{}
	if orderID, ok := data["order_id"].(int64); ok {
		order.OrderID = orderID
	}
	if clientOID, ok := data["client_order_id"].(string); ok {
		order.ClientOrderID = clientOID
	}
	if symbol, ok := data["symbol"].(string); ok {
		order.Symbol = symbol
	}
	if side, ok := data["side"].(string); ok {
		order.Side = side
	}
	if price, ok := data["price"].(float64); ok {
		order.Price = price
	}
	if quantity, ok := data["quantity"].(float64); ok {
		order.Quantity = quantity
	}
	if status, ok := data["status"].(string); ok {
		order.Status = status
	}
	if createdAt, ok := data["created_at"].(time.Time); ok {
		order.CreatedAt = utils.ToUTC(createdAt)
	} else {
		order.CreatedAt = utils.NowUTC()
	}
	order.UpdatedAt = utils.NowUTC()

	return ss.storage.SaveOrder(order)
}

// savePositionFromMap 从 map 保存持仓
func (ss *StorageService) savePositionFromMap(data map[string]interface{}) error {
	position := &Position{}
	if slotPrice, ok := data["slot_price"].(float64); ok {
		position.SlotPrice = slotPrice
	}
	if symbol, ok := data["symbol"].(string); ok {
		position.Symbol = symbol
	}
	if size, ok := data["size"].(float64); ok {
		position.Size = size
	}
	if entryPrice, ok := data["entry_price"].(float64); ok {
		position.EntryPrice = entryPrice
	}
	if currentPrice, ok := data["current_price"].(float64); ok {
		position.CurrentPrice = currentPrice
	}
	if pnl, ok := data["pnl"].(float64); ok {
		position.PnL = pnl
	}
	if openedAt, ok := data["opened_at"].(time.Time); ok {
		position.OpenedAt = utils.ToUTC(openedAt)
	} else {
		position.OpenedAt = utils.NowUTC()
	}
	if closedAt, ok := data["closed_at"].(*time.Time); ok {
		closedAtUTC := utils.ToUTC(*closedAt)
		position.ClosedAt = &closedAtUTC
	}

	return ss.storage.SavePosition(position)
}

// fallbackToLog 保底方案：写入日志文件
func (ss *StorageService) fallbackToLog(events []*storageEvent) {
	// 确保目录存在
	dataDir := filepath.Dir(ss.fallbackPath)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		logger.Error("❌ 创建日志目录失败: %v", err)
		return
	}

	file, err := os.OpenFile(ss.fallbackPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		logger.Error("❌ 打开日志文件失败: %v", err)
		return
	}
	defer file.Close()

	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			continue
		}
		line := fmt.Sprintf("%s %s\n", time.Now().Format(time.RFC3339), string(data))
		file.WriteString(line)
	}
}

