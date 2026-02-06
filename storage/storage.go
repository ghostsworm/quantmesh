package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"quantmesh/backtest"
	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/utils"
)

// Storage 存儲接口
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
	QueryPositions(limit, offset int) ([]*Position, error)
	QueryTrades(startTime, endTime time.Time, limit, offset int) ([]*Trade, error)
	// GetTradesBySellOrderIDs 根據賣單 ID 查詢對應的成交盈虧，返回 sell_order_id -> pnl 的映射
	GetTradesBySellOrderIDs(sellOrderIDs []int64) (map[int64]float64, error)
	QueryStatistics(startDate, endDate time.Time) ([]*Statistics, error)
	GetStatisticsSummary(account string) (*Statistics, error)
	GetStatisticsSummaryByExchange(exchange, account string) (*Statistics, error)
	GetStatisticsSummaryByExchangeAndSymbol(exchange, symbol, account string) (*Statistics, error)
	QueryDailyStatisticsFromTrades(account string, startDate, endDate time.Time) ([]*DailyStatisticsWithTradeCount, error)
	QueryDailyStatisticsByExchange(exchange, account string, startDate, endDate time.Time) ([]*DailyStatisticsWithTradeCount, error)
	SaveReconciliationHistory(history *ReconciliationHistory) error
	QueryReconciliationHistory(exchange, symbol, account string, startTime, endTime time.Time, limit, offset int) ([]*ReconciliationHistory, error)
	GetLatestReconciliationHistory(exchange, symbol, account string) (*ReconciliationHistory, error)
	GetReconciliationCount(exchange, symbol, account string) (int64, error)
	GetPnLBySymbol(symbol, account string, startTime, endTime time.Time) (*PnLSummary, error)
	GetPnLByTimeRange(account string, startTime, endTime time.Time) ([]*PnLBySymbol, error)
	GetActualProfitBySymbol(symbol, account string, beforeTime time.Time) (float64, error)
	GetTotalBuySellQty(symbol, account string) (totalBuyQty, totalSellQty float64, err error)
	SaveRiskCheck(record *RiskCheckRecord) error
	QueryRiskCheckHistory(startTime, endTime time.Time, limit int) ([]*RiskCheckHistory, error)
	CleanupRiskCheckHistory(beforeTime time.Time) error
	SaveFundingRate(symbol, exchange string, rate float64, timestamp time.Time) error
	GetLatestFundingRate(symbol, exchange string) (float64, error)
	GetFundingRateHistory(symbol, exchange string, limit int) ([]*FundingRate, error)
	SaveFundingPayment(payment *FundingPayment) error
	GetFundingPayments(account, exchange string, startTime, endTime time.Time) ([]*FundingPayment, error)
	GetFundingPaymentsSum(account, exchange string, startTime, endTime time.Time) (float64, error)
	GetDailyFundingPayments(account, exchange string, startTime, endTime time.Time) (map[string]float64, error)
	GetAIPromptTemplate(module string) (*AIPromptTemplate, error)
	SetAIPromptTemplate(template *AIPromptTemplate) error
	GetAllAIPromptTemplates() ([]*AIPromptTemplate, error)
	SaveBasisData(data *BasisData) error
	GetLatestBasis(symbol, exchange string) (*BasisData, error)
	GetBasisHistory(symbol, exchange string, limit int) ([]*BasisData, error)
	GetBasisStatistics(symbol, exchange string, hours int) (*BasisStats, error)

	// 盈利管理：自动提取规则
	ListProfitWithdrawRules(accountID string) ([]*ProfitWithdrawRule, error)
	ListAccountIDsWithProfitRules() ([]string, error)
	ReplaceProfitWithdrawRules(accountID string, rules []*ProfitWithdrawRule) error
	UpsertProfitWithdrawRule(accountID string, rule *ProfitWithdrawRule) error
	DeleteProfitWithdrawRule(accountID string, ruleID string) error
	UpdateRuleLastTriggeredAt(ruleID string, triggeredAt time.Time) error

	// 盈利提取記錄
	SaveWithdrawRecord(record *ProfitWithdrawRecord) error
	UpdateWithdrawRecordStatus(id, status, transferID, failedReason string) error
	GetWithdrawRecords(accountID string, limit int) ([]*ProfitWithdrawRecord, error)

	GetBacktestTaskStore() backtest.TaskStore
	GetOptimTaskStore() backtest.OptimTaskStore
	Close() error

	// 新聞分析历史
	SaveNewsAnalysisHistory(history *NewsAnalysisHistory) error
	QueryNewsAnalysisHistory(symbol string, startTime, endTime time.Time, limit, offset int) ([]*NewsAnalysisHistory, int64, error)
	GetLatestNewsAnalysisHistory(symbol string) (*NewsAnalysisHistory, error)
	GetNewsAnalysisHistoryByID(id int64) (*NewsAnalysisHistory, error)
	CleanupNewsAnalysisHistory(beforeTime time.Time) error

	// 價格历史（用於預测驗证）
	SavePriceHistory(h *PriceHistory) error
	GetPriceAtTime(assetType, symbol string, t time.Time, tolerance time.Duration) (*PriceHistory, error)
	GetPriceHistory(assetType, symbol string, startTime, endTime time.Time, limit int) ([]*PriceHistory, error)

	// 預测驗证
	SavePredictionVerification(v *PredictionVerification) error
	QueryPredictionVerifications(assetType, symbol string, startTime, endTime time.Time, limit, offset int) ([]*PredictionVerification, int64, error)
	GetPredictionVerificationsByStatus(status string, limit int) ([]*PredictionVerification, error)
	UpdatePredictionVerification(v *PredictionVerification) error
	GetPredictionAccuracyStats(assetType string, since time.Time) (total int, correct int, err error)

	// 每日快照與小時權益（未實現盈虧、日內最大回撤）
	SaveHourlyEquityRecord(record *HourlyEquityRecord) error
	SaveDailySnapshot(snapshot *DailySnapshot) error
	QueryDailySnapshots(exchange, symbol, account string, startDate, endDate time.Time) ([]*DailySnapshot, error)
	GetDailySnapshot(exchange, symbol, account string, date time.Time) (*DailySnapshot, error)
	QueryHourlyEquityRecords(exchange, symbol, account string, startTime, endTime time.Time) ([]*HourlyEquityRecord, error)
	DeleteHourlyEquityRecordsBefore(cutoff time.Time) error

	// 智子巡檢報告歷史
	SaveInspectionReport(report *InspectionReport) error

	// K線文件保護
	ProtectKlineFile(filename string) error
	UnprotectKlineFile(filename string) error
	GetProtectedKlineFiles() ([]string, error)
	IsKlineFileProtected(filename string) (bool, error)
}

// storageEvent 存儲事件
type storageEvent struct {
	eventType string
	data      interface{}
}

// StorageService 存儲服務
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

// NewStorageService 創建存儲服務
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

	// 創建數據目錄
	dataDir := filepath.Dir(cfg.Storage.Path)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("創建數據目錄失败: %w", err)
	}

	// 初始化存儲實現
	switch cfg.Storage.Type {
	case "sqlite":
		sqliteStorage, err := NewSQLiteStorage(cfg.Storage.Path)
		if err != nil {
			return nil, fmt.Errorf("初始化 SQLite 存儲失败: %w", err)
		}
		ss.storage = sqliteStorage
	default:
		return nil, fmt.Errorf("不支援的存儲類型: %s", cfg.Storage.Type)
	}

	return ss, nil
}

// GetStorage 獲取底层存儲介面（用於直接調用存儲方法）
func (ss *StorageService) GetStorage() Storage {
	return ss.storage
}

// SaveReconciliationHistoryDirect 直接保存對账历史（用於 Reconciler）
func (ss *StorageService) SaveReconciliationHistoryDirect(exchange, symbol, account string, reconcileTime time.Time, localPosition, exchangePosition, positionDiff float64,
	activeBuyOrders, activeSellOrders int, pendingSellQty, totalBuyQty, totalSellQty, estimatedProfit float64) error {
	if ss.storage == nil {
		return nil
	}

	// 计算實際盈利（從 trades 表统计截止到對账時间的累计盈亏）
	// 🔥 重要：先將 reconcileTime 轉换為 UTC，因為數據库中的 created_at 是 UTC 時间
	reconcileTimeUTC := utils.ToUTC(reconcileTime)
	actualProfit, err := ss.storage.GetActualProfitBySymbol(symbol, account, reconcileTimeUTC)
	if err != nil {
		logger.Warn("⚠️ 计算實際盈利失败: %v，使用 0 作為默认值", err)
		actualProfit = 0
	}

	history := &ReconciliationHistory{
		Exchange:         exchange,
		Symbol:           symbol,
		Account:          account,
		ReconcileTime:    utils.ToUTC(reconcileTime),
		LocalPosition:    localPosition,
		ExchangePosition: exchangePosition,
		PositionDiff:     positionDiff,
		ActiveBuyOrders:  activeBuyOrders,
		ActiveSellOrders: activeSellOrders,
		PendingSellQty:   pendingSellQty,
		TotalBuyQty:      totalBuyQty,
		TotalSellQty:     totalSellQty,
		EstimatedProfit:  estimatedProfit,
		ActualProfit:     actualProfit,
		CreatedAt:        utils.NowUTC(),
	}
	return ss.storage.SaveReconciliationHistory(history)
}

// Start 啟动存儲服務
func (ss *StorageService) Start() {
	if ss.storage == nil {
		return
	}

	go ss.processEvents()
	logger.Info("✅ 存儲服務已啟动 (類型: %s, 路径: %s)", ss.cfg.Storage.Type, ss.cfg.Storage.Path)
}

// Stop 停止存儲服務
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

	// 关闭存儲（关闭數據库连接）
	if ss.storage != nil {
		ss.storage.Close()
	}
}

// Save 保存數據（完全异步，不阻塞）
func (ss *StorageService) Save(eventType string, data interface{}) {
	if ss.storage == nil {
		return
	}

	// 检查服務是否已停止
	ss.stopMu.Lock()
	stopped := ss.stopped
	ss.stopMu.Unlock()

	if stopped {
		// 服務已停止，不再接受新事件
		return
	}

	select {
	case ss.eventCh <- &storageEvent{eventType: eventType, data: data}:
		// 成功加入队列
	default:
		// Channel 满了，記錄警告但不阻塞
		logger.Warn("⚠️ 存儲队列已满，丢弃事件: %s", eventType)
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
			// 检查缓冲区大小，防止無限增长
			maxBufferSize := ss.cfg.Storage.BatchSize * 10 // 最多保留10倍批量大小
			if len(ss.buffer) >= maxBufferSize {
				// 缓冲区過大，强制刷新
				ss.mu.Unlock()
				logger.Warn("⚠️ 存儲缓冲区過大 (%d)，强制刷新", len(ss.buffer))
				ss.flush()
				ss.mu.Lock()
			}
			ss.buffer = append(ss.buffer, event)
			bufferSize := len(ss.buffer)
			ss.mu.Unlock()

			// 达到批量大小時立即刷新
			if bufferSize >= ss.cfg.Storage.BatchSize {
				ss.flush()
			}

		case <-ticker.C:
			// 定期刷新
			ss.flush()
		}
	}
}

// flush 刷新缓冲区到數據库
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

	// 批量写入數據库（带重試和保底方案）
	if err := ss.batchSave(events); err != nil {
		logger.Error("❌ 數據库写入失败: %v", err)
		// 保底方案：写入日志文件
		ss.fallbackToLog(events)
	}
}

// batchSave 批量保存
func (ss *StorageService) batchSave(events []*storageEvent) error {
	// 检查存儲是否可用
	if ss.storage == nil {
		return fmt.Errorf("存儲服務未初始化")
	}

	// 使用事務批量写入
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
			// 系统監控數據直接通過SaveEvent处理（已在sqlite中實現）
			if data, ok := event.data.(map[string]interface{}); ok {
				err = ss.storage.SaveEvent(event.eventType, data)
			}
		default:
			// 保存為事件
			if data, ok := event.data.(map[string]interface{}); ok {
				err = ss.storage.SaveEvent(event.eventType, data)
			}
		}

		if err != nil {
			// 检查是否是數據库关闭錯误
			if err.Error() == "sql: database is closed" {
				return fmt.Errorf("數據库已关闭，停止保存")
			}
			return fmt.Errorf("保存 %s 失败: %w", event.eventType, err)
		}
	}

	return nil
}

// saveOrderFromMap 從 map 保存订單
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
	if exchange, ok := data["exchange"].(string); ok {
		order.Exchange = exchange
	}
	if orderType, ok := data["type"].(string); ok {
		order.Type = orderType
	}
	if price, ok := data["price"].(float64); ok {
		order.Price = price
	}
	if quantity, ok := data["quantity"].(float64); ok {
		order.Quantity = quantity
	}
	// 🔥 修复：事件中字段名为 executed_qty，需要正确映射
	if executedQty, ok := data["executed_qty"].(float64); ok {
		order.FilledQty = executedQty
		// 如果 quantity 未設置，使用 executed_qty 作為回退值（FILLED 订單二者相等）
		if order.Quantity <= 0 {
			order.Quantity = executedQty
		}
	}
	if status, ok := data["status"].(string); ok {
		order.Status = status
	}
	// 交易所已實現盈虧
	if rpnl, ok := data["realized_pnl"].(float64); ok {
		order.RealizedPnL = &rpnl
	}
	if createdAt, ok := data["created_at"].(time.Time); ok {
		order.CreatedAt = utils.ToUTC(createdAt)
	} else {
		order.CreatedAt = utils.NowUTC()
	}
	order.UpdatedAt = utils.NowUTC()

	if err := ss.storage.SaveOrder(order); err != nil {
		return err
	}
	// 合规审计：記錄订單事件
	if globalAuditLogger != nil {
		exchange, _ := data["exchange"].(string)
		account, _ := data["account"].(string)
		globalAuditLogger.LogOrder(exchange, order.Symbol, account, order)
	}
	return nil
}

// savePositionFromMap 從 map 保存持倉
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
	// 确保目錄存在
	dataDir := filepath.Dir(ss.fallbackPath)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		logger.Error("❌ 創建日志目錄失败: %v", err)
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
		line := fmt.Sprintf("%s %s\n", utils.NowConfiguredTimezone().Format(time.RFC3339), string(data))
		file.WriteString(line)
	}
}
