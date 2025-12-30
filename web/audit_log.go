package web

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"quantmesh/logger"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

// AuditLog 审计日志记录
type AuditLog struct {
	ID          int64     `json:"id"`
	Timestamp   time.Time `json:"timestamp"`
	Username    string    `json:"username"`
	IP          string    `json:"ip"`
	UserAgent   string    `json:"user_agent"`
	Action      string    `json:"action"`
	Resource    string    `json:"resource"`
	Details     string    `json:"details"`
	Status      string    `json:"status"` // success, failed
	ErrorMsg    string    `json:"error_msg,omitempty"`
}

// AuditLogger 审计日志记录器
type AuditLogger struct {
	db *sql.DB
}

var globalAuditLogger *AuditLogger

// InitAuditLogger 初始化审计日志系统
func InitAuditLogger(dbPath string) error {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("打开审计日志数据库失败: %w", err)
	}

	// 创建审计日志表
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS audit_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL,
		username TEXT NOT NULL,
		ip TEXT NOT NULL,
		user_agent TEXT,
		action TEXT NOT NULL,
		resource TEXT NOT NULL,
		details TEXT,
		status TEXT NOT NULL,
		error_msg TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_logs(timestamp);
	CREATE INDEX IF NOT EXISTS idx_audit_username ON audit_logs(username);
	CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_logs(action);
	`

	if _, err := db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("创建审计日志表失败: %w", err)
	}

	globalAuditLogger = &AuditLogger{db: db}
	logger.Info("✅ 审计日志系统已初始化")
	return nil
}

// Log 记录审计日志
func (al *AuditLogger) Log(log *AuditLog) error {
	if al == nil || al.db == nil {
		return fmt.Errorf("审计日志系统未初始化")
	}

	insertSQL := `
	INSERT INTO audit_logs (timestamp, username, ip, user_agent, action, resource, details, status, error_msg)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := al.db.Exec(insertSQL,
		log.Timestamp,
		log.Username,
		log.IP,
		log.UserAgent,
		log.Action,
		log.Resource,
		log.Details,
		log.Status,
		log.ErrorMsg,
	)

	if err != nil {
		logger.Error("❌ 记录审计日志失败: %v", err)
		return err
	}

	return nil
}

// Query 查询审计日志
func (al *AuditLogger) Query(username string, action string, startTime, endTime time.Time, limit int) ([]*AuditLog, error) {
	if al == nil || al.db == nil {
		return nil, fmt.Errorf("审计日志系统未初始化")
	}

	query := `
	SELECT id, timestamp, username, ip, user_agent, action, resource, details, status, error_msg
	FROM audit_logs
	WHERE 1=1
	`
	args := []interface{}{}

	if username != "" {
		query += " AND username = ?"
		args = append(args, username)
	}

	if action != "" {
		query += " AND action = ?"
		args = append(args, action)
	}

	if !startTime.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, startTime)
	}

	if !endTime.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, endTime)
	}

	query += " ORDER BY timestamp DESC"

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := al.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := []*AuditLog{}
	for rows.Next() {
		log := &AuditLog{}
		err := rows.Scan(
			&log.ID,
			&log.Timestamp,
			&log.Username,
			&log.IP,
			&log.UserAgent,
			&log.Action,
			&log.Resource,
			&log.Details,
			&log.Status,
			&log.ErrorMsg,
		)
		if err != nil {
			logger.Error("❌ 扫描审计日志失败: %v", err)
			continue
		}
		logs = append(logs, log)
	}

	return logs, nil
}

// LogAction 记录操作（便捷方法）
func LogAction(c *gin.Context, action, resource string, details interface{}, status string, errMsg string) {
	if globalAuditLogger == nil {
		return
	}

	username := "admin" // 从上下文获取用户名
	if user, exists := c.Get("username"); exists {
		username = user.(string)
	}

	detailsJSON, _ := json.Marshal(details)

	log := &AuditLog{
		Timestamp:  time.Now(),
		Username:   username,
		IP:         c.ClientIP(),
		UserAgent:  c.GetHeader("User-Agent"),
		Action:     action,
		Resource:   resource,
		Details:    string(detailsJSON),
		Status:     status,
		ErrorMsg:   errMsg,
	}

	if err := globalAuditLogger.Log(log); err != nil {
		logger.Error("❌ 记录审计日志失败: %v", err)
	}
}

// getAuditLogs 获取审计日志（HTTP 接口）
func getAuditLogs(c *gin.Context) {
	if globalAuditLogger == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "审计日志系统未启用"})
		return
	}

	username := c.Query("username")
	action := c.Query("action")
	limitStr := c.DefaultQuery("limit", "100")

	limit := 100
	fmt.Sscanf(limitStr, "%d", &limit)
	if limit > 1000 {
		limit = 1000
	}

	// 时间范围（可选）
	var startTime, endTime time.Time
	if startStr := c.Query("start_time"); startStr != "" {
		startTime, _ = time.Parse(time.RFC3339, startStr)
	}
	if endStr := c.Query("end_time"); endStr != "" {
		endTime, _ = time.Parse(time.RFC3339, endStr)
	}

	logs, err := globalAuditLogger.Query(username, action, startTime, endTime, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("查询审计日志失败: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"logs":  logs,
		"total": len(logs),
	})
}

