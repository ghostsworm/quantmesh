package storage

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"quantmesh/logger"
	"quantmesh/utils"
)

// LogStorage 日志存儲
type LogStorage struct {
	db          *sql.DB
	mu          sync.RWMutex
	logCh       chan *logEntry
	closed      bool
	subscribers []chan *LogRecord // 订阅者列表（用於實時推送）
	subMu       sync.RWMutex
}

// logEntry 日志条目
type logEntry struct {
	level     string
	message   string
	timestamp time.Time
	botID     string // 可為空；寫入 DB 時 NULL
}

// LogQueryParams 日志查詢参數
type LogQueryParams struct {
	StartTime time.Time
	EndTime   time.Time
	Level     string
	Keyword   string
	Limit     int
	Offset    int
	// Exchange/Symbol/MarketType：對 message 子串匹配（多條件為 AND）
	// 與 BotID 同時使用時：若正文不含某關鍵子串（例如 OKX 用 BTC-USDT 而 keyword 傳 BTCUSDT）會得到 0 條；Bot 詳情頁應僅依 BotID 篩選。
	Exchange   string
	Symbol     string
	MarketType string
	// BotID：優先按 logs.bot_id 列精確匹配（舊數據為 NULL 時不會命中）
	BotID string
}

// LogRecord 日志記錄
type LogRecord struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	BotID     string    `json:"bot_id,omitempty"`
}

// NewLogStorage 創建日志存儲
func NewLogStorage(path string) (*LogStorage, error) {
	_, ls, err := openLogStorageDB(path)
	if err != nil {
		return nil, err
	}

	// 啟动异步写入协程
	go ls.processLogs()

	return ls, nil
}

// logSQLiteDSN WAL + busy_timeout，避免與其它連接短暫競爭時出現 database is locked
func logSQLiteDSN(path string) string {
	return path + "?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=15000"
}

// openLogStorageDB 打开數據库，若完整性检查失败则备份並重建
func openLogStorageDB(path string) (*sql.DB, *LogStorage, error) {
	dsn := logSQLiteDSN(path)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("打开日志數據库失败: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ls := &LogStorage{
		db:          db,
		logCh:       make(chan *logEntry, 500),
		subscribers: make([]chan *LogRecord, 0),
	}

	if err := ls.createTable(); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("創建日志表失败: %w", err)
	}
	if err := migrateLogsTable(ls.db); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("遷移日志表失败: %w", err)
	}

	// 啟动時完整性检查
	if err := ls.checkIntegrity(); err != nil {
		logger.Warn("⚠️ 日志數據库完整性检查失败，尝試备份並重建: %v", err)
		db.Close()
		if backupErr := backupAndRemoveCorrupted(path); backupErr != nil {
			return nil, nil, fmt.Errorf("數據库损坏且备份失败: %w", backupErr)
		}
		// 重新打开（新建空库）
		db2, err := sql.Open("sqlite3", dsn)
		if err != nil {
			return nil, nil, fmt.Errorf("重建日志數據库失败: %w", err)
		}
		db2.SetMaxOpenConns(1)
		db2.SetMaxIdleConns(1)
		ls.db = db2
		if err := ls.createTable(); err != nil {
			db2.Close()
			return nil, nil, fmt.Errorf("重建后創建表失败: %w", err)
		}
		if err := migrateLogsTable(ls.db); err != nil {
			db2.Close()
			return nil, nil, fmt.Errorf("重建後遷移日志表失败: %w", err)
		}
		logger.Info("✅ 日志數據库已重建")
	}

	return db, ls, nil
}

// migrateLogsTable 為舊庫添加 bot_id 列及索引（可重複執行）
func migrateLogsTable(db *sql.DB) error {
	if _, err := db.Exec(`ALTER TABLE logs ADD COLUMN bot_id TEXT`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	_, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_logs_bot_id ON logs(bot_id)`)
	return err
}

// checkIntegrity 執行 PRAGMA integrity_check，若有錯误返回非 nil
func (ls *LogStorage) checkIntegrity() error {
	var result string
	err := ls.db.QueryRow("PRAGMA integrity_check").Scan(&result)
	if err != nil {
		return err
	}
	if strings.TrimSpace(result) != "ok" {
		return fmt.Errorf("integrity_check: %s", result)
	}
	return nil
}

// backupAndRemoveCorrupted 將损坏的數據库文件备份后刪除，以便重建
func backupAndRemoveCorrupted(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	backupPath := absPath + ".corrupted." + time.Now().Format("20060102_150405")
	if err := copyFile(absPath, backupPath); err != nil {
		// 文件可能不存在（首次运行），直接回傳 nil 让調用方重建
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("备份损坏文件失败: %w", err)
	}
	logger.Info("已备份损坏的日志數據库到 %s", backupPath)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		p := absPath + suffix
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("刪除 %s 失败: %w", p, err)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// createTable 創建日志表
func (ls *LogStorage) createTable() error {
	sql := `
	CREATE TABLE IF NOT EXISTS logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL,
		level TEXT NOT NULL,
		message TEXT NOT NULL,
		bot_id TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_logs_timestamp ON logs(timestamp);
	CREATE INDEX IF NOT EXISTS idx_logs_level ON logs(level);
	CREATE INDEX IF NOT EXISTS idx_logs_bot_id ON logs(bot_id);
	`

	_, err := ls.db.Exec(sql)
	return err
}

// WriteLog 写入日志（异步，不阻塞）。botID 可選，非空時寫入 bot_id 列。
func (ls *LogStorage) WriteLog(level, message string, botID ...string) {
	if ls.closed {
		return
	}

	bid := ""
	if len(botID) > 0 {
		bid = strings.TrimSpace(botID[0])
	}

	entry := &logEntry{
		level:     level,
		message:   message,
		timestamp: utils.NowUTC(),
		botID:     bid,
	}

	select {
	case ls.logCh <- entry:
		// 成功加入队列
	default:
		// Channel 满了，丢弃消息（避免阻塞）
		// 注意：這裡不能調用 logger.Warn，會導致循環調用
		log.Printf("[WARN] 日志 channel 已满，丢弃消息: %s", message[:min(50, len(message))])
	}
}

// processLogs 处理日志写入（在独立 goroutine 中运行）
func (ls *LogStorage) processLogs() {
	log.Println("[DEBUG] 日志處理協程已啟動")
	buffer := make([]*logEntry, 0, 100)
	ticker := time.NewTicker(1 * time.Second) // 每秒刷新一次
	defer ticker.Stop()
	defer log.Println("[DEBUG] 日志處理協程已退出")

	flush := func() {
		if len(buffer) == 0 {
			return
		}

		// 批量插入
		ls.mu.Lock()
		err := ls.batchInsert(buffer)
		ls.mu.Unlock()

		if err != nil {
			// 写入失败，输出到標准錯误便於調試
			log.Printf("[ERROR] 批量写入日志失败: %v", err)
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
			// 达到批量大小時立即刷新
			if len(buffer) >= 100 {
				flush()
			}

		case <-ticker.C:
			// 定期刷新
			flush()
		}
	}
}

func sqliteLogLockedRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "locked") ||
		strings.Contains(msg, "busy")
}

// batchInsert 批量插入日志（對 SQLITE_BUSY / locked 短重試，配合 DSN busy_timeout）
func (ls *LogStorage) batchInsert(entries []*logEntry) error {
	if len(entries) == 0 {
		return nil
	}
	const maxAttempts = 6
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(20+attempt*25) * time.Millisecond)
		}
		lastErr = ls.batchInsertOnce(entries)
		if lastErr == nil {
			return nil
		}
		if !sqliteLogLockedRetryable(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

// batchInsertOnce 單次事務批量插入
func (ls *LogStorage) batchInsertOnce(entries []*logEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := ls.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO logs (timestamp, level, message, bot_id)
		VALUES (?, ?, ?, NULLIF(?, ''))
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	var insertedLogs []*LogRecord
	for _, entry := range entries {
		result, err := stmt.Exec(entry.timestamp, entry.level, entry.message, entry.botID)
		if err != nil {
			return err
		}

		id, _ := result.LastInsertId()
		insertedLogs = append(insertedLogs, &LogRecord{
			ID:        id,
			Timestamp: entry.timestamp,
			Level:     entry.level,
			Message:   entry.message,
			BotID:     entry.botID,
		})
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	ls.notifySubscribers(insertedLogs)

	return nil
}

// Subscribe 订阅日志更新（返回双向 channel，但外部应該只读取）
func (ls *LogStorage) Subscribe() chan *LogRecord {
	ls.subMu.Lock()
	defer ls.subMu.Unlock()

	ch := make(chan *LogRecord, 100) // 缓冲区100条
	ls.subscribers = append(ls.subscribers, ch)
	
	// 限制订阅者數量，防止記憶體泄漏
	maxSubscribers := 100
	if len(ls.subscribers) > maxSubscribers {
		// 移除最舊的订阅者（FIFO）
		oldest := ls.subscribers[0]
		close(oldest)
		ls.subscribers = ls.subscribers[1:]
		logger.Warn("⚠️ 日志订阅者數量超過限制 (%d)，已移除最舊的订阅者", maxSubscribers)
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
					// Channel 满了，跳過（避免阻塞）
				}
			}
		}
	}()
}

// GetLogs 查詢日志
func (ls *LogStorage) GetLogs(params LogQueryParams) ([]*LogRecord, int, error) {
	ls.mu.RLock()
	defer ls.mu.RUnlock()

	// 構建查詢条件
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

	if kw := strings.TrimSpace(params.Keyword); kw != "" {
		where = append(where, "LOWER(message) LIKE ?")
		args = append(args, "%"+strings.ToLower(kw)+"%")
	}
	if id := strings.TrimSpace(params.BotID); id != "" {
		where = append(where, "bot_id = ?")
		args = append(args, id)
	}
	for _, raw := range []string{params.Exchange, params.Symbol, params.MarketType} {
		if s := strings.TrimSpace(raw); s != "" {
			where = append(where, "LOWER(message) LIKE ?")
			args = append(args, "%"+strings.ToLower(s)+"%")
		}
	}

	whereClause := strings.Join(where, " AND ")

	// 查詢總數
	var total int
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM logs WHERE %s", whereClause)
	err := ls.db.QueryRow(countSQL, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("查詢日志總數失败: %w", err)
	}

	// 查詢數據
	if params.Limit <= 0 {
		params.Limit = 100 // 預設 100条
	}
	if params.Limit > 2000 {
		params.Limit = 2000 // 最大 2000 条（Bot 详情等场景按级别筛选时需要更多）
	}

	querySQL := fmt.Sprintf(`
		SELECT id, timestamp, level, message, bot_id
		FROM logs
		WHERE %s
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	args = append(args, params.Limit, params.Offset)

	rows, err := ls.db.Query(querySQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("查詢日志失败: %w", err)
	}
	defer rows.Close()

	var logs []*LogRecord
	for rows.Next() {
		var log LogRecord
		var botID sql.NullString
		err := rows.Scan(&log.ID, &log.Timestamp, &log.Level, &log.Message, &botID)
		if err != nil {
			return nil, 0, fmt.Errorf("解析日志記錄失败: %w", err)
		}
		if botID.Valid {
			log.BotID = botID.String
		}
		logs = append(logs, &log)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("遍歷日志失败: %w", err)
	}

	return logs, total, nil
}

// CleanOldLogs 清理超過指定天數的日志
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

// CleanOldLogsByLevel 清理超過指定天數的指定级别日志
// levels: 要清理的日志级别列表，如 []string{"INFO", "WARN"}
func (ls *LogStorage) CleanOldLogsByLevel(days int, levels []string) (int64, error) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	if len(levels) == 0 {
		return 0, fmt.Errorf("至少需要指定一個日志级别")
	}

	cutoffTime := time.Now().AddDate(0, 0, -days)
	
	// 構建 IN 子句
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

// Vacuum 优化 SQLite 數據库（回收空间）
func (ls *LogStorage) Vacuum() error {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	_, err := ls.db.Exec("VACUUM")
	return err
}

// GetLogStats 獲取日志统计信息
func (ls *LogStorage) GetLogStats() (map[string]interface{}, error) {
	ls.mu.RLock()
	defer ls.mu.RUnlock()

	stats := make(map[string]interface{})

	// 總日志數
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
			return nil, fmt.Errorf("解析日志分級统计失败: %w", err)
		}
		levelStats[level] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍歷日志分級统计失败: %w", err)
	}
	stats["by_level"] = levelStats

	// 最早和最晚的日志時间
	var oldestTime, newestTime time.Time
	err = ls.db.QueryRow("SELECT MIN(timestamp), MAX(timestamp) FROM logs").Scan(&oldestTime, &newestTime)
	if err == nil {
		stats["oldest_time"] = oldestTime
		stats["newest_time"] = newestTime
	}

	return stats, nil
}

// Close 关闭日志存儲
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

	// 等待一小段時间，让 processLogs 协程完成
	time.Sleep(100 * time.Millisecond)

	return ls.db.Close()
}
