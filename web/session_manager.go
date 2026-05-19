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

// Session 會话信息
type Session struct {
	SessionID string
	Username  string
	Role      string
	IP        string
	UserAgent string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// SessionManager 會话管理器
type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	// 會话過期時间（默认24小時）
	sessionTimeout time.Duration
	// 數據库连接（用於持久化會话）
	db *sql.DB
}

// NewSessionManager 創建會话管理器
func NewSessionManager() *SessionManager {
	dataDir := "./data"
	// 確保資料目錄存在
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		logger.Warn("⚠️ 創建數據目錄失败: %v，會话將不會持久化", err)
		return &SessionManager{
			sessions:       make(map[string]*Session),
			sessionTimeout: 24 * time.Hour,
			db:             nil,
		}
	}

	// 使用與 PasswordManager 相同的數據库文件
	dbPath := filepath.Join(dataDir, "auth.db")
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_timeout=30000&_busy_timeout=30000", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		logger.Warn("⚠️ 打开會话數據库失败: %v，會话將不會持久化", err)
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

	// 初始化數據库表
	if err := sm.initDatabase(); err != nil {
		logger.Warn("⚠️ 初始化會话數據库表失败: %v，會话將不會持久化", err)
		db.Close()
		sm.db = nil
	} else {
		// 從數據库加載有效的會话
		if err := sm.loadSessionsFromDB(); err != nil {
			logger.Warn("⚠️ 從數據库加載會话失败: %v", err)
		} else {
			logger.Info("✅ 會话管理器已初始化，已從數據库加載有效會话")
		}
	}

	// 啟动清理過期會话的协程
	go sm.cleanupExpiredSessions()

	return sm
}

// initDatabase 初始化數據库表
func (sm *SessionManager) initDatabase() error {
	if sm.db == nil {
		return fmt.Errorf("數據库连接未初始化")
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
		return fmt.Errorf("創建會话表失败: %v", err)
	}

	// 創建索引
	indexSQL := `
	CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
	CREATE INDEX IF NOT EXISTS idx_sessions_username ON sessions(username);
	`
	if _, err := sm.db.Exec(indexSQL); err != nil {
		return fmt.Errorf("創建索引失败: %v", err)
	}

	return nil
}

// loadSessionsFromDB 從數據库加載有效的會话
func (sm *SessionManager) loadSessionsFromDB() error {
	if sm.db == nil {
		return nil // 數據库未初始化，跳過加載
	}

	now := time.Now()
	rows, err := sm.db.Query(`
		SELECT session_id, username, role, ip, user_agent, created_at, expires_at 
		FROM sessions 
		WHERE expires_at > ?
	`, now)
	if err != nil {
		return fmt.Errorf("查詢會话失败: %v", err)
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
			logger.Warn("⚠️ 加載會话失败: %v", err)
			continue
		}

		// 再次检查是否過期（防止時间差问题）
		if now.Before(session.ExpiresAt) {
			sm.sessions[session.SessionID] = &session
			loadedCount++
		}
	}

	if loadedCount > 0 {
		logger.Info("✅ 從數據库加載了 %d 個有效會话", loadedCount)
	}

	return rows.Err()
}

// cleanupExpiredSessions 清理過期會话
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

		// 從數據库刪除過期會话
		if sm.db != nil && len(expiredSessionIDs) > 0 {
			sm.deleteSessionsFromDB(expiredSessionIDs)
		}

		// 同時清理數據库中所有過期的會话（防止遗漏）
		if sm.db != nil {
			_, err := sm.db.Exec("DELETE FROM sessions WHERE expires_at <= ?", now)
			if err != nil {
				logger.Warn("⚠️ 清理數據库過期會话失败: %v", err)
			}
		}
	}
}

// generateSessionID 生成會话ID
func (sm *SessionManager) generateSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// 使用無填充的 URL 安全编碼，避免 Cookie 中的 '=' 被轉义導致會话查找失败
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CreateSession 創建會话
func (sm *SessionManager) CreateSession(username, role, ip, userAgent string) (*Session, error) {
	sessionID, err := sm.generateSessionID()
	if err != nil {
		return nil, fmt.Errorf("生成會话ID失败: %v", err)
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

	// 保存到數據库
	if sm.db != nil {
		if err := sm.saveSessionToDB(session); err != nil {
			logger.Warn("⚠️ 保存會话到數據库失败: %v", err)
			// 不返回錯误，因為記憶體中已經創建了會话
		}
	}

	return session, nil
}

// saveSessionToDB 保存會话到數據库
func (sm *SessionManager) saveSessionToDB(session *Session) error {
	if sm.db == nil {
		return nil // 數據库未初始化，跳過保存
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

// GetSession 獲取會话
func (sm *SessionManager) GetSession(sessionID string) (*Session, bool) {
	sm.mu.RLock()
	session, exists := sm.sessions[sessionID]
	sm.mu.RUnlock()

	// 如果記憶體中存在，检查是否過期
	if exists {
		if time.Now().After(session.ExpiresAt) {
			// 過期了，從記憶體和數據库刪除
			sm.DeleteSession(sessionID)
			return nil, false
		}
		return session, true
	}

	// 記憶體中不存在，尝試從數據库加載（防止啟动時遗漏）
	if sm.db != nil {
		session = sm.loadSessionFromDB(sessionID)
		if session != nil {
			// 检查是否過期
			if time.Now().After(session.ExpiresAt) {
				sm.DeleteSession(sessionID)
				return nil, false
			}
			// 加載到記憶體中
			sm.mu.Lock()
			sm.sessions[sessionID] = session
			sm.mu.Unlock()
			return session, true
		}
	}

	return nil, false
}

// loadSessionFromDB 從數據库加載單個會话
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

// DeleteSession 刪除會话
func (sm *SessionManager) DeleteSession(sessionID string) {
	sm.mu.Lock()
	delete(sm.sessions, sessionID)
	sm.mu.Unlock()

	// 從數據库刪除
	if sm.db != nil {
		sm.deleteSessionsFromDB([]string{sessionID})
	}
}

// DeleteSessionsForUser 刪除指定用戶的所有會话。
func (sm *SessionManager) DeleteSessionsForUser(username string) {
	sm.mu.Lock()
	var sessionIDs []string
	for sessionID, session := range sm.sessions {
		if session.Username == username {
			sessionIDs = append(sessionIDs, sessionID)
			delete(sm.sessions, sessionID)
		}
	}
	sm.mu.Unlock()

	if sm.db != nil {
		rows, err := sm.db.Query("SELECT session_id FROM sessions WHERE username = ?", username)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var sessionID string
				if rows.Scan(&sessionID) == nil {
					sessionIDs = append(sessionIDs, sessionID)
				}
			}
		}
		if len(sessionIDs) > 0 {
			sm.deleteSessionsFromDB(sessionIDs)
		}
	}
}

// deleteSessionsFromDB 從數據库刪除會话
func (sm *SessionManager) deleteSessionsFromDB(sessionIDs []string) error {
	if sm.db == nil || len(sessionIDs) == 0 {
		return nil
	}

	// 構建 IN 查詢
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

// GetSessionFromRequest 從请求中獲取會话
func (sm *SessionManager) GetSessionFromRequest(r *http.Request) (*Session, bool) {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		return nil, false
	}
	return sm.GetSession(cookie.Value)
}

// SetSessionCookie 設置會话Cookie
func (sm *SessionManager) SetSessionCookie(w http.ResponseWriter, sessionID string, secure bool) {
	// 在本地开发环境（localhost）中，强制 secure=false
	// 因為 localhost 通常使用 HTTP 而不是 HTTPS
	cookie := &http.Cookie{
		Name:     "session_id",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   false,                // 本地开发环境使用 HTTP，不需要 Secure 標志
		SameSite: http.SameSiteLaxMode, // 使用 Lax 模式，确保同站请求能正常携带 Cookie
		MaxAge:   int(sm.sessionTimeout.Seconds()),
	}
	http.SetCookie(w, cookie)

	// 調試日志：写入Web日志文件（而不是標准输出）
	logger.WriteWebLog(fmt.Sprintf("[SESSION] Cookie 已設置: Name=%s, Value=%s..., Path=%s, MaxAge=%d, HttpOnly=%v, Secure=%v, SameSite=%v",
		cookie.Name, sessionID[:20], cookie.Path, cookie.MaxAge, cookie.HttpOnly, cookie.Secure, cookie.SameSite))
}

// ClearSessionCookie 清除會话Cookie
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

// 全局會话管理器
var (
	globalSessionManager *SessionManager
	sessionManagerOnce   sync.Once
)

// GetSessionManager 獲取全局會话管理器
func GetSessionManager() *SessionManager {
	sessionManagerOnce.Do(func() {
		globalSessionManager = NewSessionManager()
	})
	return globalSessionManager
}
