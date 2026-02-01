package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// GormDatabase GORM 数据库实现
type GormDatabase struct {
	db *gorm.DB
}

// DBConfig 数据库配置
type DBConfig struct {
	Type            string        // sqlite, postgres, mysql
	DSN             string        // 数据源名称
	MaxOpenConns    int           // 最大打开连接数
	MaxIdleConns    int           // 最大空闲连接数
	ConnMaxLifetime time.Duration // 连接最大生命周期
	LogLevel        string        // 日志级别: silent, error, warn, info
}

// NewGormDatabase 创建 GORM 数据库实例
func NewGormDatabase(config *DBConfig) (*GormDatabase, error) {
	var dialector gorm.Dialector

	switch config.Type {
	case "sqlite":
		dialector = sqlite.Open(config.DSN)
	case "postgres", "postgresql":
		dialector = postgres.Open(config.DSN)
	case "mysql":
		dialector = mysql.Open(config.DSN)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", config.Type)
	}

	// 日志级别
	logLevel := logger.Silent
	switch config.LogLevel {
	case "error":
		logLevel = logger.Error
	case "warn":
		logLevel = logger.Warn
	case "info":
		logLevel = logger.Info
	}

	// 打开数据库
	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 获取底层 sql.DB
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	// 配置连接池
	if config.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(config.MaxOpenConns)
	}
	if config.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(config.MaxIdleConns)
	}
	if config.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(config.ConnMaxLifetime)
	}

	// 自动迁移
	models := []interface{}{
		&Trade{},
		&Order{},
		&Statistics{},
		&Reconciliation{},
		&RiskCheck{},
		&EventRecord{},
		&AsyncTask{},
		&PositionPlan{},
	}
	
	// 逐个迁移模型，确保每个表都被创建
	for _, model := range models {
		if err := db.AutoMigrate(model); err != nil {
			// 如果是索引已存在的错误，忽略它（这是正常的）
			errStr := err.Error()
			if strings.Contains(errStr, "already exists") || 
			   strings.Contains(errStr, "duplicate") ||
			   strings.Contains(errStr, "UNIQUE constraint failed") {
				// 索引/约束已存在，这是正常的，继续运行
			} else {
				// 对于其他错误（包括表创建失败），记录并返回错误
				// 这样可以确保表被正确创建
				return nil, fmt.Errorf("failed to auto migrate %T: %w", model, err)
			}
		}
	}
	
	// 验证关键表是否存在（特别是 async_tasks）
	var count int64
	if err := db.Model(&AsyncTask{}).Count(&count).Error; err != nil {
		// 如果查询失败，可能是表不存在，尝试强制创建
		if strings.Contains(err.Error(), "no such table") || 
		   strings.Contains(err.Error(), "does not exist") ||
		   strings.Contains(err.Error(), "table") && strings.Contains(err.Error(), "not exist") {
			// 表不存在，强制创建
			if createErr := db.AutoMigrate(&AsyncTask{}); createErr != nil {
				return nil, fmt.Errorf("failed to create async_tasks table: %w", createErr)
			}
			// 再次验证表是否创建成功
			if verifyErr := db.Model(&AsyncTask{}).Count(&count).Error; verifyErr != nil {
				return nil, fmt.Errorf("async_tasks table created but verification failed: %w", verifyErr)
			}
		} else {
			return nil, fmt.Errorf("failed to verify async_tasks table: %w", err)
		}
	}

	return &GormDatabase{db: db}, nil
}

// SaveTrade 保存交易记录
func (g *GormDatabase) SaveTrade(ctx context.Context, trade *Trade) error {
	return g.db.WithContext(ctx).Create(trade).Error
}

// GetTrades 获取交易记录
func (g *GormDatabase) GetTrades(ctx context.Context, filter *TradeFilter) ([]*Trade, error) {
	query := g.db.WithContext(ctx).Model(&Trade{})

	if filter.Exchange != "" {
		query = query.Where("exchange = ?", filter.Exchange)
	}
	if filter.Symbol != "" {
		query = query.Where("symbol = ?", filter.Symbol)
	}
	if filter.StartTime != nil {
		query = query.Where("created_at >= ?", filter.StartTime)
	}
	if filter.EndTime != nil {
		query = query.Where("created_at <= ?", filter.EndTime)
	}

	query = query.Order("created_at DESC")

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	var trades []*Trade
	if err := query.Find(&trades).Error; err != nil {
		return nil, err
	}

	return trades, nil
}

// BatchSaveTrades 批量保存交易记录
func (g *GormDatabase) BatchSaveTrades(ctx context.Context, trades []*Trade) error {
	if len(trades) == 0 {
		return nil
	}
	return g.db.WithContext(ctx).CreateInBatches(trades, 100).Error
}

// SaveOrder 保存订单记录
func (g *GormDatabase) SaveOrder(ctx context.Context, order *Order) error {
	return g.db.WithContext(ctx).Create(order).Error
}

// GetOrders 获取订单记录
func (g *GormDatabase) GetOrders(ctx context.Context, filter *OrderFilter) ([]*Order, error) {
	query := g.db.WithContext(ctx).Model(&Order{})

	if filter.Exchange != "" {
		query = query.Where("exchange = ?", filter.Exchange)
	}
	if filter.Symbol != "" {
		query = query.Where("symbol = ?", filter.Symbol)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}

	query = query.Order("created_at DESC")

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	var orders []*Order
	if err := query.Find(&orders).Error; err != nil {
		return nil, err
	}

	return orders, nil
}

// SaveStatistics 保存统计数据
func (g *GormDatabase) SaveStatistics(ctx context.Context, stats *Statistics) error {
	return g.db.WithContext(ctx).Create(stats).Error
}

// GetStatistics 获取统计数据
func (g *GormDatabase) GetStatistics(ctx context.Context, filter *StatFilter) ([]*Statistics, error) {
	query := g.db.WithContext(ctx).Model(&Statistics{})

	if filter.Exchange != "" {
		query = query.Where("exchange = ?", filter.Exchange)
	}
	if filter.Symbol != "" {
		query = query.Where("symbol = ?", filter.Symbol)
	}
	if filter.StartDate != nil {
		query = query.Where("date >= ?", filter.StartDate)
	}
	if filter.EndDate != nil {
		query = query.Where("date <= ?", filter.EndDate)
	}

	query = query.Order("date DESC")

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	var stats []*Statistics
	if err := query.Find(&stats).Error; err != nil {
		return nil, err
	}

	return stats, nil
}

// SaveReconciliation 保存对账记录
func (g *GormDatabase) SaveReconciliation(ctx context.Context, recon *Reconciliation) error {
	return g.db.WithContext(ctx).Create(recon).Error
}

// GetReconciliations 获取对账记录
func (g *GormDatabase) GetReconciliations(ctx context.Context, filter *ReconciliationFilter) ([]*Reconciliation, error) {
	query := g.db.WithContext(ctx).Model(&Reconciliation{})

	if filter.Exchange != "" {
		query = query.Where("exchange = ?", filter.Exchange)
	}
	if filter.Symbol != "" {
		query = query.Where("symbol = ?", filter.Symbol)
	}
	if filter.Type != "" {
		query = query.Where("type = ?", filter.Type)
	}
	if filter.Resolved != nil {
		query = query.Where("resolved = ?", *filter.Resolved)
	}
	if filter.StartTime != nil {
		query = query.Where("created_at >= ?", filter.StartTime)
	}
	if filter.EndTime != nil {
		query = query.Where("created_at <= ?", filter.EndTime)
	}

	query = query.Order("created_at DESC")

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	var recons []*Reconciliation
	if err := query.Find(&recons).Error; err != nil {
		return nil, err
	}

	return recons, nil
}

// SaveRiskCheck 保存风控记录
func (g *GormDatabase) SaveRiskCheck(ctx context.Context, check *RiskCheck) error {
	return g.db.WithContext(ctx).Create(check).Error
}

// GetRiskChecks 获取风控记录
func (g *GormDatabase) GetRiskChecks(ctx context.Context, filter *RiskCheckFilter) ([]*RiskCheck, error) {
	query := g.db.WithContext(ctx).Model(&RiskCheck{})

	if filter.Exchange != "" {
		query = query.Where("exchange = ?", filter.Exchange)
	}
	if filter.Symbol != "" {
		query = query.Where("symbol = ?", filter.Symbol)
	}
	if filter.IsHealthy != nil {
		query = query.Where("is_healthy = ?", *filter.IsHealthy)
	}
	if filter.StartTime != nil {
		query = query.Where("created_at >= ?", filter.StartTime)
	}
	if filter.EndTime != nil {
		query = query.Where("created_at <= ?", filter.EndTime)
	}

	query = query.Order("created_at DESC")

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	var checks []*RiskCheck
	if err := query.Find(&checks).Error; err != nil {
		return nil, err
	}

	return checks, nil
}

// BeginTx 开始事务
func (g *GormDatabase) BeginTx(ctx context.Context) (Tx, error) {
	tx := g.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	return &GormTx{tx: tx}, nil
}

// Ping 健康检查
func (g *GormDatabase) Ping(ctx context.Context) error {
	sqlDB, err := g.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// Close 关闭连接
func (g *GormDatabase) Close() error {
	sqlDB, err := g.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// SaveEvent 保存事件记录
func (g *GormDatabase) SaveEvent(ctx context.Context, event *EventRecord) error {
	return g.db.WithContext(ctx).Create(event).Error
}

// GetEvents 获取事件记录
func (g *GormDatabase) GetEvents(ctx context.Context, filter *EventFilter) ([]*EventRecord, error) {
	query := g.db.WithContext(ctx).Model(&EventRecord{})

	if filter.Type != "" {
		query = query.Where("type = ?", filter.Type)
	}
	if filter.Severity != "" {
		query = query.Where("severity = ?", filter.Severity)
	}
	if filter.Source != "" {
		query = query.Where("source = ?", filter.Source)
	}
	if filter.Exchange != "" {
		query = query.Where("exchange = ?", filter.Exchange)
	}
	if filter.Symbol != "" {
		query = query.Where("symbol = ?", filter.Symbol)
	}
	if filter.StartTime != nil {
		query = query.Where("created_at >= ?", filter.StartTime)
	}
	if filter.EndTime != nil {
		query = query.Where("created_at <= ?", filter.EndTime)
	}

	query = query.Order("created_at DESC")

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	var events []*EventRecord
	if err := query.Find(&events).Error; err != nil {
		return nil, err
	}

	return events, nil
}

// GetEventByID 根据ID获取事件
func (g *GormDatabase) GetEventByID(ctx context.Context, id int64) (*EventRecord, error) {
	var event EventRecord
	if err := g.db.WithContext(ctx).First(&event, id).Error; err != nil {
		return nil, err
	}
	return &event, nil
}

// SaveAsyncTask 保存异步任务
func (g *GormDatabase) SaveAsyncTask(ctx context.Context, task *AsyncTask) error {
	return g.db.WithContext(ctx).Create(task).Error
}

// UpdateAsyncTask 更新异步任务
func (g *GormDatabase) UpdateAsyncTask(ctx context.Context, task *AsyncTask) error {
	return g.db.WithContext(ctx).Save(task).Error
}

// GetAsyncTask 获取异步任务
func (g *GormDatabase) GetAsyncTask(ctx context.Context, id string) (*AsyncTask, error) {
	var task AsyncTask
	if err := g.db.WithContext(ctx).Where("id = ?", id).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// GetPendingAsyncTasks 获取待处理任务
func (g *GormDatabase) GetPendingAsyncTasks(ctx context.Context, limit int) ([]*AsyncTask, error) {
	var tasks []*AsyncTask
	err := g.db.WithContext(ctx).
		Where("status = ?", "pending").
		Order("created_at ASC").
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

// GetAsyncTasks 获取异步任务列表（支持过滤和分页）
func (g *GormDatabase) GetAsyncTasks(ctx context.Context, filter *AsyncTaskFilter) ([]*AsyncTask, error) {
	query := g.db.WithContext(ctx).Model(&AsyncTask{})

	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.TaskType != "" {
		query = query.Where("task_type = ?", filter.TaskType)
	}
	if filter.StartTime != nil {
		query = query.Where("created_at >= ?", filter.StartTime)
	}
	if filter.EndTime != nil {
		query = query.Where("created_at <= ?", filter.EndTime)
	}

	query = query.Order("created_at DESC")

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	} else {
		query = query.Limit(100) // 默认限制100条
	}
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	var tasks []*AsyncTask
	if err := query.Find(&tasks).Error; err != nil {
		return nil, err
	}

	return tasks, nil
}

// GetAsyncTaskStats 获取异步任务统计（Token使用量）
func (g *GormDatabase) GetAsyncTaskStats(ctx context.Context, startTime, endTime *time.Time) (*AsyncTaskStats, error) {
	stats := &AsyncTaskStats{
		DailyStats: []DailyTokenStat{},
	}

	// 总计统计（所有任务）
	var totalTasks int64
	var totalInput, totalOutput int64
	g.db.WithContext(ctx).Model(&AsyncTask{}).Count(&totalTasks)
	g.db.WithContext(ctx).Model(&AsyncTask{}).Select("COALESCE(SUM(input_tokens), 0)").Scan(&totalInput)
	g.db.WithContext(ctx).Model(&AsyncTask{}).Select("COALESCE(SUM(output_tokens), 0)").Scan(&totalOutput)
	stats.TotalTasks = int(totalTasks)
	stats.TotalInputTokens = totalInput
	stats.TotalOutputTokens = totalOutput
	stats.TotalTokens = totalInput + totalOutput

	// 今日统计（今日00:00:00开始）
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	var todayTasks int64
	var todayInput, todayOutput int64
	todayQuery := g.db.WithContext(ctx).Model(&AsyncTask{}).Where("created_at >= ?", todayStart)
	todayQuery.Count(&todayTasks)
	g.db.WithContext(ctx).Model(&AsyncTask{}).Where("created_at >= ?", todayStart).Select("COALESCE(SUM(input_tokens), 0)").Scan(&todayInput)
	g.db.WithContext(ctx).Model(&AsyncTask{}).Where("created_at >= ?", todayStart).Select("COALESCE(SUM(output_tokens), 0)").Scan(&todayOutput)
	stats.TodayInputTokens = todayInput
	stats.TodayOutputTokens = todayOutput
	stats.TodayTokens = todayInput + todayOutput

	// 每日统计（最近30天）
	thirtyDaysAgo := todayStart.AddDate(0, 0, -30)
	if startTime != nil && startTime.After(thirtyDaysAgo) {
		thirtyDaysAgo = *startTime
	}

	var dailyStats []struct {
		Date         string
		InputTokens  int64
		OutputTokens int64
		TaskCount    int64
	}

	// 每日统计（按日期分组）
	err := g.db.WithContext(ctx).Model(&AsyncTask{}).
		Select("DATE(created_at) as date, " +
			"COALESCE(SUM(input_tokens), 0) as input_tokens, " +
			"COALESCE(SUM(output_tokens), 0) as output_tokens, " +
			"COUNT(*) as task_count").
		Where("created_at >= ?", thirtyDaysAgo).
		Group("DATE(created_at)").
		Order("date DESC").
		Scan(&dailyStats).Error

	if err != nil {
		return nil, err
	}

	// 转换每日统计
	for _, ds := range dailyStats {
		// 解析日期字符串为 time.Time
		date, err := time.Parse("2006-01-02", ds.Date)
		if err != nil {
			return nil, fmt.Errorf("解析日期失败 %s: %w", ds.Date, err)
		}
		stats.DailyStats = append(stats.DailyStats, DailyTokenStat{
			Date:         date,
			InputTokens:  ds.InputTokens,
			OutputTokens: ds.OutputTokens,
			TotalTokens:  ds.InputTokens + ds.OutputTokens,
			TaskCount:    int(ds.TaskCount),
		})
	}

	return stats, nil
}

// CleanupExpiredAsyncTasks 清理过期任务
func (g *GormDatabase) CleanupExpiredAsyncTasks(ctx context.Context, cutoff time.Time) (int64, error) {
	result := g.db.WithContext(ctx).
		Where("expires_at < ? OR (status IN ('completed', 'failed', 'timeout') AND completed_at < ?)", time.Now(), cutoff).
		Delete(&AsyncTask{})
	return result.RowsAffected, result.Error
}

// SavePositionPlan 保存仓位计划
func (g *GormDatabase) SavePositionPlan(ctx context.Context, plan *PositionPlan) error {
	return g.db.WithContext(ctx).Create(plan).Error
}

// UpdatePositionPlan 更新仓位计划
func (g *GormDatabase) UpdatePositionPlan(ctx context.Context, plan *PositionPlan) error {
	return g.db.WithContext(ctx).Save(plan).Error
}

// GetPositionPlan 获取单个仓位计划
func (g *GormDatabase) GetPositionPlan(ctx context.Context, id int64) (*PositionPlan, error) {
	var plan PositionPlan
	if err := g.db.WithContext(ctx).Where("id = ?", id).First(&plan).Error; err != nil {
		return nil, err
	}
	return &plan, nil
}

// GetPositionPlans 获取仓位计划列表
func (g *GormDatabase) GetPositionPlans(ctx context.Context, filter *PositionPlanFilter) ([]*PositionPlan, error) {
	query := g.db.WithContext(ctx).Model(&PositionPlan{})

	if filter.Exchange != "" {
		query = query.Where("exchange = ?", filter.Exchange)
	}
	if filter.Symbol != "" {
		query = query.Where("symbol = ?", filter.Symbol)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}

	query = query.Order("created_at DESC")

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	} else {
		query = query.Limit(100)
	}
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	var plans []*PositionPlan
	if err := query.Find(&plans).Error; err != nil {
		return nil, err
	}
	return plans, nil
}

// GetEventStats 获取事件统计
func (g *GormDatabase) GetEventStats(ctx context.Context) (*EventStats, error) {
	stats := &EventStats{
		CountByType:   make(map[string]int),
		CountBySource: make(map[string]int),
	}

	// 总数
	var totalCount int64
	g.db.WithContext(ctx).Model(&EventRecord{}).Count(&totalCount)
	stats.TotalCount = int(totalCount)

	// 按严重程度统计
	var criticalCount, warningCount, infoCount int64
	g.db.WithContext(ctx).Model(&EventRecord{}).Where("severity = ?", "critical").Count(&criticalCount)
	g.db.WithContext(ctx).Model(&EventRecord{}).Where("severity = ?", "warning").Count(&warningCount)
	g.db.WithContext(ctx).Model(&EventRecord{}).Where("severity = ?", "info").Count(&infoCount)
	stats.CriticalCount = int(criticalCount)
	stats.WarningCount = int(warningCount)
	stats.InfoCount = int(infoCount)

	// 最近24小时
	last24h := time.Now().Add(-24 * time.Hour)
	var last24hCount int64
	g.db.WithContext(ctx).Model(&EventRecord{}).Where("created_at >= ?", last24h).Count(&last24hCount)
	stats.Last24HoursCount = int(last24hCount)

	// 按类型统计（top 20）
	var typeStats []struct {
		Type  string
		Count int
	}
	g.db.WithContext(ctx).Model(&EventRecord{}).
		Select("type, COUNT(*) as count").
		Group("type").
		Order("count DESC").
		Limit(20).
		Scan(&typeStats)
	for _, ts := range typeStats {
		stats.CountByType[ts.Type] = ts.Count
	}

	// 按来源统计
	var sourceStats []struct {
		Source string
		Count  int
	}
	g.db.WithContext(ctx).Model(&EventRecord{}).
		Select("source, COUNT(*) as count").
		Group("source").
		Scan(&sourceStats)
	for _, ss := range sourceStats {
		stats.CountBySource[ss.Source] = ss.Count
	}

	return stats, nil
}

// CleanupOldEvents 清理旧事件
func (g *GormDatabase) CleanupOldEvents(ctx context.Context, severity string, keepCount int, keepDays int) error {
	// 按时间清理：删除超过指定天数的事件
	cutoffDate := time.Now().AddDate(0, 0, -keepDays)
	if err := g.db.WithContext(ctx).
		Where("severity = ? AND created_at < ?", severity, cutoffDate).
		Delete(&EventRecord{}).Error; err != nil {
		return err
	}

	// 按数量清理：保留最新的 keepCount 条
	var count int64
	g.db.WithContext(ctx).Model(&EventRecord{}).Where("severity = ?", severity).Count(&count)
	
	if int(count) > keepCount {
		// 获取需要保留的最老记录的ID
		var cutoffID int64
		g.db.WithContext(ctx).Model(&EventRecord{}).
			Where("severity = ?", severity).
			Order("created_at DESC").
			Limit(1).
			Offset(keepCount).
			Pluck("id", &cutoffID)

		// 删除ID小于cutoffID的记录
		if cutoffID > 0 {
			if err := g.db.WithContext(ctx).
				Where("severity = ? AND id < ?", severity, cutoffID).
				Delete(&EventRecord{}).Error; err != nil {
				return err
			}
		}
	}

	return nil
}

// GormTx GORM 事务实现
type GormTx struct {
	tx *gorm.DB
}

func (t *GormTx) Commit() error {
	return t.tx.Commit().Error
}

func (t *GormTx) Rollback() error {
	return t.tx.Rollback().Error
}

func (t *GormTx) SaveTrade(ctx context.Context, trade *Trade) error {
	return t.tx.WithContext(ctx).Create(trade).Error
}

func (t *GormTx) GetTrades(ctx context.Context, filter *TradeFilter) ([]*Trade, error) {
	// 实现与 GormDatabase 相同
	return nil, fmt.Errorf("not implemented in transaction")
}

func (t *GormTx) BatchSaveTrades(ctx context.Context, trades []*Trade) error {
	return t.tx.WithContext(ctx).CreateInBatches(trades, 100).Error
}

func (t *GormTx) SaveOrder(ctx context.Context, order *Order) error {
	return t.tx.WithContext(ctx).Create(order).Error
}

func (t *GormTx) GetOrders(ctx context.Context, filter *OrderFilter) ([]*Order, error) {
	return nil, fmt.Errorf("not implemented in transaction")
}

func (t *GormTx) SaveStatistics(ctx context.Context, stats *Statistics) error {
	return t.tx.WithContext(ctx).Create(stats).Error
}

func (t *GormTx) GetStatistics(ctx context.Context, filter *StatFilter) ([]*Statistics, error) {
	return nil, fmt.Errorf("not implemented in transaction")
}

func (t *GormTx) SaveReconciliation(ctx context.Context, recon *Reconciliation) error {
	return t.tx.WithContext(ctx).Create(recon).Error
}

func (t *GormTx) GetReconciliations(ctx context.Context, filter *ReconciliationFilter) ([]*Reconciliation, error) {
	return nil, fmt.Errorf("not implemented in transaction")
}

func (t *GormTx) SaveRiskCheck(ctx context.Context, check *RiskCheck) error {
	return t.tx.WithContext(ctx).Create(check).Error
}

func (t *GormTx) GetRiskChecks(ctx context.Context, filter *RiskCheckFilter) ([]*RiskCheck, error) {
	return nil, fmt.Errorf("not implemented in transaction")
}

func (t *GormTx) BeginTx(ctx context.Context) (Tx, error) {
	return nil, fmt.Errorf("nested transactions not supported")
}

func (t *GormTx) Ping(ctx context.Context) error {
	return nil
}

func (t *GormTx) Close() error {
	return nil
}

func (t *GormTx) SaveEvent(ctx context.Context, event *EventRecord) error {
	return t.tx.WithContext(ctx).Create(event).Error
}

func (t *GormTx) GetEvents(ctx context.Context, filter *EventFilter) ([]*EventRecord, error) {
	return nil, fmt.Errorf("not implemented in transaction")
}

func (t *GormTx) GetEventByID(ctx context.Context, id int64) (*EventRecord, error) {
	return nil, fmt.Errorf("not implemented in transaction")
}

func (t *GormTx) GetEventStats(ctx context.Context) (*EventStats, error) {
	return nil, fmt.Errorf("not implemented in transaction")
}

func (t *GormTx) CleanupOldEvents(ctx context.Context, severity string, keepCount int, keepDays int) error {
	return fmt.Errorf("not implemented in transaction")
}

func (t *GormTx) SaveAsyncTask(ctx context.Context, task *AsyncTask) error {
	return t.tx.WithContext(ctx).Create(task).Error
}

func (t *GormTx) UpdateAsyncTask(ctx context.Context, task *AsyncTask) error {
	return t.tx.WithContext(ctx).Save(task).Error
}

func (t *GormTx) GetAsyncTask(ctx context.Context, id string) (*AsyncTask, error) {
	var task AsyncTask
	if err := t.tx.WithContext(ctx).Where("id = ?", id).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func (t *GormTx) GetPendingAsyncTasks(ctx context.Context, limit int) ([]*AsyncTask, error) {
	var tasks []*AsyncTask
	err := t.tx.WithContext(ctx).
		Where("status = ?", "pending").
		Order("created_at ASC").
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

func (t *GormTx) GetAsyncTasks(ctx context.Context, filter *AsyncTaskFilter) ([]*AsyncTask, error) {
	query := t.tx.WithContext(ctx).Model(&AsyncTask{})

	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.TaskType != "" {
		query = query.Where("task_type = ?", filter.TaskType)
	}
	if filter.StartTime != nil {
		query = query.Where("created_at >= ?", filter.StartTime)
	}
	if filter.EndTime != nil {
		query = query.Where("created_at <= ?", filter.EndTime)
	}

	query = query.Order("created_at DESC")

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	} else {
		query = query.Limit(100)
	}
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	var tasks []*AsyncTask
	if err := query.Find(&tasks).Error; err != nil {
		return nil, err
	}

	return tasks, nil
}

func (t *GormTx) GetAsyncTaskStats(ctx context.Context, startTime, endTime *time.Time) (*AsyncTaskStats, error) {
	// 事务中暂不实现统计，返回错误
	return nil, fmt.Errorf("GetAsyncTaskStats not implemented in transaction")
}

func (t *GormTx) CleanupExpiredAsyncTasks(ctx context.Context, cutoff time.Time) (int64, error) {
	result := t.tx.WithContext(ctx).
		Where("expires_at < ? OR (status IN ('completed', 'failed', 'timeout') AND completed_at < ?)", time.Now(), cutoff).
		Delete(&AsyncTask{})
	return result.RowsAffected, result.Error
}

func (t *GormTx) SavePositionPlan(ctx context.Context, plan *PositionPlan) error {
	return t.tx.WithContext(ctx).Create(plan).Error
}

func (t *GormTx) UpdatePositionPlan(ctx context.Context, plan *PositionPlan) error {
	return t.tx.WithContext(ctx).Save(plan).Error
}

func (t *GormTx) GetPositionPlan(ctx context.Context, id int64) (*PositionPlan, error) {
	var plan PositionPlan
	if err := t.tx.WithContext(ctx).Where("id = ?", id).First(&plan).Error; err != nil {
		return nil, err
	}
	return &plan, nil
}

func (t *GormTx) GetPositionPlans(ctx context.Context, filter *PositionPlanFilter) ([]*PositionPlan, error) {
	query := t.tx.WithContext(ctx).Model(&PositionPlan{})

	if filter.Exchange != "" {
		query = query.Where("exchange = ?", filter.Exchange)
	}
	if filter.Symbol != "" {
		query = query.Where("symbol = ?", filter.Symbol)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}

	query = query.Order("created_at DESC")

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	} else {
		query = query.Limit(100)
	}
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	var plans []*PositionPlan
	if err := query.Find(&plans).Error; err != nil {
		return nil, err
	}
	return plans, nil
}
