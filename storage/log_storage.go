package storage

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"quantmesh/utils"
	"quantmesh/logger"
)

// LogStorage 日志存储
type LogStorage struct {
	db          *sql.DB
	mu          sync.RWMutex
	logCh       chan *logEntry
	closed      bool
	subscribers []chan *LogRecord // 订阅者列表（用于实时推送）
	subMu       sync.RWMutex
}

// logEntry 日志条目
type logEntry struct {
	level     string
	message   string
	timestamp time.Time
}

// LogQueryParams 日志查询参数
type LogQueryParams struct {
	StartTime time.Time
	EndTime   time.Time
	Level     string
	Keyword   string
	Limit     int
	Offset    int
}

// LogRecord 日志记录
type LogRecord struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

// NewLogStorage 创建日志存储
func NewLogStorage(path string) (*LogStorage, error) {
	// 使用 WAL 模式提高并发性能
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_synchronous=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("打开日志数据库失败: %w", err)
	}

	// 设置连接池
	db.SetMaxOpenConns(1) // SQLite 并发限制
	db.SetMaxIdleConns(1)

	ls := &LogStorage{
		db:          db,
		logCh:       make(chan *logEntry, 500), // 缓冲区500条（优化：减少内存占用）
		subscribers: make([]chan *LogRecord, 0),
	}

	// 创建表
	if err := ls.createTable(); err != nil {
		db.Close()
		return nil, fmt.Errorf("创建日志表失败: %w", err)
	}

	// 启动异步写入协程
	go ls.processLogs()

	return ls, nil
}

// createTable 创建日志表
func (ls *LogStorage) createTable() error {
	sql := `
	CREATE TABLE IF NOT EXISTS logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL,
		level TEXT NOT NULL,
		message TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_logs_timestamp ON logs(timestamp);
	CREATE INDEX IF NOT EXISTS idx_logs_level ON logs(level);
	`

	_, err := ls.db.Exec(sql)
	return err
}

// WriteLog 写入日志（异步，不阻塞）
func (ls *LogStorage) WriteLog(level, message string) {
	if ls.closed {
		return
	}

	entry := &logEntry{
		level:     level,
		message:   message,
		timestamp: utils.NowUTC(),
	}

	select {
	case ls.logCh <- entry:
		// 成功加入队列
	default:
		// Channel 满了，丢弃消息（避免阻塞）
	}
}

// processLogs 处理日志写入（在独立 goroutine 中运行）
func (ls *LogStorage) processLogs() {
	buffer := make([]*logEntry, 0, 100)
	ticker := time.NewTicker(1 * time.Second) // 每秒刷新一次
	defer ticker.Stop()

	flush := func() {
		if len(buffer) == 0 {
			return
		}

		// 批量插入
		ls.mu.Lock()
		err := ls.batchInsert(buffer)
		ls.mu.Unlock()

		if err != nil {
			// 写入失败，静默处理（不影响主程序）
			// 可以选择输出到标准错误，但这里选择静默
		}

		// 清空缓冲区
		buffer = buffer[:0]
	}

	for {
		select {
		case entry, ok := <-ls.logCh:
			if !ok {
				// Channel 已关闭，刷新缓冲区后退出
				flush()
				return
			}
			buffer = append(buffer, entry)
			// 达到批量大小时立即刷新
			if len(buffer) >= 100 {
				flush()
			}

		case <-ticker.C:
			// 定期刷新
			flush()
		}
	}
}

// batchInsert 批量插入日志
func (ls *LogStorage) batchInsert(entries []*logEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// 使用事务批量插入
	tx, err := ls.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO logs (timestamp, level, message)
		VALUES (?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	var insertedLogs []*LogRecord
	for _, entry := range entries {
		result, err := stmt.Exec(entry.timestamp, entry.level, entry.message)
		if err != nil {
			return err
		}

		// 获取插入的 ID
		id, _ := result.LastInsertId()
		insertedLogs = append(insertedLogs, &LogRecord{
			ID:        id,
			Timestamp: entry.timestamp,
			Level:     entry.level,
			Message:   entry.message,
		})
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// 通知所有订阅者
	ls.notifySubscribers(insertedLogs)

	return nil
}

// Subscribe 订阅日志更新（返回双向 channel，但外部应该只读取）
func (ls *LogStorage) Subscribe() chan *LogRecord {
	ls.subMu.Lock()
	defer ls.subMu.Unlock()

	ch := make(chan *LogRecord, 100) // 缓冲区100条
	ls.subscribers = append(ls.subscribers, ch)
	
	// 限制订阅者数量，防止内存泄漏
	maxSubscribers := 100
	if len(ls.subscribers) > maxSubscribers {
		// 移除最旧的订阅者（FIFO）
		oldest := ls.subscribers[0]
		close(oldest)
		ls.subscribers = ls.subscribers[1:]
		logger.Warn("⚠️ 日志订阅者数量超过限制 (%d)，已移除最旧的订阅者", maxSubscribers)
	}
	
	return ch
}

// Unsubscribe 取消订阅
func (ls *LogStorage) Unsubscribe(ch chan *LogRecord) {
	ls.subMu.Lock()
	defer ls.subMu.Unlock()

	for i, sub := range ls.subscribers {
		if sub == ch {
			// 移除订阅者
			ls.subscribers = append(ls.subscribers[:i], ls.subscribers[i+1:]...)
			close(ch)
			break
		}
	}
}

// notifySubscribers 通知所有订阅者
func (ls *LogStorage) notifySubscribers(logs []*LogRecord) {
	ls.subMu.RLock()
	subscribers := make([]chan *LogRecord, len(ls.subscribers))
	copy(subscribers, ls.subscribers)
	ls.subMu.RUnlock()

	// 异步通知，避免阻塞
	go func() {
		for _, log := range logs {
			for _, sub := range subscribers {
				select {
				case sub <- log:
					// 成功发送
				default:
					// Channel 满了，跳过（避免阻塞）
				}
			}
		}
	}()
}

// GetLogs 查询日志
func (ls *LogStorage) GetLogs(params LogQueryParams) ([]*LogRecord, int, error) {
	ls.mu.RLock()
	defer ls.mu.RUnlock()

	// 构建查询条件
	where := []string{"1=1"}
	args := []interface{}{}

	if !params.StartTime.IsZero() {
		where = append(where, "timestamp >= ?")
		args = append(args, params.StartTime)
	}

	if !params.EndTime.IsZero() {
		where = append(where, "timestamp <= ?")
		args = append(args, params.EndTime)
	}

	if params.Level != "" {
		where = append(where, "level = ?")
		args = append(args, params.Level)
	}

	if params.Keyword != "" {
		where = append(where, "message LIKE ?")
		args = append(args, "%"+params.Keyword+"%")
	}

	whereClause := strings.Join(where, " AND ")

	// 查询总数
	var total int
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM logs WHERE %s", whereClause)
	err := ls.db.QueryRow(countSQL, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("查询日志总数失败: %w", err)
	}

	// 查询数据
	if params.Limit <= 0 {
		params.Limit = 100 // 默认100条
	}
	if params.Limit > 1000 {
		params.Limit = 1000 // 最大1000条
	}

	querySQL := fmt.Sprintf(`
		SELECT id, timestamp, level, message
		FROM logs
		WHERE %s
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	args = append(args, params.Limit, params.Offset)

	rows, err := ls.db.Query(querySQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("查询日志失败: %w", err)
	}
	defer rows.Close()

	var logs []*LogRecord
	for rows.Next() {
		var log LogRecord
		err := rows.Scan(&log.ID, &log.Timestamp, &log.Level, &log.Message)
		if err != nil {
			continue
		}
		logs = append(logs, &log)
	}

	return logs, total, nil
}

// CleanOldLogs 清理超过指定天数的日志
func (ls *LogStorage) CleanOldLogs(days int) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	cutoffTime := time.Now().AddDate(0, 0, -days)
	_, err := ls.db.Exec(`
		DELETE FROM logs
		WHERE timestamp < ?
	`, cutoffTime)
	return err
}

// CleanOldLogsByLevel 清理超过指定天数的指定级别日志
// levels: 要清理的日志级别列表，如 []string{"INFO", "WARN"}
func (ls *LogStorage) CleanOldLogsByLevel(days int, levels []string) (int64, error) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	if len(levels) == 0 {
		return 0, fmt.Errorf("至少需要指定一个日志级别")
	}

	cutoffTime := time.Now().AddDate(0, 0, -days)
	
	// 构建 IN 子句
	placeholders := make([]string, len(levels))
	args := make([]interface{}, len(levels)+1)
	for i, level := range levels {
		placeholders[i] = "?"
		args[i] = level
	}
	args[len(levels)] = cutoffTime

	query := fmt.Sprintf(`
		DELETE FROM logs
		WHERE level IN (%s) AND timestamp < ?
	`, strings.Join(placeholders, ","))

	result, err := ls.db.Exec(query, args...)
	if err != nil {
		return 0, err
	}

	rowsAffected, err := result.RowsAffected()
	return rowsAffected, err
}

// Vacuum 优化 SQLite 数据库（回收空间）
func (ls *LogStorage) Vacuum() error {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	_, err := ls.db.Exec("VACUUM")
	return err
}

// GetLogStats 获取日志统计信息
func (ls *LogStorage) GetLogStats() (map[string]interface{}, error) {
	ls.mu.RLock()
	defer ls.mu.RUnlock()

	stats := make(map[string]interface{})

	// 总日志数
	var totalCount int64
	err := ls.db.QueryRow("SELECT COUNT(*) FROM logs").Scan(&totalCount)
	if err != nil {
		return nil, err
	}
	stats["total"] = totalCount

	// 按级别统计
	levelStats := make(map[string]int64)
	rows, err := ls.db.Query(`
		SELECT level, COUNT(*) as count
		FROM logs
		GROUP BY level
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var level string
		var count int64
		if err := rows.Scan(&level, &count); err != nil {
			continue
		}
		levelStats[level] = count
	}
	stats["by_level"] = levelStats

	// 最早和最晚的日志时间
	var oldestTime, newestTime time.Time
	err = ls.db.QueryRow("SELECT MIN(timestamp), MAX(timestamp) FROM logs").Scan(&oldestTime, &newestTime)
	if err == nil {
		stats["oldest_time"] = oldestTime
		stats["newest_time"] = newestTime
	}

	return stats, nil
}

// Close 关闭日志存储
func (ls *LogStorage) Close() error {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	if ls.closed {
		return nil
	}

	ls.closed = true
	close(ls.logCh)

	// 关闭所有订阅者
	ls.subMu.Lock()
	for _, sub := range ls.subscribers {
		close(sub)
	}
	ls.subscribers = nil
	ls.subMu.Unlock()

	// 等待一小段时间，让 processLogs 协程完成
	time.Sleep(100 * time.Millisecond)

	return ls.db.Close()
}
