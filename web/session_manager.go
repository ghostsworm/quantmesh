package web

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"quantmesh/logger"
)

// Session 会话信息
type Session struct {
	SessionID string
	Username  string
	Role      string
	IP        string
	UserAgent string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// SessionManager 会话管理器
type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	// 会话过期时间（默认24小时）
	sessionTimeout time.Duration
	// 数据库连接（用于持久化会话）
	db *sql.DB
}

// NewSessionManager 创建会话管理器
func NewSessionManager() *SessionManager {
	dataDir := "./data"
	// 确保数据目录存在
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		logger.Warn("⚠️ 创建数据目录失败: %v，会话将不会持久化", err)
		return &SessionManager{
			sessions:       make(map[string]*Session),
			sessionTimeout: 24 * time.Hour,
			db:             nil,
		}
	}

	// 使用与 PasswordManager 相同的数据库文件
	dbPath := filepath.Join(dataDir, "auth.db")
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_timeout=30000&_busy_timeout=30000", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		logger.Warn("⚠️ 打开会话数据库失败: %v，会话将不会持久化", err)
		return &SessionManager{
			sessions:       make(map[string]*Session),
			sessionTimeout: 24 * time.Hour,
			db:             nil,
		}
	}

	// 配置连接池
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	sm := &SessionManager{
		sessions:       make(map[string]*Session),
		sessionTimeout: 24 * time.Hour,
		db:             db,
	}

	// 初始化数据库表
	if err := sm.initDatabase(); err != nil {
		logger.Warn("⚠️ 初始化会话数据库表失败: %v，会话将不会持久化", err)
		db.Close()
		sm.db = nil
	} else {
		// 从数据库加载有效的会话
		if err := sm.loadSessionsFromDB(); err != nil {
			logger.Warn("⚠️ 从数据库加载会话失败: %v", err)
		} else {
			logger.Info("✅ 会话管理器已初始化，已从数据库加载有效会话")
		}
	}

	// 启动清理过期会话的协程
	go sm.cleanupExpiredSessions()

	return sm
}

// initDatabase 初始化数据库表
func (sm *SessionManager) initDatabase() error {
	if sm.db == nil {
		return fmt.Errorf("数据库连接未初始化")
	}

	createTableSQL := `
	CREATE TABLE IF NOT EXISTS sessions (
		session_id TEXT PRIMARY KEY,
		username TEXT NOT NULL,
		role TEXT NOT NULL,
		ip TEXT,
		user_agent TEXT,
		created_at DATETIME NOT NULL,
		expires_at DATETIME NOT NULL
	);
	`

	if _, err := sm.db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("创建会话表失败: %v", err)
	}

	// 创建索引
	indexSQL := `
	CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
	CREATE INDEX IF NOT EXISTS idx_sessions_username ON sessions(username);
	`
	if _, err := sm.db.Exec(indexSQL); err != nil {
		return fmt.Errorf("创建索引失败: %v", err)
	}

	return nil
}

// loadSessionsFromDB 从数据库加载有效的会话
func (sm *SessionManager) loadSessionsFromDB() error {
	if sm.db == nil {
		return nil // 数据库未初始化，跳过加载
	}

	now := time.Now()
	rows, err := sm.db.Query(`
		SELECT session_id, username, role, ip, user_agent, created_at, expires_at 
		FROM sessions 
		WHERE expires_at > ?
	`, now)
	if err != nil {
		return fmt.Errorf("查询会话失败: %v", err)
	}
	defer rows.Close()

	sm.mu.Lock()
	defer sm.mu.Unlock()

	loadedCount := 0
	for rows.Next() {
		var session Session
		err := rows.Scan(
			&session.SessionID,
			&session.Username,
			&session.Role,
			&session.IP,
			&session.UserAgent,
			&session.CreatedAt,
			&session.ExpiresAt,
		)
		if err != nil {
			logger.Warn("⚠️ 加载会话失败: %v", err)
			continue
		}

		// 再次检查是否过期（防止时间差问题）
		if now.Before(session.ExpiresAt) {
			sm.sessions[session.SessionID] = &session
			loadedCount++
		}
	}

	if loadedCount > 0 {
		logger.Info("✅ 从数据库加载了 %d 个有效会话", loadedCount)
	}

	return rows.Err()
}

// cleanupExpiredSessions 清理过期会话
func (sm *SessionManager) cleanupExpiredSessions() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		sm.mu.Lock()
		now := time.Now()
		var expiredSessionIDs []string
		for sessionID, session := range sm.sessions {
			if now.After(session.ExpiresAt) {
				expiredSessionIDs = append(expiredSessionIDs, sessionID)
				delete(sm.sessions, sessionID)
			}
		}
		sm.mu.Unlock()

		// 从数据库删除过期会话
		if sm.db != nil && len(expiredSessionIDs) > 0 {
			sm.deleteSessionsFromDB(expiredSessionIDs)
		}

		// 同时清理数据库中所有过期的会话（防止遗漏）
		if sm.db != nil {
			_, err := sm.db.Exec("DELETE FROM sessions WHERE expires_at <= ?", now)
			if err != nil {
				logger.Warn("⚠️ 清理数据库过期会话失败: %v", err)
			}
		}
	}
}

// generateSessionID 生成会话ID
func (sm *SessionManager) generateSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// 使用无填充的 URL 安全编码，避免 Cookie 中的 '=' 被转义导致会话查找失败
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CreateSession 创建会话
func (sm *SessionManager) CreateSession(username, role, ip, userAgent string) (*Session, error) {
	sessionID, err := sm.generateSessionID()
	if err != nil {
		return nil, fmt.Errorf("生成会话ID失败: %v", err)
	}

	now := time.Now()
	session := &Session{
		SessionID: sessionID,
		Username:  username,
		Role:      role,
		IP:        ip,
		UserAgent: userAgent,
		CreatedAt: now,
		ExpiresAt: now.Add(sm.sessionTimeout),
	}

	sm.mu.Lock()
	sm.sessions[sessionID] = session
	sm.mu.Unlock()

	// 保存到数据库
	if sm.db != nil {
		if err := sm.saveSessionToDB(session); err != nil {
			logger.Warn("⚠️ 保存会话到数据库失败: %v", err)
			// 不返回错误，因为内存中已经创建了会话
		}
	}

	return session, nil
}

// saveSessionToDB 保存会话到数据库
func (sm *SessionManager) saveSessionToDB(session *Session) error {
	if sm.db == nil {
		return nil // 数据库未初始化，跳过保存
	}

	_, err := sm.db.Exec(`
		INSERT OR REPLACE INTO sessions 
		(session_id, username, role, ip, user_agent, created_at, expires_at) 
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		session.SessionID,
		session.Username,
		session.Role,
		session.IP,
		session.UserAgent,
		session.CreatedAt,
		session.ExpiresAt,
	)
	return err
}

// GetSession 获取会话
func (sm *SessionManager) GetSession(sessionID string) (*Session, bool) {
	sm.mu.RLock()
	session, exists := sm.sessions[sessionID]
	sm.mu.RUnlock()

	// 如果内存中存在，检查是否过期
	if exists {
		if time.Now().After(session.ExpiresAt) {
			// 过期了，从内存和数据库删除
			sm.DeleteSession(sessionID)
			return nil, false
		}
		return session, true
	}

	// 内存中不存在，尝试从数据库加载（防止启动时遗漏）
	if sm.db != nil {
		session = sm.loadSessionFromDB(sessionID)
		if session != nil {
			// 检查是否过期
			if time.Now().After(session.ExpiresAt) {
				sm.DeleteSession(sessionID)
				return nil, false
			}
			// 加载到内存中
			sm.mu.Lock()
			sm.sessions[sessionID] = session
			sm.mu.Unlock()
			return session, true
		}
	}

	return nil, false
}

// loadSessionFromDB 从数据库加载单个会话
func (sm *SessionManager) loadSessionFromDB(sessionID string) *Session {
	if sm.db == nil {
		return nil
	}

	var session Session
	err := sm.db.QueryRow(`
		SELECT session_id, username, role, ip, user_agent, created_at, expires_at 
		FROM sessions 
		WHERE session_id = ? AND expires_at > ?
	`, sessionID, time.Now()).Scan(
		&session.SessionID,
		&session.Username,
		&session.Role,
		&session.IP,
		&session.UserAgent,
		&session.CreatedAt,
		&session.ExpiresAt,
	)
	if err != nil {
		return nil
	}

	return &session
}

// DeleteSession 删除会话
func (sm *SessionManager) DeleteSession(sessionID string) {
	sm.mu.Lock()
	delete(sm.sessions, sessionID)
	sm.mu.Unlock()

	// 从数据库删除
	if sm.db != nil {
		sm.deleteSessionsFromDB([]string{sessionID})
	}
}

// deleteSessionsFromDB 从数据库删除会话
func (sm *SessionManager) deleteSessionsFromDB(sessionIDs []string) error {
	if sm.db == nil || len(sessionIDs) == 0 {
		return nil
	}

	// 构建 IN 查询
	query := "DELETE FROM sessions WHERE session_id IN ("
	args := make([]interface{}, len(sessionIDs))
	for i, id := range sessionIDs {
		if i > 0 {
			query += ","
		}
		query += "?"
		args[i] = id
	}
	query += ")"

	_, err := sm.db.Exec(query, args...)
	return err
}

// GetSessionFromRequest 从请求中获取会话
func (sm *SessionManager) GetSessionFromRequest(r *http.Request) (*Session, bool) {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		return nil, false
	}
	return sm.GetSession(cookie.Value)
}

// SetSessionCookie 设置会话Cookie
func (sm *SessionManager) SetSessionCookie(w http.ResponseWriter, sessionID string, secure bool) {
	// 在本地开发环境（localhost）中，强制 secure=false
	// 因为 localhost 通常使用 HTTP 而不是 HTTPS
	cookie := &http.Cookie{
		Name:     "session_id",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   false,                // 本地开发环境使用 HTTP，不需要 Secure 标志
		SameSite: http.SameSiteLaxMode, // 使用 Lax 模式，确保同站请求能正常携带 Cookie
		MaxAge:   int(sm.sessionTimeout.Seconds()),
	}
	http.SetCookie(w, cookie)

	// 调试日志：写入Web日志文件（而不是标准输出）
	logger.WriteWebLog(fmt.Sprintf("[SESSION] Cookie 已设置: Name=%s, Value=%s..., Path=%s, MaxAge=%d, HttpOnly=%v, Secure=%v, SameSite=%v",
		cookie.Name, sessionID[:20], cookie.Path, cookie.MaxAge, cookie.HttpOnly, cookie.Secure, cookie.SameSite))
}

// ClearSessionCookie 清除会话Cookie
func (sm *SessionManager) ClearSessionCookie(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	}
	http.SetCookie(w, cookie)
}

// 全局会话管理器
var (
	globalSessionManager *SessionManager
	sessionManagerOnce   sync.Once
)

// GetSessionManager 获取全局会话管理器
func GetSessionManager() *SessionManager {
	sessionManagerOnce.Do(func() {
		globalSessionManager = NewSessionManager()
	})
	return globalSessionManager
}
