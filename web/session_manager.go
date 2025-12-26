package web

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"sync"
	"time"
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
}

// NewSessionManager 创建会话管理器
func NewSessionManager() *SessionManager {
	sm := &SessionManager{
		sessions:       make(map[string]*Session),
		sessionTimeout: 24 * time.Hour,
	}

	// 启动清理过期会话的协程
	go sm.cleanupExpiredSessions()

	return sm
}

// cleanupExpiredSessions 清理过期会话
func (sm *SessionManager) cleanupExpiredSessions() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		sm.mu.Lock()
		now := time.Now()
		for sessionID, session := range sm.sessions {
			if now.After(session.ExpiresAt) {
				delete(sm.sessions, sessionID)
			}
		}
		sm.mu.Unlock()
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

	return session, nil
}

// GetSession 获取会话
func (sm *SessionManager) GetSession(sessionID string) (*Session, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return nil, false
	}

	// 检查是否过期
	if time.Now().After(session.ExpiresAt) {
		return nil, false
	}

	return session, true
}

// DeleteSession 删除会话
func (sm *SessionManager) DeleteSession(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.sessions, sessionID)
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
		Secure:   false, // 本地开发环境使用 HTTP，不需要 Secure 标志
		SameSite: http.SameSiteLaxMode, // 使用 Lax 模式，确保同站请求能正常携带 Cookie
		MaxAge:   int(sm.sessionTimeout.Seconds()),
	}
	http.SetCookie(w, cookie)
	
	// 调试日志
	println("✓ Cookie 已设置:")
	println("  Name:", cookie.Name)
	println("  Value:", sessionID[:20]+"...")
	println("  Path:", cookie.Path)
	println("  MaxAge:", cookie.MaxAge)
	println("  HttpOnly:", cookie.HttpOnly)
	println("  Secure:", cookie.Secure)
	println("  SameSite:", cookie.SameSite)
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

